#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <sys/fanotify.h>
#include <unistd.h>

#define BUF_SIZE 256

static struct event_map {
    char *name;
    unsigned int value;
} events[] = {
    {"FAN_CREATE", FAN_CREATE},
    {"FAN_MOVED_TO", FAN_MOVED_TO},
    {"FAN_OPEN", FAN_OPEN},
    {"FAN_ACCESS", FAN_ACCESS},
    {"FAN_MODIFY", FAN_MODIFY},
    {"FAN_CLOSE_WRITE", FAN_CLOSE_WRITE},
    {"FAN_CLOSE_NOWRITE", FAN_CLOSE_NOWRITE},
    {"FAN_MOVED_FROM", FAN_MOVED_FROM},
    {"FAN_DELETE", FAN_DELETE},
    {"FAN_DELETE_SELF", FAN_DELETE_SELF},
    {"FAN_MOVE_SELF", FAN_MOVE_SELF},
    {NULL, 0}
};

// Initialize with default events if no specific events are provided
unsigned int default_file_events_mask = FAN_CREATE | FAN_DELETE | FAN_MOVED_FROM | FAN_MOVED_TO | FAN_CLOSE_WRITE;
unsigned int default_dir_events_mask = FAN_CREATE | FAN_DELETE | FAN_MOVED_FROM | FAN_MOVED_TO | FAN_ONDIR;

// Parses the event names from the command line arguments and returns the corresponding fanotify mask.
unsigned int parse_events(char *events_str) {
    unsigned int mask = 0;
    char *event_name = strtok(events_str, ",");
    while (event_name != NULL) {
        for (int i = 0; events[i].name != NULL; i++) {
            if (strcmp(events[i].name, event_name) == 0) {
                mask |= events[i].value;
                break;
            }
        }
        event_name = strtok(NULL, ",");
    }

    return mask;
}


int access_is_ok(uid_t real_uid, uid_t effective_uid, const char *path) {
    struct stat statbuf;
    int result;

    // Drop privileges
    if (seteuid(real_uid) == -1) {
        perror("Failed to drop privileges");
        exit(EXIT_FAILURE);
    }
    
    if (stat(path, &statbuf) == -1) {
        if (errno == EACCES) {
            result = 0;
        } else if (errno == ENOENT) {
            result = 1;
        } else {
            perror("stat failed");
            exit(EXIT_FAILURE);
        }
    } else {
        result = 1;
    }

    // Restore privileges
    if (seteuid(effective_uid) == -1) {
        perror("Failed to restore privileges");
        exit(EXIT_FAILURE);
    }

    return result;
}

// Help message function
void print_help() {
    printf("Usage: fanotify_watch [options] <directory>\n");
    printf("\n");
    printf("Options:\n");
    printf("  -f <file_events>   Comma-separated list of events to monitor for files\n");
    printf("  -d <dir_events>    Comma-separated list of events to monitor for directories\n");
    printf("  -0                 Use null character as the terminator instead of newline\n");
    printf("  -h                 Display this help and exit\n");
    printf("\n");
    printf("Events:\n");
    for (int i = 0; events[i].name != NULL; i++) {
        printf("  %s\n", events[i].name);
    }
    printf("\n");
    printf("Default events to monitor / typical use: fanotify_watch \\\n");
    printf("  -d FAN_CREATE,FAN_DELETE,FAN_MOVED_FROM,FAN_MOVED_TO \\\n");
    printf("  -f FAN_CREATE,FAN_DELETE,FAN_MOVED_FROM,FAN_MOVED_TO,FAN_CLOSE_WRITE\\\n");
    printf("  /path/to/watch\n");
    printf("\n");
    printf("Use FAN_CLOSE_WRITE to debounce multiple writes, and just get a single\n");
    printf("  notification when a modified file is being closed.\n");
    printf("\n");
    printf("Output will come with one event per line, with |FAN_ONDIR for directory\n");
    printf("  events, or without for file events.\n");
}

int main(int argc, char *argv[]) {
    int fd, ret, event_fd, mount_fd;
    ssize_t len, path_len;
    char path[PATH_MAX];
    char procfd_path[PATH_MAX];
    char events_buf[BUF_SIZE];
    struct file_handle *file_handle;
    struct fanotify_event_metadata *metadata;
    struct fanotify_event_info_fid *fid;
    const char *file_name;
    struct stat sb;
    const char *watch_path;
    unsigned int file_events_mask = 0, dir_events_mask = 0;
    int opt;
    int null_terminate = 0; // Flag for null-terminated output

    while ((opt = getopt(argc, argv, "f:d:0h")) != -1) {
        switch (opt) {
            case 'f':
                file_events_mask = parse_events(optarg);
                break;
            case 'd':
                dir_events_mask = (parse_events(optarg) | FAN_ONDIR);
                break;
            case '0':
                null_terminate = 1; // Enable null-terminated output
                break;
            case 'h':
                print_help();
                exit(EXIT_SUCCESS);
            default:
                print_help();
                exit(EXIT_FAILURE);
        }
    }

    if (optind >= argc) {
        fprintf(stderr, "Missing path argument. Use -h for help.\n");
        exit(EXIT_FAILURE);
    }

    if (file_events_mask == 0 && dir_events_mask == 0) {
        file_events_mask = default_file_events_mask;
        dir_events_mask = default_dir_events_mask;
    }

    watch_path = argv[optind];

    mount_fd = open(watch_path, O_DIRECTORY | O_RDONLY);
    if (mount_fd == -1) {
        perror(watch_path);
        exit(EXIT_FAILURE);
    }

    /* Create an fanotify file descriptor with FAN_REPORT_DFID_NAME as
       a flag so that program can receive fid events with directory
       entry name. */

    fd = fanotify_init(FAN_CLASS_NOTIF | FAN_REPORT_DFID_NAME | FAN_UNLIMITED_QUEUE, 0);
    if (fd == -1) {
        perror("fanotify_init");
        exit(EXIT_FAILURE);
    }

    if (file_events_mask != 0) {
        ret = fanotify_mark(fd, FAN_MARK_ADD | FAN_MARK_FILESYSTEM, file_events_mask | FAN_EVENT_ON_CHILD,
                            AT_FDCWD, argv[1]);
        if (ret == -1) {
            perror("fanotify_mark");
            exit(EXIT_FAILURE);
        }
    }

    if (dir_events_mask != 0) {
        ret = fanotify_mark(fd, FAN_MARK_ADD | FAN_MARK_FILESYSTEM, dir_events_mask | FAN_ONDIR,
                            AT_FDCWD, argv[1]);
        if (ret == -1) {
            perror("fanotify_mark");
            exit(EXIT_FAILURE);
        }
    }

    printf("Listening for events.\n");

    uid_t real_uid = getuid();
    uid_t effective_uid = geteuid();

    while(1) {
        /* Read events from the event queue into a buffer. */

        len = read(fd, events_buf, sizeof(events_buf));
        if (len == -1 && errno != EAGAIN) {
            perror("read");
            exit(EXIT_FAILURE);
        }

        /* Process all events within the buffer. */

        for (metadata = (struct fanotify_event_metadata *) events_buf;
                FAN_EVENT_OK(metadata, len);
                metadata = FAN_EVENT_NEXT(metadata, len)) {
            fid = (struct fanotify_event_info_fid *) (metadata + 1);
            file_handle = (struct file_handle *) fid->handle;

            /* Ensure that the event info is of the correct type. */

            if (fid->hdr.info_type == FAN_EVENT_INFO_TYPE_FID ||
                fid->hdr.info_type == FAN_EVENT_INFO_TYPE_DFID) {
                file_name = NULL;
            } else if (fid->hdr.info_type == FAN_EVENT_INFO_TYPE_DFID_NAME
                || fid->hdr.info_type == FAN_EVENT_INFO_TYPE_OLD_DFID_NAME
                || fid->hdr.info_type == FAN_EVENT_INFO_TYPE_NEW_DFID_NAME)
            {
                file_name = file_handle->f_handle +
                            file_handle->handle_bytes;
            } else {
                printf("Skipping info type %d\n", fid->hdr.info_type);
                continue;
            }

            /* metadata->fd is set to FAN_NOFD when the group identifies
            objects by file handles.  To obtain a file descriptor for
            the file object corresponding to an event you can use the
            struct file_handle that's provided within the
            fanotify_event_info_fid in conjunction with the
            open_by_handle_at(2) system call.  A check for ESTALE is
            done to accommodate for the situation where the file handle
            for the object was deleted prior to this system call. */

            event_fd = open_by_handle_at(mount_fd, file_handle, O_RDONLY);
            if (event_fd == -1) {
                if (errno == ESTALE) {
                    printf("File handle is no longer valid. "
                            "File has been deleted\n");
                    continue;
                } else {
                    perror("open_by_handle_at");
                    exit(EXIT_FAILURE);
                }
            }

            snprintf(procfd_path, sizeof(procfd_path), "/proc/self/fd/%d",
                    event_fd);

            /* Retrieve and print the path of the modified dentry. */

            path_len = readlink(procfd_path, path, sizeof(path) - 1);
            if (path_len == -1) {
                perror("readlink");
                exit(EXIT_FAILURE);
            }
            path[path_len] = 0;

            int full_path_len = strlen(path) + 1;
            if (file_name != NULL)
                full_path_len += strlen(file_name) + 1;
            
            char full_path[full_path_len];
            if (file_name != NULL)
                snprintf(full_path, full_path_len, "%s/%s", path, file_name);
            else
                snprintf(full_path, full_path_len, "%s", path);

            if (strncmp(full_path, watch_path, strlen(watch_path)) == 0
                && access_is_ok(real_uid, effective_uid, full_path))
            {
                const char *dir_or_file = (metadata->mask & FAN_ONDIR) ? "|FAN_ONDIR" : "";
                char terminator = (null_terminate ? '\0' : '\n');

                for(int i=0; events[i].name != NULL; i++) {
                    if (metadata->mask & events[i].value) {
                        printf("%s%s %s/%s%c", events[i].name, dir_or_file, path, file_name, terminator);
                    }
                }
            }

            /* Close associated file descriptor for this event. */
            if (close(event_fd) == -1) {
                perror("close");
                exit(EXIT_FAILURE);
            }
        }
    }

    printf("All events processed successfully. Program exiting.\n");
    exit(EXIT_SUCCESS);
}

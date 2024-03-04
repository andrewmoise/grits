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

int
main(int argc, char *argv[])
{
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
    const char *watch_dir;

    if (argc != 2) {
        fprintf(stderr, "Invalid number of command line arguments.\n");
        exit(EXIT_FAILURE);
    }

    watch_dir = argv[1];

    mount_fd = open(watch_dir, O_DIRECTORY | O_RDONLY);
    if (mount_fd == -1) {
        perror(watch_dir);
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

    /* Place a mark on the filesystem object supplied in argv[1]. */

    ret = fanotify_mark(fd, FAN_MARK_ADD | FAN_MARK_FILESYSTEM,
                        FAN_CREATE | FAN_DELETE | FAN_CLOSE_WRITE | FAN_MOVED_FROM | FAN_MOVED_TO | FAN_EVENT_ON_CHILD,
                        AT_FDCWD, argv[1]);
    if (ret == -1) {
        perror("fanotify_mark");
        exit(EXIT_FAILURE);
    }

    ret = fanotify_mark(fd, FAN_MARK_ADD | FAN_MARK_FILESYSTEM,
                        FAN_CREATE | FAN_DELETE | FAN_MOVED_FROM | FAN_MOVED_TO | FAN_ONDIR,
                        AT_FDCWD, argv[1]);
    if (ret == -1) {
        perror("fanotify_mark");
        exit(EXIT_FAILURE);
    }

    printf("Listening for events.\n");

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

            if (strncmp(path, watch_dir, strlen(watch_dir)) == 0) {
                int only_mask;
                const char *dir_or_file;

                dir_or_file = (metadata->mask & FAN_ONDIR) ? "|FAN_ONDIR" : "";

                if (metadata->mask & FAN_CREATE)
                    printf("FAN_CREATE%s %s/%s\n", dir_or_file, path, file_name);
                if (metadata->mask & FAN_MOVED_TO)
                    printf("FAN_MOVED_TO%s %s/%s\n", dir_or_file, path, file_name);
                if (metadata->mask & FAN_CLOSE_WRITE)
                    printf("FAN_CLOSE_WRITE%s %s/%s\n", dir_or_file, path, file_name);
                if (metadata->mask & FAN_DELETE)
                    printf("FAN_DELETE%s %s/%s\n", dir_or_file, path, file_name);
                if (metadata->mask & FAN_MOVED_FROM)
                    printf("FAN_MOVED_FROM%s %s/%s\n", dir_or_file, path, file_name);
            }

            /* Close associated file descriptor for this event. */
            close(event_fd);
        }
    }

    printf("All events processed successfully. Program exiting.\n");
    exit(EXIT_SUCCESS);
}
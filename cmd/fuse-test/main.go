// Copyright 2016 the Go-FUSE Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
	"github.com/hanwen/go-fuse/v2/fuse/pathfs"
)

/////
// File stuff

// LoopbackFile delegates all operations back to an underlying os.File.
func NewLoopbackFile(f *os.File) nodefs.File {
	return &loopbackFile{File: f}
}

type loopbackFile struct {
	File *os.File

	// os.File is not threadsafe. Although fd themselves are
	// constant during the lifetime of an open file, the OS may
	// reuse the fd number after it is closed. When open races
	// with another close, they may lead to confusion as which
	// file gets written in the end.
	lock sync.Mutex
}

func (f *loopbackFile) InnerFile() nodefs.File {
	return nil
}

func (f *loopbackFile) SetInode(n *nodefs.Inode) {
}

func (f *loopbackFile) String() string {
	return fmt.Sprintf("loopbackFile(%s)", f.File.Name())
}

func (f *loopbackFile) Read(buf []byte, off int64) (res fuse.ReadResult, code fuse.Status) {
	f.lock.Lock()
	// This is not racy by virtue of the kernel properly
	// synchronizing the open/write/close.
	r := fuse.ReadResultFd(f.File.Fd(), off, len(buf))
	f.lock.Unlock()
	return r, fuse.OK
}

func (f *loopbackFile) Write(data []byte, off int64) (uint32, fuse.Status) {
	f.lock.Lock()
	n, err := f.File.WriteAt(data, off)
	f.lock.Unlock()
	return uint32(n), fuse.ToStatus(err)
}

func (f *loopbackFile) Release() {
	f.lock.Lock()
	f.File.Close()
	f.lock.Unlock()
}

func (f *loopbackFile) Flush() fuse.Status {
	f.lock.Lock()

	// Since Flush() may be called for each dup'd fd, we don't
	// want to really close the file, we just want to flush. This
	// is achieved by closing a dup'd fd.
	newFd, err := syscall.Dup(int(f.File.Fd()))
	f.lock.Unlock()

	if err != nil {
		return fuse.ToStatus(err)
	}
	err = syscall.Close(newFd)
	return fuse.ToStatus(err)
}

func (f *loopbackFile) Fsync(flags int) (code fuse.Status) {
	f.lock.Lock()
	r := fuse.ToStatus(syscall.Fsync(int(f.File.Fd())))
	f.lock.Unlock()

	return r
}

const (
	F_OFD_GETLK  = 36
	F_OFD_SETLK  = 37
	F_OFD_SETLKW = 38
)

func (f *loopbackFile) GetLk(owner uint64, lk *fuse.FileLock, flags uint32, out *fuse.FileLock) (code fuse.Status) {
	flk := syscall.Flock_t{}
	lk.ToFlockT(&flk)
	code = fuse.ToStatus(syscall.FcntlFlock(f.File.Fd(), F_OFD_GETLK, &flk))
	out.FromFlockT(&flk)
	return
}

func (f *loopbackFile) SetLk(owner uint64, lk *fuse.FileLock, flags uint32) (code fuse.Status) {
	return f.setLock(owner, lk, flags, false)
}

func (f *loopbackFile) SetLkw(owner uint64, lk *fuse.FileLock, flags uint32) (code fuse.Status) {
	return f.setLock(owner, lk, flags, true)
}

func (f *loopbackFile) setLock(owner uint64, lk *fuse.FileLock, flags uint32, blocking bool) (code fuse.Status) {
	if (flags & fuse.FUSE_LK_FLOCK) != 0 {
		var op int
		switch lk.Typ {
		case syscall.F_RDLCK:
			op = syscall.LOCK_SH
		case syscall.F_WRLCK:
			op = syscall.LOCK_EX
		case syscall.F_UNLCK:
			op = syscall.LOCK_UN
		default:
			return fuse.EINVAL
		}
		if !blocking {
			op |= syscall.LOCK_NB
		}
		return fuse.ToStatus(syscall.Flock(int(f.File.Fd()), op))
	} else {
		flk := syscall.Flock_t{}
		lk.ToFlockT(&flk)
		var op int
		if blocking {
			op = F_OFD_SETLKW
		} else {
			op = F_OFD_SETLK
		}
		return fuse.ToStatus(syscall.FcntlFlock(f.File.Fd(), op, &flk))
	}
}

func (f *loopbackFile) Truncate(size uint64) fuse.Status {
	f.lock.Lock()
	r := fuse.ToStatus(syscall.Ftruncate(int(f.File.Fd()), int64(size)))
	f.lock.Unlock()

	return r
}

func (f *loopbackFile) Chmod(mode uint32) fuse.Status {
	f.lock.Lock()
	r := fuse.ToStatus(f.File.Chmod(os.FileMode(mode)))
	f.lock.Unlock()

	return r
}

func (f *loopbackFile) Chown(uid uint32, gid uint32) fuse.Status {
	f.lock.Lock()
	r := fuse.ToStatus(f.File.Chown(int(uid), int(gid)))
	f.lock.Unlock()

	return r
}

func (f *loopbackFile) GetAttr(a *fuse.Attr) fuse.Status {
	st := syscall.Stat_t{}
	f.lock.Lock()
	err := syscall.Fstat(int(f.File.Fd()), &st)
	f.lock.Unlock()
	if err != nil {
		return fuse.ToStatus(err)
	}
	a.FromStat(&st)

	return fuse.OK
}

// Utimens implemented in files_linux.go

// Allocate implemented in files_linux.go

////////////////////////////////////////////////////////////////

// NewReadOnlyFile wraps a File so all write operations are denied.
func NewReadOnlyFile(f nodefs.File) nodefs.File {
	return &readOnlyFile{File: f}
}

type readOnlyFile struct {
	nodefs.File
}

func (f *readOnlyFile) InnerFile() nodefs.File {
	return f.File
}

func (f *readOnlyFile) String() string {
	return fmt.Sprintf("readOnlyFile(%s)", f.File.String())
}

func (f *readOnlyFile) Write(data []byte, off int64) (uint32, fuse.Status) {
	return 0, fuse.EPERM
}

func (f *readOnlyFile) Fsync(flag int) (code fuse.Status) {
	return fuse.OK
}

func (f *readOnlyFile) Truncate(size uint64) fuse.Status {
	return fuse.EPERM
}

func (f *readOnlyFile) Chmod(mode uint32) fuse.Status {
	return fuse.EPERM
}

func (f *readOnlyFile) Chown(uid uint32, gid uint32) fuse.Status {
	return fuse.EPERM
}

func (f *readOnlyFile) Allocate(off uint64, sz uint64, mode uint32) fuse.Status {
	return fuse.EPERM
}

// Note: This stuff is Linux only:

func (f *loopbackFile) Allocate(off uint64, sz uint64, mode uint32) fuse.Status {
	f.lock.Lock()
	err := syscall.Fallocate(int(f.File.Fd()), mode, int64(off), int64(sz))
	f.lock.Unlock()
	if err != nil {
		return fuse.ToStatus(err)
	}
	return fuse.OK
}

// Utimens - file handle based version of loopbackFileSystem.Utimens()
func (f *loopbackFile) Utimens(a *time.Time, m *time.Time) fuse.Status {
	var ts [2]syscall.Timespec
	ts[0] = fuse.UtimeToTimespec(a)
	ts[1] = fuse.UtimeToTimespec(m)
	f.lock.Lock()
	err := futimens(int(f.File.Fd()), &ts)
	f.lock.Unlock()
	return fuse.ToStatus(err)
}

// futimens - futimens(3) calls utimensat(2) with "pathname" set to null and
// "flags" set to zero
func futimens(fd int, times *[2]syscall.Timespec) (err error) {
	_, _, e1 := syscall.Syscall6(syscall.SYS_UTIMENSAT, uintptr(fd), 0, uintptr(unsafe.Pointer(times)), uintptr(0), 0, 0)
	if e1 != 0 {
		err = syscall.Errno(e1)
	}
	return
}

/////
// Filesystem stuff

type loopbackFileSystem struct {
	// TODO - this should need default fill in.
	pathfs.FileSystem
	Root string
}

// A FUSE filesystem that shunts all request to an underlying file
// system.  Its main purpose is to provide test coverage without
// having to build a synthetic filesystem.
func NewLoopbackFileSystem(root string) pathfs.FileSystem {
	// Make sure the Root path is absolute to avoid problems when the
	// application changes working directory.
	root, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}
	return &loopbackFileSystem{
		FileSystem: pathfs.NewDefaultFileSystem(),
		Root:       root,
	}
}

func (fs *loopbackFileSystem) StatFs(name string) *fuse.StatfsOut {
	s := syscall.Statfs_t{}
	err := syscall.Statfs(fs.GetPath(name), &s)
	if err == nil {
		out := &fuse.StatfsOut{}
		out.FromStatfsT(&s)
		return out
	}
	return nil
}

func (fs *loopbackFileSystem) OnMount(nodeFs *pathfs.PathNodeFs) {
}

func (fs *loopbackFileSystem) OnUnmount() {}

func (fs *loopbackFileSystem) GetPath(relPath string) string {
	return filepath.Join(fs.Root, relPath)
}

func (fs *loopbackFileSystem) GetAttr(name string, context *fuse.Context) (a *fuse.Attr, code fuse.Status) {
	fullPath := fs.GetPath(name)
	var err error = nil
	st := syscall.Stat_t{}
	if name == "" {
		// When GetAttr is called for the toplevel directory, we always want
		// to look through symlinks.
		err = syscall.Stat(fullPath, &st)
	} else {
		err = syscall.Lstat(fullPath, &st)
	}
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	a = &fuse.Attr{}
	a.FromStat(&st)
	return a, fuse.OK
}

func (fs *loopbackFileSystem) OpenDir(name string, context *fuse.Context) (stream []fuse.DirEntry, status fuse.Status) {
	// What other ways beyond O_RDONLY are there to open
	// directories?
	f, err := os.Open(fs.GetPath(name))
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	want := 500
	output := make([]fuse.DirEntry, 0, want)
	for {
		infos, err := f.Readdir(want)
		for i := range infos {
			// workaround for https://code.google.com/p/go/issues/detail?id=5960
			if infos[i] == nil {
				continue
			}
			n := infos[i].Name()
			d := fuse.DirEntry{
				Name: n,
			}
			if s := fuse.ToStatT(infos[i]); s != nil {
				d.Mode = uint32(s.Mode)
				d.Ino = s.Ino
			} else {
				log.Printf("ReadDir entry %q for %q has no stat info", n, name)
			}
			output = append(output, d)
		}
		if len(infos) < want || err == io.EOF {
			break
		}
		if err != nil {
			log.Println("Readdir() returned err:", err)
			break
		}
	}
	f.Close()

	return output, fuse.OK
}

func (fs *loopbackFileSystem) Open(name string, flags uint32, context *fuse.Context) (fuseFile nodefs.File, status fuse.Status) {
	// filter out append. The kernel layer will translate the
	// offsets for us appropriately.
	flags = flags &^ syscall.O_APPEND
	f, err := os.OpenFile(fs.GetPath(name), int(flags), 0)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	return nodefs.NewLoopbackFile(f), fuse.OK
}

func (fs *loopbackFileSystem) Chmod(path string, mode uint32, context *fuse.Context) (code fuse.Status) {
	err := os.Chmod(fs.GetPath(path), os.FileMode(mode))
	return fuse.ToStatus(err)
}

func (fs *loopbackFileSystem) Chown(path string, uid uint32, gid uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Chown(fs.GetPath(path), int(uid), int(gid)))
}

func (fs *loopbackFileSystem) Truncate(path string, offset uint64, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Truncate(fs.GetPath(path), int64(offset)))
}

func (fs *loopbackFileSystem) Readlink(name string, context *fuse.Context) (out string, code fuse.Status) {
	f, err := os.Readlink(fs.GetPath(name))
	return f, fuse.ToStatus(err)
}

func (fs *loopbackFileSystem) Mknod(name string, mode uint32, dev uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(syscall.Mknod(fs.GetPath(name), mode, int(dev)))
}

func (fs *loopbackFileSystem) Mkdir(path string, mode uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Mkdir(fs.GetPath(path), os.FileMode(mode)))
}

// Don't use os.Remove, it removes twice (unlink followed by rmdir).
func (fs *loopbackFileSystem) Unlink(name string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(syscall.Unlink(fs.GetPath(name)))
}

func (fs *loopbackFileSystem) Rmdir(name string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(syscall.Rmdir(fs.GetPath(name)))
}

func (fs *loopbackFileSystem) Symlink(pointedTo string, linkName string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Symlink(pointedTo, fs.GetPath(linkName)))
}

func (fs *loopbackFileSystem) Rename(oldPath string, newPath string, context *fuse.Context) (codee fuse.Status) {
	oldPath = fs.GetPath(oldPath)
	newPath = fs.GetPath(newPath)
	err := os.Rename(oldPath, newPath)
	return fuse.ToStatus(err)
}

func (fs *loopbackFileSystem) Link(orig string, newName string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Link(fs.GetPath(orig), fs.GetPath(newName)))
}

func (fs *loopbackFileSystem) Access(name string, mode uint32, context *fuse.Context) (code fuse.Status) {
	_, status := fs.GetAttr(name, context)
	if !status.Ok() {
		return status

	}
	//if !internal.HasAccess(context.Uid, context.Gid, attr.Uid, attr.Gid, attr.Mode, mode) {
	//	return fuse.EACCES
	//}

	return fuse.OK
}

func (fs *loopbackFileSystem) Create(path string, flags uint32, mode uint32, context *fuse.Context) (fuseFile nodefs.File, code fuse.Status) {
	flags = flags &^ syscall.O_APPEND
	f, err := os.OpenFile(fs.GetPath(path), int(flags)|os.O_CREATE, os.FileMode(mode))
	return nodefs.NewLoopbackFile(f), fuse.ToStatus(err)
}

// Linux only stuff

func (fs *loopbackFileSystem) ListXAttr(name string, context *fuse.Context) ([]string, fuse.Status) {
	//attrs, err := listXAttr(fs.GetPath(name))
	//return attrs, fuse.ToStatus(err)

	return nil, fuse.EACCES
}

func (fs *loopbackFileSystem) RemoveXAttr(name string, attr string, context *fuse.Context) fuse.Status {
	err := syscall.Removexattr(fs.GetPath(name), attr)
	return fuse.ToStatus(err)
}

func (fs *loopbackFileSystem) String() string {
	return fmt.Sprintf("LoopbackFs(%s)", fs.Root)
}

func (fs *loopbackFileSystem) GetXAttr(name string, attr string, context *fuse.Context) ([]byte, fuse.Status) {

	bufsz := 1024
	for {
		data := make([]byte, bufsz)
		sz, err := syscall.Getxattr(fs.GetPath(name), attr, data)
		if err == nil {
			return data[:sz], fuse.OK
		}
		if err == syscall.ERANGE {
			bufsz = sz
			continue
		}

		return nil, fuse.ToStatus(err)
	}

}

func (fs *loopbackFileSystem) SetXAttr(name string, attr string, data []byte, flags int, context *fuse.Context) fuse.Status {
	err := syscall.Setxattr(fs.GetPath(name), attr, data, flags)
	return fuse.ToStatus(err)
}

// Utimens - path based version of loopbackFile.Utimens()
func (fs *loopbackFileSystem) Utimens(path string, a *time.Time, m *time.Time, context *fuse.Context) (code fuse.Status) {
	//var ts [2]syscall.Timespec
	//ts[0] = fuse.UtimeToTimespec(a)
	//ts[1] = fuse.UtimeToTimespec(m)
	//err := sysUtimensat(0, fs.GetPath(path), &ts, _AT_SYMLINK_NOFOLLOW)
	//return fuse.ToStatus(err)

	return fuse.EACCES
}

/////
// All main stuff

func writeMemProfile(fn string, sigs <-chan os.Signal) {
	i := 0
	for range sigs {
		fn := fmt.Sprintf("%s-%d.memprof", fn, i)
		i++

		log.Printf("Writing mem profile to %s\n", fn)
		f, err := os.Create(fn)
		if err != nil {
			log.Printf("Create: %v", err)
			continue
		}
		pprof.WriteHeapProfile(f)
		if err := f.Close(); err != nil {
			log.Printf("close %v", err)
		}
	}
}

func main() {
	log.SetFlags(log.Lmicroseconds)
	// Scans the arg list and sets up flags
	debug := flag.Bool("debug", false, "print debugging messages.")
	other := flag.Bool("allow-other", false, "mount with -o allowother.")
	quiet := flag.Bool("q", false, "quiet")
	ro := flag.Bool("ro", false, "mount read-only")
	directmount := flag.Bool("directmount", false, "try to call the mount syscall instead of executing fusermount")
	directmountstrict := flag.Bool("directmountstrict", false, "like directmount, but don't fall back to fusermount")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to this file")
	memprofile := flag.String("memprofile", "", "write memory profile to this file")
	flag.Parse()
	if flag.NArg() < 2 {
		fmt.Printf("usage: %s MOUNTPOINT ORIGINAL\n", path.Base(os.Args[0]))
		fmt.Printf("\noptions:\n")
		flag.PrintDefaults()
		os.Exit(2)
	}
	if *cpuprofile != "" {
		if !*quiet {
			fmt.Printf("Writing cpu profile to %s\n", *cpuprofile)
		}
		f, err := os.Create(*cpuprofile)
		if err != nil {
			fmt.Println(err)
			os.Exit(3)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}
	if *memprofile != "" {
		if !*quiet {
			log.Printf("send SIGUSR1 to %d to dump memory profile", os.Getpid())
		}
		profSig := make(chan os.Signal, 1)
		signal.Notify(profSig, syscall.SIGUSR1)
		go writeMemProfile(*memprofile, profSig)
	}
	if *cpuprofile != "" || *memprofile != "" {
		if !*quiet {
			fmt.Printf("Note: You must unmount gracefully, otherwise the profile file(s) will stay empty!\n")
		}
	}

	orig := flag.Arg(1)
	loopbackRoot, err := fs.NewLoopbackRoot(orig)
	if err != nil {
		log.Fatalf("NewLoopbackRoot(%s): %v\n", orig, err)
	}

	sec := time.Second
	opts := &fs.Options{
		// The timeout options are to be compatible with libfuse defaults,
		// making benchmarking easier.
		AttrTimeout:  &sec,
		EntryTimeout: &sec,

		NullPermissions: true, // Leave file permissions on "000" files as-is

		MountOptions: fuse.MountOptions{
			AllowOther:        *other,
			Debug:             *debug,
			DirectMount:       *directmount,
			DirectMountStrict: *directmountstrict,
			FsName:            orig,       // First column in "df -T": original dir
			Name:              "loopback", // Second column in "df -T" will be shown as "fuse." + Name
		},
	}
	if opts.AllowOther {
		// Make the kernel check file permissions for us
		opts.MountOptions.Options = append(opts.MountOptions.Options, "default_permissions")
	}
	if *ro {
		opts.MountOptions.Options = append(opts.MountOptions.Options, "ro")
	}
	// Enable diagnostics logging
	if !*quiet {
		opts.Logger = log.New(os.Stderr, "", 0)
	}
	server, err := fs.Mount(flag.Arg(0), loopbackRoot, opts)
	if err != nil {
		log.Fatalf("Mount fail: %v\n", err)
	}
	if !*quiet {
		fmt.Println("Mounted!")
	}

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		server.Unmount()
	}()

	server.Wait()
}

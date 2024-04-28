// Copyright 2016 the Go-FUSE Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
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

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/hanwen/go-fuse/v2/fuse/nodefs"
	"github.com/hanwen/go-fuse/v2/fuse/pathfs"
	"golang.org/x/sys/unix"
)

/////
// Filesystem stuff

type gritsFileSystem struct {
	// TODO - this should need default fill in.
	pathfs.FileSystem
	Root string
}

// A FUSE filesystem that shunts all request to an underlying file
// system.  Its main purpose is to provide test coverage without
// having to build a synthetic filesystem.
func NewGritsFileSystem(root string) pathfs.FileSystem {
	// Make sure the Root path is absolute to avoid problems when the
	// application changes working directory.
	root, err := filepath.Abs(root)
	if err != nil {
		panic(err)
	}
	return &gritsFileSystem{
		FileSystem: pathfs.NewDefaultFileSystem(),
		Root:       root,
	}
}

func (fs *gritsFileSystem) StatFs(name string) *fuse.StatfsOut {
	s := syscall.Statfs_t{}
	err := syscall.Statfs(fs.GetPath(name), &s)
	if err == nil {
		out := &fuse.StatfsOut{}
		out.FromStatfsT(&s)
		return out
	}
	return nil
}

func (fs *gritsFileSystem) OnMount(nodeFs *pathfs.PathNodeFs) {
}

func (fs *gritsFileSystem) OnUnmount() {}

func (fs *gritsFileSystem) GetPath(relPath string) string {
	return filepath.Join(fs.Root, relPath)
}

func (fs *gritsFileSystem) GetAttr(name string, context *fuse.Context) (a *fuse.Attr, code fuse.Status) {
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

func (fs *gritsFileSystem) OpenDir(name string, context *fuse.Context) (stream []fuse.DirEntry, status fuse.Status) {
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

func (fs *gritsFileSystem) Open(name string, flags uint32, context *fuse.Context) (fuseFile nodefs.File, status fuse.Status) {
	// filter out append. The kernel layer will translate the
	// offsets for us appropriately.
	flags = flags &^ syscall.O_APPEND
	f, err := os.OpenFile(fs.GetPath(name), int(flags), 0)
	if err != nil {
		return nil, fuse.ToStatus(err)
	}
	return nodefs.NewLoopbackFile(f), fuse.OK
}

func (fs *gritsFileSystem) Chmod(path string, mode uint32, context *fuse.Context) (code fuse.Status) {
	err := os.Chmod(fs.GetPath(path), os.FileMode(mode))
	return fuse.ToStatus(err)
}

func (fs *gritsFileSystem) Chown(path string, uid uint32, gid uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Chown(fs.GetPath(path), int(uid), int(gid)))
}

func (fs *gritsFileSystem) Truncate(path string, offset uint64, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Truncate(fs.GetPath(path), int64(offset)))
}

func (fs *gritsFileSystem) Readlink(name string, context *fuse.Context) (out string, code fuse.Status) {
	f, err := os.Readlink(fs.GetPath(name))
	return f, fuse.ToStatus(err)
}

func (fs *gritsFileSystem) Mknod(name string, mode uint32, dev uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(syscall.Mknod(fs.GetPath(name), mode, int(dev)))
}

func (fs *gritsFileSystem) Mkdir(path string, mode uint32, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Mkdir(fs.GetPath(path), os.FileMode(mode)))
}

// Don't use os.Remove, it removes twice (unlink followed by rmdir).
func (fs *gritsFileSystem) Unlink(name string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(syscall.Unlink(fs.GetPath(name)))
}

func (fs *gritsFileSystem) Rmdir(name string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(syscall.Rmdir(fs.GetPath(name)))
}

func (fs *gritsFileSystem) Symlink(pointedTo string, linkName string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Symlink(pointedTo, fs.GetPath(linkName)))
}

func (fs *gritsFileSystem) Rename(oldPath string, newPath string, context *fuse.Context) (codee fuse.Status) {
	oldPath = fs.GetPath(oldPath)
	newPath = fs.GetPath(newPath)
	err := os.Rename(oldPath, newPath)
	return fuse.ToStatus(err)
}

func (fs *gritsFileSystem) Link(orig string, newName string, context *fuse.Context) (code fuse.Status) {
	return fuse.ToStatus(os.Link(fs.GetPath(orig), fs.GetPath(newName)))
}

func (fs *gritsFileSystem) Access(name string, mode uint32, context *fuse.Context) (code fuse.Status) {
	_, status := fs.GetAttr(name, context)
	if !status.Ok() {
		return status

	}
	//if !internal.HasAccess(context.Uid, context.Gid, attr.Uid, attr.Gid, attr.Mode, mode) {
	//	return fuse.EACCES
	//}

	return fuse.OK
}

func (fs *gritsFileSystem) Create(path string, flags uint32, mode uint32, context *fuse.Context) (fuseFile nodefs.File, code fuse.Status) {
	flags = flags &^ syscall.O_APPEND
	f, err := os.OpenFile(fs.GetPath(path), int(flags)|os.O_CREATE, os.FileMode(mode))
	return nodefs.NewLoopbackFile(f), fuse.ToStatus(err)
}

// Linux only stuff

func (fs *gritsFileSystem) ListXAttr(name string, context *fuse.Context) ([]string, fuse.Status) {
	//attrs, err := listXAttr(fs.GetPath(name))
	//return attrs, fuse.ToStatus(err)

	return nil, fuse.EACCES
}

func (fs *gritsFileSystem) RemoveXAttr(name string, attr string, context *fuse.Context) fuse.Status {
	err := syscall.Removexattr(fs.GetPath(name), attr)
	return fuse.ToStatus(err)
}

func (fs *gritsFileSystem) String() string {
	return fmt.Sprintf("GritsFs(%s)", fs.Root)
}

func (fs *gritsFileSystem) GetXAttr(name string, attr string, context *fuse.Context) ([]byte, fuse.Status) {

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

func (fs *gritsFileSystem) SetXAttr(name string, attr string, data []byte, flags int, context *fuse.Context) fuse.Status {
	err := syscall.Setxattr(fs.GetPath(name), attr, data, flags)
	return fuse.ToStatus(err)
}

// Utimens - path based version of gritsFile.Utimens()
func (fs *gritsFileSystem) Utimens(path string, a *time.Time, m *time.Time, context *fuse.Context) (code fuse.Status) {
	//var ts [2]syscall.Timespec
	//ts[0] = fuse.UtimeToTimespec(a)
	//ts[1] = fuse.UtimeToTimespec(m)
	//err := sysUtimensat(0, fs.GetPath(path), &ts, _AT_SYMLINK_NOFOLLOW)
	//return fuse.ToStatus(err)

	return fuse.EACCES
}

/////
// Loopback stuff

// GritsRoot holds the parameters for creating a new loopback
// filesystem. Loopback filesystem delegate their operations to an
// underlying POSIX file system.
type GritsRoot struct {
	// The path to the root of the underlying file system.
	Path string

	// The device on which the Path resides. This must be set if
	// the underlying filesystem crosses file systems.
	Dev uint64

	// NewNode returns a new InodeEmbedder to be used to respond
	// to a LOOKUP/CREATE/MKDIR/MKNOD opcode. If not set, use a
	// GritsNode.
	NewNode func(rootData *GritsRoot, parent *fs.Inode, name string, st *syscall.Stat_t) fs.InodeEmbedder
}

func (r *GritsRoot) newNode(parent *fs.Inode, name string, st *syscall.Stat_t) fs.InodeEmbedder {
	if r.NewNode != nil {
		return r.NewNode(r, parent, name, st)
	}
	return &GritsNode{
		RootData: r,
	}
}

func (r *GritsRoot) idFromStat(st *syscall.Stat_t) fs.StableAttr {
	// We compose an inode number by the underlying inode, and
	// mixing in the device number. In traditional filesystems,
	// the inode numbers are small. The device numbers are also
	// small (typically 16 bit). Finally, we mask out the root
	// device number of the root, so a loopback FS that does not
	// encompass multiple mounts will reflect the inode numbers of
	// the underlying filesystem
	swapped := (uint64(st.Dev) << 32) | (uint64(st.Dev) >> 32)
	swappedRootDev := (r.Dev << 32) | (r.Dev >> 32)
	return fs.StableAttr{
		Mode: uint32(st.Mode),
		Gen:  1,
		// This should work well for traditional backing FSes,
		// not so much for other go-fuse FS-es
		Ino: (swapped ^ swappedRootDev) ^ st.Ino,
	}
}

// NewGritsRoot returns a root node for a loopback file system whose
// root is at the given root. This node implements all NodeXxxxer
// operations available.
func NewGritsRoot(rootPath string) (fs.InodeEmbedder, error) {
	var st syscall.Stat_t
	err := syscall.Stat(rootPath, &st)
	if err != nil {
		return nil, err
	}

	root := &GritsRoot{
		Path: rootPath,
		Dev:  uint64(st.Dev),
	}

	return root.newNode(nil, "", &st), nil
}

// GritsNode is a filesystem node in a loopback file system. It is
// public so it can be used as a basis for other loopback based
// filesystems. See NewGritsFile or GritsRoot for more
// information.
type GritsNode struct {
	fs.Inode

	// RootData points back to the root of the loopback filesystem.
	RootData *GritsRoot
}

var _ = (fs.NodeStatfser)((*GritsNode)(nil))

func (n *GritsNode) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	s := syscall.Statfs_t{}
	err := syscall.Statfs(n.path(), &s)
	if err != nil {
		return fs.ToErrno(err)
	}
	out.FromStatfsT(&s)
	return fs.OK
}

// path returns the full path to the file in the underlying file
// system.
func (n *GritsNode) path() string {
	path := n.Path(n.Root())
	return filepath.Join(n.RootData.Path, path)
}

var _ = (fs.NodeLookuper)((*GritsNode)(nil))

func (n *GritsNode) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	p := filepath.Join(n.path(), name)

	st := syscall.Stat_t{}
	err := syscall.Lstat(p, &st)
	if err != nil {
		return nil, fs.ToErrno(err)
	}

	out.Attr.FromStat(&st)
	node := n.RootData.newNode(n.EmbeddedInode(), name, &st)
	ch := n.NewInode(ctx, node, n.RootData.idFromStat(&st))
	return ch, 0
}

// preserveOwner sets uid and gid of `path` according to the caller information
// in `ctx`.
func (n *GritsNode) preserveOwner(ctx context.Context, path string) error {
	if os.Getuid() != 0 {
		return nil
	}
	caller, ok := fuse.FromContext(ctx)
	if !ok {
		return nil
	}
	return syscall.Lchown(path, int(caller.Uid), int(caller.Gid))
}

var _ = (fs.NodeMknoder)((*GritsNode)(nil))

func intDev(dev uint32) int {
	return int(dev)
}

func (n *GritsNode) Mknod(ctx context.Context, name string, mode, rdev uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	p := filepath.Join(n.path(), name)
	err := syscall.Mknod(p, mode, intDev(rdev))
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	n.preserveOwner(ctx, p)
	st := syscall.Stat_t{}
	if err := syscall.Lstat(p, &st); err != nil {
		syscall.Rmdir(p)
		return nil, fs.ToErrno(err)
	}

	out.Attr.FromStat(&st)

	node := n.RootData.newNode(n.EmbeddedInode(), name, &st)
	ch := n.NewInode(ctx, node, n.RootData.idFromStat(&st))

	return ch, 0
}

var _ = (fs.NodeMkdirer)((*GritsNode)(nil))

func (n *GritsNode) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	p := filepath.Join(n.path(), name)
	err := os.Mkdir(p, os.FileMode(mode))
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	n.preserveOwner(ctx, p)
	st := syscall.Stat_t{}
	if err := syscall.Lstat(p, &st); err != nil {
		syscall.Rmdir(p)
		return nil, fs.ToErrno(err)
	}

	out.Attr.FromStat(&st)

	node := n.RootData.newNode(n.EmbeddedInode(), name, &st)
	ch := n.NewInode(ctx, node, n.RootData.idFromStat(&st))

	return ch, 0
}

var _ = (fs.NodeRmdirer)((*GritsNode)(nil))

func (n *GritsNode) Rmdir(ctx context.Context, name string) syscall.Errno {
	p := filepath.Join(n.path(), name)
	err := syscall.Rmdir(p)
	return fs.ToErrno(err)
}

var _ = (fs.NodeUnlinker)((*GritsNode)(nil))

func (n *GritsNode) Unlink(ctx context.Context, name string) syscall.Errno {
	p := filepath.Join(n.path(), name)
	err := syscall.Unlink(p)
	return fs.ToErrno(err)
}

var _ = (fs.NodeRenamer)((*GritsNode)(nil))

func (n *GritsNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	if flags&fs.RENAME_EXCHANGE != 0 {
		return n.renameExchange(name, newParent, newName)
	}

	p1 := filepath.Join(n.path(), name)
	p2 := filepath.Join(n.RootData.Path, newParent.EmbeddedInode().Path(nil), newName)

	err := syscall.Rename(p1, p2)
	return fs.ToErrno(err)
}

var _ = (fs.NodeCreater)((*GritsNode)(nil))

func (n *GritsNode) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (inode *fs.Inode, fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	p := filepath.Join(n.path(), name)
	flags = flags &^ syscall.O_APPEND
	fd, err := syscall.Open(p, int(flags)|os.O_CREATE, mode)
	if err != nil {
		return nil, nil, 0, fs.ToErrno(err)
	}
	n.preserveOwner(ctx, p)
	st := syscall.Stat_t{}
	if err := syscall.Fstat(fd, &st); err != nil {
		syscall.Close(fd)
		return nil, nil, 0, fs.ToErrno(err)
	}

	node := n.RootData.newNode(n.EmbeddedInode(), name, &st)
	ch := n.NewInode(ctx, node, n.RootData.idFromStat(&st))
	lf := fs.NewLoopbackFile(fd)

	out.FromStat(&st)
	return ch, lf, 0, 0
}

func (n *GritsNode) renameExchange(name string, newparent fs.InodeEmbedder, newName string) syscall.Errno {
	fd1, err := syscall.Open(n.path(), syscall.O_DIRECTORY, 0)
	if err != nil {
		return fs.ToErrno(err)
	}
	defer syscall.Close(fd1)
	p2 := filepath.Join(n.RootData.Path, newparent.EmbeddedInode().Path(nil))
	fd2, err := syscall.Open(p2, syscall.O_DIRECTORY, 0)
	defer syscall.Close(fd2)
	if err != nil {
		return fs.ToErrno(err)
	}

	var st syscall.Stat_t
	if err := syscall.Fstat(fd1, &st); err != nil {
		return fs.ToErrno(err)
	}

	// Double check that nodes didn't change from under us.
	inode := &n.Inode
	if inode.Root() != inode && inode.StableAttr().Ino != n.RootData.idFromStat(&st).Ino {
		return syscall.EBUSY
	}
	if err := syscall.Fstat(fd2, &st); err != nil {
		return fs.ToErrno(err)
	}

	newinode := newparent.EmbeddedInode()
	if newinode.Root() != newinode && newinode.StableAttr().Ino != n.RootData.idFromStat(&st).Ino {
		return syscall.EBUSY
	}

	return fs.ToErrno(unix.Renameat2(fd1, name, fd2, newName, unix.RENAME_EXCHANGE))
}

var _ = (fs.NodeSymlinker)((*GritsNode)(nil))

func (n *GritsNode) Symlink(ctx context.Context, target, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	p := filepath.Join(n.path(), name)
	err := syscall.Symlink(target, p)
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	n.preserveOwner(ctx, p)
	st := syscall.Stat_t{}
	if err := syscall.Lstat(p, &st); err != nil {
		syscall.Unlink(p)
		return nil, fs.ToErrno(err)
	}
	node := n.RootData.newNode(n.EmbeddedInode(), name, &st)
	ch := n.NewInode(ctx, node, n.RootData.idFromStat(&st))

	out.Attr.FromStat(&st)
	return ch, 0
}

var _ = (fs.NodeLinker)((*GritsNode)(nil))

func (n *GritsNode) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {

	p := filepath.Join(n.path(), name)
	err := syscall.Link(filepath.Join(n.RootData.Path, target.EmbeddedInode().Path(nil)), p)
	if err != nil {
		return nil, fs.ToErrno(err)
	}
	st := syscall.Stat_t{}
	if err := syscall.Lstat(p, &st); err != nil {
		syscall.Unlink(p)
		return nil, fs.ToErrno(err)
	}
	node := n.RootData.newNode(n.EmbeddedInode(), name, &st)
	ch := n.NewInode(ctx, node, n.RootData.idFromStat(&st))

	out.Attr.FromStat(&st)
	return ch, 0
}

var _ = (fs.NodeReadlinker)((*GritsNode)(nil))

func (n *GritsNode) Readlink(ctx context.Context) ([]byte, syscall.Errno) {
	p := n.path()

	for l := 256; ; l *= 2 {
		buf := make([]byte, l)
		sz, err := syscall.Readlink(p, buf)
		if err != nil {
			return nil, fs.ToErrno(err)
		}

		if sz < len(buf) {
			return buf[:sz], 0
		}
	}
}

var _ = (fs.NodeOpener)((*GritsNode)(nil))

type gritsFile struct {
	mu sync.Mutex
	fd int
}

func NewGritsFile(fd int) fs.FileHandle {
	return &gritsFile{fd: fd}
}

func (n *GritsNode) Open(ctx context.Context, flags uint32) (fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	flags = flags &^ syscall.O_APPEND
	p := n.path()
	f, err := syscall.Open(p, int(flags), 0)
	if err != nil {
		return nil, 0, fs.ToErrno(err)
	}
	lf := NewGritsFile(f)
	return lf, 0, 0
}

var _ = (fs.NodeOpendirer)((*GritsNode)(nil))

func (n *GritsNode) Opendir(ctx context.Context) syscall.Errno {
	fd, err := syscall.Open(n.path(), syscall.O_DIRECTORY, 0755)
	if err != nil {
		return fs.ToErrno(err)
	}
	syscall.Close(fd)
	return fs.OK
}

var _ = (fs.NodeReaddirer)((*GritsNode)(nil))

func (n *GritsNode) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	return fs.NewLoopbackDirStream(n.path())
}

var _ = (fs.NodeGetattrer)((*GritsNode)(nil))

func (n *GritsNode) Getattr(ctx context.Context, f fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	if f != nil {
		return f.(fs.FileGetattrer).Getattr(ctx, out)
	}

	p := n.path()

	var err error
	st := syscall.Stat_t{}
	if &n.Inode == n.Root() {
		err = syscall.Stat(p, &st)
	} else {
		err = syscall.Lstat(p, &st)
	}

	if err != nil {
		return fs.ToErrno(err)
	}
	out.FromStat(&st)
	return fs.OK
}

var _ = (fs.NodeSetattrer)((*GritsNode)(nil))

func (n *GritsNode) Setattr(ctx context.Context, f fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	p := n.path()
	fsa, ok := f.(fs.FileSetattrer)
	if ok && fsa != nil {
		fsa.Setattr(ctx, in, out)
	} else {
		if m, ok := in.GetMode(); ok {
			if err := syscall.Chmod(p, m); err != nil {
				return fs.ToErrno(err)
			}
		}

		uid, uok := in.GetUID()
		gid, gok := in.GetGID()
		if uok || gok {
			suid := -1
			sgid := -1
			if uok {
				suid = int(uid)
			}
			if gok {
				sgid = int(gid)
			}
			if err := syscall.Chown(p, suid, sgid); err != nil {
				return fs.ToErrno(err)
			}
		}

		mtime, mok := in.GetMTime()
		atime, aok := in.GetATime()

		if mok || aok {

			ap := &atime
			mp := &mtime
			if !aok {
				ap = nil
			}
			if !mok {
				mp = nil
			}
			var ts [2]syscall.Timespec
			ts[0] = fuse.UtimeToTimespec(ap)
			ts[1] = fuse.UtimeToTimespec(mp)

			if err := syscall.UtimesNano(p, ts[:]); err != nil {
				return fs.ToErrno(err)
			}
		}

		if sz, ok := in.GetSize(); ok {
			if err := syscall.Truncate(p, int64(sz)); err != nil {
				return fs.ToErrno(err)
			}
		}
	}

	fga, ok := f.(fs.FileGetattrer)
	if ok && fga != nil {
		fga.Getattr(ctx, out)
	} else {
		st := syscall.Stat_t{}
		err := syscall.Lstat(p, &st)
		if err != nil {
			return fs.ToErrno(err)
		}
		out.FromStat(&st)
	}
	return fs.OK
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
	gritsRoot, err := NewGritsRoot(orig)
	if err != nil {
		log.Fatalf("NewGritsRoot(%s): %v\n", orig, err)
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
			FsName:            orig,    // First column in "df -T": original dir
			Name:              "grits", // Second column in "df -T" will be shown as "fuse." + Name
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
	server, err := fs.Mount(flag.Arg(0), gritsRoot, opts)
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

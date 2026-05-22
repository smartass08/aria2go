//go:build linux

package nullfs

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
)

var (
	memMu    sync.Mutex
	memFiles = make(map[string][]byte)
	created  = make(map[string]bool)
)

func isMemFile(name string) bool {
	return strings.Contains(name, ".aria2") || strings.HasPrefix(name, "bench-file-")
}

type root struct{ fs.Inode }
type file struct {
	fs.Inode
	name string
}

func (r *root) Lookup(_ context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	memMu.Lock()
	exists := created[name]
	size := uint64(len(memFiles[name]))
	memMu.Unlock()

	if !exists {
		return nil, syscall.ENOENT
	}

	child := r.NewInode(context.Background(), &file{name: name}, fs.StableAttr{Mode: syscall.S_IFREG})
	out.Mode = 0o644 | syscall.S_IFREG
	out.Size = size
	return child, 0
}

func (r *root) Create(_ context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	child := r.NewInode(context.Background(), &file{name: name}, fs.StableAttr{Mode: syscall.S_IFREG})
	out.Mode = mode | syscall.S_IFREG
	isMem := isMemFile(name)

	memMu.Lock()
	created[name] = true
	if isMem {
		out.Size = uint64(len(memFiles[name]))
	} else {
		out.Size = 0
	}
	memMu.Unlock()

	return child, &fh{name: name, isMem: isMem}, 0, 0
}

func (r *root) Open(_ context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	return nil, 0, syscall.ENOTSUP
}

func (r *root) Rename(ctx context.Context, oldName string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	memMu.Lock()
	defer memMu.Unlock()

	// Move file data in memFiles
	if data, exists := memFiles[oldName]; exists {
		memFiles[newName] = data
		delete(memFiles, oldName)
	}
	// Move file created state
	if exists := created[oldName]; exists {
		created[newName] = true
		delete(created, oldName)
	}
	return 0
}

func (f *file) Getattr(_ context.Context, _ fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0o644 | syscall.S_IFREG
	if isMemFile(f.name) {
		memMu.Lock()
		out.Size = uint64(len(memFiles[f.name]))
		memMu.Unlock()
	} else {
		out.Size = 0
	}
	now := uint64(time.Now().Unix())
	out.Atime, out.Mtime, out.Ctime = now, now, now
	return 0
}

func (f *file) Setattr(_ context.Context, _ fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	out.Mode = 0o644 | syscall.S_IFREG
	if isMemFile(f.name) {
		memMu.Lock()
		if in.Valid&fuse.FATTR_SIZE != 0 {
			buf := memFiles[f.name]
			if uint64(len(buf)) != in.Size {
				newBuf := make([]byte, in.Size)
				copy(newBuf, buf)
				memFiles[f.name] = newBuf
			}
		}
		out.Size = uint64(len(memFiles[f.name]))
		memMu.Unlock()
	} else {
		out.Size = 0
	}
	now := uint64(time.Now().Unix())
	out.Atime, out.Mtime, out.Ctime = now, now, now
	return 0
}

func (f *file) Open(_ context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	isMem := isMemFile(f.name)
	return &fh{name: f.name, isMem: isMem}, 0, 0
}

type fh struct {
	name  string
	isMem bool
}

func (h *fh) Read(_ context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	if h.isMem {
		memMu.Lock()
		defer memMu.Unlock()
		buf := memFiles[h.name]
		if off >= int64(len(buf)) {
			return fuse.ReadResultData(nil), 0
		}
		n := int64(len(buf)) - off
		if n > int64(len(dest)) {
			n = int64(len(dest))
		}
		copy(dest[:n], buf[off:off+n])
		return fuse.ReadResultData(dest[:n]), 0
	}
	for i := range dest {
		dest[i] = 0
	}
	return fuse.ReadResultData(dest), 0
}

func (h *fh) Write(_ context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	TotalBytesWritten.Add(int64(len(data)))
	if h.isMem {
		memMu.Lock()
		defer memMu.Unlock()
		buf := memFiles[h.name]
		end := off + int64(len(data))
		if int64(cap(buf)) < end {
			newBuf := make([]byte, end, end*2)
			copy(newBuf, buf)
			buf = newBuf
		}
		if int64(len(buf)) < end {
			buf = buf[:end]
		}
		copy(buf[off:], data)
		memFiles[h.name] = buf
		return uint32(len(data)), 0
	}
	return uint32(len(data)), 0
}

func (h *fh) Flush(_ context.Context) syscall.Errno             { return 0 }
func (h *fh) Release(_ context.Context) syscall.Errno           { return 0 }
func (h *fh) Fsync(_ context.Context, flags uint32) syscall.Errno { return 0 }

func mountFUSE(mountPoint string) (*Mount, error) {
	srv, err := fs.Mount(mountPoint, &root{}, &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther: false,
			FsName:     "nullfs",
			Name:       "nullfs",
		},
	})
	if err != nil {
		return nil, fmt.Errorf("fuse mount: %w", err)
	}
	return &Mount{
		Path: mountPoint,
		cleanup: func() {
			srv.Unmount()
		},
	}, nil
}

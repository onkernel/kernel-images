// Package daemon provides the FUSE filesystem mounting functionality for fspipe.
// This package exposes the internal daemon functionality for use by external packages.
package daemon

import (
	"context"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/onkernel/kernel-images/server/lib/fspipe/logging"
	"github.com/onkernel/kernel-images/server/lib/fspipe/protocol"
	"github.com/onkernel/kernel-images/server/lib/fspipe/transport"
)

var oneSecond = time.Second

// Mount mounts the fspipe FUSE filesystem at the specified path
func Mount(mountpoint string, client transport.Transport) (*fuse.Server, error) {
	root := newPipeDir(client, nil, "")

	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther: true, // Allow Chrome (running as different user) to access the mount
			Debug:      false,
			FsName:     "fspipe",
			Name:       "fspipe",
		},
		AttrTimeout:  &oneSecond,
		EntryTimeout: &oneSecond,
	}

	server, err := fs.Mount(mountpoint, root, opts)
	if err != nil {
		return nil, err
	}

	logging.Info("Mounted fspipe at %s", mountpoint)
	return server, nil
}

// defaultAttr returns default attributes for nodes
func defaultAttr(mode uint32) fuse.Attr {
	now := time.Now()
	return fuse.Attr{
		Mode:  mode,
		Nlink: 1,
		Owner: fuse.Owner{
			Uid: uint32(syscall.Getuid()),
			Gid: uint32(syscall.Getgid()),
		},
		Atime: uint64(now.Unix()),
		Mtime: uint64(now.Unix()),
		Ctime: uint64(now.Unix()),
	}
}

// pipeDir represents a directory in the virtual filesystem
type pipeDir struct {
	fs.Inode

	client transport.Transport
	parent *pipeDir
	name   string

	mu       sync.RWMutex
	children map[string]fs.InodeEmbedder
}

var _ fs.InodeEmbedder = (*pipeDir)(nil)
var _ fs.NodeGetattrer = (*pipeDir)(nil)
var _ fs.NodeLookuper = (*pipeDir)(nil)
var _ fs.NodeCreater = (*pipeDir)(nil)
var _ fs.NodeMkdirer = (*pipeDir)(nil)
var _ fs.NodeUnlinker = (*pipeDir)(nil)
var _ fs.NodeRmdirer = (*pipeDir)(nil)
var _ fs.NodeRenamer = (*pipeDir)(nil)
var _ fs.NodeReaddirer = (*pipeDir)(nil)
var _ fs.NodeStatfser = (*pipeDir)(nil)

func newPipeDir(client transport.Transport, parent *pipeDir, name string) *pipeDir {
	return &pipeDir{
		client:   client,
		parent:   parent,
		name:     name,
		children: make(map[string]fs.InodeEmbedder),
	}
}

func (d *pipeDir) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	// Use 0777 to allow Chrome (running as different user) full access
	out.Attr = defaultAttr(fuse.S_IFDIR | 0777)
	return 0
}

// Statfs returns filesystem statistics. Chrome checks this before downloading.
func (d *pipeDir) Statfs(ctx context.Context, out *fuse.StatfsOut) syscall.Errno {
	// Return generous fake stats - this is a pipe filesystem, space is "unlimited"
	// These values are designed to make Chrome happy when checking disk space
	const blockSize = 4096
	const totalBlocks = 1024 * 1024 * 1024 // ~4TB worth of blocks
	const freeBlocks = 1024 * 1024 * 512   // ~2TB free

	out.Blocks = totalBlocks
	out.Bfree = freeBlocks
	out.Bavail = freeBlocks
	out.Files = 1000000
	out.Ffree = 999999
	out.Bsize = blockSize
	out.NameLen = 255
	out.Frsize = blockSize

	return 0
}

func (d *pipeDir) Lookup(ctx context.Context, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	// Hold lock for entire operation to prevent TOCTOU race
	d.mu.RLock()
	defer d.mu.RUnlock()

	child, ok := d.children[name]
	if !ok {
		return nil, syscall.ENOENT
	}

	inode := d.GetChild(name)
	if inode != nil {
		switch n := child.(type) {
		case *pipeFile:
			// Ensure world-writable permission for cross-user access
			n.mu.RLock()
			mode := n.mode
			size := n.size
			n.mu.RUnlock()
			out.Attr = defaultAttr(fuse.S_IFREG | mode | 0666)
			out.Attr.Size = uint64(size)
		case *pipeDir:
			out.Attr = defaultAttr(fuse.S_IFDIR | 0777)
		}
		return inode, 0
	}

	return nil, syscall.ENOENT
}

func (d *pipeDir) Create(ctx context.Context, name string, flags uint32, mode uint32, out *fuse.EntryOut) (node *fs.Inode, fh fs.FileHandle, fuseFlags uint32, errno syscall.Errno) {
	relPath := d.relPath(name)
	logging.Debug("Create: %s (mode=%o)", relPath, mode)

	file := newPipeFile(d.client, d, name, mode)

	msg := protocol.FileCreate{
		FileID:   file.id,
		Filename: relPath,
		Mode:     mode,
	}

	// Use SendAndReceive to get ACK from listener - ensures file was created
	respType, respData, err := d.client.SendAndReceive(protocol.MsgFileCreate, &msg)
	if err != nil {
		logging.Debug("Create: failed to send FileCreate: %v", err)
		return nil, nil, 0, syscall.EIO
	}

	if respType != protocol.MsgFileCreateAck {
		logging.Debug("Create: unexpected response type: 0x%02x", respType)
		return nil, nil, 0, syscall.EIO
	}

	var ack protocol.FileCreateAck
	if err := protocol.DecodePayload(respData, &ack); err != nil {
		logging.Debug("Create: failed to decode ack: %v", err)
		return nil, nil, 0, syscall.EIO
	}

	if !ack.Success {
		logging.Debug("Create: listener error: %s", ack.Error)
		return nil, nil, 0, syscall.EIO
	}

	d.mu.Lock()
	d.children[name] = file
	d.mu.Unlock()

	stable := fs.StableAttr{Mode: fuse.S_IFREG}
	inode := d.NewInode(ctx, file, stable)

	// Ensure world-writable permission for cross-user access
	out.Attr = defaultAttr(fuse.S_IFREG | mode | 0666)

	handle := newPipeHandle(file)
	return inode, handle, fuse.FOPEN_DIRECT_IO, 0
}

func (d *pipeDir) Mkdir(ctx context.Context, name string, mode uint32, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	logging.Debug("Mkdir: %s", d.relPath(name))

	newDir := newPipeDir(d.client, d, name)

	d.mu.Lock()
	d.children[name] = newDir
	d.mu.Unlock()

	stable := fs.StableAttr{Mode: fuse.S_IFDIR}
	inode := d.NewInode(ctx, newDir, stable)

	// Ensure world-writable permission for cross-user access
	out.Attr = defaultAttr(fuse.S_IFDIR | 0777)
	return inode, 0
}

func (d *pipeDir) Unlink(ctx context.Context, name string) syscall.Errno {
	relPath := d.relPath(name)
	logging.Debug("Unlink: %s", relPath)

	d.mu.Lock()
	child, ok := d.children[name]
	if ok {
		delete(d.children, name)
	}
	d.mu.Unlock()

	if !ok {
		return syscall.ENOENT
	}

	if file, isFile := child.(*pipeFile); isFile {
		file.mu.Lock()
		file.deleted = true
		file.mu.Unlock()
	}

	msg := protocol.Delete{Filename: relPath}
	if err := d.client.Send(protocol.MsgDelete, &msg); err != nil {
		logging.Debug("Unlink: failed to send Delete: %v", err)
	}

	return 0
}

func (d *pipeDir) Rmdir(ctx context.Context, name string) syscall.Errno {
	logging.Debug("Rmdir: %s", d.relPath(name))

	d.mu.Lock()
	child, ok := d.children[name]
	if !ok {
		d.mu.Unlock()
		return syscall.ENOENT
	}

	dir, isDir := child.(*pipeDir)
	if !isDir {
		d.mu.Unlock()
		return syscall.ENOTDIR
	}

	dir.mu.RLock()
	empty := len(dir.children) == 0
	dir.mu.RUnlock()

	if !empty {
		d.mu.Unlock()
		return syscall.ENOTEMPTY
	}

	delete(d.children, name)
	d.mu.Unlock()

	return 0
}

func (d *pipeDir) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	oldPath := d.relPath(name)

	newParentDir, ok := newParent.(*pipeDir)
	if !ok {
		return syscall.EINVAL
	}

	newPath := newParentDir.relPath(newName)
	logging.Debug("Rename: %s -> %s", oldPath, newPath)

	d.mu.Lock()
	child, ok := d.children[name]
	if !ok {
		d.mu.Unlock()
		return syscall.ENOENT
	}
	delete(d.children, name)
	d.mu.Unlock()

	// Get the file ID if this is a pipeFile
	var fileID string
	switch c := child.(type) {
	case *pipeFile:
		c.name = newName
		c.parent = newParentDir
		fileID = c.id
	case *pipeDir:
		c.name = newName
		c.parent = newParentDir
	}

	newParentDir.mu.Lock()
	newParentDir.children[newName] = child
	newParentDir.mu.Unlock()

	msg := protocol.Rename{
		FileID:  fileID,
		OldName: oldPath,
		NewName: newPath,
	}
	if err := d.client.Send(protocol.MsgRename, &msg); err != nil {
		logging.Debug("Rename: failed to send Rename: %v", err)
	}

	return 0
}

func (d *pipeDir) Readdir(ctx context.Context) (fs.DirStream, syscall.Errno) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	entries := make([]fuse.DirEntry, 0, len(d.children))
	for name, child := range d.children {
		var mode uint32
		switch child.(type) {
		case *pipeFile:
			mode = fuse.S_IFREG
		case *pipeDir:
			mode = fuse.S_IFDIR
		}
		entries = append(entries, fuse.DirEntry{
			Name: name,
			Mode: mode,
		})
	}

	return fs.NewListDirStream(entries), 0
}

func (d *pipeDir) relPath(name string) string {
	if d.parent == nil {
		return name
	}
	return filepath.Join(d.parent.relPath(d.name), name)
}

// pipeFile represents a file in the virtual filesystem
type pipeFile struct {
	fs.Inode

	client transport.Transport
	parent *pipeDir
	name   string
	id     string
	mode   uint32

	mu      sync.RWMutex
	size    int64
	deleted bool
}

var _ fs.InodeEmbedder = (*pipeFile)(nil)
var _ fs.NodeGetattrer = (*pipeFile)(nil)
var _ fs.NodeSetattrer = (*pipeFile)(nil)
var _ fs.NodeOpener = (*pipeFile)(nil)

func newPipeFile(client transport.Transport, parent *pipeDir, name string, mode uint32) *pipeFile {
	return &pipeFile{
		client: client,
		parent: parent,
		name:   name,
		id:     uuid.New().String(),
		mode:   mode,
	}
}

func (f *pipeFile) Getattr(ctx context.Context, fh fs.FileHandle, out *fuse.AttrOut) syscall.Errno {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// Ensure world-writable permission for cross-user access (Chrome runs as different user)
	mode := f.mode | 0666
	out.Attr = defaultAttr(fuse.S_IFREG | mode)
	out.Attr.Size = uint64(f.size)
	return 0
}

func (f *pipeFile) Setattr(ctx context.Context, fh fs.FileHandle, in *fuse.SetAttrIn, out *fuse.AttrOut) syscall.Errno {
	f.mu.Lock()
	defer f.mu.Unlock()

	if sz, ok := in.GetSize(); ok {
		logging.Debug("Setattr: truncate %s to %d", f.name, sz)

		f.size = int64(sz)

		msg := protocol.Truncate{
			FileID: f.id,
			Size:   f.size,
		}
		if err := f.client.Send(protocol.MsgTruncate, &msg); err != nil {
			logging.Debug("Setattr: failed to send Truncate: %v", err)
			return syscall.EIO
		}
	}

	if mode, ok := in.GetMode(); ok {
		f.mode = mode
	}

	// Ensure world-writable permission for cross-user access
	mode := f.mode | 0666
	out.Attr = defaultAttr(fuse.S_IFREG | mode)
	out.Attr.Size = uint64(f.size)
	return 0
}

func (f *pipeFile) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	f.mu.RLock()
	deleted := f.deleted
	f.mu.RUnlock()

	if deleted {
		return nil, 0, syscall.ENOENT
	}

	logging.Debug("Open: %s (flags=%d)", f.name, flags)

	handle := newPipeHandle(f)
	return handle, fuse.FOPEN_DIRECT_IO, 0
}

func (f *pipeFile) relPath() string {
	if f.parent == nil {
		return f.name
	}
	return f.parent.relPath(f.name)
}

// pipeHandle is a file handle for write operations
type pipeHandle struct {
	file *pipeFile
}

var _ fs.FileHandle = (*pipeHandle)(nil)
var _ fs.FileWriter = (*pipeHandle)(nil)
var _ fs.FileReader = (*pipeHandle)(nil)
var _ fs.FileFlusher = (*pipeHandle)(nil)
var _ fs.FileReleaser = (*pipeHandle)(nil)
var _ fs.FileFsyncer = (*pipeHandle)(nil)
var _ fs.FileAllocater = (*pipeHandle)(nil)

func newPipeHandle(file *pipeFile) *pipeHandle {
	return &pipeHandle{file: file}
}

func (h *pipeHandle) Write(ctx context.Context, data []byte, off int64) (uint32, syscall.Errno) {
	h.file.mu.Lock()
	defer h.file.mu.Unlock()

	if h.file.deleted {
		return 0, syscall.ENOENT
	}

	remaining := data
	offset := off
	totalWritten := uint32(0)

	for len(remaining) > 0 {
		chunkSize := protocol.ChunkSize
		if len(remaining) < chunkSize {
			chunkSize = len(remaining)
		}

		chunk := remaining[:chunkSize]
		remaining = remaining[chunkSize:]

		msg := protocol.WriteChunk{
			FileID: h.file.id,
			Offset: offset,
			Data:   chunk,
		}

		respType, respData, err := h.file.client.SendAndReceive(protocol.MsgWriteChunk, &msg)
		if err != nil {
			logging.Debug("Write: failed to send chunk: %v", err)
			return totalWritten, syscall.EIO
		}

		if respType != protocol.MsgWriteAck {
			logging.Debug("Write: unexpected response type: 0x%02x", respType)
			return totalWritten, syscall.EIO
		}

		var ack protocol.WriteAck
		if err := protocol.DecodePayload(respData, &ack); err != nil {
			logging.Debug("Write: failed to decode ack: %v", err)
			return totalWritten, syscall.EIO
		}

		if ack.Error != "" {
			logging.Debug("Write: remote error: %s", ack.Error)
			return totalWritten, syscall.EIO
		}

		offset += int64(ack.Written)
		totalWritten += uint32(ack.Written)
	}

	newSize := off + int64(totalWritten)
	if newSize > h.file.size {
		h.file.size = newSize
	}

	return totalWritten, 0
}

func (h *pipeHandle) Read(ctx context.Context, dest []byte, off int64) (fuse.ReadResult, syscall.Errno) {
	return fuse.ReadResultData(nil), 0
}

func (h *pipeHandle) Flush(ctx context.Context) syscall.Errno {
	logging.Debug("Flush: %s", h.file.name)
	return 0
}

func (h *pipeHandle) Fsync(ctx context.Context, flags uint32) syscall.Errno {
	logging.Debug("Fsync: %s (flags=%d)", h.file.name, flags)
	// For a streaming pipe, fsync is a no-op since data is sent immediately
	// Return success to allow Chrome downloads to complete
	return 0
}

func (h *pipeHandle) Allocate(ctx context.Context, off uint64, size uint64, mode uint32) syscall.Errno {
	logging.Debug("Allocate: %s (off=%d, size=%d, mode=%d)", h.file.name, off, size, mode)
	// Pre-allocate space for the file. For a streaming pipe, we just update the size.
	h.file.mu.Lock()
	defer h.file.mu.Unlock()

	newSize := int64(off + size)
	if newSize > h.file.size {
		h.file.size = newSize
	}
	return 0
}

func (h *pipeHandle) Release(ctx context.Context) syscall.Errno {
	logging.Debug("Release: %s", h.file.name)

	h.file.mu.RLock()
	deleted := h.file.deleted
	h.file.mu.RUnlock()

	if deleted {
		return 0
	}

	msg := protocol.FileClose{
		FileID: h.file.id,
	}
	if err := h.file.client.Send(protocol.MsgFileClose, &msg); err != nil {
		logging.Debug("Release: failed to send FileClose: %v", err)
	}

	return 0
}

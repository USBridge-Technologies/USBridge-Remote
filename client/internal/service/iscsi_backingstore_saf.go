// Package service: a gotgt backing store backed by an already-open
// *os.File instead of a real filesystem path. gotgt's stock "file" backing
// store (github.com/gostor/gotgt/pkg/scsi/backingstore) always calls
// os.OpenFile(path, ...) itself, which can't work for Android SAF
// content:// URIs — there's no real path, only a file descriptor obtained
// via platform.SAFHelper.OpenFileDescriptor. This registers a second
// backing store ("androidsaf") that looks its *os.File up from a small
// in-process registry instead of opening one itself, so
// config.BackendStorage.Path can be "androidsaf:<key>" for SAF-backed
// exports and "file:<path>" (the stock store) for everything else.
package service

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/gostor/gotgt/pkg/api"
	"github.com/gostor/gotgt/pkg/scsi"
)

// AndroidSAFBackingStorage is the config.BackendStorage.Path scheme prefix
// for SAF-backed exports (see registerSAFBackingFile).
const AndroidSAFBackingStorage = "androidsaf"

var (
	safFilesMu sync.Mutex
	safFiles   = map[string]*os.File{}
)

// registerSAFBackingFile makes f available to the "androidsaf" backing
// store under key (the caller picks a key unique to this export, e.g. the
// target IQN) before starting the iSCSI target driver, and must
// unregisterSAFBackingFile when the export stops.
func registerSAFBackingFile(key string, f *os.File) {
	safFilesMu.Lock()
	safFiles[key] = f
	safFilesMu.Unlock()
}

func unregisterSAFBackingFile(key string) {
	safFilesMu.Lock()
	delete(safFiles, key)
	safFilesMu.Unlock()
}

func init() {
	scsi.RegisterBackingStore(AndroidSAFBackingStorage, newSAFBackingStore)
}

type safBackingStore struct {
	scsi.BaseBackingStore
	file *os.File
}

func newSAFBackingStore() (api.BackingStore, error) {
	return &safBackingStore{
		BaseBackingStore: scsi.BaseBackingStore{Name: AndroidSAFBackingStorage},
	}, nil
}

// Open looks the registered file up by key (the string after
// "androidsaf:" in config.BackendStorage.Path) rather than opening
// anything itself.
func (bs *safBackingStore) Open(dev *api.SCSILu, key string) error {
	safFilesMu.Lock()
	f, ok := safFiles[key]
	safFilesMu.Unlock()
	if !ok {
		return fmt.Errorf("androidsaf backing store: no file registered for key %q", key)
	}
	stat, err := f.Stat()
	if err != nil {
		return fmt.Errorf("androidsaf backing store: stat: %w", err)
	}
	bs.DataSize = uint64(stat.Size())
	bs.file = f
	return nil
}

// Close is a no-op: the fd's lifecycle belongs to the caller
// (IscsiTargetRunner.Stop, via platform.SAFHelper.CloseFD), matching how
// the old NBD SAF flow worked — gotgt didn't open the file, so it doesn't
// own closing it either.
func (bs *safBackingStore) Close(dev *api.SCSILu) error { return nil }

func (bs *safBackingStore) Init(dev *api.SCSILu, opts string) error { return nil }
func (bs *safBackingStore) Exit(dev *api.SCSILu) error              { return nil }
func (bs *safBackingStore) Size(dev *api.SCSILu) uint64             { return bs.DataSize }

func (bs *safBackingStore) Read(offset, tl int64) ([]byte, error) {
	if bs.file == nil {
		return nil, fmt.Errorf("androidsaf backing store: not open")
	}
	buf := make([]byte, tl)
	n, err := bs.file.ReadAt(buf, offset)
	if err != nil && err != io.EOF {
		return nil, err
	}
	if int64(n) != tl {
		return nil, fmt.Errorf("androidsaf backing store: short read (got %d, want %d)", n, tl)
	}
	return buf, nil
}

func (bs *safBackingStore) Write(wbuf []byte, offset int64) error {
	if bs.file == nil {
		return fmt.Errorf("androidsaf backing store: not open")
	}
	n, err := bs.file.WriteAt(wbuf, offset)
	if err != nil {
		return err
	}
	if n != len(wbuf) {
		return fmt.Errorf("androidsaf backing store: short write (wrote %d, want %d)", n, len(wbuf))
	}
	return nil
}

func (bs *safBackingStore) DataSync(offset, tl int64) error {
	if bs.file == nil {
		return nil
	}
	return bs.file.Sync()
}

// DataAdvise is a no-op: there's no posix_fadvise-equivalent for an
// Android ParcelFileDescriptor-backed fd.
func (bs *safBackingStore) DataAdvise(offset, length int64, advise uint32) error { return nil }

func (bs *safBackingStore) Unmap([]api.UnmapBlockDescriptor) error { return nil }

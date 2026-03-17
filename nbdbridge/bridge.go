// Package nbdbridge provides NBD (Network Block Device) server functionality
// for streaming disk images over the network using file descriptors from Android SAF
package nbdbridge

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"syscall"

	nbd "github.com/pojntfx/go-nbd/pkg/server"
	"github.com/sirupsen/logrus"
)

var (
	// ErrAlreadyRunning is returned when trying to start NBD server that's already running
	ErrAlreadyRunning = errors.New("NBD server already running")
	// ErrUnknownSize is returned when file size cannot be determined
	ErrUnknownSize = errors.New("cannot determine file size")
	// ErrNotRunning is returned when trying to stop a server that's not running
	ErrNotRunning = errors.New("NBD server not running")
)

// serverState holds the global NBD server state
type serverState struct {
	mu       sync.Mutex
	listener net.Listener
	file     *os.File
	backend  *fileBackend
	addr     string
	size     int64
	running  bool
	readOnly bool
	errMsg   string
}

var state = &serverState{}

// fileBackend implements the NBD backend interface using an os.File
type fileBackend struct {
	file     *os.File
	size     int64
	readOnly bool
}

// Size returns the total size of the backend in bytes
func (fb *fileBackend) Size() (int64, error) {
	return fb.size, nil
}

// ReadAt reads up to len(p) bytes from the backend at offset off.
// Ограничиваем чтение размером устройства, чтобы не запрашивать данные за концом (SAF/файл может вернуть ошибку и ядро даёт I/O error на nbd0).
func (fb *fileBackend) ReadAt(p []byte, off int64) (int, error) {
	if off >= fb.size {
		return 0, io.EOF
	}
	limit := int64(len(p))
	if off+limit > fb.size {
		limit = fb.size - off
	}
	if limit <= 0 {
		return 0, nil
	}
	buf := p[:limit]
	n, err := fb.file.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		logrus.Errorf("NBD ReadAt error at offset %d: %v", off, err)
		return n, err
	}
	return n, nil
}

// WriteAt writes data to the file when readOnly is false; otherwise returns permission error
func (fb *fileBackend) WriteAt(p []byte, off int64) (int, error) {
	if fb.readOnly {
		return 0, os.ErrPermission
	}
	return fb.file.WriteAt(p, off)
}

// Flush syncs the file when RW; no-op for read-only
func (fb *fileBackend) Flush() error {
	if fb.readOnly {
		return nil
	}
	return fb.file.Sync()
}

// Trim is a no-op (optional hint)
func (fb *fileBackend) Trim(off, length int64) error {
	return nil
}

// Sync syncs the file to disk when RW; no-op for read-only
func (fb *fileBackend) Sync() error {
	if fb.readOnly {
		return nil
	}
	return fb.file.Sync()
}

// Close closes the underlying file
func (fb *fileBackend) Close() error {
	if fb.file != nil {
		return fb.file.Close()
	}
	return nil
}

// getFileSize attempts to determine the file size from fd
func getFileSize(fd int) (int64, error) {
	var stat syscall.Stat_t
	err := syscall.Fstat(fd, &stat)
	if err != nil {
		return -1, fmt.Errorf("fstat failed: %w", err)
	}

	if stat.Size > 0 {
		return stat.Size, nil
	}

	return -1, ErrUnknownSize
}

// StartNBD starts the NBD server with the given file descriptor, size, address and read-only flag.
// fd: file descriptor from Android SAF ParcelFileDescriptor.detachFd()
// size: file size in bytes (if < 0, will attempt to determine from fd)
// addr: TCP address to listen on (e.g., "127.0.0.1:10809" or ":10809")
// readOnly: if true, export is read-only; if false, writes are allowed (for RW overlay usage)
func StartNBD(fd int, size int64, addr string, readOnly bool) error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.running {
		return ErrAlreadyRunning
	}

	// Wrap fd in os.File
	file := os.NewFile(uintptr(fd), "sdimage")
	if file == nil {
		return fmt.Errorf("failed to create file from fd %d", fd)
	}

	// Determine size if not provided
	if size < 0 {
		detectedSize, err := getFileSize(fd)
		if err != nil {
			file.Close()
			return fmt.Errorf("%w: %v", ErrUnknownSize, err)
		}
		size = detectedSize
		logrus.Infof("NBD: detected file size: %d bytes", size)
	}

	if size <= 0 {
		file.Close()
		return fmt.Errorf("invalid file size: %d", size)
	}

	// Create backend (readOnly controls WriteAt/Flush/Sync)
	backend := &fileBackend{
		file:     file,
		size:     size,
		readOnly: readOnly,
	}

	// Create listener
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		file.Close()
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	// Update state
	state.listener = listener
	state.file = file
	state.backend = backend
	state.addr = addr
	state.size = size
	state.running = true
	state.readOnly = readOnly
	state.errMsg = ""

	logrus.Infof("NBD server starting on %s (size: %d bytes, read-only=%v)", addr, size, readOnly)

	// NBD options: handshake advertises read-only so client (e.g. loop) allows writes when false
	options := &nbd.Options{
		ReadOnly:        readOnly,
		MinimumBlockSize: 4096,
	}

	// Create export
	exports := []*nbd.Export{
		{
			Name:        "",
			Description: "USB Bridge NBD Export",
			Backend:     backend,
		},
	}

	// Start serving in goroutine
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				state.mu.Lock()
				if state.running {
					logrus.Errorf("NBD accept error: %v", err)
					state.errMsg = fmt.Sprintf("accept error: %v", err)
				}
				state.mu.Unlock()
				return
			}

			logrus.Infof("NBD client connected from %s", conn.RemoteAddr())

			// Handle client in goroutine
			go func(c net.Conn) {
				defer c.Close()

				err := nbd.Handle(c, exports, options)

				if err != nil {
					logrus.Errorf("NBD handle error: %v", err)
				} else {
					logrus.Info("NBD client disconnected")
				}
			}(conn)
		}
	}()

	return nil
}

// StopNBD stops the NBD server and releases resources
func StopNBD() error {
	state.mu.Lock()
	defer state.mu.Unlock()

	if !state.running {
		return ErrNotRunning
	}

	logrus.Info("Stopping NBD server...")

	// Close listener (stops accepting new connections)
	if state.listener != nil {
		state.listener.Close()
		state.listener = nil
	}

	// Close file
	if state.file != nil {
		state.file.Close()
		state.file = nil
	}

	// Reset state
	state.backend = nil
	state.addr = ""
	state.size = 0
	state.running = false
	state.readOnly = false
	state.errMsg = ""

	logrus.Info("NBD server stopped")
	return nil
}

// Status returns the current NBD server status as a string
func Status() string {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.errMsg != "" {
		return fmt.Sprintf("error: %s", state.errMsg)
	}

	if state.running {
		return fmt.Sprintf("serving %s", state.addr)
	}

	return "stopped"
}

// IsRunning returns true if the NBD server is currently running
func IsRunning() bool {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.running
}

// GetAddr returns the current listening address (empty if not running)
func GetAddr() string {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.addr
}

// GetSize returns the exported image size in bytes (0 if not running)
func GetSize() int64 {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.size
}

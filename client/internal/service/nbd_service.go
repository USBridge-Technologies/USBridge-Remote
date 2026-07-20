package service

import (
	"context"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"

	"fyne.io/fyne/v2"
	"github.com/pojntfx/go-nbd/pkg/server"
	"github.com/sirupsen/logrus"
)

// NBDServer NBD server for serving ISO images
type NBDServer struct {
	bindHost     string
	port         int
	exports      map[string]*DiskExport
	listener     net.Listener
	isRunning    bool
	mutex        sync.RWMutex
	clients      map[string]*NBDClient
	clientMutex  sync.RWMutex
	healthTicker *time.Ticker
	stopHealth   chan bool
	app          fyne.App      // For accessing SAF on Android
	readyChan    chan struct{} // Channel signaling that the server is ready
	allowedIP    string        // if non-empty, reject connections not from this IP
}

// SetAllowedIP restricts NBD connections to a single remote IP.
// Call before Start(); safe to call concurrently.
func (ns *NBDServer) SetAllowedIP(ip string) {
	ns.mutex.Lock()
	ns.allowedIP = ip
	ns.mutex.Unlock()
	if ip != "" {
		logrus.Infof("🔒 [NBD] Connection filter set: only %s is allowed", ip)
	}
}

// DiskExport device export for the NBD server
type DiskExport struct {
	Name        string
	FilePath    string
	Size        int64
	ReadOnly    bool
	Description string
	IsActive    bool
	ExportName  string
	Backend     *FileBackend
	OverlayPath string // path to the overlay (not removed when the export is dropped — reused)
}

// NBDClient information about a connected client
type NBDClient struct {
	Conn         net.Conn
	Export       *DiskExport
	ConnectedAt  time.Time
	LastActivity time.Time
}

// FileBackend backend for working with files
type FileBackend struct {
	file        *os.File
	lock        sync.RWMutex
	virtualSize int64 // if > 0, Size() returns it (for qcow2 overlays)
}

// NewFileBackend creates a new file backend (size comes from file.Stat()).
func NewFileBackend(file *os.File) *FileBackend {
	return &FileBackend{file: file, virtualSize: 0}
}

// NewFileBackendWithSize creates a backend with an explicit size (for overlays: virtual size).
func NewFileBackendWithSize(file *os.File, virtualSize int64) *FileBackend {
	return &FileBackend{file: file, virtualSize: virtualSize}
}

// ReadAt reads data from the file
func (b *FileBackend) ReadAt(p []byte, off int64) (n int, err error) {
	b.lock.RLock()
	defer b.lock.RUnlock()
	return b.file.ReadAt(p, off)
}

// WriteAt writes data to the file
func (b *FileBackend) WriteAt(p []byte, off int64) (n int, err error) {
	b.lock.Lock()
	defer b.lock.Unlock()
	return b.file.WriteAt(p, off)
}

// Size returns the size: virtualSize if set, otherwise file.Stat().
func (b *FileBackend) Size() (int64, error) {
	if b.virtualSize > 0 {
		return b.virtualSize, nil
	}
	stat, err := b.file.Stat()
	if err != nil {
		return -1, err
	}
	return stat.Size(), nil
}

// Sync syncs the file
func (b *FileBackend) Sync() error {
	return b.file.Sync()
}

// NewNBDServer creates a new NBD server
func NewNBDServer(bindHost string) *NBDServer {
	return &NBDServer{
		bindHost:   bindHost,
		exports:    make(map[string]*DiskExport),
		clients:    make(map[string]*NBDClient),
		stopHealth: make(chan bool),
		app:        nil,
		readyChan:  make(chan struct{}),
	}
}

// NewNBDServerWithApp creates a new NBD server with SAF support
func NewNBDServerWithApp(bindHost string, app fyne.App) *NBDServer {
	logrus.Infof("📍 [NBD-INIT] Creating NBD server with SAF support (app: %T)", app)
	return &NBDServer{
		bindHost:   bindHost,
		exports:    make(map[string]*DiskExport),
		clients:    make(map[string]*NBDClient),
		stopHealth: make(chan bool),
		app:        app,
		readyChan:  make(chan struct{}),
	}
}

// SetApp sets the app used for SAF support
func (ns *NBDServer) SetApp(app fyne.App) {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()
	logrus.Infof("📍 [NBD-SETAPP] Setting app on the NBD server (app: %T)", app)
	ns.app = app
}

// Start starts the NBD server
func (ns *NBDServer) Start(port int) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	if ns.isRunning {
		return fmt.Errorf("NBD server is already running")
	}

	// Create a ListenConfig with SO_REUSEADDR for fast port reuse
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			// Set SO_REUSEADDR for immediate port reuse
			// Platform-specific implementation in nbd_sockopt_*.go
			return setSocketReuseAddr(c)
		},
	}

	bindHost := ns.bindHost
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	ns.port = port
	addr := fmt.Sprintf("%s:%d", bindHost, port)
	logrus.Infof("📍 [NBD-START-1] Creating TCP listener on %s (port %d)", addr, port)
	listener, err := lc.Listen(context.Background(), "tcp", addr)
	if err != nil {
		logrus.Errorf("❌ [NBD-START-1-ERROR] Error creating listener on %s: %v", addr, err)
		return fmt.Errorf("error starting NBD server: %v", err)
	}
	// Confirm the actual listener address (may differ when port is 0)
	actualAddr := listener.Addr().String()
	logrus.Infof("✅ [NBD-START-1-SUCCESS] TCP listener created: requested=%s, actual=%s", addr, actualAddr)

	ns.listener = listener
	ns.isRunning = true

	logrus.Infof("📍 [NBD-START-2] Starting the acceptConnections goroutine to accept connections")
	go ns.acceptConnections()

	logrus.Infof("📍 [NBD-START-3] Starting health monitoring")
	ns.startHealthMonitoring()

	logrus.Infof("🚀 [NBD-START-4-SUCCESS] NBD server started on %s, waiting for exports to be added", actualAddr)

	// We DO NOT close readyChan here - the server isn't ready to accept connections yet
	// readyChan will be closed after exports are added via SignalReady()

	return nil
}

// Stop stops the NBD server
func (ns *NBDServer) Stop() error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	if !ns.isRunning {
		return fmt.Errorf("NBD server is not running")
	}

	// Recreate the ready channel for the next start
	ns.readyChan = make(chan struct{})

	// Stop health monitoring
	ns.stopHealthMonitoring()

	// Close all client connections
	ns.clientMutex.Lock()
	for _, client := range ns.clients {
		client.Conn.Close()
	}
	ns.clients = make(map[string]*NBDClient)
	ns.clientMutex.Unlock()

	// Close all file backends and remove overlay files
	for _, export := range ns.exports {
		if export.Backend != nil && export.Backend.file != nil {
			export.Backend.file.Close()

			// If this is an Android content:// URI, clear the SAF cache
			if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
				safHelper := platform.GetSAFHelper(ns.app)
				if err := safHelper.CloseFD(export.FilePath); err != nil {
					logrus.Warnf("⚠️ Error clearing SAF cache for %s: %v", export.FilePath, err)
				}
			}
		}
		// We don't remove the overlay — it will be reused on the next mount
	}

	// First set the stopped flag
	ns.isRunning = false

	// Close the listener
	if ns.listener != nil {
		// Force-close the listener
		if err := ns.listener.Close(); err != nil {
			logrus.Warnf("⚠️ Error closing listener: %v", err)
		}
		ns.listener = nil

		// Give the acceptConnections goroutine time to finish and the OS time to free the port
		time.Sleep(200 * time.Millisecond)
	}

	logrus.Info("🛑 NBD server stopped")
	return nil
}

// IsRunning checks whether the server is running
func (ns *NBDServer) IsRunning() bool {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()
	return ns.isRunning
}

// WaitReady returns a channel that closes once the server is ready to accept connections
func (ns *NBDServer) WaitReady() <-chan struct{} {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()
	return ns.readyChan
}

// SignalReady signals that the server is ready to accept connections (exports have been added)
// Starts an asynchronous check of NBD protocol readiness
func (ns *NBDServer) SignalReady() {
	ns.mutex.Lock()

	logrus.Infof("📍 [NBD-SIGNAL-READY-1] SignalReady called, checking readyChan state")

	// Check that the channel isn't already closed
	select {
	case <-ns.readyChan:
		// The channel is already closed
		ns.mutex.Unlock()
		logrus.Warn("⚠️ [NBD-SIGNAL-READY-ALREADY] readyChan is already closed, skipping")
		return
	default:
		// The channel is open, start the readiness check in a goroutine
	}

	// Log the current server state
	logrus.Infof("📍 [NBD-SIGNAL-READY-2] Current state:")
	logrus.Infof("   - isRunning: %v", ns.isRunning)
	logrus.Infof("   - listener != nil: %v", ns.listener != nil)
	logrus.Infof("   - exports count: %d", len(ns.exports))
	for exportName, export := range ns.exports {
		logrus.Infof("     • %s: %s (size: %d bytes)", exportName, export.FilePath, export.Size)
	}

	port := ns.port
	hasExports := len(ns.exports) > 0
	hasListener := ns.listener != nil
	ns.mutex.Unlock()

	// Run the readiness check in a separate goroutine so we don't block the caller
	go ns.verifyReadiness(port, hasListener, hasExports)
}

// verifyReadiness checks NBD protocol readiness and closes readyChan once ready
func (ns *NBDServer) verifyReadiness(port int, hasListener bool, hasExports bool) {
	checkHost := ns.bindHost
	if checkHost == "" {
		checkHost = "127.0.0.1"
	}
	checkAddr := net.JoinHostPort(checkHost, fmt.Sprint(port))
	logrus.Infof("📍 [NBD-VERIFY-1] Starting readiness check: connecting to %s (hasListener=%v, hasExports=%v)", checkAddr, hasListener, hasExports)

	// Check that the server is actually listening on the port and ready to handle the NBD protocol
	if hasListener && hasExports {
		logrus.Infof("📍 [NBD-VERIFY-2] Checking NBD handshake: TCP connect -> expecting 8 bytes of NBDMAGIC (0x4e42444d41474943) from the server")

		// Try a test connection and verify the NBD handshake
		maxAttempts := 20
		success := false
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			logrus.Infof("📍 [NBD-VERIFY-ATTEMPT] Attempt %d/%d: DialTimeout(%s, 200ms)...", attempt, maxAttempts, checkAddr)
			conn, err := net.DialTimeout("tcp", checkAddr, 200*time.Millisecond)
			if err != nil {
				if attempt < maxAttempts {
					logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Port %d not accepting connections (attempt %d/%d): %v", port, attempt, maxAttempts, err)
					time.Sleep(100 * time.Millisecond)
					continue
				} else {
					logrus.Errorf("❌ [NBD-VERIFY-FAIL] Port %d did not respond after %d attempts: %v", port, maxAttempts, err)
					break
				}
			}

			// The port accepted the connection, now check the NBD handshake
			// The NBD server must send a magic number upon connection
			conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))

			// The NBD protocol begins with "NBDMAGIC" (0x4e42444d41474943)
			magic := make([]byte, 8)
			n, err := conn.Read(magic)
			conn.Close()

			magicHex := hex.EncodeToString(magic)
			if err != nil {
				logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Port %d: connection established, but Read returned err=%v, n=%d, got hex=%s", port, err, n, magicHex)
			} else if n != 8 {
				logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Port %d: read %d bytes (expected 8), hex=%s", port, n, magicHex)
			} else {
				// Check the NBD magic number
				expectedMagic := []byte{0x4e, 0x42, 0x44, 0x4d, 0x41, 0x47, 0x49, 0x43} // "NBDMAGIC"
				isValid := true
				for i := 0; i < 8; i++ {
					if magic[i] != expectedMagic[i] {
						isValid = false
						break
					}
				}

				if isValid {
					logrus.Infof("✅ [NBD-VERIFY-SUCCESS] Port %d: server responded with NBDMAGIC (hex=%s), NBD protocol ready (attempt %d/%d)", port, magicHex, attempt, maxAttempts)
					success = true
					break
				} else {
					logrus.Warnf("⏳ [NBD-VERIFY-RETRY] Port %d: got an incorrect magic value (expected NBDMAGIC), hex=%s", port, magicHex)
				}
			}

			if attempt < maxAttempts {
				time.Sleep(100 * time.Millisecond)
			}
		}

		if !success {
			logrus.Warnf("⚠️ [NBD-VERIFY-WARN] NBD protocol did not confirm readiness after %d attempts, continuing anyway", maxAttempts)
		}
	} else {
		logrus.Warnf("⚠️ [NBD-VERIFY-SKIP] Skipping check: hasListener=%v, hasExports=%v", hasListener, hasExports)
	}

	// Close the ready channel
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	select {
	case <-ns.readyChan:
		// The channel is already closed
		logrus.Warn("⚠️ [NBD-VERIFY-ALREADY-CLOSED] readyChan already closed by another goroutine")
	default:
		close(ns.readyChan)
		logrus.Info("✅ [NBD-VERIFY-READY] NBD server ready to accept connections (readyChan closed)")
	}
}

// AddExport adds an export to the server
func (ns *NBDServer) AddExport(export *models.DiskExport) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	logrus.Infof("📍 [NBD-ADDEXPORT-1] AddExport called for: %s (FilePath: %s, ExportName: %s)",
		export.Name, export.FilePath, export.ExportName)

	// Check if an export with this name already exists
	if _, exists := ns.exports[export.ExportName]; exists {
		err := fmt.Errorf("export with name '%s' already exists", export.ExportName)
		logrus.Errorf("❌ [NBD-ADDEXPORT-1-ERROR] %v", err)
		return err
	}

	var file *os.File
	var fileSize int64
	var err error
	var overlayPathForCleanup string // path to overlay (desktop only; not removed when unmounted)

	// On Android with content:// URI use SAF
	if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") {
		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-2] Android content:// URI detected")

		if ns.app == nil {
			err := fmt.Errorf("NBDServer: app is not set, cannot use SAF")
			logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-2-ERROR] %v", err)
			return err
		}

		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-3] Using SAF to open file")

		// Get SAF helper
		safHelper := platform.GetSAFHelper(ns.app)

		// Open file via SAF
		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-4] Calling OpenFileDescriptor for URI: %s", export.FilePath)

		// Determine opening mode based on file source
		var mode string
		if strings.Contains(export.FilePath, "com.google.android.apps.docs.storage") {
			// Google Drive - read-only
			mode = "r"
			export.ReadOnly = true
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-4] Google Drive detected, using 'r' (read-only) mode")
		} else {
			// Local files - try read-write
			mode = "rw"
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-4] Local file, using 'rw' mode")
		}

		file, err = safHelper.OpenFileDescriptor(export.FilePath, mode)
		if err != nil {
			// If failed to open with selected mode, try fallback to read-only
			if mode == "rw" {
				logrus.Warnf("⚠️ [NBD-ADDEXPORT-ANDROID-4-FALLBACK] Failed to open in 'rw' mode: %v, trying 'r'", err)
				file, err = safHelper.OpenFileDescriptor(export.FilePath, "r")
				if err != nil {
					logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-4-ERROR] Failed to open file: %v", err)
					return fmt.Errorf("error opening file via SAF %s: %v", export.FilePath, err)
				}
				export.ReadOnly = true
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-4-SUCCESS] File opened via SAF in read-only mode (fallback)")
			} else {
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-4-ERROR] Failed to open file: %v", err)
				return fmt.Errorf("error opening file via SAF %s: %v", export.FilePath, err)
			}
		} else {
			if mode == "r" {
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-4-SUCCESS] File opened via SAF in read-only mode")
			} else {
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-4-SUCCESS] File opened via SAF in read-write mode")
			}
		}

		// Get size via file.Stat()
		logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-5] Getting file size")
		stat, err := file.Stat()
		if err != nil {
			file.Close()
			logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-5-ERROR] Failed to get file size: %v", err)
			return fmt.Errorf("error getting file size via SAF %s: %v", export.FilePath, err)
		}
		fileSize = stat.Size()
		logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-5-SUCCESS] File size: %d bytes", fileSize)

		// Google Drive DOES NOT SUPPORT random access/seek via SAF!
		// NBD protocol requires ReadAt (random access)
		// Strategy: read the entire file sequentially so Android caches it, then close and reopen
		if strings.Contains(export.FilePath, "com.google.android.apps.docs.storage") {
			logrus.Warnf("⚠️ [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Google Drive file detected!")
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Google Drive does not support random access via SAF")
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Reading entire file for Android caching...")

			// Read the entire file sequentially so Android caches it
			startTime := time.Now()
			logrus.Infof("📥 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Started reading %d bytes from Google Drive for caching...", fileSize)

			// Rewind file to beginning
			if _, err := file.Seek(0, 0); err != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-ERROR] Failed to rewind file: %v", err)
				return fmt.Errorf("error reading file from Google Drive %s: %v", export.FilePath, err)
			}

			// Read the entire file sequentially using io.Discard
			// Android will automatically cache data upon reading
			written, err := io.Copy(io.Discard, file)
			duration := time.Since(startTime)

			if err != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-ERROR] Error reading data for caching: %v (read %d of %d bytes in %v)", err, written, fileSize, duration)
				return fmt.Errorf("error caching file from Google Drive %s: %v", export.FilePath, err)
			}

			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-SUCCESS] Read %d bytes in %v (%.2f MB/s)",
				written, duration, float64(written)/1024/1024/duration.Seconds())

			// CRITICAL: io.Copy might return BEFORE Android fully downloaded the file into cache!
			// Use comprehensive cache readiness check (size is stable + ReadAt is fast)
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Checking cache readiness (size + ReadAt speed)...")
			if err := WaitForGDriveCache(file, fileSize, 3*time.Minute); err != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-CACHE-TIMEOUT] %v", err)
				return fmt.Errorf("Google Drive file %s was not cached: %v", export.FilePath, err)
			}
			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-CACHE-VERIFIED] Cache is fully ready (size and speed verified)")

			// CRITICAL: Close and reopen the file!
			// Android caches the file, but ParcelFileDescriptor might not switch to local cache
			// Reopening forces Android to return a descriptor to the cached file with seek support
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] Closing and reopening file to use cache...")

			file.Close()
			safHelper.CloseFD(export.FilePath)

			// Reopen the file via SAF - Android should now return the cached version
			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE-REOPEN] Reopening file via SAF...")
			file, err = safHelper.OpenFileDescriptor(export.FilePath, "r")
			if err != nil {
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-REOPEN-ERROR] Failed to reopen file: %v", err)
				return fmt.Errorf("error reopening file from Google Drive %s: %v", export.FilePath, err)
			}

			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-REOPEN-SUCCESS] File reopened, seek should work now")

			// Check that seek works now
			seekOffset, seekErr := file.Seek(0, 0)
			if seekErr != nil {
				file.Close()
				logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-SEEK-TEST-ERROR] Seek still not working after reopening: %v", seekErr)
				return fmt.Errorf("file from Google Drive %s does not support seek even after caching: %v", export.FilePath, seekErr)
			}
			logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-SEEK-TEST] Seek works! (offset=%d)", seekOffset)

			// Test ReadAt with various offsets (critical for NBD!)
			// NBD client will read from arbitrary offsets, so we test multiple
			verifyBuf := make([]byte, 512)
			testOffsets := []int64{0, 512, 1024, fileSize / 2, fileSize - 512}

			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE-READAT-TEST] Testing ReadAt with %d different offsets...", len(testOffsets))
			for i, offset := range testOffsets {
				if offset < 0 || offset >= fileSize {
					continue // Skip invalid offsets
				}

				n, testErr := file.ReadAt(verifyBuf, offset)
				if testErr != nil && testErr != io.EOF {
					file.Close()
					logrus.Errorf("❌ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-READAT-TEST-ERROR] ReadAt fails at offset %d (test %d/%d): %v", offset, i+1, len(testOffsets), testErr)
					return fmt.Errorf("file from Google Drive %s does not support ReadAt at offset %d: %v", export.FilePath, offset, testErr)
				}
				logrus.Infof("✅ [NBD-ADDEXPORT-ANDROID-6-GDRIVE-READAT-TEST] ReadAt #%d: offset=%d, read=%d bytes", i+1, offset, n)
			}

			logrus.Infof("📍 [NBD-ADDEXPORT-ANDROID-6-GDRIVE] File is ready for NBD (cached by Android, seek and ReadAt work at all offsets)")
		}

	} else {
		// Desktop or standard path
		logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-2] Desktop mode, regular file open")

		// Check if file exists
		if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
			logrus.Errorf("❌ [NBD-ADDEXPORT-DESKTOP-2-ERROR] File does not exist: %s", export.FilePath)
			return fmt.Errorf("file %s does not exist", export.FilePath)
		}

		ext := strings.ToLower(filepath.Ext(export.FilePath))
		useOverlay := !export.ReadOnly && IsOverlayCapableExtension(ext)
		openPath := export.FilePath
		overlayPath := ""

		if useOverlay {
			// RW without corrupting the base: create qcow2 overlay next to the image
			created, vs, createErr := createOverlay(export.FilePath)
			if createErr != nil {
				logrus.Warnf("⚠️ [NBD-ADDEXPORT-DESKTOP-OVERLAY] Failed to create overlay (exporting base as RO): %v", createErr)
				useOverlay = false
				export.ReadOnly = true
			} else {
				overlayPath = created
				overlayPathForCleanup = created
				openPath = created
				fileSize = vs
				logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-OVERLAY] Exporting via overlay: %s (virtual size: %d)", overlayPath, fileSize)
			}
		}

		if !useOverlay {
			// Open file (RO or direct RW)
			flags := os.O_RDONLY
			if !export.ReadOnly {
				flags = os.O_RDWR
			}
			logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-3] Opening file: %s (readOnly=%v)", openPath, export.ReadOnly)
			file, err = os.OpenFile(openPath, flags, 0)
			if err != nil {
				logrus.Errorf("❌ [NBD-ADDEXPORT-DESKTOP-3-ERROR] Error opening file: %v", err)
				return fmt.Errorf("error opening file %s: %v", openPath, err)
			}
			stat, err := file.Stat()
			if err != nil {
				file.Close()
				return fmt.Errorf("error getting file size %s: %v", openPath, err)
			}
			fileSize = stat.Size()
			logrus.Infof("✅ [NBD-ADDEXPORT-DESKTOP-4-SUCCESS] File size: %d bytes", fileSize)
		} else {
			// Open created overlay for RW
			logrus.Infof("📍 [NBD-ADDEXPORT-DESKTOP-3] Opening overlay: %s", openPath)
			file, err = os.OpenFile(openPath, os.O_RDWR, 0)
			if err != nil {
				logrus.Errorf("❌ [NBD-ADDEXPORT-DESKTOP-3-ERROR] Error opening overlay: %v", err)
				return fmt.Errorf("error opening overlay %s: %v", openPath, err)
			}
		}
	}

	// Generate export name if not set
	if export.ExportName == "" {
		export.ExportName = filepath.Base(export.FilePath)
		logrus.Infof("📍 [NBD-ADDEXPORT-6] Generated export name: %s", export.ExportName)
	}

	// Create file backend (for overlay - with virtual size)
	logrus.Infof("📍 [NBD-ADDEXPORT-7] Creating file backend")
	var backend *FileBackend
	if overlayPathForCleanup != "" {
		backend = NewFileBackendWithSize(file, fileSize)
	} else {
		backend = NewFileBackend(file)
	}

	// Create export
	diskExport := &DiskExport{
		Name:        export.Name,
		FilePath:    export.FilePath,
		Size:        fileSize,
		ReadOnly:    export.ReadOnly,
		Description: export.Description,
		IsActive:    true,
		ExportName:  export.ExportName,
		Backend:     backend,
		OverlayPath: overlayPathForCleanup,
	}

	ns.exports[export.ExportName] = diskExport
	logrus.Infof("✅ [NBD-ADDEXPORT-8-SUCCESS] Export %s added: %s (size: %d bytes, total exports: %d)",
		export.ExportName, export.FilePath, fileSize, len(ns.exports))

	// CRITICAL: Give go-nbd library time to initialize AFTER adding the export
	// Without this delay the NBD protocol might not be ready for handshake
	logrus.Infof("⏳ [NBD-ADDEXPORT-9] Waiting for go-nbd protocol initialization (500ms)...")
	time.Sleep(500 * time.Millisecond)
	logrus.Infof("✅ [NBD-ADDEXPORT-9-SUCCESS] go-nbd protocol initialized for export %s", export.ExportName)

	return nil
}

// RemoveExport removes an export from the server
func (ns *NBDServer) RemoveExport(exportName string) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	// Log stack trace to understand where removal is called from
	logrus.Infof("🔍 RemoveExport called for export %s", exportName)
	logrus.Debugf("📍 Call stack trace for RemoveExport:")

	// Get stack trace
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	logrus.Debugf("📍 %s", string(buf[:n]))

	export, exists := ns.exports[exportName]
	if !exists {
		logrus.Warnf("⚠️ Export %s not found when trying to remove", exportName)
		return fmt.Errorf("export %s not found", exportName)
	}

	// Close file
	if export.Backend != nil && export.Backend.file != nil {
		export.Backend.file.Close()

		// If this is Android content:// URI, clear SAF cache
		if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
			safHelper := platform.GetSAFHelper(ns.app)
			if err := safHelper.CloseFD(export.FilePath); err != nil {
				logrus.Warnf("⚠️ Error clearing SAF cache for %s: %v", export.FilePath, err)
			}
		}
	}

	// Do not remove overlay - the same overlay will be used on next mount

	delete(ns.exports, exportName)
	logrus.Infof("🗑️ Export %s removed", exportName)
	return nil
}

// GetExports returns the list of exports
func (ns *NBDServer) GetExports() []*models.DiskExport {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()

	exports := make([]*models.DiskExport, 0, len(ns.exports))
	for _, export := range ns.exports {
		modelsExport := &models.DiskExport{
			Name:        export.Name,
			FilePath:    export.FilePath,
			Size:        export.Size,
			ReadOnly:    export.ReadOnly,
			Description: export.Description,
			IsActive:    export.IsActive,
			ExportName:  export.ExportName,
		}
		exports = append(exports, modelsExport)
	}
	return exports
}

// GetClients returns the list of connected clients
func (ns *NBDServer) GetClients() []*NBDClient {
	ns.clientMutex.RLock()
	defer ns.clientMutex.RUnlock()

	clients := make([]*NBDClient, 0, len(ns.clients))
	for _, client := range ns.clients {
		clients = append(clients, client)
	}
	return clients
}

// NBDExportNameForConnection returns the export name for NBD handshake (in go-nbd - the name of added export).
func (ns *NBDServer) NBDExportNameForConnection() string {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()
	for name := range ns.exports {
		return name
	}
	return ""
}

// NBDExportNameForAPI returns the export name for API (in go-nbd - file/export name).
func (ns *NBDServer) NBDExportNameForAPI() string {
	return ns.NBDExportNameForConnection()
}

// NBDHandshakeEmptyExport - in go-nbd we use the export name in handshake.
func (ns *NBDServer) NBDHandshakeEmptyExport() bool {
	return false
}

// acceptConnections accepts incoming connections
func (ns *NBDServer) acceptConnections() {
	logrus.Infof("🎧 [NBD-ACCEPT-START] acceptConnections goroutine started, listening for incoming connections")

	for {
		// Check that server is still running and listener is not nil
		ns.mutex.RLock()
		if !ns.isRunning || ns.listener == nil {
			ns.mutex.RUnlock()
			logrus.Infof("🛑 [NBD-ACCEPT-STOP] acceptConnections exiting (isRunning=%v, listener=%v)", ns.isRunning, ns.listener != nil)
			return
		}
		listener := ns.listener
		ns.mutex.RUnlock()

		ns.mutex.RLock()
		listenAddr := "unknown"
		if ns.listener != nil {
			listenAddr = ns.listener.Addr().String()
		}
		ns.mutex.RUnlock()
		logrus.Infof("⏳ [NBD-ACCEPT-WAITING] Waiting for incoming connection on %s (Accept)...", listenAddr)

		conn, err := listener.Accept()
		if err != nil {
			// Check again after error - server might have been stopped
			ns.mutex.RLock()
			stillRunning := ns.isRunning
			ns.mutex.RUnlock()

			if stillRunning {
				logrus.Errorf("❌ [NBD-ACCEPT-ERROR] Error accepting connection: %v", err)
			} else {
				logrus.Infof("ℹ️ [NBD-ACCEPT-STOPPED] Server stopped, ignoring Accept error")
			}

			// If server stopped, break the loop
			if !stillRunning {
				return
			}
			continue
		}

		remoteAddr := conn.RemoteAddr().String()
		logrus.Infof("✅ [NBD-ACCEPT-SUCCESS] Accepted incoming connection: remote=%s -> local=%s, starting handleClient", remoteAddr, listenAddr)
		go ns.handleClient(conn)
	}
}

// handleClient handles a client connection
func (ns *NBDServer) handleClient(conn net.Conn) {
	defer conn.Close()

	clientAddr := conn.RemoteAddr().String()
	connStartTime := time.Now()

	ns.mutex.RLock()
	allowedIP := ns.allowedIP
	ns.mutex.RUnlock()
	if allowedIP != "" {
		remoteIP, _, _ := net.SplitHostPort(clientAddr)
		if remoteIP != allowedIP {
			logrus.Warnf("🚫 [NBD] Rejected connection from %s (only %s is allowed)", remoteIP, allowedIP)
			return
		}
	}

	logrus.Infof("📥 [NBD-CLIENT-CONNECT] New NBD client connected: %s", clientAddr)

	// Enable TCP KeepAlive to detect disconnection faster than default 2 hours.
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		tcpConn.SetKeepAlive(true)
		tcpConn.SetKeepAlivePeriod(30 * time.Second)
		logrus.Debugf("📡 [NBD-CLIENT-KEEPALIVE] TCP KeepAlive enabled for %s (period: 30s)", clientAddr)
	}

	// Get list of exports and ReadOnly of the first one in deterministic order (by name)
	// Note: NBD handshake passes single ReadOnly flag - with one export per port this is its flag
	ns.mutex.RLock()
	names := make([]string, 0, len(ns.exports))
	for name := range ns.exports {
		names = append(names, name)
	}
	sort.Strings(names)
	exports := make([]*server.Export, 0, len(names)+1)
	firstExportRO := true
	for _, name := range names {
		export := ns.exports[name]
		exports = append(exports, &server.Export{
			Name:        export.ExportName,
			Description: export.Description,
			Backend:     export.Backend,
		})
		if firstExportRO {
			firstExportRO = export.ReadOnly
		}
	}
	// The gadget (usbridge) uses nbd://host:port/ when nbd_handshake_empty_export is set, and nbd-client connects without -N (empty name).
	// go-nbd looks for candidate.Name == string(exportName); without an export with Name "" handshake gives NBD_REP_ERR_UNKNOWN and reading nbd0 gives I/O error.
	// When there is one export, we append a duplicate with empty name so both (empty and non-empty) work.
	if len(exports) == 1 {
		exports = append(exports, &server.Export{
			Name:        "",
			Description: exports[0].Description,
			Backend:     exports[0].Backend,
		})
	}
	ns.mutex.RUnlock()

	if len(exports) == 0 {
		logrus.Warnf("⚠️ [NBD-CLIENT-NO-EXPORTS] No available exports for client %s, closing connection", clientAddr)
		return
	}

	// Log available exports for the client
	logrus.Infof("📋 [NBD-CLIENT-EXPORTS] Available exports for client %s (total: %d):", clientAddr, len(exports))
	for i, export := range exports {
		logrus.Infof("  %d. %s: %s", i+1, export.Name, export.Description)
	}

	// Add client to the list
	client := &NBDClient{
		Conn:         conn,
		ConnectedAt:  connStartTime,
		LastActivity: connStartTime,
	}

	ns.clientMutex.Lock()
	ns.clients[clientAddr] = client
	ns.clientMutex.Unlock()
	logrus.Infof("✅ [NBD-CLIENT-REGISTERED] Client %s registered, total active clients: %d", clientAddr, len(ns.clients))

	defer func() {
		sessionDuration := time.Since(connStartTime).Round(time.Second)
		ns.clientMutex.Lock()
		delete(ns.clients, clientAddr)
		remainingClients := len(ns.clients)
		ns.clientMutex.Unlock()
		logrus.Infof("👋 [NBD-CLIENT-DISCONNECT] NBD client disconnected: %s, session duration: %v, active clients left: %d", clientAddr, sessionDuration, remainingClients)
	}()

	// Handle connection via go-nbd
	// Pass all exports so the client can choose
	// IMPORTANT: do not set Deadline on conn — go-nbd manages the connection
	options := &server.Options{
		ReadOnly:           firstExportRO,
		MinimumBlockSize:   512,
		PreferredBlockSize: 4096,
		MaximumBlockSize:   65536,
	}

	logrus.Infof("🤝 [NBD-CLIENT-HANDSHAKE] Starting NBD handshake with client %s (options: ReadOnly=%v, BlockSize=%d-%d)",
		clientAddr, options.ReadOnly, options.MinimumBlockSize, options.MaximumBlockSize)

	if err := server.Handle(conn, exports, options); err != nil {
		sessionDuration := time.Since(connStartTime).Round(time.Second)
		logrus.Errorf("❌ [NBD-CLIENT-HANDLE-ERROR] NBD connection error with %s after %v: %v", clientAddr, sessionDuration, err)
	} else {
		sessionDuration := time.Since(connStartTime).Round(time.Second)
		logrus.Infof("✅ [NBD-CLIENT-HANDLE-SUCCESS] NBD session with %s finished normally after %v", clientAddr, sessionDuration)
	}
}

// startHealthMonitoring starts health monitoring for the NBD server
func (ns *NBDServer) startHealthMonitoring() {
	logrus.Infof("📍 [NBD-HEALTH-START-1] Creating ticker for monitoring (interval: 10s)")
	ns.healthTicker = time.NewTicker(10 * time.Second) // Check every 10 seconds

	logrus.Infof("📍 [NBD-HEALTH-START-2] Starting health monitoring goroutine")
	go func() {
		logrus.Infof("✅ [NBD-HEALTH-GOROUTINE] Health monitoring goroutine started")
		for {
			select {
			case <-ns.healthTicker.C:
				ns.checkClientsHealth()
				ns.validateExports()
			case <-ns.stopHealth:
				logrus.Infof("🛑 [NBD-HEALTH-GOROUTINE-STOP] Health monitoring goroutine stopped")
				return
			}
		}
	}()

	logrus.Info("✅ [NBD-HEALTH-START-SUCCESS] NBD health monitoring started")
}

// stopHealthMonitoring stops health monitoring
func (ns *NBDServer) stopHealthMonitoring() {
	if ns.healthTicker != nil {
		ns.healthTicker.Stop()
		close(ns.stopHealth)
		ns.stopHealth = make(chan bool) // Recreate channel for next start
		logrus.Info("🔍 NBD health monitoring stopped")
	}
}

// checkClientsHealth checks the health of connected clients.
// WARNING: DO NOT modify connections (SetWriteDeadline/Write) - this breaks go-nbd.
// Dead clients are cleaned up automatically in handleClient.defer when server.Handle() completes.
func (ns *NBDServer) checkClientsHealth() {
	ns.clientMutex.RLock()
	defer ns.clientMutex.RUnlock()

	for addr, client := range ns.clients {
		idleDuration := time.Since(client.LastActivity)
		if idleDuration > 60*time.Second {
			logrus.Debugf("📊 NBD client %s: inactive for %v (connection managed by go-nbd, ignoring)", addr, idleDuration.Round(time.Second))
		}
	}
}

// validateExports checks the correctness of exports
func (ns *NBDServer) validateExports() {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	for exportName, export := range ns.exports {
		// For Android content:// URI we don't check via os.Stat
		// Instead we verify availability via Backend
		if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") {
			// For Android only check availability via backend
			if export.Backend != nil && export.Backend.file != nil {
				_, err := export.Backend.Size()
				if err != nil {
					logrus.Warnf("⚠️ Export %s is unavailable: %v", exportName, err)
					export.IsActive = false
				}
			}
			continue
		}

		// For normal files check existence
		if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
			logrus.Warnf("⚠️ Export file %s not found: %s", exportName, export.FilePath)
			export.IsActive = false
			continue
		}

		// Check if we can read the file
		if export.Backend != nil && export.Backend.file != nil {
			_, err := export.Backend.Size()
			if err != nil {
				logrus.Warnf("⚠️ Export %s is unavailable: %v", exportName, err)
				export.IsActive = false
			}
		}
	}
}

// ForceRemoveExport forcefully removes an export (idempotent)
func (ns *NBDServer) ForceRemoveExport(exportName string) error {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	// Log stack trace to understand where forced removal is called from
	logrus.Infof("🔍 ForceRemoveExport called for export %s", exportName)
	logrus.Debugf("📍 Call stack trace for ForceRemoveExport:")

	// Get stack trace
	buf := make([]byte, 1024)
	n := runtime.Stack(buf, false)
	logrus.Debugf("📍 %s", string(buf[:n]))

	export, exists := ns.exports[exportName]
	if !exists {
		logrus.Warnf("⚠️ Export %s not found, might be already removed", exportName)
		return nil // Idempotent - no error if already removed
	}

	// Forcefully close file, ignoring errors
	if export.Backend != nil && export.Backend.file != nil {
		if err := export.Backend.file.Close(); err != nil {
			logrus.Warnf("⚠️ Error closing file for export %s: %v", exportName, err)
		}

		// If this is Android content:// URI, clear SAF cache
		if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
			safHelper := platform.GetSAFHelper(ns.app)
			if err := safHelper.CloseFD(export.FilePath); err != nil {
				logrus.Warnf("⚠️ Error clearing SAF cache for %s: %v", export.FilePath, err)
			}
		}
	}
	// Do not remove overlay - the same overlay will be used on next mount

	delete(ns.exports, exportName)
	logrus.Infof("🗑️ Export %s forcefully removed", exportName)
	return nil
}

// CleanupDeadExports cleans up "dead" exports
func (ns *NBDServer) CleanupDeadExports() int {
	ns.mutex.Lock()
	defer ns.mutex.Unlock()

	logrus.Debugf("🧹 CleanupDeadExports called, checking %d exports", len(ns.exports))
	cleaned := 0
	for exportName, export := range ns.exports {
		logrus.Debugf("🔍 Checking export %s for \"deadness\"...", exportName)

		// Check file (for overlay check base FilePath)
		if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
			logrus.Warnf("🧹 Cleaning up dead export %s: file not found", exportName)
			if export.Backend != nil && export.Backend.file != nil {
				export.Backend.file.Close()

				// If this is Android content:// URI, clear SAF cache
				if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
					safHelper := platform.GetSAFHelper(ns.app)
					if err := safHelper.CloseFD(export.FilePath); err != nil {
						logrus.Warnf("⚠️ Error clearing SAF cache for %s: %v", export.FilePath, err)
					}
				}
			}
			// Do not remove overlay
			delete(ns.exports, exportName)
			cleaned++
			continue
		}

		// Check file availability
		if export.Backend != nil && export.Backend.file != nil {
			if _, err := export.Backend.Size(); err != nil {
				logrus.Warnf("🧹 Cleaning up unavailable export %s: %v", exportName, err)
				export.Backend.file.Close()

				// If this is Android content:// URI, clear SAF cache
				if runtime.GOOS == "android" && strings.HasPrefix(export.FilePath, "content://") && ns.app != nil {
					safHelper := platform.GetSAFHelper(ns.app)
					if err := safHelper.CloseFD(export.FilePath); err != nil {
						logrus.Warnf("⚠️ Error clearing SAF cache for %s: %v", export.FilePath, err)
					}
				}
				// Do not remove overlay - the same overlay will be used on next mount

				delete(ns.exports, exportName)
				cleaned++
			} else {
				logrus.Debugf("✅ Export %s is healthy", exportName)
			}
		} else {
			logrus.Debugf("✅ Export %s has no backend, but that's fine", exportName)
		}
	}

	if cleaned > 0 {
		logrus.Infof("🧹 Cleaned up %d dead exports", cleaned)
	}

	return cleaned
}

// IsExportHealthy checks the health of a specific export
func (ns *NBDServer) IsExportHealthy(exportName string) bool {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()

	export, exists := ns.exports[exportName]
	if !exists {
		return false
	}

	// Check file
	if _, err := os.Stat(export.FilePath); os.IsNotExist(err) {
		return false
	}

	// Check backend
	if export.Backend == nil || export.Backend.file == nil {
		return false
	}

	// Check availability
	if _, err := export.Backend.Size(); err != nil {
		return false
	}

	return export.IsActive
}

// GetServerStatus returns detailed NBD server status
func (ns *NBDServer) GetServerStatus() map[string]interface{} {
	ns.mutex.RLock()
	defer ns.mutex.RUnlock()

	ns.clientMutex.RLock()
	clientCount := len(ns.clients)
	ns.clientMutex.RUnlock()

	exportCount := len(ns.exports)
	exportList := make([]map[string]interface{}, 0, exportCount)

	for name, export := range ns.exports {
		exportInfo := map[string]interface{}{
			"name":        export.ExportName,
			"file_path":   export.FilePath,
			"size":        export.Size,
			"read_only":   export.ReadOnly,
			"description": export.Description,
			"is_active":   export.IsActive,
			"is_healthy":  ns.IsExportHealthy(name),
		}
		exportList = append(exportList, exportInfo)
	}

	return map[string]interface{}{
		"is_running":   ns.isRunning,
		"export_count": exportCount,
		"client_count": clientCount,
		"exports":      exportList,
		"server_port":  ns.port,
	}
}

// LogServerStatus logs detailed server status
func (ns *NBDServer) LogServerStatus() {
	status := ns.GetServerStatus()

	logrus.Infof("📊 NBD server status:")
	logrus.Infof("  - Running: %v", status["is_running"])
	logrus.Infof("  - Exports: %d", status["export_count"])
	logrus.Infof("  - Clients: %d", status["client_count"])

	if exports, ok := status["exports"].([]map[string]interface{}); ok {
		for _, export := range exports {
			logrus.Infof("  - Export '%s': %v (size: %d, active: %v, healthy: %v)",
				export["name"], export["description"], export["size"], export["is_active"], export["is_healthy"])
		}
	}
}

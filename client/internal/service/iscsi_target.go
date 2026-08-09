// Package service: export a disk image as an iSCSI target via gostor/gotgt
// (pure-Go, no external daemon — the disk-export data plane). Implements
// BlockExportRunner (block_export.go) so the mount/unmount flow in
// disk_widget_export.go can start/stop it uniformly.
package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/platform"

	"fyne.io/fyne/v2"
	"github.com/gostor/gotgt/pkg/config"
	"github.com/gostor/gotgt/pkg/scsi"
	"github.com/sirupsen/logrus"

	_ "github.com/gostor/gotgt/pkg/port/iscsit"
	_ "github.com/gostor/gotgt/pkg/scsi/backingstore"
)

// iscsiDeviceIDCounter hands out unique gotgt storage device IDs for the
// lifetime of the process — gotgt tracks LUNs in a process-global map
// keyed by this ID (see scsi.InitSCSILUMap), so every export needs one.
var iscsiDeviceIDCounter uint64 = 1000

func nextIscsiDeviceID() uint64 {
	return atomic.AddUint64(&iscsiDeviceIDCounter, 1)
}

// IscsiTargetRunner serves one disk image as a single-LUN iSCSI target.
type IscsiTargetRunner struct {
	filePath  string
	readOnly  bool
	bindHost  string
	allowedIP string
	iqn       string
	app       fyne.App // for Android SAF access; nil on desktop

	mu            sync.RWMutex
	driver        scsi.SCSITargetDriver
	running       bool
	port          int
	readyChan     chan struct{}
	proxyListener net.Listener
	proxyCancel   context.CancelFunc
	safFile       *os.File // set when serving an Android content:// URI
}

// BuildTargetIQN derives a stable, RFC-3720-friendly target IQN from an
// export name (e.g. a disk's file name) plus a short uniqueness suffix.
// Convention: iqn.2026-01.com.usbridge.client:<sanitized-name>-<suffix>.
func BuildTargetIQN(exportName string, uniqueSuffix string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(exportName)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune('-')
		}
	}
	name := b.String()
	if name == "" {
		name = "disk"
	}
	if len(name) > 48 {
		name = name[:48]
	}
	return fmt.Sprintf("iqn.2026-01.com.usbridge.client:%s-%s", name, uniqueSuffix)
}

// NewIscsiTargetRunner creates a runner for filePath, to be exported under
// iqn (a full IQN string, e.g. "iqn.2026-01.com.usbridge:client-<id>").
func NewIscsiTargetRunner(filePath string, readOnly bool, bindHost, iqn string) *IscsiTargetRunner {
	return &IscsiTargetRunner{
		filePath:  filePath,
		readOnly:  readOnly,
		bindHost:  bindHost,
		iqn:       iqn,
		readyChan: make(chan struct{}),
	}
}

// NewIscsiTargetRunnerWithApp is like NewIscsiTargetRunner but also carries
// the fyne.App needed to reach platform.SAFHelper — required when filePath
// is an Android content:// URI (see the SAF branch in Start()).
func NewIscsiTargetRunnerWithApp(filePath string, readOnly bool, bindHost, iqn string, app fyne.App) *IscsiTargetRunner {
	r := NewIscsiTargetRunner(filePath, readOnly, bindHost, iqn)
	r.app = app
	return r
}

// SetAllowedIP restricts iSCSI connections to a single remote IP, mirroring
// NBDServer.SetAllowedIP / QemuNBDRunner.SetAllowedIP. Call before Start().
func (r *IscsiTargetRunner) SetAllowedIP(ip string) {
	r.mu.Lock()
	r.allowedIP = ip
	r.mu.Unlock()
}

// IQN returns the target IQN this runner serves (needed by the caller to
// populate DeviceStartRequest.TargetIQN).
func (r *IscsiTargetRunner) IQN() string {
	return r.iqn
}

// Start builds a single-target, single-LUN gotgt config in memory (no
// config.json on disk) and starts the iSCSI service. gotgt has no built-in
// per-connection IP allow-list, so — exactly like QemuNBDRunner — we bind
// gotgt to a loopback port and front it with a small filtering TCP proxy
// on the real bind address when allowedIP is set.
func (r *IscsiTargetRunner) Start(port int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.running {
		return fmt.Errorf("iSCSI target is already running")
	}

	bindHost := r.bindHost
	if strings.TrimSpace(bindHost) == "" {
		bindHost = "127.0.0.1"
	}

	isAndroidSAF := runtime.GOOS == "android" && strings.HasPrefix(r.filePath, "content://")
	defer func() {
		// If we opened a SAF fd above but Start() is returning without
		// having reached r.running = true, don't leak it.
		if isAndroidSAF && !r.running && r.safFile != nil {
			unregisterSAFBackingFile(r.iqn)
			platform.GetSAFHelper(r.app).CloseFD(r.filePath)
			r.safFile = nil
		}
	}()

	var storagePath string
	if isAndroidSAF {
		// gotgt's stock "file" backing store calls os.OpenFile(path) itself,
		// which can't work for a SAF content:// URI (no real filesystem
		// path). Open it ourselves via platform.SAFHelper (same call the
		// old NBD path used) and hand gotgt the already-open fd through the
		// "androidsaf" backing store (iscsi_backingstore_saf.go), keyed by
		// this export's IQN.
		if r.app == nil {
			return fmt.Errorf("iSCSI SAF export: fyne.App not set (use NewIscsiTargetRunnerWithApp)")
		}
		safHelper := platform.GetSAFHelper(r.app)
		mode := "rw"
		if r.readOnly {
			mode = "r"
		}
		f, err := safHelper.OpenFileDescriptor(r.filePath, mode)
		if err != nil && mode == "rw" {
			// Some SAF providers (e.g. Google Drive) only allow read-only
			// access; fall back rather than fail outright.
			logrus.Warnf("⚠️ [ISCSI] SAF open in rw mode failed (%v), retrying read-only", err)
			f, err = safHelper.OpenFileDescriptor(r.filePath, "r")
			r.readOnly = true
		}
		if err != nil {
			return fmt.Errorf("opening SAF file %s: %w", r.filePath, err)
		}
		r.safFile = f
		registerSAFBackingFile(r.iqn, f)
		storagePath = AndroidSAFBackingStorage + ":" + r.iqn
		logrus.Infof("📍 [ISCSI] Serving Android SAF file %s via androidsaf backing store (key=%s)", r.filePath, r.iqn)
	} else {
		if _, err := os.Stat(r.filePath); os.IsNotExist(err) {
			return fmt.Errorf("file %s does not exist", r.filePath)
		}
		storagePath = "file:" + r.filePath
	}
	// KNOWN LIMITATION: unlike the old NBD path (where qemu-nbd decoded
	// qcow2/vmdk/vdi into a raw MBR/GPT view on the wire), gotgt's flat-file
	// backing store serves a file's bytes as-is with no format awareness.
	// The qcow2-overlay-for-RW trick (overlay.go, still used for the old
	// NBD/qemu-nbd path if it's ever revived) would therefore hand the
	// initiator a qcow2 container as if it were the raw disk — not usable.
	// So for now the iSCSI target always serves the source file directly,
	// with no overlay: correct for genuinely raw-format sources
	// (.iso/.img/.raw) in both RO and RW; vmdk/qcow2/vdi *source* images
	// are not properly supported over iSCSI yet (same root cause — no
	// decode step) and will serve their container bytes as-is, which is
	// not a valid disk to the initiator. Revisit if/when gotgt gains
	// format-aware backing stores.
	if r.readOnly && !isAndroidSAF {
		// KNOWN LIMITATION: gotgt v0.2.2's stock "file" backing store always
		// opens with O_RDWR internally and has no read-only LUN concept —
		// unlike the old NBD/qemu-nbd path which enforced this via file
		// open flags or a -r CLI flag. A malicious/misbehaving initiator
		// could still write. Acceptable for now (trusted LAN initiator, no
		// untrusted third parties); revisit if gotgt gains WRITE PROTECT
		// support. (The androidsaf backing store above does honor
		// read-only: it simply never gets a writable fd in that case.)
		logrus.Warnf("⚠️ [ISCSI] Read-only requested for %s, but gotgt v0.2.2 has no read-only LUN enforcement — the underlying file remains writable by the initiator", r.filePath)
	}

	deviceID := nextIscsiDeviceID()
	cfg := &config.Config{
		Storages: []config.BackendStorage{
			{DeviceID: deviceID, Path: storagePath, Online: true},
		},
		// ISCSIPortals is required even though the actual bind address is
		// determined by driver.Run(listenPort) below — TPGTs references
		// portal ID 0 by index, and gotgt panics (index out of range) on
		// NewTarget if the portals list is empty.
		ISCSIPortals: []config.ISCSIPortalInfo{
			{ID: 0, Portal: fmt.Sprintf("%s:%d", bindHost, port)},
		},
		ISCSITargets: map[string]config.ISCSITarget{
			r.iqn: {
				TPGTs: map[string][]uint64{"1": {0}},
				LUNs:  map[string]uint64{"0": deviceID},
			},
		},
	}

	if err := scsi.InitSCSILUMap(cfg); err != nil {
		return fmt.Errorf("iSCSI LUN map init: %w", err)
	}

	target := scsi.NewSCSITargetService()
	driver, err := scsi.NewTargetDriver("iscsi", target)
	if err != nil {
		return fmt.Errorf("iSCSI target driver: %w", err)
	}
	if err := driver.NewTarget(r.iqn, cfg); err != nil {
		return fmt.Errorf("iSCSI NewTarget: %w", err)
	}
	r.driver = driver

	listenPort := port
	if r.allowedIP != "" {
		// Serve on loopback; the proxy owns the real bind address/port.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("allocating loopback port for iSCSI target: %w", err)
		}
		listenPort = ln.Addr().(*net.TCPAddr).Port
		ln.Close()
	}

	go func() {
		if runErr := driver.Run(listenPort); runErr != nil {
			logrus.Warnf("⚠️ [ISCSI] target driver stopped: %v", runErr)
		}
	}()

	if r.allowedIP != "" {
		if err := r.startProxy(bindHost, port, listenPort); err != nil {
			driver.Close()
			return err
		}
		logrus.Infof("✅ [ISCSI] target %q started with IP-filter proxy: %s:%d → 127.0.0.1:%d (allowed: %s)",
			r.iqn, bindHost, port, listenPort, r.allowedIP)
	} else {
		logrus.Infof("✅ [ISCSI] target %q started on %s:%d (no IP filter)", r.iqn, bindHost, port)
	}

	r.port = port
	r.running = true

	go func() {
		time.Sleep(500 * time.Millisecond)
		r.mu.Lock()
		ch := r.readyChan
		r.mu.Unlock()
		select {
		case <-ch:
		default:
			close(ch)
		}
	}()

	return nil
}

// startProxy binds bindHost:port and forwards only allowedIP connections to
// 127.0.0.1:loopPort, where the real gotgt target is listening.
func (r *IscsiTargetRunner) startProxy(bindHost string, port, loopPort int) error {
	listenAddr := fmt.Sprintf("%s:%d", bindHost, port)
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("proxy listen %s: %w", listenAddr, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	r.proxyCancel = cancel
	r.proxyListener = ln
	targetAddr := fmt.Sprintf("127.0.0.1:%d", loopPort)

	go func() {
		defer ln.Close()
		for {
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
				default:
					logrus.Warnf("⚠️ [ISCSI-PROXY] Accept error: %v", err)
				}
				return
			}
			go r.proxyConn(ctx, conn, targetAddr)
		}
	}()
	return nil
}

func (r *IscsiTargetRunner) proxyConn(ctx context.Context, conn net.Conn, targetAddr string) {
	defer conn.Close()

	remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	r.mu.RLock()
	allowed := r.allowedIP
	r.mu.RUnlock()
	if remoteIP != allowed {
		logrus.Warnf("🚫 [ISCSI] Rejected connection from %s (only %s is allowed)", remoteIP, allowed)
		return
	}

	target, err := net.Dial("tcp", targetAddr)
	if err != nil {
		logrus.Errorf("❌ [ISCSI-PROXY] connect to gotgt: %v", err)
		return
	}
	defer target.Close()

	done := make(chan struct{}, 2)
	go func() { io.Copy(target, conn); done <- struct{}{} }()
	go func() { io.Copy(conn, target); done <- struct{}{} }()

	select {
	case <-done:
	case <-ctx.Done():
	}
}

// Stop stops the iSCSI target driver and (if running) the filter proxy.
func (r *IscsiTargetRunner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.running {
		return fmt.Errorf("iSCSI target is not running")
	}

	if r.proxyCancel != nil {
		r.proxyCancel()
		r.proxyCancel = nil
	}
	if r.proxyListener != nil {
		r.proxyListener.Close()
		r.proxyListener = nil
	}
	if r.driver != nil {
		if err := r.driver.Close(); err != nil {
			logrus.Warnf("⚠️ [ISCSI] error closing target driver: %v", err)
		}
		r.driver = nil
	}
	if r.safFile != nil {
		unregisterSAFBackingFile(r.iqn)
		if r.app != nil {
			if err := platform.GetSAFHelper(r.app).CloseFD(r.filePath); err != nil {
				logrus.Warnf("⚠️ [ISCSI] error closing SAF fd for %s: %v", r.filePath, err)
			}
		}
		r.safFile = nil
	}

	r.running = false
	r.readyChan = make(chan struct{})
	logrus.Infof("🛑 [ISCSI] target %q stopped", r.iqn)
	return nil
}

// IsRunning reports whether the target is currently serving.
func (r *IscsiTargetRunner) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.running
}

// GetServerStatus mirrors NBDServer.GetServerStatus's shape for UI reuse.
func (r *IscsiTargetRunner) GetServerStatus() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return map[string]interface{}{
		"is_running":  r.running,
		"server_port": r.port,
		"iqn":         r.iqn,
		"transport":   "iscsi",
	}
}

// WaitReady returns a channel that closes once the target is accepting connections.
func (r *IscsiTargetRunner) WaitReady() <-chan struct{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.readyChan
}

// SignalReady is a no-op for iSCSI: readiness is signaled by Start() itself
// (there is no separate "add export" step in this transport).
func (r *IscsiTargetRunner) SignalReady() {}

// ExportNameForConnection returns the target IQN — what the initiator uses
// to address this target during login.
func (r *IscsiTargetRunner) ExportNameForConnection() string {
	return r.iqn
}

// ExportNameForAPI returns the target IQN, reported to the agent via
// DeviceStartRequest.TargetIQN/ExportName.
func (r *IscsiTargetRunner) ExportNameForAPI() string {
	return r.iqn
}

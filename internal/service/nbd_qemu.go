// Package service: export virtual disk via qemu-nbd (VMDK/QCOW2/VDI - client sees MBR/GPT, not container).

package service

import (
	"context"
	"fmt"
	"io"
	"net"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// NBDRunner - common interface for our NBD server (go-nbd) and qemu-nbd process.
// Allows replacing export of "file as is" with export of decrypted disk.
type NBDRunner interface {
	Start(port int) error
	Stop() error
	IsRunning() bool
	GetServerStatus() map[string]interface{}
	WaitReady() <-chan struct{}
	SignalReady()
	GetClients() []*NBDClient
	// NBDExportNameForConnection - export name for NBD handshake. qemu-nbd has one export with an empty name.
	NBDExportNameForConnection() string
	// NBDExportNameForAPI - name for API (export_name in request). Non-empty, unique (for qemu-nbd - nbd_<port>, for go-nbd - export name).
	NBDExportNameForAPI() string
	// NBDHandshakeEmptyExport - true = Bridge must use empty export name in NBD handshake (qemu-nbd provides one disk without name).
	NBDHandshakeEmptyExport() bool
}

// QemuNBDRunner starts qemu-nbd for image (vmdk/qcow2/vdi/qcow) so that via NBD
// a virtual disk is served (first bytes - MBR/GPT), not a container.
type QemuNBDRunner struct {
	filePath    string
	format      string
	readOnly    bool
	bindHost    string
	allowedIP   string        // if set, only this remote IP is forwarded through the proxy
	port        int
	cmd         *exec.Cmd
	overlayPath string
	readyChan   chan struct{}
	running     bool
	mu          sync.RWMutex
	proxyCancel context.CancelFunc // cancels the IP-filter proxy goroutine
}

// SetAllowedIP restricts qemu-nbd connections to a single remote IP via a TCP proxy.
// Must be called before Start().
func (q *QemuNBDRunner) SetAllowedIP(ip string) {
	q.mu.Lock()
	q.allowedIP = ip
	q.mu.Unlock()
	if ip != "" {
		logrus.Infof("🔒 [NBD-QEMU] Connection filter set: only %s is allowed", ip)
	}
}

// qemuNbdFormat returns format for qemu-nbd -f by extension.
func qemuNbdFormat(ext string) string {
	switch strings.ToLower(ext) {
	case ".qcow2", ".qcow":
		return "qcow2"
	case ".vmdk":
		return "vmdk"
	case ".vdi":
		return "vdi"
	case ".raw", ".img", ".vmi":
		return "raw"
	default:
		return "raw"
	}
}

// IsQemuNbdFormatForPath returns true if the image at the path needs to be exported via qemu-nbd
// (virtual disk - MBR/GPT), not as a raw file (VMDK/QCOW2 container).
func IsQemuNbdFormatForPath(path string) bool {
	ext := path
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = strings.ToLower(ext[i:])
	} else {
		return false
	}
	switch ext {
	case ".vmdk", ".vdi", ".qcow2", ".qcow":
		return true
	default:
		return false
	}
}

// NewQemuNBDRunner creates a runner for qemu-nbd. filePath - path to image (on desktop).
// For RW, an overlay is created if necessary; then the overlay is exported.
func NewQemuNBDRunner(filePath string, readOnly bool, bindHost string) *QemuNBDRunner {
	ext := strings.ToLower(filePath)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}
	format := qemuNbdFormat(ext)
	return &QemuNBDRunner{
		filePath:  filePath,
		format:    format,
		readOnly:  readOnly,
		bindHost:  strings.TrimSpace(bindHost),
		readyChan: make(chan struct{}),
	}
}

// Start launches qemu-nbd on a port. The virtual disk (internal contents of the image) is exported.
func (q *QemuNBDRunner) Start(port int) error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.cmd != nil {
		return fmt.Errorf("qemu-nbd already started")
	}

	path := q.filePath
	if q.overlayPath != "" {
		path = q.overlayPath
	}

	qemuNbd := lookQemuTool("qemu-nbd")
	if qemuNbd == "" {
		return fmt.Errorf("qemu-nbd not found. Install QEMU (e.g.: apt install qemu-utils, brew install qemu)")
	}

	bindHost := q.getBindHost()
	if q.allowedIP != "" && bindHost != "127.0.0.1" {
		// IP-filter mode: qemu-nbd on loopback, filtering TCP proxy on the real bind address.
		return q.startWithProxy(path, port, qemuNbd, bindHost)
	}

	args := []string{
		"-f", q.format,
		"-p", fmt.Sprintf("%d", port),
		"-b", bindHost,
	}
	if q.readOnly {
		args = append(args, "-r")
	}
	args = append(args, path)

	q.cmd = exec.Command(qemuNbd, args...)
	maybeHideWindow(q.cmd)
	q.cmd.Stdout = nil
	q.cmd.Stderr = nil
	if err := q.cmd.Start(); err != nil {
		q.cmd = nil
		return fmt.Errorf("starting qemu-nbd: %w", err)
	}

	q.port = port
	q.running = true
	logrus.Infof("✅ [NBD-QEMU] qemu-nbd started: bind=%s port=%d, format=%s, readOnly=%v, path=%s", bindHost, port, q.format, q.readOnly, path)

	// Readiness: give qemu-nbd time to open the port
	go func() {
		time.Sleep(500 * time.Millisecond)
		q.mu.Lock()
		ch := q.readyChan
		q.mu.Unlock()
		select {
		case <-ch:
		default:
			close(ch)
		}
	}()

	return nil
}

// startWithProxy starts qemu-nbd on a loopback port and fronts it with a
// TCP proxy on the real bind address that only forwards connections from allowedIP.
func (q *QemuNBDRunner) startWithProxy(path string, port int, qemuNbd, bindHost string) error {
	listenAddr := fmt.Sprintf("%s:%d", bindHost, port)

	// Bind the proxy listener FIRST so we fail fast if the port is busy,
	// without leaving a zombie qemu-nbd on loopback.
	proxyLn, err := net.Listen("tcp", listenAddr)
	if err != nil {
		// Port is held by a stale qemu-nbd from a previous run — kill it and retry.
		logrus.Warnf("⚠️ [NBD-QEMU] Port %s busy, killing stale qemu-nbd and retrying...", listenAddr)
		exec.Command("pkill", "-f", fmt.Sprintf("qemu-nbd.*%d", port)).Run()
		exec.Command("pkill", "-f", fmt.Sprintf("qemu-nbd.*-p %d", port)).Run()
		time.Sleep(400 * time.Millisecond)
		proxyLn, err = net.Listen("tcp", listenAddr)
		if err != nil {
			return fmt.Errorf("proxy listen %s: %w", listenAddr, err)
		}
	}

	// Pick a free loopback port for qemu-nbd.
	loopLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		proxyLn.Close()
		return fmt.Errorf("allocating loopback port: %w", err)
	}
	loopPort := loopLn.Addr().(*net.TCPAddr).Port
	loopLn.Close()

	args := []string{"-f", q.format, "-p", fmt.Sprintf("%d", loopPort), "-b", "127.0.0.1"}
	if q.readOnly {
		args = append(args, "-r")
	}
	args = append(args, path)

	q.cmd = exec.Command(qemuNbd, args...)
	maybeHideWindow(q.cmd)
	if err := q.cmd.Start(); err != nil {
		proxyLn.Close()
		q.cmd = nil
		return fmt.Errorf("starting qemu-nbd: %w", err)
	}

	// Give qemu-nbd time to open its loopback port.
	time.Sleep(400 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	q.proxyCancel = cancel
	targetAddr := fmt.Sprintf("127.0.0.1:%d", loopPort)

	go func() {
		defer proxyLn.Close()
		for {
			conn, err := proxyLn.Accept()
			if err != nil {
				select {
				case <-ctx.Done():
				default:
					logrus.Warnf("⚠️ [NBD-QEMU-PROXY] Accept error: %v", err)
				}
				return
			}
			go q.proxyConn(ctx, conn, targetAddr)
		}
	}()

	q.port = port
	q.running = true
	logrus.Infof("✅ [NBD-QEMU] started with IP-filter proxy: %s → 127.0.0.1:%d (allowed: %s), format=%s ro=%v",
		listenAddr, loopPort, q.allowedIP, q.format, q.readOnly)

	go func() {
		time.Sleep(500 * time.Millisecond)
		q.mu.Lock()
		ch := q.readyChan
		q.mu.Unlock()
		select {
		case <-ch:
		default:
			close(ch)
		}
	}()

	return nil
}

// proxyConn forwards a single connection to targetAddr after IP validation.
func (q *QemuNBDRunner) proxyConn(ctx context.Context, conn net.Conn, targetAddr string) {
	defer conn.Close()

	remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())
	if remoteIP != q.allowedIP {
		logrus.Warnf("🚫 [NBD-QEMU] Rejected connection from %s (only %s is allowed)", remoteIP, q.allowedIP)
		return
	}
	logrus.Infof("📥 [NBD-QEMU] Forwarding connection from %s → %s", remoteIP, targetAddr)

	target, err := net.Dial("tcp", targetAddr)
	if err != nil {
		logrus.Errorf("❌ [NBD-QEMU-PROXY] connect to qemu-nbd: %v", err)
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

func (q *QemuNBDRunner) getBindHost() string {
	if strings.TrimSpace(q.bindHost) == "" {
		return "127.0.0.1"
	}
	return strings.TrimSpace(q.bindHost)
}

// Stop stops qemu-nbd (and the IP-filter proxy if running).
func (q *QemuNBDRunner) Stop() error {
	q.mu.Lock()
	defer q.mu.Unlock()

	if q.proxyCancel != nil {
		q.proxyCancel()
		q.proxyCancel = nil
	}

	if q.cmd == nil || q.cmd.Process == nil {
		return nil
	}

	if err := q.cmd.Process.Kill(); err != nil {
		logrus.Warnf("⚠️ Error terminating qemu-nbd: %v", err)
	}
	_ = q.cmd.Wait()
	q.cmd = nil
	q.running = false

	if q.overlayPath != "" {
		// Do not delete overlay - the same overlay will be used on the next mount
		q.overlayPath = ""
	}

	return nil
}

// IsRunning returns true if the qemu-nbd process is still running.
func (q *QemuNBDRunner) IsRunning() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.running
}

// GetServerStatus returns status in the same format as NBDServer (server_port, etc.).
func (q *QemuNBDRunner) GetServerStatus() map[string]interface{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return map[string]interface{}{
		"is_running":   q.running,
		"export_count": 1,
		"client_count": 0,
		"server_port":  q.port,
		"exports": []map[string]interface{}{
			{
				"name":        "",
				"file_path":   q.filePath,
				"description": "qemu-nbd (virtual disk)",
			},
		},
	}
}

// WaitReady returns a channel that is closed when qemu-nbd is ready to accept connections.
func (q *QemuNBDRunner) WaitReady() <-chan struct{} {
	q.mu.RLock()
	defer q.mu.RUnlock()

	return q.readyChan
}

// SignalReady for qemu-nbd does not change state (readiness is set in Start).
func (q *QemuNBDRunner) SignalReady() {
	q.mu.Lock()
	defer q.mu.Unlock()

	select {
	case <-q.readyChan:
	default:
		close(q.readyChan)
	}
}

// GetClients clients are not tracked for qemu-nbd.
func (q *QemuNBDRunner) GetClients() []*NBDClient {
	return nil
}

// NBDExportNameForConnection returns an empty name - qemu-nbd exports one disk without a name.
func (q *QemuNBDRunner) NBDExportNameForConnection() string {
	return ""
}

// NBDExportNameForAPI - unique non-empty name for API (display, identification). By port: nbd_10809 - without confusion with multiple disks.
func (q *QemuNBDRunner) NBDExportNameForAPI() string {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if q.port == 0 {
		return "nbd_0"
	}
	return "nbd_" + strconv.Itoa(q.port)
}

// NBDHandshakeEmptyExport - qemu-nbd has one export with an empty name.
func (q *QemuNBDRunner) NBDHandshakeEmptyExport() bool {
	return true
}

// EnsureQemuNbdForExport creates an overlay if necessary and configures the runner for export via qemu-nbd.
// Call before Start. For RW and overlay-compatible format, an overlay is created, path is substituted.
func (q *QemuNBDRunner) EnsureQemuNbdForExport() error {
	if runtime.GOOS == "android" {
		return fmt.Errorf("qemu-nbd is not supported on Android")
	}

	ext := strings.ToLower(q.filePath)
	if i := strings.LastIndex(ext, "."); i >= 0 {
		ext = ext[i:]
	} else {
		ext = ""
	}

	if !q.readOnly && IsOverlayCapableExtension(ext) {
		overlayPath, _, err := createOverlay(q.filePath)
		if err != nil {
			logrus.Warnf("⚠️ [NBD-QEMU] overlay not created, export read-only: %v", err)
			q.readOnly = true
			return nil
		}
		q.overlayPath = overlayPath
		q.format = "qcow2"
		logrus.Infof("✅ [NBD-QEMU] Export RW via overlay: %s", overlayPath)
	}

	return nil
}

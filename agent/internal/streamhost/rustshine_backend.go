//go:build rustshine

// rustshineBackend drives a private "rust-shine" gamestream-server instance
// (bin/gamestream-server in the private itsme228/rust-shine repo) as this
// build's streamhost.Backend. Only buildable with -tags rustshine, and only
// useful if that binary is actually bundled next to the agent (see
// agent/scripts/fetch_rustshine.sh) — this file contains no secrets or
// bundled binary of its own, just an HTTP/CLI client for its admin API.
//
// Protocol facts below (routes, JSON field names, CLI flags, config keys,
// port offsets) were confirmed directly against crates/gamestream-proto and
// bin/gamestream-server's clap CLI in the private repo at the time this was
// written. If that server's API changes, this file needs updating to match.
package streamhost

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const rustshineAdminUser = "usbridge"

type rustshineBackend struct {
	mu          sync.Mutex
	exeDir      string
	stateDir    string
	launchPath  string
	capExecPath string // set via SetCapExecPath once CAP_SYS_ADMIN is granted; see Start
	logPath     string
	cmd         *exec.Cmd
	watchdog    *exec.Cmd // macOS only, see rustshine_process_other.go

	activeAdminPassword string
	adminPort           int // set by Start; CurrentVideoCodec needs it despite taking no args itself

	supportedCodecsCache struct {
		mu        sync.Mutex
		codecs    []string
		fetchedAt time.Time
	}
}

var _ Backend = (*rustshineBackend)(nil)

// NewRustshine constructs the rust-shine-backed Backend implementation.
// exeDir/stateDir/logPath have the same meaning as NewSunshine's.
func NewRustshine(exeDir, stateDir, logPath string) Backend {
	return &rustshineBackend{
		exeDir:   exeDir,
		stateDir: stateDir,
		logPath:  logPath,
	}
}

// DisplayName identifies this backend for display purposes only (GUI
// status, /api/status, logs) — see streamhost.Identity.
func (b *rustshineBackend) DisplayName() string { return "RustShine (Proprietary)" }

// binaryName is bin/gamestream-server's build output name, per its
// Cargo.toml package name — "gamestream-server(.exe)", not "rust-shine".
func binaryName() string {
	if runtime.GOOS == "windows" {
		return "gamestream-server.exe"
	}
	return "gamestream-server"
}

// BinaryPath resolves the bundled gamestream-server binary: exeDir/rustshine/
// next to the agent (mirrors Sunshine's exeDir/sunshine/ layout — see
// agent/scripts/fetch_rustshine.sh for how it gets staged there), falling
// back to PATH for local dev where it's just been cargo-built and symlinked.
func (b *rustshineBackend) BinaryPath() string {
	if b.launchPath != "" {
		return b.launchPath
	}
	local := filepath.Join(b.exeDir, "rustshine", binaryName())
	if _, err := os.Stat(local); err == nil {
		return local
	}
	if path, err := exec.LookPath(binaryName()); err == nil {
		return path
	}
	return ""
}

// capExecPathFor returns the path to the bundled sunshine-capexec launcher
// (cmd/sunshine_capexec — a generic "raise CAP_SYS_ADMIN into ambient caps,
// then exec <target>" wrapper, not Sunshine-specific despite the name), or
// "" if not bundled (non-Linux, or a dev build without the AppImage/staged
// layout). gamestream-server's KMS/DRM capture (crates/capture-kms in the
// private rust-shine repo) needs CAP_SYS_ADMIN exactly like Sunshine's does,
// and for the identical reason a file capability can't go directly on
// gamestream-server itself: it resolves bundled shared libs (e.g.
// libvulkan.so.1) via RPATH=$ORIGIN/../lib, and a file capability would put
// it into secure-execution mode, breaking that resolution. See
// internal/permissions.RequestKMSCapture.
func (b *rustshineBackend) capExecPathFor() string {
	if runtime.GOOS != "linux" {
		return ""
	}
	p := filepath.Join(b.exeDir, "sunshine-capexec")
	if info, err := os.Stat(p); err == nil && !info.IsDir() {
		return p
	}
	return ""
}

// runtimeCapExecPath returns the path sunshine-capexec should actually be
// setcap'd from. Inside an AppImage the bundled copy lives on the read-only
// squashfs mount, so `pkexec setcap` on it always fails silently (the
// pkexec prompt succeeds, but the capability is never actually written) —
// stage a writable copy into stateDir first, same fix as sunshineBackend's
// runtimeCapExecPath. Unlike Sunshine, gamestream-server itself does NOT
// need staging: only the file setcap actually writes to (capexec) has to be
// writable — the target binary capexec execs stays wherever it already is,
// its own RPATH resolution is unaffected by where capexec sits.
func (b *rustshineBackend) runtimeCapExecPath() string {
	capexecSrc := b.capExecPathFor()
	if runtime.GOOS != "linux" || capexecSrc == "" || b.stateDir == "" {
		return capexecSrc
	}
	if os.Getenv("APPIMAGE") == "" {
		return capexecSrc
	}
	staged, err := stageCapExecBinary(capexecSrc, filepath.Join(b.stateDir, "rustshine-capexec-runtime"))
	if err != nil {
		log.Printf("[rustshine] failed to stage writable copy for KMS setcap: %v", err)
		return capexecSrc
	}
	return staged
}

// CapExecPath returns the (staged-if-needed) path to the bundled
// sunshine-capexec launcher, or "" if not present/granted yet.
func (b *rustshineBackend) CapExecPath() string { return b.runtimeCapExecPath() }

// SetCapExecPath sets the sunshine-capexec launcher path (see
// runtimeCapExecPath) that Start uses to launch gamestream-server with
// CAP_SYS_ADMIN via ambient capabilities. Only ever set once the capability
// has actually been granted on that path (internal/app), mirroring
// sunshineBackend's SetCapExecPath.
func (b *rustshineBackend) SetCapExecPath(path string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.capExecPath = path
}

func (b *rustshineBackend) Running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.cmd != nil && b.cmd.Process != nil
}

func (b *rustshineBackend) Pid() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil || b.cmd.Process == nil {
		return 0
	}
	return b.cmd.Process.Pid
}

// generatePassword creates a cryptographically-random 20-character hex
// password, same scheme as sunshineBackend's.
func generateRustshinePassword() string {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		log.Panicf("[rustshine] crypto/rand unavailable: %v", err)
	}
	return hex.EncodeToString(buf)
}

// credentialsPath is the file gamestream-server itself owns via
// --credentials-path (its own AdminCredentials format — not necessarily
// plaintext, and not something this launcher parses).
func (b *rustshineBackend) credentialsPath() string {
	cp := b.ConfigPath()
	if cp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cp), "rustshine_server_credentials")
}

// ourPassFile is a separate sidecar file THIS launcher writes the plaintext
// password to right after a successful --creds bootstrap, so AdminPass can
// recover it in a later process (or after Start's "already running,
// nothing to bootstrap" fast path) without needing to parse
// gamestream-server's own credentials-path file format. Same pattern as
// sunshineBackend's adminPassFile.
func (b *rustshineBackend) ourPassFile() string {
	cp := b.ConfigPath()
	if cp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cp), "usbridge_admin_pass")
}

// AdminUser returns the fixed admin-API username this launcher always
// bootstraps via --creds. gamestream-server itself doesn't hardcode a
// username (unlike Sunshine) — any value works, this is just ours.
func (b *rustshineBackend) AdminUser() string { return rustshineAdminUser }

// AdminPass returns the current session admin password, falling back to
// the persisted file from a previous session (same pattern as
// sunshineBackend.AdminPass).
func (b *rustshineBackend) AdminPass() string {
	b.mu.Lock()
	p := b.activeAdminPassword
	b.mu.Unlock()
	if p != "" {
		return p
	}
	if pf := b.ourPassFile(); pf != "" {
		if data, err := os.ReadFile(pf); err == nil {
			if pass := strings.TrimSpace(string(data)); pass != "" {
				return pass
			}
		}
	}
	return ""
}

// Start launches gamestream-server if it isn't already running (by this
// backend, or reachable on adminPort). No-op if the binary can't be found.
func (b *rustshineBackend) Start(adminPort int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adminPort = adminPort
	if b.cmd != nil && b.cmd.Process != nil {
		return nil
	}
	launchPath := b.BinaryPath()
	if launchPath == "" {
		return nil
	}
	if _, err := os.Stat(launchPath); err != nil {
		log.Printf("[rustshine] launch path not found, skipping auto-start: %s", launchPath)
		return nil
	}
	if adminPort > 0 && portReachable(adminPort, 300*time.Millisecond) {
		log.Printf("[rustshine] admin port %d already reachable, assuming gamestream-server is already running", adminPort)
		if pf := b.ourPassFile(); pf != "" {
			if data, err := os.ReadFile(pf); err == nil {
				if pass := strings.TrimSpace(string(data)); pass != "" {
					b.activeAdminPassword = pass
				}
			}
		}
		return nil
	}

	// Backfill adapter_name if capture=kms was persisted without one (e.g.
	// a sunshine_capture_mode:"kms" preference inherited from a previous
	// Sunshine session via app.syncSunshineCaptureMode, written straight
	// into this backend's own conf without going through SetCaptureMode's
	// auto-fill). Belt-and-suspenders: SetCaptureMode fills this in on the
	// path that sets "capture" itself, but a config file that already had
	// "capture = kms" on disk before this backend ever ran wouldn't have
	// gone through that path at all.
	if runtime.GOOS == "linux" && b.CaptureMode() == "kms" && b.ConfigKey("adapter_name") == "" {
		if card := b.firstKmsCardPath(); card != "" {
			if err := b.SetConfigKey("adapter_name", card); err != nil {
				log.Printf("[rustshine] failed to backfill adapter_name: %v", err)
			}
		}
	}

	basePort := adminPort - 1 // gamestream-server's --http-port is the NvHTTP base port; admin listens on base+1.
	credsPath := b.credentialsPath()

	// Bootstrap a fresh random admin password via --creds (one-shot mode:
	// writes credentials-path and exits) before starting the real server,
	// same reasoning as Sunshine's --creds bootstrap. The plaintext is
	// persisted to our own sidecar file (ourPassFile), not read back from
	// gamestream-server's own credentials-path file — that file's on-disk
	// format is internal to it and not something this launcher parses.
	if credsPath != "" {
		newPass := generateRustshinePassword()
		credsCmd := exec.Command(launchPath, "--credentials-path", credsPath, "--creds", rustshineAdminUser, newPass)
		configureRustshineProcess(credsCmd)
		if out, err := credsCmd.CombinedOutput(); err != nil {
			log.Printf("[rustshine] --creds failed: %v: %s", err, out)
		} else {
			b.activeAdminPassword = newPass
			if pf := b.ourPassFile(); pf != "" {
				_ = os.WriteFile(pf, []byte(newPass), 0600)
			}
			log.Printf("[rustshine] admin password set (user=%s)", rustshineAdminUser)
		}
	}

	args := []string{}
	if cfgPath := b.ConfigPath(); cfgPath != "" {
		args = append(args, cfgPath)
	}
	args = append(args, "--http-port", strconv.Itoa(basePort))
	if credsPath != "" {
		args = append(args, "--credentials-path", credsPath)
	}

	// If a capability-granted sunshine-capexec launcher is set (Linux KMS
	// capture only — see SetCapExecPath), launch gamestream-server through
	// it so it inherits CAP_SYS_ADMIN via ambient capabilities instead of
	// carrying a file capability itself, which would break its RPATH-based
	// library resolution. Mirrors sunshineBackend.Start()'s identical branch.
	var cmd *exec.Cmd
	if b.capExecPath != "" {
		cmd = exec.Command(b.capExecPath, append([]string{launchPath}, args...)...)
	} else {
		cmd = exec.Command(launchPath, args...)
	}
	configureRustshineProcess(cmd)
	if dir := filepath.Dir(launchPath); dir != "" && dir != "." {
		cmd.Dir = dir
	}
	if b.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(b.logPath), 0o755); err != nil {
			log.Printf("[rustshine] failed to create log dir for %s: %v", b.logPath, err)
		} else if f, err := os.OpenFile(b.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			log.Printf("[rustshine] failed to open log file %s: %v", b.logPath, err)
		} else {
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	b.launchPath = launchPath
	b.cmd = cmd
	log.Printf("[rustshine] started pid=%d launch=%s", cmd.Process.Pid, launchPath)
	rustshineAfterStart(b, cmd)
	go b.watchProcessExit(cmd)

	return nil
}

func (b *rustshineBackend) watchProcessExit(cmd *exec.Cmd) {
	err := cmd.Wait()
	log.Printf("[rustshine] process exited: %v", err)
	b.mu.Lock()
	if b.cmd == cmd {
		b.cmd = nil
	}
	b.mu.Unlock()
}

// Stop terminates a gamestream-server instance started by this backend.
func (b *rustshineBackend) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var err error
	if b.cmd != nil && b.cmd.Process != nil {
		log.Printf("[rustshine] stopping pid=%d", b.cmd.Process.Pid)
		err = b.cmd.Process.Kill()
		b.cmd = nil
	}
	if b.watchdog != nil && b.watchdog.Process != nil {
		_ = b.watchdog.Process.Kill()
		b.watchdog = nil
	}
	return err
}

// WaitReady polls adminPort until the admin HTTPS listener accepts a
// connection, or the deadline passes. Shares portReachable/adminHost with
// sunshineBackend (same package, same semantics: 127.0.0.1 only).
func (b *rustshineBackend) WaitReady(adminPort int, deadline time.Duration) bool {
	if adminPort <= 0 {
		return true
	}
	start := time.Now()
	for time.Since(start) < deadline {
		if portReachable(adminPort, 250*time.Millisecond) {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// Ports returns gamestream-server's fixed TCP ports (HTTPS, HTTP, admin
// HTTPS, RTSP) and UDP ports (video, control/ENet, audio) relative to its
// --http-port base — confirmed identical TCP offsets to Sunshine's, but
// only 3 UDP ports (no separate mic port).
func (b *rustshineBackend) Ports(basePort int) (tcp []int, udp []int) {
	tcp = []int{basePort - 5, basePort, basePort + 1, basePort + 21}
	udp = []int{basePort + 9, basePort + 10, basePort + 11}
	return tcp, udp
}

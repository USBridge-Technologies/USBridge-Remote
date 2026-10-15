// punktfunkBackend locates the Punktfunk host binary (punktfunk-host) and
// manages its GameStream-compat plane only (Moonlight-compatible: nvhttp
// pairing, RTSP, ENet) -- the native punktfunk/1 protocol plane is out of
// scope for this backend until the client gains support for it. See
// agent/docs/PUNKTFUNK_BACKEND_TODO.md for the full scoping and what was
// confirmed directly from punktfunk's api/openapi.json and source.
//
// Unlike Sunshine, Punktfunk's admin API is bearer-token auth, not
// username+password: the host either mints its own token or adopts one
// pinned via the PUNKTFUNK_MGMT_TOKEN environment variable (see punktfunk's
// own mgmt_token.rs doc comment: "env (an operator override, persisted) ->
// file -> generate"). This backend always pins its own, generated fresh in
// Start() and handed to the child process's environment, so it never has to
// read punktfunk's mgmt-token file back -- it already holds the value.
// PairingAPI's HTTP-Basic-shaped AdminUser()/AdminPass() still has to be
// satisfied to implement Backend: AdminUser() is unused ("") and AdminPass()
// carries the bearer token instead of a password. Every punktfunk_*.go
// admin-API call sends "Authorization: Bearer <token>", never SetBasicAuth.
package streamhost

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// punktfunkAdminTokenEnv is the environment variable punktfunk-host reads to
// adopt an operator-pinned bearer token instead of generating/persisting its
// own.
const punktfunkAdminTokenEnv = "PUNKTFUNK_MGMT_TOKEN"

// punktfunkConfigDirEnv pins punktfunk-host's config directory (token files,
// settings, TLS identity, pairing store) to this agent's own stateDir,
// exactly like sunshineDataDir avoids colliding with an independently
// installed standalone Punktfunk on the same machine -- see
// sunshineBackend's doc comment for the original bug this pattern fixes
// (confirmed env var from punktfunk's management-api.md: "<config> is
// ~/.config/punktfunk ... (PUNKTFUNK_CONFIG_DIR overrides it)").
const punktfunkConfigDirEnv = "PUNKTFUNK_CONFIG_DIR"

// punktfunkSessionLaunch starts punktfunk-host in the active console session when the
// agent is the Session 0 service; set on Windows (punktfunk_process_windows.go).
var punktfunkSessionLaunch func(exe string, args []string, workDir string, out *os.File, extraEnv map[string]string) (punktfunkProcess, error)

type punktfunkProcess interface {
	Pid() int
	Kill() error
	Wait() error
}

type punktfunkExecCmdProcess struct{ cmd *exec.Cmd }

func (p punktfunkExecCmdProcess) Pid() int    { return p.cmd.Process.Pid }
func (p punktfunkExecCmdProcess) Kill() error { return p.cmd.Process.Kill() }
func (p punktfunkExecCmdProcess) Wait() error { return p.cmd.Wait() }

// trackedPunktfunkProc mirrors trackedSunshineProc (sunshine_backend.go):
// lets Start()/Stop() tell "the kernel finished reaping this process" apart
// from "Kill() was sent but it's still stuck" without blocking on Wait().
type trackedPunktfunkProc struct {
	punktfunkProcess
	done chan struct{}
	once sync.Once
}

func newTrackedPunktfunkProc(p punktfunkProcess) *trackedPunktfunkProc {
	return &trackedPunktfunkProc{punktfunkProcess: p, done: make(chan struct{})}
}

func (t *trackedPunktfunkProc) Wait() error {
	err := t.punktfunkProcess.Wait()
	t.once.Do(func() { close(t.done) })
	return err
}

// terminate asks the process to exit cleanly (SIGTERM) and reports whether
// the request was delivered -- mirrors trackedSunshineProc.terminate.
// Windows has no SIGTERM for another process, so there it returns false and
// Stop() kills right away.
func (t *trackedPunktfunkProc) terminate() bool {
	if p, ok := t.punktfunkProcess.(punktfunkExecCmdProcess); ok {
		return p.cmd.Process.Signal(syscall.SIGTERM) == nil
	}
	return false
}

func (t *trackedPunktfunkProc) exited(d time.Duration) bool {
	select {
	case <-t.done:
		return true
	case <-time.After(d):
		return false
	}
}

// punktfunkStopGrace mirrors sunshineStopGrace: how long Stop()/hang
// recovery waits after SIGTERM (and again after SIGKILL) before giving up.
var punktfunkStopGrace = 3 * time.Second

func stopPunktfunkProcLocked(proc punktfunkProcess) error {
	tp, tracked := proc.(*trackedPunktfunkProc)
	if tracked && tp.terminate() {
		if tp.exited(punktfunkStopGrace) {
			return nil
		}
		log.Printf("[punktfunk] pid=%d ignored SIGTERM for %s, sending SIGKILL", tp.Pid(), punktfunkStopGrace)
	}
	if err := proc.Kill(); err != nil {
		return err
	}
	if tracked && !tp.exited(punktfunkStopGrace) {
		log.Printf("[punktfunk] pid=%d still running %s after SIGKILL", tp.Pid(), punktfunkStopGrace)
	}
	return nil
}

// punktfunkBackend implements Backend against a bundled punktfunk-host,
// GameStream (Moonlight-compat) plane only -- see this file's package doc
// comment.
type punktfunkBackend struct {
	mu        sync.Mutex
	exeDir    string
	stateDir  string
	logPath   string
	proc      punktfunkProcess
	adminPort int
	// token is the bearer credential this Start() pinned via
	// PUNKTFUNK_MGMT_TOKEN. AdminPass() returns it; AdminUser() is unused.
	token          string
	onExit         func()
	unhealthySince time.Time
}

var _ Backend = (*punktfunkBackend)(nil)

// NewPunktfunk constructs the Punktfunk-backed Backend implementation.
// Mirrors NewSunshine's parameters exactly: exeDir is the agent binary's own
// directory, stateDir is the agent's persistent state directory, logPath
// captures punktfunk-host's stdout/stderr.
func NewPunktfunk(exeDir, stateDir, logPath string) Backend {
	return &punktfunkBackend{exeDir: exeDir, stateDir: stateDir, logPath: logPath}
}

// DisplayName identifies this backend for display purposes only -- see
// streamhost.Identity. "(GameStream)" flags that the native punktfunk/1
// plane is not in use by this agent yet (see package doc comment).
func (b *punktfunkBackend) DisplayName() string { return "Punktfunk (GameStream)" }

// punktfunkBinEnv names a punktfunk-host outside the agent's own bundle.
const punktfunkBinEnv = "USBRIDGE_PUNKTFUNK_HOST"

// punktfunkStageDir is where the agent downloads punktfunk-host to
// (forkrelease.PunktfunkDir: <stateDir>/punktfunk, signature-verified from
// Streamers-Forks' latest release); set once at startup.
var punktfunkStageDir atomic.Value // string

// SetPunktfunkStageDir tells punktfunkBinaryPath where the agent's own
// downloaded punktfunk-host lives.
func SetPunktfunkStageDir(dir string) { punktfunkStageDir.Store(dir) }

// punktfunkBinaryPath locates punktfunk-host, or returns "" when there is
// none: the copy bundled next to the agent (<exeDir>/punktfunk/), then the
// one the agent downloaded (SetPunktfunkStageDir), then the one
// punktfunkBinEnv names, then a system-installed one on PATH. On KDE the path matters beyond finding the binary:
// KWin only lets a punktfunk-host capture when an installed .desktop file
// names that exact executable path (see ListCaptureDevices' doc comment),
// which a copy inside an AppImage's per-run mount point can never satisfy.
// Punktfunk's own usage text ("punktfunk-host -- Linux streaming host") and
// architecture doc confirm Linux and Windows host builds only -- no macOS
// host.
func punktfunkBinaryPath(exeDir string) string {
	name := "punktfunk-host"
	switch runtime.GOOS {
	case "linux":
	case "windows":
		name += ".exe"
	default:
		return ""
	}
	if exeDir != "" {
		p := filepath.Join(exeDir, "punktfunk", name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if dir, _ := punktfunkStageDir.Load().(string); dir != "" {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p := os.Getenv(punktfunkBinEnv); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// PunktfunkAvailable reports whether a punktfunk-host binary can be found
// for an agent whose own executable lives in exeDir.
func PunktfunkAvailable(exeDir string) bool { return punktfunkBinaryPath(exeDir) != "" }

func (b *punktfunkBackend) binaryPath() string { return punktfunkBinaryPath(b.exeDir) }

// BinaryPath returns the resolved path punktfunk-host is launched from, or
// "" if not bundled on this OS.
func (b *punktfunkBackend) BinaryPath() string { return b.binaryPath() }

// CapExecPath/SetCapExecPath: no KMS-capexec story for Punktfunk yet (the
// Linux capture-privilege-grant path Sunshine uses -- see
// sunshineBackend.CapExecPath's doc comment). Always "" / no-op.
func (b *punktfunkBackend) CapExecPath() string   { return "" }
func (b *punktfunkBackend) SetCapExecPath(string) {}

// Running reports whether this backend's punktfunk-host instance is
// currently alive.
func (b *punktfunkBackend) Running() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.proc != nil
}

// Pid returns the running punktfunk-host process's PID, or 0 if not running.
func (b *punktfunkBackend) Pid() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.proc == nil {
		return 0
	}
	return b.proc.Pid()
}

// generatePunktfunkToken mints a 32-byte hex bearer token -- same shape as
// punktfunk-host's own mint (mgmt_token.rs: `rand::rng().fill_bytes(&mut
// [0u8; 32])`, hex-encoded), so a value this backend pins is
// indistinguishable from one punktfunk-host would have generated itself.
func generatePunktfunkToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// adminTokenLocked returns the in-memory bearer token (internal, b.mu held
// by caller).
func (b *punktfunkBackend) adminTokenLocked() string { return b.token }

// AdminUser is unused by Punktfunk's bearer-token admin API -- see package
// doc comment.
func (b *punktfunkBackend) AdminUser() string { return "" }

// AdminPass returns the current session's bearer token (see package doc
// comment: PairingAPI's Basic-auth-shaped accessor carries a bearer token
// here instead of a password).
func (b *punktfunkBackend) AdminPass() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.token
}

// Start launches punktfunk-host serve --gamestream if it isn't already
// running under this backend. No-op if the binary isn't bundled (mirrors
// sunshineBackend.Start's same no-op-when-missing contract).
func (b *punktfunkBackend) Start(adminPort int) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.adminPort = adminPort
	if b.proc != nil {
		if adminPort <= 0 || portReachable(adminPort, 300*time.Millisecond) {
			b.unhealthySince = time.Time{}
			return nil
		}
		// Simplified hang-recovery compared to sunshineBackend.Start: one
		// grace window, no separate "stuck in the kernel" error type yet --
		// revisit if that's observed in the field for this backend too.
		now := time.Now()
		if b.unhealthySince.IsZero() {
			b.unhealthySince = now
			return nil
		}
		if now.Sub(b.unhealthySince) < sunshineHangGrace {
			return nil
		}
		if tp, ok := b.proc.(*trackedPunktfunkProc); ok {
			log.Printf("[punktfunk] pid=%d alive but admin port %d unreachable for %s -- treating as hung, killing it",
				tp.Pid(), adminPort, now.Sub(b.unhealthySince).Round(time.Second))
			_ = tp.Kill()
			tp.exited(punktfunkStopGrace)
		}
		b.proc = nil
		b.unhealthySince = time.Time{}
	}

	bin := b.binaryPath()
	if bin == "" {
		return nil
	}

	if adminPort > 0 && portReachable(adminPort, 300*time.Millisecond) {
		// Mirrors sunshineBackend.Start's identical reasoning: something is
		// answering on adminPort that this backend isn't tracking -- never
		// adopt it, since every call afterward would send a bearer token it
		// doesn't recognize.
		log.Printf("[punktfunk] admin port %d is reachable but not tracked by this process -- clearing it instead of adopting unverified credentials", adminPort)
		killOrphanStreamerProcesses()
		deadline := time.Now().Add(3 * time.Second)
		for portReachable(adminPort, 200*time.Millisecond) && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
	}

	dir := b.punktfunkConfigDir()
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Printf("[punktfunk] warning: could not create %s: %v", dir, err)
		}
		repairPunktfunkConfigDir(dir)
	}

	// Provisions the shared TLS identity (see shared_auth.go) into
	// cert.pem/key.pem/uniqueid, and converges paired.json/
	// client-labels.json with Sunshine's/rust-shine's own trust lists --
	// same reasoning as sunshineBackend.Start()'s identical call.
	// Best-effort: never blocks Start over it.
	ReconcileSharedAuth(b.stateDir)

	// A host KWin refused at startup keeps that answer, so this comes first.
	b.ensureCapture()

	token, err := generatePunktfunkToken()
	if err != nil {
		return err
	}

	args := []string{"serve", "--gamestream"}
	if adminPort > 0 {
		args = append(args, "--mgmt-bind", net.JoinHostPort(adminHost(), strconv.Itoa(adminPort)))
	}

	extra := map[string]string{punktfunkAdminTokenEnv: token}
	if dir != "" {
		extra[punktfunkConfigDirEnv] = dir
	}
	if monitor := b.OutputName(); monitor != "" && !strings.HasPrefix(monitor, punktfunkVirtualPrefix) {
		extra[punktfunkCaptureMonitorEnv] = monitor
	}
	// One line a second while a client streams: frames sent, how many were
	// new ones (uniq) rather than repeats, and the slowest capture/encode/
	// send of that second. Without it the host log says nothing about the
	// frame rate it actually delivered, which is the first thing to look at
	// when a benchmark run comes out slow. RustShine logs the same kind of
	// line unasked ("frame stage timing sample").
	extra["PUNKTFUNK_PERF"] = "1"
	if k, v, ok := strings.Cut(usbBrokerEnviron(), "="); ok {
		extra[k] = v
	}
	launchDir := filepath.Dir(bin)

	var logFile *os.File
	if b.logPath != "" {
		if err := os.MkdirAll(filepath.Dir(b.logPath), 0o755); err != nil {
			log.Printf("[punktfunk] failed to create log dir for %s: %v", b.logPath, err)
		} else if f, err := os.OpenFile(b.logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			log.Printf("[punktfunk] failed to open log file %s: %v", b.logPath, err)
		} else {
			logFile = f
		}
	}

	var proc punktfunkProcess
	if useSunshineSessionBroker() && punktfunkSessionLaunch != nil {
		p, err := punktfunkSessionLaunch(bin, args, launchDir, logFile, extra)
		if err != nil {
			if errors.Is(err, errSunshineNoActiveSessionMarker) {
				// Nobody is logged on yet: the watchdog calls Start again.
				log.Printf("[punktfunk] no active console session yet, deferring start")
				return nil
			}
			return err
		}
		log.Printf("[punktfunk] started (session-broker) pid=%d launch=%s", p.Pid(), bin)
		proc = p
	} else {
		cmd := exec.Command(bin, args...)
		configureProcess(cmd)
		env := os.Environ()
		for k, v := range extra {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
		if launchDir != "" && launchDir != "." {
			cmd.Dir = launchDir
		}
		if logFile != nil {
			cmd.Stdout = logFile
			cmd.Stderr = logFile
		}
		if err := cmd.Start(); err != nil {
			return err
		}
		log.Printf("[punktfunk] started pid=%d launch=%s", cmd.Process.Pid, bin)
		afterStartPunktfunk(cmd)
		proc = punktfunkExecCmdProcess{cmd}
	}

	// Only commit the token/proc once the process has actually started --
	// an error path above must leave b.token at its previous value rather
	// than claim credentials for a process that never launched.
	b.token = token
	tracked := newTrackedPunktfunkProc(proc)
	b.proc = tracked
	b.unhealthySince = time.Time{}
	go b.watchProcessExit(tracked)

	return nil
}

// watchProcessExit mirrors sunshineBackend.watchProcessExit exactly: blocks
// until proc exits, clears b.proc so Start()'s "already running" fast path
// stops believing a dead process is alive, and fires the onExit callback
// (SetOnExit) outside the lock.
func (b *punktfunkBackend) watchProcessExit(proc punktfunkProcess) {
	err := proc.Wait()
	log.Printf("[punktfunk] process exited: %v", err)
	b.mu.Lock()
	wasOurs := b.proc == proc
	if wasOurs {
		b.proc = nil
	}
	onExit := b.onExit
	b.mu.Unlock()
	if wasOurs && onExit != nil {
		onExit()
	}
}

// SetOnExit registers a callback fired the instant watchProcessExit notices
// this backend's own child process has died. See sunshineBackend.SetOnExit's
// doc comment for why this matters (recovery latency).
func (b *punktfunkBackend) SetOnExit(fn func()) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onExit = fn
}

// Stop terminates a punktfunk-host instance started by this backend. If this
// backend never actually spawned it, it still kills any orphaned
// punktfunk-host process by name -- mirrors sunshineBackend.Stop()'s
// identical fallback and the reason for it (see orphankill.go).
func (b *punktfunkBackend) Stop() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	var err error
	if b.proc != nil {
		log.Printf("[punktfunk] stopping pid=%d", b.proc.Pid())
		err = stopPunktfunkProcLocked(b.proc)
		if err != nil && isAccessDenied(err) {
			if elevErr := elevatedKillByPID(b.proc.Pid()); elevErr != nil {
				log.Printf("[punktfunk] elevated kill also failed: %v", elevErr)
			} else {
				err = nil
			}
		}
		b.proc = nil
	} else {
		log.Printf("[punktfunk] stopping orphaned process by name")
		killOrphanStreamerProcesses()
	}
	return err
}

// WaitReady polls adminPort until punktfunk-host's management API answers
// GET /api/v1/health (unauthenticated -- see punktfunk_pairing.go), or the
// deadline passes. Mirrors sunshineBackend.WaitReady's contract exactly.
func (b *punktfunkBackend) WaitReady(adminPort int, deadline time.Duration) bool {
	if adminPort <= 0 {
		return true
	}
	start := time.Now()
	for time.Since(start) < deadline {
		if punktfunkHealthy(adminPort) {
			return true
		}
		time.Sleep(150 * time.Millisecond)
	}
	return false
}

// Ports returns Punktfunk's GameStream-plane TCP/UDP ports relative to its
// NvHTTP-style base port, confirmed directly from
// crates/punktfunk-host/src/gamestream/mod.rs: HTTP_PORT=47989,
// HTTPS_PORT=47984 (basePort-5), RTSP_PORT=48010 (basePort+21),
// CONTROL_PORT=47999 (basePort+10, UDP/ENet) -- the exact same NvHTTP offset
// scheme sunshineBackend.Ports uses, by design (Punktfunk's GameStream plane
// targets wire-level Moonlight-client compatibility). basePort+1 is this
// backend's own --mgmt-bind pin, the same slot Sunshine's web-UI occupies
// (punktfunk's own ports doc explicitly calls out sharing that slot with
// Sunshine/Apollo/Vibeshine).
func (b *punktfunkBackend) Ports(basePort int) (tcp []int, udp []int) {
	tcp = []int{basePort - 5, basePort, basePort + 1, basePort + 21}
	udp = []int{basePort + 9, basePort + 10, basePort + 11, basePort + 13}
	return tcp, udp
}

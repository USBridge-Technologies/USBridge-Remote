package app

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	"usbridge_agent/assets"
	"usbridge_agent/internal/account"
	"usbridge_agent/internal/adminapi"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/audio"
	"usbridge_agent/internal/autostart"
	"usbridge_agent/internal/benchvideo"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/clipboard"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/devicecert"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/forkrelease"
	"usbridge_agent/internal/hwid"
	"usbridge_agent/internal/input"
	"usbridge_agent/internal/logcap"
	"usbridge_agent/internal/netutil"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/sasinput"
	"usbridge_agent/internal/streamerlaunch"
	"usbridge_agent/internal/streamhost"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/tlshost"
	"usbridge_agent/internal/ui"
	"usbridge_agent/internal/ui/design"
	"usbridge_agent/internal/update"
	"usbridge_agent/internal/usbpass"
)

type deviceState struct {
	mu              sync.Mutex
	startedAt       time.Time
	devices         []api.DeviceInfo
	mountInProgress bool
	lastMountError  string
}

type App struct {
	cfgPath string
	cfg     config.Config

	benchOnce   sync.Once
	benchPlayer *benchvideo.Player
	// benchMonitor is the monitor the running benchmark captures and plays
	// on (a monitors.Monitor ID, "" when none is pinned); benchOrigOutput
	// holds each backend's own output pick from before the benchmark
	// changed it, restored when the pin is cleared. Guarded by streamMu.
	benchMonitor    string
	benchOrigOutput map[string]string
	// benchRestartPending: a deferred SetBenchMonitor changed the running
	// streamer's monitor without restarting it; the next SetStreamBackend
	// restarts it (same kind) or replaces it (other kind). Guarded by
	// streamMu.
	benchRestartPending bool
	// lastSwitch is how the last SetStreamBackend spent its time, for the
	// benchmark's statistics. Guarded by streamMu.
	lastSwitch api.BackendSwitchTiming

	state     *deviceState
	input     *input.Controller
	screen    *capture.Service
	perms     *permissions.Service
	ts        *tailscale.Service
	usbBridge *tailscale.UsbTunnelBridge
	stream    streamhost.Backend
	tsProxy   *tailscale.StreamProxy
	server    *http.Server
	tsHTTP    *http.Server
	tlsServer *http.Server
	tlsMgr    *tlshost.Manager
	// runCtx is Run's own signal.NotifyContext, stashed here (rather than
	// only passed to the watchdogs started synchronously in Run) so
	// restartTLS can start deviceCertWatchdog on demand if the user enables
	// HTTPS at runtime after starting with it off -- see
	// tlsWatchdogOnce's doc comment.
	runCtx context.Context
	// tlsWatchdogOnce guards deviceCertWatchdog against starting twice --
	// once at Run() startup if HTTPS starts enabled, or once from
	// restartTLS if the user turns it on later; whichever happens first.
	tlsWatchdogOnce sync.Once
	handler         http.Handler
	apiServer       *api.Server
	fyneApp         fyne.App
	clipboard       *clipboard.Manager
	usbBroker       *usbpass.Service
	adminSrv        *adminapi.Server

	// clientLeft and clientStarting wake virtualOthersWatchdog when a client says
	// its stream is over or about to start (the latter closed once handled).
	clientLeft     chan struct{}
	clientStarting chan chan struct{}

	// usbPassBridgeAddr is StartUSBPassBridge's localhost address (see
	// api.Server.StartUSBPassBridge's doc comment) -- rustshine dials this
	// to relay browser-sourced USB/IP passthrough DataChannel bytes into
	// this agent process. Re-applied to each new streamhost.Backend by
	// applyStreamUSBPassBridgeAddr (SetStreamBackend switches backends at
	// runtime; the bridge listener itself is started once and outlives any
	// individual backend).
	usbPassBridgeAddr string

	// gpuClockArmed records whether applyGPUClockLock has already launched
	// the elevated lock daemon for this agent process, so repeated calls
	// (the sunshineWatchdog re-invokes startSunshine every 15s, and every
	// stream-host restart -- e.g. SetSunshineOutputName switching the
	// captured monitor -- gets a brand new stream-host PID) don't relaunch
	// the elevated helper, which would pop a fresh UAC prompt each time. The
	// daemon watches this *agent's* PID (see applyGPUClockLock), not the
	// stream host's, specifically so it only ever needs arming once per
	// agent run regardless of how many times the stream host itself
	// restarts -- a UAC prompt mid-switch can't be dismissed from a remote
	// session (it runs on the secure desktop), so the whole point is to get
	// consent once, up front, via the Permissions checkbox.
	gpuClockMu    sync.Mutex
	gpuClockArmed bool

	// exeDir/logPath are the same triple every streamhost.New* constructor
	// takes (alongside cfg.StateDir), stashed here so SetStreamBackend can
	// construct a replacement Backend on demand without New() needing to
	// export them separately.
	exeDir  string
	logPath string
	// engineLock is the open handle on this state dir's exclusive
	// engine.lock -- see acquireEngineLock's doc comment. Never closed while
	// the process is alive: the OS drops the lock (and lets the next
	// launch's acquireEngineLock succeed) the instant this fd closes, for
	// any reason including a crash or SIGKILL, which is the actual
	// single-instance guarantee. Kept only so the *os.File isn't garbage
	// collected out from under that guarantee -- nothing reads it again.
	engineLock *os.File
	// headless mirrors the flag Run was started with -- RelinquishEngine
	// reads it to decide whether there's a GUI window worth relaunching as
	// a thin client after stepping down (see its own doc comment).
	headless bool
	// streamMu serializes SetStreamBackend calls against each other (a GUI
	// click racing the entitlement watchdog's own downgrade, say) --
	// a.stream/a.streamKind must only ever be read/written while held.
	streamMu sync.Mutex
	// Crash-loop backoff for restartStreamerAfterExit.
	streamerExitMu     sync.Mutex
	streamerLastExit   time.Time
	streamerExitStreak int
	streamKind         string // "sunshine" | "rustshine" | "punktfunk" | "vibepollo" -- bookkeeping only, mirrors which concrete type a.stream currently is
	// streamKindView mirrors streamKind for readers that must not wait on
	// streamMu: SetStreamBackend holds it through the new backend's whole
	// startup (~25-40 s for Sunshine), and currentStreamKind used to take
	// it -- so the GUI's status poll and the protocol picker's hover/click
	// handlers (EntitlementStatus) froze for that long after every switch,
	// the benchmark's included, and a click in the picker did nothing
	// visible. Written together with streamKind via setStreamKind.
	streamKindView atomic.Value // string

	// entMu guards the fields below, all touched from both the GUI/adminapi
	// goroutine (user clicks) and entitlementWatchdog's background goroutine.
	entMu                 sync.Mutex
	entStatus             entitlement.Status
	entPollCancel         context.CancelFunc // cancels an in-flight StartPurchase's post-checkout poll loop, if any
	pendingStreamerUpdate string             // newer USBridge-streamer tag seen while auto-update is off
	lastPunktfunkCheck    time.Time          // checkPunktfunkUpdate's last look at Streamers-Forks (entMu)
	lastSunshineCheck     time.Time          // checkSunshineUpdate's, the same (entMu)
	lastSunshineFetch     time.Time          // fetchSunshineInBackground's last first-run attempt (entMu)
	lastVibepolloCheck    time.Time          // checkVibepolloUpdate's last look at Streamers-Forks (entMu)

	// accMu guards the account-login fields below -- see StartAccountLogin's
	// doc comment. Separate mutex/status from entMu above: this is a
	// different login entirely (account.Status, not entitlement.Status),
	// touched from the same GUI goroutine but never from
	// entitlementWatchdog's background one.
	accMu         sync.Mutex
	accStatus     account.Status
	accPollCancel context.CancelFunc
}

// StartOptions configures Start -- see main.go's flag definitions for the
// user-facing meaning of each field.
type StartOptions struct {
	// Headless runs the engine (HTTP server, Sunshine, tsnet) with no GUI
	// at all -- see Start's doc comment.
	Headless bool
	// Tray, when this launch ends up showing a GUI (either owning the
	// engine or attaching to one already running), starts that window
	// hidden -- minimized to the tray -- instead of shown. Used by the
	// login-time tray helper (see internal/autostart's per-platform
	// Enable) so it never visibly pops a window at login; a no-op if this
	// session turns out to have no usable tray host at all (see
	// ui.Window's startHidden field doc).
	Tray bool
	// Attach, if non-empty, dials this exact admin-socket path directly and
	// attaches a thin-client GUI to it, bypassing the normal config-path
	// discovery below entirely. Windows-only in practice: a LocalSystem
	// service already knows its own socket path, which lives under a
	// different profile (SYSTEM's) than whatever interactive user session
	// this process gets launched into via sessionlaunch -- see
	// service_windows.go's SessionChange handling, which is the only
	// caller that ever sets this.
	Attach string
}

// Start is the sole entry point from main(). It decides whether this
// process owns the engine (HTTP server, Sunshine, tsnet) or just attaches a
// GUI to one that's already running headless — see runThinClientGUI. This
// is what lets the same binary/AppImage work both as a `--headless`
// systemd/launchd/autostart service and as the normal GUI app without ever
// running two engines (and two Sunshine/tsnet instances) at once on the
// same machine.
//
// Ownership is decided by engine.lock (see acquireEngineLock in
// enginelock.go), not by "is the admin socket reachable" alone. That used
// to be the whole check, and it was racy: a dial that spuriously timed out
// against an otherwise-healthy instance (or two launches racing the same
// check) could make two processes each conclude "nobody's home" and each go
// on to bind their own HTTP server, streamhost backend, and admin socket —
// confirmed live, a headless LaunchAgent and a manually-launched GUI both
// ended up owning a streamhost.Backend at once, fighting over the same
// GameStream ports with the loser's stale credentials used against the
// winner's process. flock-based mutual exclusion doesn't have that TOCTOU
// window: at most one process can ever hold the lock, full stop.
//
// A --headless launch (autostart/login item, or the Windows service) always
// ends up owning the engine — if another process already holds the lock, it
// gets evicted (gracefully asked to step down first, then killed if
// unresponsive; see evictEngineLockHolder) rather than this launch quietly
// refusing to start. That is what makes "enable launch at login" actually
// take effect immediately: it starts a --headless instance right away (see
// autostart_darwin.go's Enable, activateNow=true), which now reliably
// replaces whatever GUI-owned engine was running instead of dying on
// "already running" and leaving the old one in charge.
//
// A normal GUI launch prefers to attach as a thin client to whoever already
// owns the engine, and only takes ownership itself as a last resort (lock
// genuinely free, or the existing holder turned out to be unresponsive to
// both the graceful ask and a dial retry).
func Start(opts StartOptions, version string) error {
	// One GUI per state dir: a second normal/tray launch asks the running
	// one to come forward and exits, instead of piling up windows and tray
	// icons (see acquireGUILock).
	if !opts.Headless {
		if !acquireGUILockForStart(!opts.Tray) {
			return nil
		}
	}

	if opts.Attach != "" {
		// The Windows service can hand us --attach a few hundred ms before
		// the engine has actually called Listen on that socket (it used to
		// bind only after tsnet came up). Retry instead of dying on the
		// first refused connection — otherwise the tray helper exits and
		// the user is left with a running engine and no window.
		client, err := dialAdminSocket(opts.Attach, 15*time.Second)
		if err != nil {
			return fmt.Errorf("attach to admin socket %s: %w", opts.Attach, err)
		}
		return runThinClientGUI(client, opts.Tray)
	}

	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	if !config.DirIsUsable(cfg.StateDir) {
		fallback := config.Default().StateDir
		log.Printf("[app] state dir %s is not writable by this process; using %s", cfg.StateDir, fallback)
		cfg.StateDir = fallback
	}
	if err := cfg.EnsureState(); err != nil {
		return err
	}

	socketPath := adminapi.SocketPath(cfg.StateDir)

	lockFile, holderPID, acquired, lockErr := acquireEngineLock(cfg.StateDir)
	if lockErr != nil {
		log.Printf("[app] warning: engine lock (%s) unusable, proceeding without single-instance protection: %v", engineLockPath(cfg.StateDir), lockErr)
	}

	if !acquired && lockErr == nil {
		log.Printf("[app] engine lock already held by pid=%d (headless=%v)", holderPID, opts.Headless)
		if !opts.Headless {
			// GUI: the normal, fast, happy path is to attach to whoever
			// already owns the engine rather than fight over it.
			if client, dialErr := dialAdminSocket(socketPath, 3*time.Second); dialErr == nil {
				log.Printf("[app] attaching as a thin-client GUI to the existing engine (pid=%d)", holderPID)
				return runThinClientGUI(client, opts.Tray)
			} else {
				log.Printf("[app] lock is held by pid=%d but its admin socket did not respond (%v) -- treating it as stale", holderPID, dialErr)
			}
		} else {
			log.Printf("[app] headless launch always takes ownership -- evicting existing holder pid=%d instead of refusing to start", holderPID)
		}

		if opts.Headless && engineHolderIsHeadless(cfg.StateDir) {
			// A second headless copy (a duplicate autostart entry, a manual
			// launch on top of the systemd unit) must not kill a live headless
			// engine just to replace it with itself -- even one whose admin
			// socket isn't up yet (it only binds after tsnet comes up). The
			// flock is only ever held by a live process, so holder-alive is
			// enough.
			log.Printf("[app] a headless engine is already running (pid=%d) -- exiting instead of replacing it", holderPID)
			return nil
		}

		if evictEngineLockHolder(cfg.StateDir, socketPath, holderPID) {
			lockFile, holderPID, acquired, lockErr = acquireEngineLock(cfg.StateDir)
		}
		if !acquired {
			if lockErr != nil {
				return fmt.Errorf("could not become the engine owner: %w", lockErr)
			}
			return fmt.Errorf("usbridge-agent is already running (pid=%d) and could not be replaced", holderPID)
		}
	}

	stampEngineMode(lockFile, opts.Headless)
	log.Printf("[app] this process (pid=%d) now owns the engine at %s", os.Getpid(), socketPath)

	// update.BeforeRelaunch lets apply() (any platform) release our engine
	// lock right before it spawns the process/helper that becomes the new
	// owner, instead of leaving that to this process's eventual os.Exit —
	// closes a real race where the replacement's own acquireEngineLock
	// attempt could otherwise land before this fd actually closes. Reset to
	// close instance.engineLock (not this local lockFile) once New()
	// succeeds below, since a GUI launch's own later user-confirmed update
	// (internal/ui's ShowAndRun -> DownloadAndApply) happens long after
	// this function has returned, against the *App's* copy of the lock.
	update.BeforeRelaunch = func() {
		if lockFile != nil {
			log.Printf("[app] releasing engine lock before self-update relaunch")
			_ = lockFile.Close()
			lockFile = nil
		}
	}

	// Mandatory startup update check — only here, not on a thin-GUI attach
	// above: this is exactly the branch where this process is about to
	// become the one actually owning the engine, so it's the only launch
	// path where replacing the on-disk binary and re-exec'ing is
	// safe/meaningful. A --headless launch has no GUI to ask through, so it
	// applies silently like before (CheckAndApply); a normal GUI launch
	// instead asks via a confirm dialog once the window exists — see
	// internal/ui's ShowAndRun, which runs the same Check/DownloadAndApply
	// pair gated on the user's answer.
	if opts.Headless {
		update.CheckAndApply(context.Background(), version)
	}

	instance, err := New()
	if err != nil {
		if lockFile != nil {
			_ = lockFile.Close()
		}
		return err
	}
	instance.engineLock = lockFile
	update.BeforeRelaunch = func() {
		if instance.engineLock != nil {
			log.Printf("[app] releasing engine lock before self-update relaunch")
			_ = instance.engineLock.Close()
			instance.engineLock = nil
		}
	}
	return instance.Run(opts.Headless, opts.Tray)
}

// runThinClientGUI shows the GUI backed by an already-running headless
// instance's admin socket instead of starting a second engine. Closing the
// window here does NOT stop the headless instance — only a process actually
// owning the engine (see App.Run/shutdownEngine) does that. startHidden is
// forwarded to ui.Window.SetStartHidden -- see StartOptions.Tray's doc
// comment.
func runThinClientGUI(client *adminapi.Client, startHidden bool) error {
	cfg, err := client.CurrentConfig()
	if err != nil {
		client.Close()
		return fmt.Errorf("fetch config from running instance: %w", err)
	}

	restoreXWayland := forceXWaylandForGUI()
	fyneApp := fyneapp.NewWithID("io.usbridge.agent")
	restoreXWayland()
	ui.ApplySavedUIScale(fyneApp)
	fyneApp.Settings().SetTheme(design.NewBrandTheme())
	fyneApp.SetIcon(assets.AppIcon)

	// Permission grants (uinput udev rule, KMS CAP_SYS_ADMIN) run through
	// pkexec, which needs a real polkit authentication agent for the
	// caller's login session. The headless instance behind client is a
	// systemd system service with no session of its own — pkexec run
	// inside it can never reach one, it falls back to a textual agent and
	// fails immediately ("Error opening current controlling terminal...").
	// This process, by contrast, is launched interactively in the user's
	// actual desktop session, where a polkit GUI agent (e.g.
	// polkit-kde-authentication-agent-1) is reachable — so run pkexec here
	// instead, against the same on-disk targets (/dev/uinput, the udev
	// rule, the daemon's own capexec launcher path), then tell the daemon
	// to notice and pick up the change.
	localPerms := permissions.New()
	token := &thinClientToken{Client: client, perms: localPerms}
	win := ui.NewWindow(fyneApp, cfg, localPerms, client, token)
	win.SetStartHidden(startHidden)
	win.ShowAndRun(func() {
		client.Close()
	})
	return nil
}

// thinClientToken adapts *adminapi.Client for runThinClientGUI, overriding
// only the KMS capability methods to grant locally instead of proxying the
// pkexec call into the headless daemon — see runThinClientGUI for why.
type thinClientToken struct {
	*adminapi.Client
	perms *permissions.Service
}

func (t *thinClientToken) KMSCaptureGranted() bool {
	return t.perms.KMSCaptureGranted(t.Client.KMSCaptureTargetPath())
}

func (t *thinClientToken) RequestKMSCapture() bool {
	path := t.Client.KMSCaptureTargetPath()
	if path == "" {
		return false
	}
	if !t.perms.RequestKMSCapture(path) {
		return false
	}
	return t.Client.RecheckKMSCapture()
}

// currentInstance holds the most recently started App, set by Run just
// before it blocks -- the only external reader is NotifySessionChange
// (called from cmd/usbridge_agent/service_windows.go's SessionChange
// handler), which otherwise has no way to reach the *App that Start()
// constructs internally and never returns out to main.
var currentInstance struct {
	mu sync.Mutex
	a  *App
}

// NotifySessionChange restarts the active stream backend (see
// RestartSunshine) so it picks up whichever session Windows now reports as
// the active console session. Called by the USBridgeAgent Windows service's
// SessionChange handler on WTS_SESSION_LOGON/WTS_CONSOLE_CONNECT: a
// LocalSystem service never automatically re-homes an already-running
// gamestream-server child into a session that only becomes interactive
// after the service itself started (see internal/sessionlaunch's package
// doc, and internal/streamhost's rustshineProcess/useSessionBroker) -- this
// is what actually notices "a session just became available/changed" and
// forces the kill+relaunch that picks it up, instead of waiting on
// sunshineWatchdog's blind 15s poll (which would otherwise leave a client
// staring at dead/wrong-resolution video for up to that long after every
// login). No-op if no App is running yet (e.g. the notification races the
// service's own startup) or if RustShine isn't actually the active backend
// (Sunshine's own Windows service tooling, unused by this agent, handles
// this differently and doesn't need the restart).
func NotifySessionChange() {
	currentInstance.mu.Lock()
	a := currentInstance.a
	currentInstance.mu.Unlock()
	if a == nil {
		return
	}
	if a.currentStreamKind() != "rustshine" {
		return
	}
	log.Printf("[app] Windows session change detected, restarting rustshine to pick it up")
	if err := a.RestartSunshine(); err != nil {
		log.Printf("[app] restart after session change failed: %v", err)
	}
}

// AdminSocketPath returns the currently running instance's admin-socket
// path, or ok=false if no instance has started yet (e.g. this races the
// service's own startup). Windows-only caller: service_windows.go hands
// this to LaunchTrayHelperInActiveSession as an explicit --attach target,
// since a LocalSystem service's own config/state dir lives under a
// different profile than whatever interactive session that helper actually
// runs in -- see StartOptions.Attach's doc comment for the full picture.
func AdminSocketPath() (string, bool) {
	currentInstance.mu.Lock()
	a := currentInstance.a
	currentInstance.mu.Unlock()
	if a == nil {
		return "", false
	}
	return adminapi.SocketPath(a.cfg.StateDir), true
}

// AdminSocketReady is AdminSocketPath plus a live Dial — the path exists as
// soon as Run() assigns currentInstance, but Listen only happens once the
// admin server is up. The Windows service's tray helper must wait for the
// latter or it dies on "connection refused" and the user gets no window.
func AdminSocketReady() (string, bool) {
	path, ok := AdminSocketPath()
	if !ok {
		return "", false
	}
	client, err := adminapi.Dial(path)
	if err != nil {
		return "", false
	}
	client.Close()
	return path, true
}

func dialAdminSocket(path string, wait time.Duration) (*adminapi.Client, error) {
	deadline := time.Now().Add(wait)
	var last error
	for {
		client, err := adminapi.Dial(path)
		if err == nil {
			return client, nil
		}
		last = err
		if wait <= 0 || time.Now().After(deadline) {
			return nil, last
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func New() (*App, error) {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.EnsureState(); err != nil {
		fallback := config.Default().StateDir
		if fallback == cfg.StateDir || !config.DirIsUsable(fallback) {
			return nil, err
		}
		log.Printf("[app] state dir %s is not writable; using %s", cfg.StateDir, fallback)
		cfg.StateDir = fallback
	}

	// Generate master key on first run.
	if strings.TrimSpace(cfg.MasterKey) == "" {
		key, err := api.GenerateMasterKey()
		if err != nil {
			return nil, fmt.Errorf("generate master key: %w", err)
		}
		cfg.MasterKey = key
		if err := config.Save(cfgPath, cfg); err != nil {
			log.Printf("[app] warning: failed to persist master key: %v", err)
		}
	}

	// The master key is used as an opaque secret string (never hex-decoded) —
	// this must match usbridge_client and the canonical usbridge server, which
	// both derive the HMAC/AES key via SHA256(rawMasterKeyBytes).
	masterKeyBytes := []byte(cfg.MasterKey)

	clipboardMgr := clipboard.NewManager(clipboard.NewBackend(nil), cfg.ClipboardMaxBytes)
	clipboardMgr.SetEnabled(cfg.ClipboardSyncEnabled)

	instance := &App{
		cfgPath:   cfgPath,
		cfg:       cfg,
		state:     &deviceState{startedAt: time.Now()},
		input:     input.New(),
		perms:     permissions.New(),
		ts:        tailscale.New(cfg.StateDir),
		clipboard: clipboardMgr,
	}
	// fyneApp is created lazily in Run(), only for a GUI-owning process — a
	// --headless engine never touches Fyne at all, so it never needs a
	// display connection (see Run).
	instance.exeDir = resolveExeDir()
	instance.logPath = filepath.Join(cfg.StateDir, "logs", "sunshine-stdout.log")
	// The streamer's output: a ring too (see logcap).
	logcap.Watch(instance.logPath)
	instance.setStreamKind("sunshine")

	// If this install was already switched to RustShine last run, pick it
	// back up from a cold start too -- whenever its binary is staged. No
	// license is needed to run it: without a valid token (never linked, an
	// offline machine whose token expired) RustShine runs its free tier,
	// which lacks only pen tablets and 4:4:4 color; the license only ever
	// decides those (rust-shine reads the token file itself).
	if cfg.PreferredBackend == "rustshine" {
		if _, err := os.Stat(entitlement.StagePath(cfg.StateDir)); err == nil {
			instance.setStreamKind("rustshine")
		}
	}
	// Punktfunk needs no entitlement, only its binary; without one (it was
	// uninstalled since) the agent comes back on Sunshine.
	streamhost.SetPunktfunkStageDir(forkrelease.PunktfunkDir(cfg.StateDir))
	// Where the release has a build for this platform Sunshine is downloaded,
	// not shipped (see sunshine_update.go).
	if forkrelease.SunshineAssetName() != "" {
		streamhost.SetSunshineStageBinary(forkrelease.SunshineBinary(cfg.StateDir))
	}
	if cfg.PreferredBackend == "punktfunk" && streamhost.PunktfunkAvailable(instance.exeDir) {
		instance.setStreamKind("punktfunk")
	}
	// Vibepollo: downloaded when picked (vibepollo_update.go), never shipped.
	if forkrelease.VibepolloAssetName() != "" {
		streamhost.SetVibepolloStageBinary(forkrelease.VibepolloBinary(cfg.StateDir))
	}
	if cfg.PreferredBackend == "vibepollo" && forkrelease.VibepolloStaged(cfg.StateDir) {
		instance.setStreamKind("vibepollo")
	}
	if instance.streamKind == "rustshine" {
		instance.stream = streamhost.NewRustshine(instance.exeDir, cfg.StateDir, instance.logPath)
		applyStreamSharedSecret(instance.stream, masterKeyBytes)
		applyStreamWebRTCEnabled(instance.stream, !cfg.RustShineWebRTCDisabled)
	} else if instance.streamKind == "punktfunk" {
		instance.stream = streamhost.NewPunktfunk(instance.exeDir, cfg.StateDir, instance.logPath)
	} else if instance.streamKind == "vibepollo" {
		instance.stream = streamhost.NewVibepollo(instance.exeDir, cfg.StateDir, instance.logPath)
	} else {
		instance.stream = streamhost.NewDefault(instance.exeDir, cfg.StateDir, instance.logPath)
	}
	instance.screen = capture.New(instance.stream)
	instance.wireStreamedBounds()
	instance.syncSunshineCaptureMode()
	instance.removeLegacyKMSGrants()
	instance.syncSunshineCapExec()
	apiServer := api.NewServerWithAuth(instance, masterKeyBytes, cfg.SunshinePort)
	apiServer.SetSelfHTTPPort(cfg.HTTPPort)
	// Started unconditionally (like ts itself, which doesn't actually spin up
	// tsnet until Server() is first called) rather than gated on
	// cfg.TailscaleEnabled: Tailscale can be toggled on later without this
	// agent process restarting, but the broker subprocess spawned by
	// usbpass.New below only gets --tsnet-bridge baked in once, at its own
	// spawn time — the bridge address needs to already be valid then
	// regardless of what Tailscale's enablement looks like right now. Until
	// RememberPeer is ever called (which only happens once StreamProxy
	// actually relays a Tailscale connection), this bridge just sits idle.
	instance.usbBridge = tailscale.NewUsbTunnelBridge(instance.ts)
	usbBridgeAddr, err := instance.usbBridge.Start(tailscale.DefaultUsbBridgeAddr)
	if err != nil {
		log.Printf("[app] usb tunnel bridge: %v (USB passthrough over Tailscale will not work; Direct/LAN unaffected)", err)
		usbBridgeAddr = ""
	}
	instance.clientLeft = make(chan struct{}, 1)
	instance.clientStarting = make(chan chan struct{})
	instance.usbBroker = usbpass.New(instance.exeDir, cfg.StateDir, cfg.MasterKey, cfg.UsbPassthroughPort, usbBridgeAddr)
	apiServer.SetUSBPassthrough(instance.usbBroker)
	if addr, err := apiServer.StartUSBPassBridge(); err != nil {
		log.Printf("[app] usbpass webrtc bridge: %v (browser gamepad/pen passthrough over WebRTC will not work; legacy WebSocket path unaffected)", err)
	} else {
		instance.usbPassBridgeAddr = addr
		applyStreamUSBPassBridgeAddr(instance.stream, addr)
	}
	instance.apiServer = apiServer
	handler := apiServer.Routes()
	instance.handler = handler
	instance.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.EffectiveListenHost(), cfg.HTTPPort),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	instance.tsHTTP = &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	instance.tlsMgr = tlshost.NewManager(filepath.Join(cfg.StateDir, "web-tls"))
	instance.tlsMgr.LoadPersisted()
	instance.tlsServer = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.EffectiveListenHost(), cfg.TLSPort),
		Handler:           handler,
		TLSConfig:         &tls.Config{GetCertificate: instance.tlsMgr.GetCertificate, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
	}
	instance.refreshLocalEntitlementStatus()
	if cfg.AccountToken != "" {
		instance.accStatus.LoggedIn = true
		instance.accStatus.Email = cfg.AccountEmail
		go instance.refreshAccountLicenses(context.Background())
	}
	return instance, nil
}

func resolveExeDir() string {
	if exePath, err := os.Executable(); err == nil {
		return filepath.Dir(exePath)
	}
	return "."
}

func resolveConfigPath() string {
	candidates := make([]string, 0, 8)
	// Prefer this process's own config dir first — it matches
	// config.Default().StateDir. Putting exeDir first meant a LocalSystem
	// Windows service would write dist/windows/config.yaml with
	// state_dir under SYSTEM's profile; the next interactive launch then
	// loaded that file and died in MkdirAll.
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		candidates = append(candidates, filepath.Join(base, "usbridge-agent", "config.yaml"))
	}
	// Under an AppImage, exeDir is the AppImage's read-only squashfs mount
	// (a fresh, ephemeral path each launch) — never usable as a config
	// location, so it's excluded both from the search and from the fallback
	// below (which would otherwise pick it and every later config.Save would
	// fail with "read-only file system").
	skipExeDir := runtime.GOOS == "linux" && os.Getenv("APPIMAGE") != ""
	if !skipExeDir {
		if exePath, err := os.Executable(); err == nil {
			exeDir := filepath.Dir(exePath)
			candidates = append(candidates,
				filepath.Join(exeDir, "config.yaml"),
				filepath.Clean(filepath.Join(exeDir, "..", "..", "..", "config.yaml")),
			)
		}
	}
	candidates = append(candidates, filepath.Join(".", "config.yaml"))
	var homeCandidate string
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		homeCandidate = filepath.Join(homeDir, ".config", "usbridge-agent", "config.yaml")
		candidates = append(candidates, homeCandidate)
		if runtime.GOOS == "darwin" {
			// macOS: the UI saves via StateDir which defaults to ~/Library/Application Support/
			candidates = append(candidates,
				filepath.Join(homeDir, "Library", "Application Support", "usbridge-agent", "config.yaml"),
			)
		}
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err != nil {
			continue
		}
		cfg, err := config.Load(candidate)
		if err != nil {
			continue
		}
		if !config.DirIsUsable(cfg.StateDir) {
			log.Printf("[app] ignoring %s: state dir %s is not writable by this process", candidate, cfg.StateDir)
			continue
		}
		return candidate
	}
	if skipExeDir && homeCandidate != "" {
		return homeCandidate
	}
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, "usbridge-agent", "config.yaml")
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return filepath.Join(".", "config.yaml")
}

// Run starts the engine (HTTP server, Sunshine, tsnet, admin socket) and
// then either blocks headlessly on ctx.Done() (headless==true — no Fyne
// driver ever touched, so no display connection is required) or shows the
// GUI window backed directly by this same in-process engine (headless==false).
// startHidden is forwarded to ui.Window.SetStartHidden when a window is
// shown at all -- see StartOptions.Tray's doc comment.
func (a *App) Run(headless, startHidden bool) error {
	a.headless = headless
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	a.runCtx = ctx

	// Diagnostic-only, additive: signal.Notify fans a delivered signal out to
	// every channel registered for it, so this doesn't steal anything from
	// NotifyContext's own internal channel above -- it just also logs which
	// exact signal arrived before the graceful shutdown it triggers proceeds.
	// Added to chase a live symptom (full agent — tsnet, HTTP, the rustshine
	// child — self-terminating cleanly with no Windows Event Log crash
	// record, no scheduled task, and no self-update in the log) where nothing
	// so far has identified *what* delivered the interrupt: on Windows, Go's
	// runtime maps CTRL_C_EVENT/CTRL_BREAK_EVENT to os.Interrupt and
	// CTRL_CLOSE_EVENT/CTRL_LOGOFF_EVENT/CTRL_SHUTDOWN_EVENT to
	// syscall.SIGTERM -- so which one of these two fires tells us whether
	// this is a console control event at all, or (if this line never logs
	// when the next occurrence happens) that shutdownEngine is instead being
	// reached via the GUI window's own close-intercept path (see
	// ui/window.go's SetCloseIntercept, which also logs now) with no OS
	// signal involved at all.
	diagSigCh := make(chan os.Signal, 2)
	signal.Notify(diagSigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-diagSigCh
		log.Printf("[app] DIAG: OS signal received: %v (os.Interrupt=CTRL_C/CTRL_BREAK; SIGTERM=CTRL_CLOSE/CTRL_LOGOFF/CTRL_SHUTDOWN) -- this will trigger shutdownEngine via ctx cancellation", sig)
	}()

	// See NotifySessionChange's doc comment for why this needs to be
	// reachable from outside the normal Start()->New()->Run() call chain.
	currentInstance.mu.Lock()
	currentInstance.a = a
	currentInstance.mu.Unlock()

	// Bind the admin socket before Sunshine/tsnet so a Windows tray helper
	// can attach as soon as AdminSocketPath is known. This used to run
	// after initTailscale(), which can block for tens of seconds; the
	// helper then dialed a path that wasn't listening yet and exited.
	if srv, err := adminapi.NewServer(adminapi.SocketPath(a.cfg.StateDir), a, a.perms, a.ts, func() config.Config { return a.cfg }); err != nil {
		log.Printf("[app] warning: admin socket unavailable: %v", err)
	} else {
		a.adminSrv = srv
		go func() {
			if err := srv.Serve(); err != nil {
				log.Printf("[app] admin socket server error: %v", err)
			}
		}()
	}

	keepDisplayAwake(ctx)

	log.Printf("[app] starting http=%s:%d headless=%v", a.cfg.EffectiveListenHost(), a.cfg.HTTPPort, headless)

	a.startSunshine()
	autostart.RefreshX11SessionEnv()
	// See streamhost.ProcessWatcher's doc comment: without this, a crashed
	// stream host only gets noticed (and relaunched) on sunshineWatchdog's
	// next periodic tick, up to sunshineWatchdogInterval (15s) of dead air
	// on a connected client's screen -- confirmed live. startSunshine()
	// itself is always safe to call from here: it no-ops if something else
	// already restarted the process first (e.g. this same tick racing
	// sunshineWatchdog).
	if pw, ok := a.stream.(streamhost.ProcessWatcher); ok {
		pw.SetOnExit(a.restartStreamerAfterExit)
	}
	go a.sunshineWatchdog(ctx)
	go a.x11SessionEnvWatchdog(ctx)
	go a.awdlWatchdog(ctx)
	go a.wlanStreamingWatchdog(ctx)
	go a.virtualOthersWatchdog(ctx)
	// Always started, even before ever linking -- recheckEntitlement no-ops
	// immediately (no network call) whenever cfg.EntitlementToken is
	// empty, so this is cheap, and it means a purchase/trial made
	// mid-session (via StartPurchase/StartFreeTrial, no restart) is
	// covered by the same ticker without needing separate "start the
	// watchdog now" bookkeeping.
	go a.entitlementWatchdog(ctx)
	go a.streamerUpdateWatchdog(ctx)
	go a.turnCredentialsWatchdog(ctx)
	go a.webrtcSignalRelayWatchdog(ctx)
	go a.usbBrokerWatchdog(ctx)
	go a.recheckEntitlement(ctx) // one immediate check, don't wait a full entitlementRecheckInterval after a restart
	go func() { _ = a.server.ListenAndServe() }()
	// Gated by the "Enable HTTPS" checkbox (see ui's HTTP Listen Address
	// dialog, UpdateTLSAddr) -- on by default (TLSEnabledOK's nil-means-true
	// convention), so this runs for every existing install unless a user
	// explicitly turned it off.
	if a.cfg.TLSEnabledOK() {
		// Self-signed baseline generated synchronously, before the TLS
		// listener starts accepting -- deviceCertWatchdog's first tick
		// (below) then upgrades to its own per-device Let's Encrypt cert once the
		// backend round trip completes, but a self-signed fallback must
		// already exist so the very first TLS handshake (offline, or before
		// that tick lands) doesn't hit tlshost.Manager's "no certificate
		// available yet" error.
		selfSignedIPs := []net.IP{net.ParseIP("127.0.0.1")}
		if ip := net.ParseIP(netutil.PreferredIPv4()); ip != nil {
			selfSignedIPs = append(selfSignedIPs, ip)
		}
		if err := a.tlsMgr.EnsureSelfSigned(selfSignedIPs, nil); err != nil {
			log.Printf("[app] self-signed TLS cert unavailable: %v", err)
		}
		go func() { _ = a.tlsServer.ListenAndServeTLS("", "") }()
		a.startDeviceCertWatchdogOnce()
	}
	if a.usbBroker != nil {
		if err := a.usbBroker.Start(); err != nil {
			log.Printf("[usbpass] broker not started: %v", err)
		}
	}
	if a.clipboard != nil {
		go a.clipboard.Run(ctx)
	}

	a.initTailscale(ctx)

	if headless {
		<-ctx.Done()
		a.shutdownEngine()
		return nil
	}

	restoreXWayland := forceXWaylandForGUI()
	a.fyneApp = fyneapp.NewWithID("io.usbridge.agent")
	restoreXWayland()
	ui.ApplySavedUIScale(a.fyneApp)
	a.fyneApp.Settings().SetTheme(design.NewBrandTheme())
	a.fyneApp.SetIcon(assets.AppIcon)

	go a.handleShutdown(ctx, cancel)
	win := ui.NewWindow(a.fyneApp, a.cfg, a.perms, a.ts, a)
	win.SetOwnsEngine(true)
	win.SetStartHidden(startHidden)
	win.ShowAndRun(cancel)
	return nil
}

// keepDisplayAwake holds a system-wide power assertion for as long as the
// agent runs, on macOS only. Without this, the only thing keeping the
// display awake is Sunshine's own capture-session assertion (see
// startSunshine's "Sunshine display capture" assertion) -- which only exists
// once a capture session is already up, i.e. exactly when it's too late to
// matter: if the display went to sleep (or the whole system slept) during
// the idle stretch between the agent starting and the first incoming
// connection, VTCompressionSessionCreate fails outright with "Cannot create
// compression session: -12903" the moment Sunshine tries to start streaming,
// because macOS revokes hardware H.264/HEVC encoder access while the display
// isn't awake -- confirmed live against a Mac that had been running this
// agent for ~21h idle: every hardware encoder creation attempt on the next
// client connect failed with -12903 and streaming never started, while the
// identical flow against a Windows/Linux host (software encode, no
// "display must be awake" requirement) worked every time. That's also why it
// silently looks like "no video" instead of an obvious error: the agent's
// own HTTP API and Sunshine's process both stay up and keep answering
// requests, so nothing about the connection *looks* down.
//
// Spawning `caffeinate` for the agent's own lifetime (rather than rolling a
// cgo/IOKit power assertion) keeps this a plain subprocess, matching how the
// rest of this package already shells out to tailscale/sunshine/ffmpeg.
func keepDisplayAwake(ctx context.Context) {
	if runtime.GOOS != "darwin" {
		return
	}
	// -d: prevent display sleep -- the one VTCompressionSessionCreate actually
	// needs. -i: prevent idle system sleep too, so the display assertion is
	// never moot because the whole machine suspended around it. -s: also
	// prevent sleep on AC power, the ordinary configuration for a machine
	// left set up as a remote host.
	cmd := exec.CommandContext(ctx, "caffeinate", "-d", "-i", "-s")
	if err := cmd.Start(); err != nil {
		log.Printf("[app] warning: could not start caffeinate — display may sleep and break hardware video encoding: %v", err)
		return
	}
	log.Printf("[app] caffeinate started (pid=%d) — preventing display/system sleep while the agent runs", cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
}

// startSunshine is the guarded entry point every caller *except* the
// RustShine update flow itself should use: onExit (fired by
// watchProcessExit on literally any process exit, deliberate or not -- see
// its own doc comment) and sunshineWatchdog's periodic tick both go through
// this. Skipping while RustShineUpdateInProgress is set closes a real race
// confirmed live: stopRustShineForUpdate() calls a.stream.Stop() to release
// the .exe's file lock before entitlement.StageRustShine's rename, but that
// same Stop() also makes watchProcessExit fire onExit -- which used to call
// this function directly and relaunch the *old* binary on the spot,
// re-locking the file before the rename (or the periodic watchdog's own
// independent 15s tick, landing in the same window) ever got a chance to
// run. The update flow itself calls startSunshineNow directly (see
// checkRustShineUpdate/CheckRustShineUpdateNow's own failure-recovery
// calls, and RestartSunshine's self-contained stop+start for the success
// path) precisely so its own deliberate restarts aren't the ones this
// guard suppresses.
// Crash-loop backoff for the streamer's exit callback.
const (
	// Exits closer together than this count as one crash loop.
	streamerCrashLoopWindow = 60 * time.Second
	streamerCrashLoopBase   = 500 * time.Millisecond
	streamerCrashLoopMax    = 30 * time.Second
)

// restartStreamerAfterExit is the streamer's exit callback
// (streamhost.ProcessWatcher.SetOnExit). One crash is restarted at once, so a
// connected client sees as little dead air as possible. A streamer that keeps
// dying right after it starts (an unwritable config dir, a broken binary) was
// relaunched with no pause at all -- about 15 times a second, thousands of
// starts, observed with punktfunk-host on Windows. Exits within
// streamerCrashLoopWindow of the previous one wait 0.5s, 1s, 2s ... up to 30s;
// sunshineWatchdog's own tick may restart it sooner, at its usual pace.
func (a *App) restartStreamerAfterExit() {
	a.streamerExitMu.Lock()
	now := time.Now()
	if !a.streamerLastExit.IsZero() && now.Sub(a.streamerLastExit) < streamerCrashLoopWindow {
		a.streamerExitStreak++
	} else {
		a.streamerExitStreak = 0
	}
	a.streamerLastExit = now
	streak := a.streamerExitStreak
	a.streamerExitMu.Unlock()

	if streak == 0 {
		a.startSunshine()
		return
	}
	delay := streamerCrashLoopBase << min(streak-1, 6)
	if delay > streamerCrashLoopMax {
		delay = streamerCrashLoopMax
	}
	if streak == 1 || streak%10 == 0 {
		log.Printf("[app] streamer exited %d times in a row within %s of each other -- next restart in %s", streak+1, streamerCrashLoopWindow, delay)
	}
	time.AfterFunc(delay, a.startSunshine)
}

func (a *App) startSunshine() {
	a.entMu.Lock()
	updateInProgress := a.entStatus.RustShineUpdateInProgress
	a.entMu.Unlock()
	if updateInProgress {
		log.Printf("[app] startSunshine skipped -- rustshine update in progress, the update flow owns restarting it")
		return
	}
	a.startSunshineNow()
}

// healCapExecLaunch restarts a RustShine streamer that ended up running by
// plain exec -- no CAP_SYS_ADMIN, so KMS capture fails with "framebuffer has
// no exportable plane-0 handle" -- although the verified launcher is set,
// i.e. some start raced SetCapExecPath (see syncCapExecTo). Belt and braces
// for any launch path, including the restarts done by the update flow.
func (a *App) healCapExecLaunch() {
	a.entMu.Lock()
	updateInProgress := a.entStatus.RustShineUpdateInProgress
	a.entMu.Unlock()
	if updateInProgress {
		return
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	b, ok := a.stream.(interface{ LaunchedWithoutCapExec() bool })
	if !ok || !b.LaunchedWithoutCapExec() {
		return
	}
	log.Printf("[app] rustshine: streamer runs without usbridge-streamer-launch (no CAP_SYS_ADMIN) although the launcher is ready -- restarting it through the launcher")
	if err := a.RestartSunshine(); err != nil {
		log.Printf("[app] rustshine: restart through the launcher failed: %v", err)
	}
}

// startSunshineNow is startSunshine's actual body, callable directly by the
// RustShine update flow (see startSunshine's doc comment for why those
// callers need to bypass the RustShineUpdateInProgress guard rather than
// trip over it).
func (a *App) startSunshineNow() {
	if a.stream == nil {
		return
	}
	// Sync bind_address with external_ip so Sunshine only binds streaming
	// ports to the configured IP — but never to the agent's own tsnet IP:
	// tsnet is a userspace-only netstack with no kernel interface for that
	// address, so a real kernel bind() to it fails (or silently binds
	// nowhere reachable) on any host that isn't also running a full system
	// Tailscale client. Reachability over tsnet is handled instead by
	// restartStreamProxy, which relays tsnet's netstack to Sunshine's real
	// 127.0.0.1 ports.
	if tsIP := a.stream.ExternalIP(); tsIP != "" && tsIP != "0.0.0.0" && !a.isTsnetSelfIP(tsIP) {
		if err := a.stream.SetBindAddress(tsIP); err != nil {
			log.Printf("[app] warning: could not set Sunshine bind address: %v", err)
		}
	}
	// Runs on every call, including the no-op "already running" path inside
	// Start() below: a stuck-in-an-encoder-retry-loop Sunshine never exits
	// on its own (see reconcileOutputName's doc comment), so this periodic
	// tick from sunshineWatchdog is the only thing that ever re-checks and
	// fixes a stale output_name for an already-"running" process.
	a.reconcileOutputName()
	a.reconcileAudioSink()
	// A stream host with its own virtual monitor reads "only the virtual monitor"
	// from its own settings when it starts (Punktfunk's display policy).
	if o, ok := a.stream.(virtualMonitorOwner); ok && o.OwnsVirtualMonitor() {
		if err := a.stream.SetVirtualDisplayExclusive(a.cfg.VirtualDisplayExclusive); err != nil {
			log.Printf("[app] only the virtual monitor -> stream host: %v", err)
		}
	}
	a.applyNvencPrefs(a.stream, a.streamKind)
	// A Sunshine that failed every encoder with the old value keeps running
	// without ever re-reading its config, so restart it on a change.
	if a.applySunshineEncoder(a.stream, a.streamKind) && a.stream.Running() {
		if err := a.RestartSunshine(); err != nil {
			log.Printf("[app] restart Sunshine after encoder change: %v", err)
		}
	}
	if a.streamKind == "vibepollo" && !forkrelease.VibepolloStaged(a.cfg.StateDir) {
		// Vibepollo was uninstalled since it was picked: nothing to start.
		log.Printf("[app] vibepollo is not downloaded; not starting it")
		return
	}
	if a.streamKind == "sunshine" && a.sunshineMissing() {
		// First start of an agent that doesn't ship Sunshine: fetch it,
		// then start it.
		a.fetchSunshineInBackground()
		return
	}
	if err := a.stream.Start(a.cfg.SunshinePort); err != nil {
		log.Printf("[app] failed to start Sunshine: %v", err)
	} else {
		a.applyGPUClockLock()
	}
	// startSunshine() is invoked unconditionally every sunshineWatchdogInterval
	// (see sunshineWatchdog) and is a no-op whenever Sunshine is already
	// reachable — which is true almost every tick. Only (re)build the tsnet
	// relay when it isn't already up: rebuilding it on every no-op tick tore
	// down and recreated its listeners mid-stream, racing the new listen
	// against the old one's async netstack teardown and intermittently
	// failing with "port is in use" — surfacing as flaky input (ENet control
	// relay) and video (RTP relay) during an active session.
	if a.tsProxy == nil {
		a.restartStreamProxy()
	}
}

// sunshineWatchdogInterval is how often sunshineWatchdog re-checks that
// Sunshine is still alive. Short enough to recover within a bounded time
// after a crash, long enough that a spell of Sunshine being genuinely busy
// (e.g. mid capability-probe on every new session negotiation) never looks
// like a restart storm.
const sunshineWatchdogInterval = 15 * time.Second

// sunshineWatchdog periodically re-invokes startSunshine so a crashed
// Sunshine process gets relaunched automatically instead of leaving
// streaming broken until the agent itself is restarted. This matters
// because the backend's Start()'s "already running" fast path only reflects
// reality once the exited process's Wait() goroutine has cleared its cmd
// (see streamhost.NewSunshine's Start) -- after that, calling startSunshine() again
// here is what actually notices and relaunches it. startSunshine() itself
// is always safe to call repeatedly: it no-ops whenever Sunshine (ours or
// externally managed) is already reachable.
func (a *App) sunshineWatchdog(ctx context.Context) {
	if a.stream == nil {
		return
	}
	ticker := time.NewTicker(sunshineWatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.startSunshine()
			a.healCapExecLaunch()
		}
	}
}

// x11SessionEnvRefreshInterval is how often x11SessionEnvWatchdog re-syncs
// the X11 fallback's greeter-env files (see RefreshX11SessionEnv's doc
// comment for why this needs to happen at all). Much shorter than
// sunshineWatchdogInterval deliberately: this used to piggyback on that 15s
// ticker, but every one of those 15 seconds is dead air on a connected
// client's screen during the exact SDDM login/logout window this exists
// for (confirmed live) -- a dedicated, much tighter interval is what
// actually shrinks that window, and capture-kms's own
// BLANK_RECHECK_INTERVAL (250ms, only engaged once the *capture* side has
// independently noticed several seconds of blank content) is the other,
// finer-grained half of that same fix. Still cheap enough (a /proc scan,
// usually no writes at all once a session has settled) to run this often.
const x11SessionEnvRefreshInterval = 2 * time.Second

// x11SessionEnvWatchdog periodically re-syncs RefreshX11SessionEnv on its
// own schedule (see x11SessionEnvRefreshInterval's doc comment for why this
// isn't just folded into sunshineWatchdog), and piggybacks
// EnsureDisplayActive on the same tick -- confirmed live: a fresh SDDM login
// can leave a physical output completely dark (no active mode at all) even
// though the remote KMS capture keeps working fine, because the desktop
// environment's own display auto-configuration on session start doesn't
// reliably reapply whatever mode was working before. No-op on non-Linux
// builds.
func (a *App) x11SessionEnvWatchdog(ctx context.Context) {
	ticker := time.NewTicker(x11SessionEnvRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			autostart.RefreshX11SessionEnv()
			autostart.EnsureDisplayActive()
		}
	}
}

// isTsnetSelfIP reports whether host is the agent's own embedded tsnet
// node's tailnet IP (as opposed to a LAN IP or a different tailnet peer).
func (a *App) isTsnetSelfIP(host string) bool {
	if a.ts == nil {
		return false
	}
	srv, err := a.ts.Server()
	if err != nil {
		return false
	}
	ip4, _ := srv.TailscaleIPs()
	return ip4.IsValid() && ip4.String() == strings.TrimSpace(host)
}

// restartStreamProxy stops any running tsnet↔Sunshine stream relay and, if
// Tailscale is enabled, starts a new one bound to the current Sunshine
// stream port. Safe to call whenever Sunshine's port or Tailscale's
// enablement may have changed.
func (a *App) restartStreamProxy() {
	if a.tsProxy != nil {
		a.tsProxy.Stop()
		a.tsProxy = nil
	}
	if a.ts == nil || !a.cfg.TailscaleEnabled {
		return
	}
	basePort := a.cfg.SunshinePort - 1 // SunshinePort is the admin port; NvHTTP base = admin - 1
	usbPort := a.cfg.UsbPassthroughPort
	if usbPort <= 0 {
		usbPort = usbpass.DefaultURBPort
	}
	var usbLocalPort func() int
	if a.usbBroker != nil {
		usbLocalPort = a.usbBroker.ListenPort
	}
	a.tsProxy = a.ts.StartStreamProxy(basePort, a.usbBridge, usbLocalPort, usbPort)
}

func (a *App) initTailscale(ctx context.Context) {
	if a.ts == nil {
		return
	}
	if a.cfg.TailscaleEnabled {
		go a.startTailscaleHTTP(ctx)
		// startSunshineTSNetForwarding is NOT started here: it binds the same
		// fixed ports (Sunshine/rustshine's NvHTTP/RTSP/control/video/audio
		// set) on the same tsnet.Server as StreamProxy (see restartStreamProxy,
		// called from startSunshine/sunshineWatchdog). Running both raced two
		// independent ListenPacket registrations for 47998/47999/48000 --
		// confirmed live: tsnet's netstack kept accepting the client's video/
		// audio ping packets ("[v1] Accept: UDP{...:47998} ok" in the tsnet
		// log) indefinitely, but gamestream-server never saw them ("waiting
		// for client video ping" timing out every ~4.5s, forever) because
		// forwardSunshineUDPPort's reader goroutine had silently died (no
		// error logging on ReadFrom failure) the moment StreamProxy's own
		// later bind attempt on the same port collided with it. StreamProxy
		// alone already covers this port set (TCP http/https/rtsp directly,
		// UDP control directly, UDP video/audio via RTSP SETUP-response
		// snooping) and additionally retries through the ECONNREFUSED
		// startup race -- this generic forwarder is redundant, and the two
		// running together is strictly worse than StreamProxy alone.
	}
}

func (a *App) startTailscaleHTTP(_ context.Context) {
	tsSrv, err := a.ts.Server()
	if err != nil {
		log.Printf("[app] tsnet server unavailable: %v", err)
		return
	}
	ln, err := tsSrv.Listen("tcp", fmt.Sprintf(":%d", a.cfg.HTTPPort))
	if err != nil {
		log.Printf("[app] tsnet listen error: %v", err)
		return
	}
	log.Printf("[app] tsnet http listening on :%d", a.cfg.HTTPPort)
	if err := a.tsHTTP.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Printf("[app] tsnet http server error: %v", err)
	}
}

// handleShutdown is used by the GUI-owning path only: it waits for ctx to be
// cancelled (e.g. the window's close intercept calling cancel), tears down
// the engine, then quits the Fyne app loop so ShowAndRun's blocking call in
// Run returns. The headless path calls shutdownEngine directly instead,
// since there's no Fyne loop to quit.
func (a *App) handleShutdown(ctx context.Context, cancel context.CancelFunc) {
	<-ctx.Done()
	a.shutdownEngine()
	fyne.Do(func() {
		a.fyneApp.Quit()
	})
}

// shutdownEngine tears down everything Run started except the GUI: the HTTP
// server(s), Sunshine, the tsnet stream proxy/service, and the admin socket.
func (a *App) shutdownEngine() {
	_ = a.server.Shutdown(context.Background())
	if a.tsHTTP != nil && a.tsHTTP.Addr != "" {
		_ = a.tsHTTP.Shutdown(context.Background())
	}
	if a.tsProxy != nil {
		a.tsProxy.Stop()
	}
	if a.ts != nil {
		_ = a.ts.Close()
	}
	if a.stream != nil {
		_ = a.stream.Stop()
	}
	if a.usbBroker != nil {
		a.usbBroker.Stop()
	}
	if a.adminSrv != nil {
		_ = a.adminSrv.Close()
	}
}

func (a *App) RegenerateMasterKey() (config.Config, error) {
	key, err := api.GenerateMasterKey()
	if err != nil {
		return a.cfg, fmt.Errorf("generate master key: %w", err)
	}
	next := a.cfg
	next.MasterKey = key
	if err := a.SaveConfig(next); err != nil {
		return a.cfg, fmt.Errorf("save config: %w", err)
	}
	// Apply to the already-running HTTP server too — without this, the new
	// key only takes effect after a manual restart (SecurityMiddleware and
	// the Sync/AES-GCM handler both keep verifying against whatever key was
	// baked in at process start), even though config.yaml and a.cfg already
	// hold the new one.
	if a.apiServer != nil {
		a.apiServer.SetMasterKey([]byte(key))
	}
	// rustshine's own native WebRTC signaling endpoint (POST /webrtc/offer)
	// authenticates against a copy of the master key handed to it at launch
	// (see rustshineBackend.SetSharedSecret's doc comment) -- refresh it
	// here too, before RestartSunshine below picks it up on the next
	// Start(), same as apiServer.SetMasterKey does for the agent's own API.
	applyStreamSharedSecret(a.stream, []byte(key))
	// A regenerated master key is meant to revoke access wholesale, but
	// Sunshine's Moonlight pairing is its own cert-based handshake that the
	// master key never governed — a client paired under the old key would
	// otherwise keep streaming right through the rotation. Best-effort and
	// non-fatal: the key has already been rotated above regardless of
	// whether any of this succeeds.
	if clients, err := a.ListSunshineClients(); err == nil {
		for _, c := range clients {
			_ = a.UnpairSunshineClient(c.UniqueID)
		}
	}
	// Unpairing alone only stops *future* pairing/reconnect attempts — a
	// session that was already streaming keeps going, since Sunshine
	// doesn't re-check its authorized-client list against connections it
	// already accepted. Restarting the stream host is what actually drops
	// those live sessions, so the key rotation above can't be quietly
	// undermined by a session that predates it.
	if err := a.RestartSunshine(); err != nil {
		log.Printf("[app] failed to restart stream host after master key regeneration: %v", err)
	}
	return a.cfg, nil
}

// SunshineBinaryPath returns the path to the bundled Sunshine binary, or ""
// if it isn't present (not bundled, or unsupported OS).
func (a *App) SunshineBinaryPath() string {
	if a.stream == nil {
		return ""
	}
	path := a.stream.BinaryPath()
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// SunshineCapExecPath returns the active backend's KMS grant target (see
// kmsCaptureTarget) if it exists on disk, or "".
func (a *App) SunshineCapExecPath() string {
	if a.stream == nil {
		return ""
	}
	path := a.stream.CapExecPath()
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// syncSunshineCaptureMode writes the capture mode into the active backend's
// own config file. On Linux this is never taken from user input or a stale
// persisted preference — it's unconditionally re-derived from the live
// session via capture.AutoCaptureMode() and (re)written on every start, even
// over an existing on-disk value, so a value written by some earlier version
// of this agent (back when the mode was user-selectable, e.g. a leftover
// "capture = kms" in sunshine.conf from a desktop X11 session) self-heals on
// the next start instead of staying wrong forever. See capture.AutoCaptureMode's
// doc for why Wayland/headless/SDDM/console all keep getting "kms" while a
// live X11 desktop session gets "x11".
//
// Non-Linux platforms have no such concept (AutoCaptureMode returns ""
// there): there, the old behavior is kept exactly — only fill in the
// backend's capture mode from the persisted cfg value when the backend
// doesn't already have its own value set, so switching streamers still
// propagates a preference instead of overwriting it every start.
func (a *App) syncSunshineCaptureMode() {
	a.syncCaptureModeTo(a.stream)
}

// syncCaptureModeTo is syncSunshineCaptureMode for an explicit backend, so
// SetStreamBackend can configure the next backend before publishing it.
func (a *App) syncCaptureModeTo(b streamhost.Backend) {
	if b == nil {
		return
	}
	if mode := capture.AutoCaptureMode(); mode != "" {
		if err := b.SetCaptureMode(mode); err != nil {
			log.Printf("[app] failed to sync capture mode %q to backend: %v", mode, err)
		}
		return
	}
	if b.CaptureMode() != "" || a.cfg.SunshineCaptureMode == "" {
		return
	}
	mode := a.cfg.SunshineCaptureMode
	if err := b.SetCaptureMode(mode); err != nil {
		log.Printf("[app] failed to sync capture mode %q to backend: %v", mode, err)
	}
}

// syncSunshineCapExec sets or clears the backend's usbridge-streamer-launch
// path so Start launches the streamer with CAP_SYS_ADMIN exactly when the
// capture mode is "kms" AND the launcher can actually run it right now --
// never based on mode alone, since the launcher exits with an error when it
// can't (not installed, bundle not verifying), which would stop the stream
// from starting at all instead of gracefully running without KMS.
func (a *App) syncSunshineCapExec() {
	a.syncSunshineCapExecFor(a.currentStreamKind())
}

// syncSunshineCapExecFor is syncSunshineCapExec's lock-free core, taking the
// active backend kind as a parameter instead of resolving it itself via
// currentStreamKind() -- SetStreamBackend already holds streamMu and already
// knows kind when it calls this, and currentStreamKind() takes that same
// (non-reentrant) lock, so calling it from inside SetStreamBackend deadlocks
// that goroutine forever (confirmed live: every other caller of
// currentStreamKind()/EntitlementStatus(), including the GUI's own hover
// handler and the background update watchdog, then piles up waiting on
// streamMu too -- this is the "Changing protocol"/"Check update" hang).
func (a *App) syncSunshineCapExecFor(kind string) {
	a.syncCapExecTo(a.stream, kind)
}

// syncCapExecTo is syncSunshineCapExecFor for an explicit backend. SetStreamBackend
// calls it on the next backend *before* publishing it as a.stream: the
// sunshineWatchdog calls startSunshine() without streamMu, and a tick that
// landed between `a.stream = next` and this call started the streamer by
// plain exec, i.e. without CAP_SYS_ADMIN -- KMS capture then fails with
// "framebuffer has no exportable plane-0 handle" (confirmed live after a
// benchmark Sunshine -> RustShine switch).
func (a *App) syncCapExecTo(b streamhost.Backend, kind string) {
	if b == nil {
		return
	}
	// RustShine goes through the root-owned usbridge-streamer-launch
	// instead (see kmsCaptureTarget), and only once that launcher itself
	// verified the staged signed bundle -- otherwise a plain exec, which
	// still honors a legacy setcap directly on the streamer binary.
	if kind == "rustshine" {
		b.SetCapExecPath(a.rustshineLauncherPathFor(b))
		return
	}
	// Sunshine: launch through the launcher whenever the root-owned tree is
	// installed, even if it's older than the bundled Sunshine (then
	// KMSCaptureGranted reports false so the UI offers a refresh) -- an
	// agent update must not cost KMS capture on a remote session.
	if a.captureModeOf(b) == "kms" && a.perms != nil && a.perms.SunshineLaunchReady() {
		b.SetCapExecPath(streamerlaunch.InstallPath)
	} else {
		b.SetCapExecPath("")
	}
}

// SunshineCaptureMode returns the configured Linux capture backend ("",
// "portal", or "kms"), read from sunshine.conf if present, falling back to
// the persisted agent config.
func (a *App) SunshineCaptureMode() string {
	return a.captureModeOf(a.stream)
}

// captureModeOf is SunshineCaptureMode for an explicit backend.
func (a *App) captureModeOf(b streamhost.Backend) string {
	if b != nil {
		if mode := b.CaptureMode(); mode != "" {
			return mode
		}
	}
	return a.cfg.SunshineCaptureMode
}

// SetSunshineCaptureMode re-applies the capture backend into both the agent
// config and Sunshine's own sunshine.conf, then restarts the bundled
// Sunshine instance so the change actually takes effect — a config edit
// alone is silently ignored by an already-running Sunshine process.
//
// On Linux the requested mode is not a free-form choice: it's overridden by
// capture.AutoCaptureMode() so this can't be pointed at a combination known
// not to work (see its doc). The mode argument only takes effect on other
// platforms, where AutoCaptureMode returns "" and has no opinion.
//
// Switching to "kms" without the capability granted yet is deliberately NOT
// restarted here: Sunshine would immediately fail KMS and fall back to
// portal, popping its portal permission dialog right after the user picked
// KMS — confusing, and pointless since RequestKMSCapture already restarts
// once the capability is actually granted.
func (a *App) SetSunshineCaptureMode(mode string) error {
	if a.stream == nil {
		return nil
	}
	if auto := capture.AutoCaptureMode(); auto != "" {
		mode = auto
	}
	if err := a.stream.SetCaptureMode(mode); err != nil {
		return fmt.Errorf("write sunshine.conf: %w", err)
	}
	next := a.cfg
	next.SunshineCaptureMode = mode
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	a.syncSunshineCapExec()
	if mode == "kms" && !a.KMSCaptureGranted() {
		return nil
	}
	if err := a.RestartSunshine(); err != nil {
		log.Printf("[app] failed to restart Sunshine after capture mode change: %v", err)
	}
	return nil
}

// SendSAS raises a Secure Attention Sequence (Ctrl+Alt+Del) in whatever
// session is currently on the physical console -- see sasinput's package
// doc for why this exists at all: on a Windows machine with "require
// CTRL+ALT+DEL" enabled, this is the one input a client's ordinary keyboard
// injection can never reach on its own (Windows deliberately blocks
// synthesized input from raising SAS), so a client stuck looking at that
// prompt after the target locked has no way to actually get to the
// password field without this. Windows-only; every other platform's
// sasinput implementation just returns an error (there's no equivalent
// concept to raise).
func (a *App) SendSAS() error {
	return sasinput.SendSecureAttentionSequence()
}

// RestartSunshine stops and relaunches the bundled Sunshine instance (if the
// agent owns its lifecycle) so a config or capability change takes effect.
func (a *App) RestartSunshine() error {
	if a.stream == nil {
		return nil
	}
	a.stopStreamAndWait(a.stream)
	return a.RestartSunshineStartOnly()
}

// RestartSunshineStartOnly is RestartSunshine's second half, for a caller
// that already stopped the stream (and timed that separately): starts it
// and waits until it's reachable.
func (a *App) RestartSunshineStartOnly() error {
	if a.stream == nil {
		return nil
	}
	err := a.stream.Start(a.cfg.SunshinePort)
	if err == nil {
		a.applyGPUClockLock()
	}
	// Start() only waits for the OS to fork the Sunshine process, not for its
	// own bootstrap (config parse, KMS/Wayland monitor enumeration, binding
	// its HTTPS/RTSP listeners) to finish. Callers of RestartSunshine (e.g.
	// SetSunshineOutputName, switching the captured monitor) return straight
	// to a client that immediately reconnects — without this wait, that
	// reconnect can race Sunshine's bootstrap and land a session whose
	// control-stream encryption never gets fully wired up, which then fails
	// to decrypt every subsequent input packet for the rest of that session.
	// Waiting here for the same admin port the client's own Launch() call
	// hits closes that window.
	if err == nil {
		a.stream.WaitReady(a.cfg.SunshinePort, streamReadyTimeout)
		a.waitForMonitorCorrelation()
	}
	a.restartStreamProxy()
	return err
}

// SetStreamBackend switches the active streamhost.Backend at runtime
// between "sunshine", "rustshine", "punktfunk" and "vibepollo" -- stops whichever is
// running, builds the other, and starts it. No-op if kind is already active. "rustshine"
// requires the binary to already be staged (see entitlement.StageRustShine)
// -- this method never downloads it itself, so callers (the GUI's license
// dialog) must download-then-switch, not switch-then-download.
//
// Persists the choice as cfg.PreferredBackend so a restart picks it back up
// (see New()'s local, offline re-derivation of the initial backend) without
// needing to ask the entitlement backend again just to boot.
func (a *App) SetStreamBackend(kind string) error {
	if kind != "sunshine" && kind != "rustshine" && kind != "punktfunk" && kind != "vibepollo" {
		return fmt.Errorf("unknown stream backend %q", kind)
	}
	// Vibepollo is downloaded when picked (DownloadVibepollo), like Punktfunk.
	if kind == "vibepollo" && !forkrelease.VibepolloStaged(a.cfg.StateDir) {
		if forkrelease.VibepolloAssetName() == "" {
			return fmt.Errorf("vibepollo is not available on this platform")
		}
		if err := a.DownloadVibepollo(nil); err != nil {
			return err
		}
	}
	// Punktfunk is downloaded when picked (DownloadPunktfunk), not shipped
	// with the agent -- also when another punktfunk-host is on PATH (an
	// upstream install): that one has no PyroWave, which the fork adds. It
	// is only the fallback when the download fails.
	if kind == "punktfunk" && forkrelease.PunktfunkAssetName() != "" && !forkrelease.PunktfunkStaged(a.cfg.StateDir) {
		if err := a.DownloadPunktfunk(nil); err != nil {
			if !streamhost.PunktfunkAvailable(a.exeDir) {
				return err
			}
			log.Printf("[app] punktfunk fork not downloaded (%v) -- using the punktfunk-host found on this machine", err)
		}
	} else if kind == "punktfunk" && !streamhost.PunktfunkAvailable(a.exeDir) {
		if err := a.DownloadPunktfunk(nil); err != nil {
			return err
		}
	}
	// So is Sunshine where the agent doesn't ship it (sunshineDownloadable).
	if kind == "sunshine" && sunshineDownloadable() && !a.sunshineOnDisk() {
		if err := a.DownloadSunshine(nil); err != nil {
			return err
		}
	}

	a.streamMu.Lock()
	defer a.streamMu.Unlock()

	a.lastSwitch = api.BackendSwitchTiming{}
	if kind == a.streamKind {
		if a.benchRestartPending && a.stream != nil {
			// A deferred benchmark monitor change is due now.
			a.benchRestartPending = false
			a.lastSwitch.Stopped = kind
			stopStart := time.Now()
			a.stopStreamAndWait(a.stream)
			a.lastSwitch.StopMs = time.Since(stopStart).Milliseconds()
			startStart := time.Now()
			err := a.RestartSunshineStartOnly()
			a.lastSwitch.StartMs = time.Since(startStart).Milliseconds()
			return err
		}
		return nil
	}
	if kind == "rustshine" {
		if _, err := os.Stat(entitlement.StagePath(a.cfg.StateDir)); err != nil {
			return fmt.Errorf("rustshine is not downloaded yet")
		}
	}
	if kind == "punktfunk" && !streamhost.PunktfunkAvailable(a.exeDir) {
		return fmt.Errorf("punktfunk-host is not installed")
	}

	// The two backends number monitors differently (Sunshine: its own KMS
	// index; RustShine: "cardPath|connector"), so each keeps its own pin
	// and a switch would otherwise come up on whatever the other one was
	// last left on -- live: RustShine on HDMI-A-1, then Sunshine captured
	// its stale "0", which by then was the vkms Virtual-1 (cursor on black).
	pinnedConnector := ""
	if a.stream != nil {
		pinnedConnector = pinConnector(a.stream)
	}

	stopStart := time.Now()
	if a.stream != nil {
		a.lastSwitch.Stopped = a.streamKind
		a.stopStreamAndWait(a.stream)
	}
	a.lastSwitch.StopMs = time.Since(stopStart).Milliseconds()
	// The stopped streamer's pending restart went with it; the new one
	// starts on the current configuration anyway.
	a.benchRestartPending = false
	startStart := time.Now()
	defer func() { a.lastSwitch.StartMs = time.Since(startStart).Milliseconds() }()

	var next streamhost.Backend
	switch kind {
	case "rustshine":
		next = streamhost.NewRustshine(a.exeDir, a.cfg.StateDir, a.logPath)
		applyStreamSharedSecret(next, []byte(a.cfg.MasterKey))
		applyStreamWebRTCEnabled(next, !a.cfg.RustShineWebRTCDisabled)
		applyStreamUSBPassBridgeAddr(next, a.usbPassBridgeAddr)
	case "punktfunk":
		next = streamhost.NewPunktfunk(a.exeDir, a.cfg.StateDir, a.logPath)
	case "vibepollo":
		next = streamhost.NewVibepollo(a.exeDir, a.cfg.StateDir, a.logPath)
	default:
		next = streamhost.NewSunshine(a.exeDir, a.cfg.StateDir, a.logPath)
	}
	// Fully configure next before publishing it -- see syncCapExecTo.
	a.syncCaptureModeTo(next)
	a.syncCapExecTo(next, kind)
	a.stream = next
	a.setStreamKind(kind)
	if a.screen != nil {
		a.screen.SetDevices(next)
	}
	if pw, ok := next.(streamhost.ProcessWatcher); ok {
		pw.SetOnExit(a.restartStreamerAfterExit)
	}
	carryPin(next, pinnedConnector)
	// Before the start, so the backend comes up on the benchmark's monitor.
	_, benchPinned := a.applyBenchMonitor(next, kind)

	a.startSunshine() // generic despite the name -- starts whatever a.stream now is

	// Mirrors RestartSunshine's own wait, for the same reason (see its doc
	// comment): startSunshine only waits for the OS to fork the new
	// backend's process, not for its own bootstrap (config parse, capture
	// device enumeration, binding its HTTPS/RTSP/pairing listeners) to
	// finish. Without this, a client that reconnects the instant this call
	// returns can hit a port the new backend hasn't bound yet -- observed
	// live as the client's pairing GET stalling for its full ~45s HTTP
	// timeout, then one more failed attempt (RTSP DESCRIBE -1), before a
	// third attempt finally landed after the backend had caught up on its
	// own. WaitReady closes that window instead of relying on the client's
	// own retry/backoff to eventually paper over it.
	a.stream.WaitReady(a.cfg.SunshinePort, streamReadyTimeout)
	a.waitForMonitorCorrelation()
	if !benchPinned && a.benchMonitor == "" && carryPin(a.stream, pinnedConnector) {
		// Sunshine's index for the connector is only known for certain from
		// its own log of this start.
		if err := a.RestartSunshine(); err != nil {
			log.Printf("[app] restarting %s on %s: %v", kind, pinnedConnector, err)
		}
	}
	if !benchPinned && a.benchMonitor != "" {
		// Sunshine names monitors by a GUID only its own log reveals, so a
		// Sunshine that never ran here could only be pinned once it's up.
		if changed, ok := a.applyBenchMonitor(a.stream, kind); ok && changed {
			if err := a.RestartSunshine(); err != nil {
				log.Printf("[bench] restarting %s on monitor %s: %v", kind, a.benchMonitor, err)
			}
		}
	}
	a.restartStreamProxy()

	saved := a.cfg
	saved.PreferredBackend = kind
	if err := a.SaveConfig(saved); err != nil {
		log.Printf("[app] warning: failed to persist preferred stream backend: %v", err)
	}
	return nil
}

// pinConnector is the connector name ("HDMI-A-1") b is pinned to capture, or
// "" when the pin is not a connector (unset, a virtual display spec, or a
// platform without correlation by name).
func pinConnector(b streamhost.Backend) string {
	pin := b.OutputName()
	if pin == "" || strings.HasPrefix(pin, "virtual:") {
		return ""
	}
	if _, conn, ok := strings.Cut(pin, "|"); ok {
		return conn
	}
	for _, d := range b.ListCaptureDevices() {
		if d.OutputName == pin {
			return d.Key
		}
	}
	return ""
}

// carryPin pins b to connector in b's own numbering, when b knows the
// connector and is pinned elsewhere. Reports whether the pin changed.
func carryPin(b streamhost.Backend, connector string) bool {
	if connector == "" {
		return false
	}
	for _, d := range b.ListCaptureDevices() {
		if d.Key != connector || d.OutputName == "" {
			continue
		}
		if d.OutputName == b.OutputName() {
			return false
		}
		if err := b.SetOutputName(d.OutputName); err != nil {
			log.Printf("[app] carrying the %s pin over: %v", connector, err)
			return false
		}
		log.Printf("[app] output pin %s carried over as %q", connector, d.OutputName)
		return true
	}
	return false
}

// streamReadyTimeout bounds how long a backend (re)start waits for the
// backend to bind its listeners before telling clients it's up. WaitReady
// returns as soon as the port answers, so this only matters for a slow
// start: Sunshine 2026.9 takes ~25s (gamepad driver probing plus encoder
// probing) before it binds anything, and with the old 5s the client
// reconnected into closed ports and gave up on the stream.
const streamReadyTimeout = 45 * time.Second

// currentStreamKind is the active backend kind, readable without waiting
// for a backend switch in progress (see streamKindView). During a switch it
// already names the backend being started.
// LastBackendSwitch is the timing of the most recent SetStreamBackend.
func (a *App) LastBackendSwitch() api.BackendSwitchTiming {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	return a.lastSwitch
}

// stopStreamAndWait stops b and waits until its ports are free, so the next
// Start (of it or of the other backend) never overlaps the old process.
// Replaces a fixed 1 s sleep. The short settle after the ports free up is
// for Start()'s "already running" fast path, which only reflects reality
// once the exited process's Wait() goroutine has cleared its handle.
func (a *App) stopStreamAndWait(b streamhost.Backend) {
	_ = b.Stop()
	tcp, udp := b.Ports(a.cfg.SunshinePort - 1)
	start := time.Now()
	if !streamhost.WaitPortsFree(tcp, udp, streamPortsFreeTimeout) {
		log.Printf("[app] %s's ports %v/%v still held %s after stop -- starting anyway", b.DisplayName(), tcp, udp, streamPortsFreeTimeout)
	} else if waited := time.Since(start); waited > time.Second {
		log.Printf("[app] %s released its ports after %s", b.DisplayName(), waited.Round(time.Millisecond))
	}
	time.Sleep(100 * time.Millisecond)
}

// streamPortsFreeTimeout bounds stopStreamAndWait's wait for a stopped
// backend's ports.
const streamPortsFreeTimeout = 15 * time.Second

func (a *App) currentStreamKind() string {
	if kind, _ := a.streamKindView.Load().(string); kind != "" {
		return kind
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	return a.streamKind
}

// setStreamKind records the active backend kind. Caller holds streamMu, or
// is still constructing the App.
func (a *App) setStreamKind(kind string) {
	a.streamKind = kind
	a.streamKindView.Store(kind)
}

func (a *App) rustshineStaged() bool {
	_, err := os.Stat(entitlement.StagePath(a.cfg.StateDir))
	return err == nil
}

// refreshLocalEntitlementStatus re-derives entStatus's Linked/Tier/ExpiresAt
// fields purely from cfg.EntitlementToken (local Ed25519 verify, no
// network) -- called after anything that changes that token, and once at
// startup. LinkInProgress/DownloadInProgress/LastError are process-only
// state, untouched here.
//
// Also the single place that mirrors cfg.EntitlementToken out to
// entitlement.TokenFilePath (see that function's doc comment) -- every
// call site that changes the token already calls this afterward, so
// centralizing the file write here means none of them need to remember to
// do it themselves. Only written while the token verifies locally as
// current -- there's no reason to hand a consumer something already
// known-bad.
func (a *App) refreshLocalEntitlementStatus() {
	a.entMu.Lock()
	defer a.entMu.Unlock()

	hwID, hwErr := hwid.Get()
	if hwErr != nil {
		// Can't determine this machine's own identity -- nothing to bind
		// a token to, so there is no way this install can ever be
		// considered entitled. Logged once per call (cheap, this isn't a
		// hot path) rather than cached/silenced, since a persistently
		// failing hwid.Get() is a real configuration problem worth seeing
		// in logs (e.g. a locked-down registry on Windows).
		log.Printf("[app] warning: could not determine hardware id, entitlement disabled: %v", hwErr)
		a.entStatus.Linked = false
		a.entStatus.Tier = ""
		a.entStatus.ExpiresAt = time.Time{}
		return
	}

	if claims, err := entitlement.VerifyForHardware(a.cfg.EntitlementToken, hwID); err == nil {
		a.entStatus.Linked = true
		a.entStatus.Tier = claims.Tier
		a.entStatus.ExpiresAt = time.Unix(claims.ExpireAt, 0)
		if err := entitlement.WriteTokenFile(a.cfg.StateDir, a.cfg.EntitlementToken); err != nil {
			log.Printf("[app] warning: failed to write entitlement token file for rustshine: %v", err)
		}
	} else {
		a.entStatus.Linked = false
		a.entStatus.Tier = ""
		a.entStatus.ExpiresAt = time.Time{}
	}
}

// EntitlementStatus reports the current license state for the GUI (and,
// over adminapi, a thin-client GUI attached to a separate headless engine)
// to render the four-way license switcher and, once entitled, the
// Sunshine/RustShine switch. Cheap enough to poll on the GUI's existing 2s
// refresh ticker (see ui.Window.performRefresh) -- no push/SSE channel
// needed for this.
func (a *App) EntitlementStatus() entitlement.Status {
	a.entMu.Lock()
	st := a.entStatus
	pending := a.pendingStreamerUpdate
	a.entMu.Unlock()
	st.ActiveBackend = a.currentStreamKind()
	st.RustShineStaged = a.rustshineStaged()
	// Offered wherever it's installed or Streamers-Forks publishes a build
	// for this platform (picking it downloads it, see SetStreamBackend).
	st.PunktfunkAvailable = streamhost.PunktfunkAvailable(a.exeDir) || forkrelease.PunktfunkAssetName() != ""
	st.PunktfunkStaged = forkrelease.PunktfunkStaged(a.cfg.StateDir)
	st.PunktfunkVersion = forkrelease.PunktfunkStagedVersion(a.cfg.StateDir)
	st.VibepolloAvailable = forkrelease.VibepolloAssetName() != ""
	st.VibepolloStaged = forkrelease.VibepolloStaged(a.cfg.StateDir)
	st.VibepolloVersion = forkrelease.VibepolloStagedVersion(a.cfg.StateDir)
	st.SunshineUpdatable = sunshineDownloadable()
	st.SunshineVersion = forkrelease.SunshineStagedVersion(a.cfg.StateDir)
	st.RustShineVersion = entitlement.StagedVersion(a.cfg.StateDir)
	st.WebRTCEnabled = !a.cfg.RustShineWebRTCDisabled
	st.RustShineAvailableVersion = pending
	st.RustShineUpdateOffer = pending != "" && pending != a.cfg.StreamerUpdateSnoozed
	return st
}

// SetRustShineWebRTCEnabled persists cfg.RustShineWebRTCDisabled and, if
// RustShine is the currently active backend, restarts it so the new
// --webrtc-disable flag (or its absence) actually takes effect --
// gamestream-server has no live toggle for this, it's read once at
// startup. A no-op on the running process while Sunshine is active: the
// preference is still saved either way, so switching to RustShine later
// picks it up without needing to touch this checkbox again.
func (a *App) SetRustShineWebRTCEnabled(enabled bool) error {
	saved := a.cfg
	saved.RustShineWebRTCDisabled = !enabled
	if err := a.SaveConfig(saved); err != nil {
		return fmt.Errorf("save config: %w", err)
	}

	a.streamMu.Lock()
	kind := a.streamKind
	stream := a.stream
	a.streamMu.Unlock()
	if kind != "rustshine" || stream == nil {
		return nil
	}

	applyStreamWebRTCEnabled(stream, enabled)
	if err := a.RestartSunshine(); err != nil {
		log.Printf("[app] failed to restart rustshine after webrtc toggle: %v", err)
	}
	return nil
}

func (a *App) setEntError(msg string) {
	a.entMu.Lock()
	a.entStatus.LinkInProgress = false
	a.entStatus.LastError = msg
	a.entMu.Unlock()
}

// StartFreeTrial fetches this machine's current (always-available) free
// tier token synchronously -- kept mainly for internal/adminapi's
// thin-client "start-trial" call; there is nothing left to "start", the
// backend's free tier is unconditional and permanent (see
// entitlement.StartTrial's doc comment). Also handy as a manual "refresh
// now" for the license dialog's default/no-purchase state.
func (a *App) StartFreeTrial() error {
	hwID, err := hwid.Get()
	if err != nil {
		a.setEntError(fmt.Sprintf("could not determine this machine's hardware id: %v", err))
		return err
	}

	a.entMu.Lock()
	a.entStatus.LinkInProgress = true
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	defer func() {
		a.entMu.Lock()
		a.entStatus.LinkInProgress = false
		a.entMu.Unlock()
	}()

	res, err := entitlement.StartTrial(context.Background(), hwID)
	if err != nil {
		a.setEntError(fmt.Sprintf("could not reach the entitlement backend: %v", err))
		return err
	}

	a.applyIssuedToken(res.Token, hwID)
	return nil
}

// StartPurchase asks the backend for a Stripe Checkout Session URL bound to
// this machine's hardware id for the given tier ("pro" or "enterprise")
// and begins polling for the purchase to land in the background. Callers
// (the GUI) open the returned URL in the system browser; EntitlementStatus
// reflects progress from here on (the GUI already polls it on its own
// refresh ticker) -- no separate "purchase complete" callback into this
// process, see pollForLicense.
func (a *App) StartPurchase(tier string) (string, error) {
	hwID, err := hwid.Get()
	if err != nil {
		a.setEntError(fmt.Sprintf("could not determine this machine's hardware id: %v", err))
		return "", err
	}

	checkoutURL, err := entitlement.StartCheckoutURL(context.Background(), hwID, tier)
	if err != nil {
		a.setEntError(fmt.Sprintf("could not start checkout: %v", err))
		return "", err
	}

	a.entMu.Lock()
	if a.entPollCancel != nil {
		a.entPollCancel() // a previous attempt's poll loop, if any, is now stale
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	a.entPollCancel = cancel
	a.entStatus.LinkInProgress = true
	a.entStatus.LastError = ""
	a.entMu.Unlock()

	go a.pollForLicense(pollCtx, hwID)
	return checkoutURL, nil
}

// CancelPurchase gives up on an in-flight StartPurchase: stops the
// background pollForLicense loop and clears LinkInProgress so the GUI's
// license dialog falls back to showing the trial/buy buttons again instead
// of being stuck on "waiting for checkout" -- previously there was no way
// out of that screen short of a completed purchase or pollForLicenseTimeout
// (15 minutes), even across closing and reopening the dialog, since
// LinkInProgress lives on entStatus (this App, not the dialog) and nothing
// ever cleared it if the browser tab was simply closed without paying. Safe
// to call even if nothing is in flight (entPollCancel nil, or already
// finished) -- a no-op in that case, not an error, since the GUI can't
// always tell which case it's in before offering a "Cancel" button.
func (a *App) CancelPurchase() {
	a.entMu.Lock()
	if a.entPollCancel != nil {
		a.entPollCancel()
		a.entPollCancel = nil
	}
	a.entStatus.LinkInProgress = false
	a.entStatus.LastError = ""
	a.entMu.Unlock()
}

// pollForLicenseTimeout bounds how long pollForLicense keeps checking after
// a checkout URL was opened -- generous (most Stripe webhooks land within
// seconds, but this also covers someone taking their time entering card
// details) without polling forever if the browser tab was simply closed
// without paying. The GUI's "Refresh" affordance (if the dialog is
// reopened later) calls RefreshLicense directly and picks up a late
// webhook regardless of whether this loop already gave up.
const pollForLicenseTimeout = 15 * time.Minute

// pollForLicense repeatedly asks the backend for hwID's current tier every
// 2s, until it comes back as a paid tier (anything other than "free"), ctx
// is cancelled (a newer StartPurchase call superseded this one), or
// pollForLicenseTimeout elapses. There is no server-side correlation state
// the way the old OAuth poll had (no "state" param) -- hwID itself is the
// only thing being polled for, which is also why this is safe to just call
// again from the GUI's own "Refresh" button without needing to restart a
// checkout.
//
// Checking Status != "free" here (not just "did RefreshLicense succeed")
// matters: register/refresh never fails anymore (see IssueResult's doc
// comment) -- every call, including the very first one right after opening
// a checkout tab, returns a perfectly valid "free" token. Treating any
// successful response as "done" (as this loop used to, back when
// NotLicensed was the only failure signal to check) would apply that free
// token immediately and stop polling before the purchase even completed.
func (a *App) pollForLicense(ctx context.Context, hwID string) {
	deadline := time.Now().Add(pollForLicenseTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			a.setEntError("Didn't detect a completed payment yet — if you finished checkout, try again in a moment.")
			return
		}
		res, err := entitlement.RefreshLicense(ctx, hwID)
		if err != nil {
			continue // transient network hiccup -- keep polling until the deadline or ctx cancellation
		}
		if res.Status == "free" {
			continue // payment not completed / webhook not landed yet
		}
		a.applyIssuedToken(res.Token, hwID)
		return
	}
}

// applyIssuedToken is StartFreeTrial and pollForLicense's shared tail end:
// verify (hardware-bound, see entitlement.VerifyForHardware), persist, and
// kick off the first RustShine download so there's no separate "Download
// RustShine" click required.
func (a *App) applyIssuedToken(token, hwID string) {
	if _, err := entitlement.VerifyForHardware(token, hwID); err != nil {
		a.setEntError("Received an invalid license token — try again.")
		return
	}

	next := a.cfg
	next.EntitlementToken = token
	if err := a.SaveConfig(next); err != nil {
		log.Printf("[app] warning: failed to persist entitlement token: %v", err)
	}

	a.entMu.Lock()
	a.entStatus.LinkInProgress = false
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	a.refreshLocalEntitlementStatus()

	// Start the first RustShine download immediately -- no "Download
	// RustShine" button click required -- rather than waiting for
	// whatever's left of entitlementWatchdog's 6h cadence. Backgrounded:
	// applyIssuedToken returns to pollForLicense's polling loop (or
	// StartFreeTrial's synchronous caller), either of which a GUI is
	// actively waiting on to stop showing its spinner: it shouldn't also
	// block on a multi-MB download completing first.
	go a.ensureRustShineFresh(context.Background(), token)
}

// bootstrapFreeTier silently links a fresh install (or a just-downgraded
// one, see recheckEntitlement's call sites) to today's unconditional free
// tier -- deliberately NOT applyIssuedToken: this runs with no user action
// at all (just the app starting up, or a refund just having been detected),
// so it must not also kick off an immediate multi-MB RustShine download on
// every single install regardless of whether that install ever wants
// RustShine. A later explicit tier pick in the license dialog
// (StartFreeTrial/StartPurchase's own applyIssuedToken) is what actually
// triggers that download.
//
// Returns true once a fresh, locally-verifying free token is cached; false
// on any transient failure (network down, backend unreachable, or a
// well-formed but non-verifying response), so entitlementWatchdog knows to
// retry at entitlementRetryInterval rather than leaving this install
// without a token until the next full entitlementRecheckInterval tick.
func (a *App) bootstrapFreeTier(ctx context.Context, hwID string) bool {
	res, err := entitlement.RefreshLicense(ctx, hwID)
	if err != nil {
		log.Printf("[app] entitlement bootstrap failed (will retry sooner than the next scheduled interval): %v", err)
		return false
	}
	if _, err := entitlement.VerifyForHardware(res.Token, hwID); err != nil {
		log.Printf("[app] entitlement bootstrap: backend returned a token that doesn't verify locally: %v", err)
		return false
	}

	next := a.cfg
	next.EntitlementToken = res.Token
	if err := a.SaveConfig(next); err != nil {
		log.Printf("[app] warning: failed to persist entitlement token: %v", err)
	}
	a.refreshLocalEntitlementStatus()
	// A fresh install whose installer already picked RustShine and gave USB
	// consent (preferred_backend / usb_broker_consent in config.yaml) gets
	// both right away instead of on the next recheck.
	if a.cfg.PreferredBackend == "rustshine" {
		a.ensureRustShineFresh(ctx, res.Token)
	}
	a.ensureUSBBroker(ctx, res.Token)
	return true
}

// AccountStatus returns a snapshot of the account-login state (see
// account.Status) for the GUI to render -- same "poll a status struct on a
// refresh ticker" pattern EntitlementStatus already uses, just for the
// separate account login.
func (a *App) AccountStatus() account.Status {
	a.accMu.Lock()
	st := a.accStatus
	if n := len(st.Licenses); n > 0 {
		st.Licenses = append([]account.License(nil), st.Licenses...)
	}
	a.accMu.Unlock()
	hwID, _ := hwid.Get()
	st.Licenses = account.MarkOnThisDevice(st.Licenses, hwID)
	return st
}

func (a *App) setAccError(msg string) {
	a.accMu.Lock()
	a.accStatus.LoginInProgress = false
	a.accStatus.RebindInProgress = false
	a.accStatus.LastError = msg
	a.accMu.Unlock()
}

// StartAccountLogin begins a device-code login (see internal/account's
// package doc comment) and returns the URL to open in the system browser.
// Mirrors StartPurchase's shape: kicks off a background poll goroutine and
// returns immediately, LoginInProgress on accStatus is what the GUI's
// refresh ticker watches.
func (a *App) StartAccountLogin() (string, error) {
	start, err := account.StartLogin(context.Background())
	if err != nil {
		a.setAccError(fmt.Sprintf("could not start login: %v", err))
		return "", err
	}

	a.accMu.Lock()
	if a.accPollCancel != nil {
		a.accPollCancel() // a previous attempt's poll loop, if any, is now stale
	}
	pollCtx, cancel := context.WithCancel(context.Background())
	a.accPollCancel = cancel
	a.accStatus.LoginInProgress = true
	a.accStatus.LastError = ""
	a.accMu.Unlock()

	go a.pollAccountLogin(pollCtx, start.Code)
	return start.VerificationURL, nil
}

// CancelAccountLogin mirrors CancelPurchase's doc comment exactly, for the
// account login's own in-flight poll instead of the entitlement one.
func (a *App) CancelAccountLogin() {
	a.accMu.Lock()
	if a.accPollCancel != nil {
		a.accPollCancel()
		a.accPollCancel = nil
	}
	a.accStatus.LoginInProgress = false
	a.accStatus.LastError = ""
	a.accMu.Unlock()
}

// accountLoginPollTimeout bounds how long pollAccountLogin keeps checking
// after the verification URL was opened -- mirrors
// pollForLicenseTimeout's own reasoning, just shorter: a Google login
// round trip is seconds, not "however long it takes to enter card details".
const accountLoginPollTimeout = 5 * time.Minute

// pollAccountLogin mirrors pollForLicense's shape exactly (see that
// function's doc comment) for the device-code login instead of the Stripe
// checkout poll.
func (a *App) pollAccountLogin(ctx context.Context, code string) {
	deadline := time.Now().Add(accountLoginPollTimeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if time.Now().After(deadline) {
			a.setAccError("Didn't detect a completed login yet — if you finished signing in, try \"Log in\" again.")
			return
		}
		result, err := account.Poll(ctx, code)
		if err != nil {
			continue // transient network hiccup -- keep polling until the deadline or ctx cancellation
		}
		if result.Status == "expired" {
			a.setAccError("Login link expired — click \"Log in\" again.")
			return
		}
		if result.Status != "complete" {
			continue // still pending -- human hasn't finished the browser step yet
		}

		next := a.cfg
		next.AccountEmail = result.Email
		next.AccountToken = result.AccountToken
		if err := a.SaveConfig(next); err != nil {
			log.Printf("[app] warning: failed to persist account token: %v", err)
		}

		a.accMu.Lock()
		a.accStatus.LoggedIn = true
		a.accStatus.Email = result.Email
		a.accStatus.LoginInProgress = false
		a.accStatus.LastError = ""
		a.accMu.Unlock()

		a.refreshAccountLicenses(context.Background())
		return
	}
}

// refreshAccountLicenses re-fetches the logged-in account's desktop
// licenses -- called right after login completes and again after a
// successful Rebind (so the GUI's list reflects the new owner immediately
// rather than waiting for the dialog to be reopened).
func (a *App) refreshAccountLicenses(ctx context.Context) {
	// a.cfg.AccountToken, not accStatus -- same convention every other
	// entitlement/account field on App follows (see DownloadRustShine's
	// own `token := a.cfg.EntitlementToken`): the persisted config is the
	// source of truth, accStatus is only ever a read-optimized mirror of
	// it for the GUI.
	token := a.cfg.AccountToken
	if token == "" {
		return
	}

	licenses, err := account.ListLicenses(ctx, token)
	if err != nil {
		a.setAccError(fmt.Sprintf("could not load your licenses: %v", err))
		return
	}
	a.accMu.Lock()
	a.accStatus.Licenses = licenses
	a.accMu.Unlock()
}

// RebindLicenseToThisDevice moves oldIdentifier (one of the logged-in
// account's own desktop licenses, per accStatus.Licenses) onto this
// machine's hardware id -- the account-login replacement for the manual
// "paste the old machine's token" rebind flow (see internal/entitlement's
// StartCheckoutURL-adjacent flows; that one is untouched and still works).
// Applies the freshly-rebound license locally exactly like applyIssuedToken
// would, except there's no fresh token to verify here: RefreshLicense
// (below) is what actually confirms the backend now considers this
// hardware id licensed and returns a real, hardware-bound token for it.
func (a *App) RebindLicenseToThisDevice(oldIdentifier string) error {
	hwID, err := hwid.Get()
	if err != nil {
		a.setAccError(fmt.Sprintf("could not determine this machine's hardware id: %v", err))
		return err
	}

	token := a.cfg.AccountToken
	a.accMu.Lock()
	a.accStatus.RebindInProgress = true
	a.accStatus.LastError = ""
	a.accMu.Unlock()
	defer func() {
		a.accMu.Lock()
		a.accStatus.RebindInProgress = false
		a.accMu.Unlock()
	}()
	if token == "" {
		err := fmt.Errorf("not logged in")
		a.setAccError("Log in first.")
		return err
	}

	if err := account.Rebind(context.Background(), token, oldIdentifier, hwID); err != nil {
		msg := account.UserFacingError(err.Error())
		if msg == "" || msg == err.Error() {
			msg = "Couldn't move this license to this device."
		}
		a.setAccError(msg)
		return err
	}

	res, err := entitlement.RefreshLicense(context.Background(), hwID)
	if err != nil || res.Status == "free" {
		// The rebind itself succeeded (backend confirmed it above) -- a
		// still-"free" (or failed) refresh here just means this machine
		// hasn't picked up the fresh paid-tier token yet (KV read lag, or a
		// transient network hiccup); entitlementWatchdog's own periodic
		// RefreshLicense call will catch up shortly, and the GUI's
		// "Refresh" affordance re-drives this same path on demand.
		log.Printf("[app] rebind succeeded but refresh-license didn't pick it up yet: err=%v", err)
	} else {
		a.applyIssuedToken(res.Token, hwID)
	}

	a.refreshAccountLicenses(context.Background())
	return nil
}

// RefreshAccountLicenses re-fetches the logged-in account's licenses in the
// background -- exported so the GUI can drive it on demand (opening the
// License dialog, a "Refresh" click) via the TokenProvider interface,
// mirroring refreshAccountLicenses's unexported synchronous version used
// internally right after a login/rebind completes.
func (a *App) RefreshAccountLicenses() {
	go a.refreshAccountLicenses(context.Background())
}

// LogoutAccount forgets the locally-stored account login -- purely local
// (there's no server-side session to invalidate: the Bearer account token
// simply expires on its own after 30 days, see deviceAuth.ts's
// ACCOUNT_TOKEN_TTL_SECONDS). Does not touch EntitlementToken/PreferredBackend
// -- RustShine keeps running under whatever hardware-bound license/trial it
// already had; only the account identity used for rebinding is cleared.
func (a *App) LogoutAccount() error {
	next := a.cfg
	next.AccountEmail = ""
	next.AccountToken = ""
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	a.accMu.Lock()
	a.accStatus = account.Status{}
	a.accMu.Unlock()
	return nil
}

// DownloadPunktfunk downloads punktfunk-host from Streamers-Forks' latest
// release into <stateDir>/punktfunk (forkrelease: the manifest's Ed25519
// signature and the archive's SHA-256 are checked before anything is put in
// place). Progress goes through entStatus like DownloadRustShine's. Does not
// switch to it -- SetStreamBackend("punktfunk") calls this itself when the
// binary is missing.
func (a *App) DownloadPunktfunk(onProgress forkrelease.ProgressFunc) error {
	a.entMu.Lock()
	a.entStatus.DownloadInProgress = true
	a.entStatus.DownloadName = "Punktfunk"
	a.entStatus.Progress = -1
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	defer func() {
		a.entMu.Lock()
		a.entStatus.DownloadInProgress = false
		a.entStatus.DownloadName = ""
		a.entMu.Unlock()
	}()
	combined := func(downloaded, total int64) {
		frac := -1.0
		if total > 0 {
			frac = float64(downloaded) / float64(total)
		}
		a.entMu.Lock()
		a.entStatus.Progress = frac
		a.entMu.Unlock()
		if onProgress != nil {
			onProgress(downloaded, total)
		}
	}
	log.Printf("[app] downloading punktfunk-host (Streamers-Forks latest release)")
	prep, err := forkrelease.PreparePunktfunk(context.Background(), a.cfg.StateDir, combined)
	if err == nil {
		err = prep.Commit()
	}
	if err != nil {
		a.setEntError(fmt.Sprintf("punktfunk download failed: %v", err))
		return fmt.Errorf("punktfunk-host download failed: %w", err)
	}
	log.Printf("[app] punktfunk-host %s downloaded and verified", prep.Version)
	return nil
}

// punktfunkUpdateInterval: Streamers-Forks releases are rare; the streamer
// update tick runs far more often than this needs.
const punktfunkUpdateInterval = 6 * time.Hour

// checkPunktfunkUpdate re-downloads punktfunk-host when Streamers-Forks'
// latest release carries a newer one than the agent downloaded earlier --
// only for a build the agent itself staged (a bundled or system install is
// the user's to update). A running Punktfunk backend is stopped for the
// swap and started again; on Windows its .exe can't be replaced while it
// runs.
func (a *App) checkPunktfunkUpdate(ctx context.Context) {
	// Not downloaded yet while Punktfunk runs anyway: an upstream install
	// found on PATH (no PyroWave). The fork replaces it here.
	if !forkrelease.PunktfunkStaged(a.cfg.StateDir) && (forkrelease.PunktfunkAssetName() == "" || a.currentStreamKind() != "punktfunk") {
		return
	}
	a.entMu.Lock()
	due := time.Since(a.lastPunktfunkCheck) >= punktfunkUpdateInterval && !a.entStatus.PunktfunkUpdateInProgress
	if due {
		a.lastPunktfunkCheck = time.Now()
		a.entStatus.PunktfunkUpdateInProgress = true
	}
	a.entMu.Unlock()
	if !due {
		return
	}
	defer a.endPunktfunkUpdate()
	if err := a.updatePunktfunk(ctx); err != nil {
		log.Printf("[app] punktfunk update failed (will retry): %v", err)
	}
}

func (a *App) endPunktfunkUpdate() {
	a.entMu.Lock()
	a.entStatus.PunktfunkUpdateInProgress = false
	a.entMu.Unlock()
}

// CheckPunktfunkUpdateNow is the Punktfunk card's "check for updates":
// checkPunktfunkUpdate's GitHub check, right now. Errors also land in
// entStatus.LastError for a thin-client GUI.
func (a *App) CheckPunktfunkUpdateNow() error {
	if !forkrelease.PunktfunkStaged(a.cfg.StateDir) {
		return fmt.Errorf("punktfunk-host was not downloaded by the agent")
	}
	a.entMu.Lock()
	if a.entStatus.PunktfunkUpdateInProgress {
		a.entMu.Unlock()
		return fmt.Errorf("punktfunk update already in progress")
	}
	a.entStatus.PunktfunkUpdateInProgress = true
	a.entStatus.LastError = ""
	a.lastPunktfunkCheck = time.Now()
	a.entMu.Unlock()
	defer a.endPunktfunkUpdate()
	if err := a.updatePunktfunk(context.Background()); err != nil {
		a.setEntError(fmt.Sprintf("punktfunk update failed: %v", err))
		return err
	}
	return nil
}

// updatePunktfunk installs the latest release's punktfunk-host if it is
// newer than the staged one; see checkPunktfunkUpdate.
func (a *App) updatePunktfunk(ctx context.Context) error {
	latest, err := forkrelease.LatestPunktfunkVersion(ctx)
	if err != nil {
		return err
	}
	if latest == "" || latest == forkrelease.PunktfunkStagedVersion(a.cfg.StateDir) {
		return nil
	}
	log.Printf("[app] punktfunk-host %s available -- downloading", latest)
	prep, err := forkrelease.PreparePunktfunk(ctx, a.cfg.StateDir, nil)
	if err != nil {
		return err
	}
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	running := a.streamKind == "punktfunk" && a.stream != nil
	if running {
		a.stopStreamAndWait(a.stream)
	}
	commitErr := prep.Commit()
	if commitErr != nil {
		prep.Discard()
		commitErr = fmt.Errorf("punktfunk update not applied: %w", commitErr)
	} else {
		log.Printf("[app] punktfunk-host updated to %s", prep.Version)
	}
	if running {
		if err := a.RestartSunshineStartOnly(); err != nil {
			log.Printf("[app] punktfunk restart after update failed: %v", err)
		}
	}
	return commitErr
}

// DownloadRustShine downloads and stages the RustShine build for this
// platform. onProgress mirrors entitlement.ProgressFunc's threading
// contract exactly (internal/update.ProgressFunc's twin) — called from
// this goroutine, GUI callers must hop to their own UI thread themselves.
// Does not switch to it; call SetStreamBackend("rustshine") once this
// returns successfully.
func (a *App) DownloadRustShine(onProgress entitlement.ProgressFunc) error {
	token := a.cfg.EntitlementToken
	hwID, err := hwid.Get()
	if err != nil {
		return fmt.Errorf("could not determine this machine's hardware id: %w", err)
	}
	if _, err := entitlement.VerifyForHardware(token, hwID); err != nil {
		return fmt.Errorf("not currently entitled: %w", err)
	}

	a.entMu.Lock()
	a.entStatus.DownloadInProgress = true
	a.entStatus.DownloadName = "USBridge Streamer"
	a.entStatus.Progress = -1
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	defer func() {
		a.entMu.Lock()
		a.entStatus.DownloadInProgress = false
		a.entStatus.DownloadName = ""
		a.entMu.Unlock()
	}()

	// Always update entStatus.Progress (so a thin-client GUI polling
	// EntitlementStatus over adminapi sees it, per handleDownloadRustShine's
	// doc comment on why that download call is fire-and-forget), and also
	// forward to onProgress when the caller is an engine-owning GUI that
	// wants lower-latency feedback than the 2s poll.
	combined := func(downloaded, total int64) {
		frac := -1.0
		if total > 0 {
			frac = float64(downloaded) / float64(total)
		}
		a.entMu.Lock()
		a.entStatus.Progress = frac
		a.entMu.Unlock()
		if onProgress != nil {
			onProgress(downloaded, total)
		}
	}

	if err := a.stageRustShine(context.Background(), token, combined); err != nil {
		a.setEntError(fmt.Sprintf("download failed: %v", err))
		return err
	}

	// USB passthrough (usbridge-usb-broker) is a SEPARATE, closed-source
	// component from RustShine and must never be downloaded as a side
	// effect of picking a video backend -- it needs its own explicit,
	// one-time user consent first (see App.EnableUSBBroker, wired to the
	// USB status row's button in ui.Window). Staging it here, unconditional
	// on nothing but "the user clicked Download RustShine", was exactly the
	// silent-proprietary-download this product must not do by default.
	return nil
}

// USBPassthroughStatus reports the USB passthrough broker/driver status for
// this machine (see usbpass.Service.Status's own doc comment for the
// per-platform driver detection it does). Nil-safe: usbBroker is always
// constructed today (New's call site in New()), but this mirrors every
// other a.usbBroker != nil guard in this file rather than assuming that
// stays true.
func (a *App) USBPassthroughStatus() usbpass.Status {
	if a.usbBroker == nil {
		return usbpass.Status{Available: false, Platform: "disabled", ConsentGiven: a.cfg.USBBrokerConsentGiven()}
	}
	st := a.usbBroker.Status()
	st.ConsentGiven = a.cfg.USBBrokerConsentGiven()
	return st
}

// InstallUSBDriver installs this platform's USB passthrough driver
// (usbip + vhci-hcd on Linux; see usbpass.Service.InstallDrivers). Windows
// has no equivalent call: usbip-win2 ships its own signed installer, so the
// GUI just opens https://github.com/vadimgrn/usbip-win2/releases/latest in
// the browser directly (ui/window.go) instead of routing through here.
func (a *App) InstallUSBDriver() error {
	if a.usbBroker == nil {
		return fmt.Errorf("usb passthrough not available")
	}
	return a.usbBroker.InstallDrivers()
}

// GrantUSBAttach installs the one-time passwordless-usbip grant (Linux; see
// usbpass.Service.GrantAttachAccess).
func (a *App) GrantUSBAttach() error {
	if a.usbBroker == nil {
		return fmt.Errorf("usb passthrough not available")
	}
	return a.usbBroker.GrantAttachAccess()
}

// ClearLicense clears the saved entitlement token and switches back to
// Sunshine if RustShine was active. Does not delete the already-staged
// RustShine binary -- buying/trialing again later can reuse it without
// re-downloading (see rustshineBackend.BinaryPath's staged-file check).
func (a *App) ClearLicense() error {
	if a.currentStreamKind() == "rustshine" {
		if err := a.SetStreamBackend("sunshine"); err != nil {
			return err
		}
	}
	next := a.cfg
	next.EntitlementToken = ""
	next.PreferredBackend = ""
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	a.refreshLocalEntitlementStatus()
	// Immediately re-bootstrap the free tier rather than leaving the
	// license dialog on its "Setting up…" placeholder until
	// entitlementWatchdog's next interval -- see recheckEntitlement's
	// empty-token branch. Backgrounded: ClearLicense's own caller (the
	// GUI) doesn't need to block on a network round trip just to clear a
	// local file.
	if hwID, err := hwid.Get(); err == nil {
		go a.bootstrapFreeTier(context.Background(), hwID)
	}
	return nil
}

// dropEntitlementToken clears a saved entitlement token that no longer
// verifies locally (expired while offline, corrupted, copied from another
// machine). The stream backend stays as it is: RustShine without a token is
// its free tier (no pen tablets, no 4:4:4), not something to switch away
// from. The mirrored token file (entitlement.WriteTokenFile) is left alone
// -- rust-shine verifies it itself and treats a stale one as the free tier.
func (a *App) dropEntitlementToken() {
	next := a.cfg
	next.EntitlementToken = ""
	_ = a.SaveConfig(next)
	a.refreshLocalEntitlementStatus()
}

// entitlementRecheckInterval is how often entitlementWatchdog re-verifies
// the cached license against the backend in the steady state (both to catch
// a license refund/cancellation and to proactively renew a free-tier token
// well before its own local expiry -- see recheckEntitlement's own doc
// comment). Far longer than sunshineWatchdogInterval deliberately: there's
// no reason to check more than a few times a day in the healthy case, and
// a refund is a rare, human-initiated event, not something that needs
// sub-hour detection latency. USBridge-streamer version checks are a
// separate, cheaper ticker -- see streamerUpdateCheckInterval.
const entitlementRecheckInterval = 6 * time.Hour

// streamerUpdateCheckInterval is how often streamerUpdateWatchdog asks the
// backend whether a newer USBridge-streamer build exists -- a cheap
// metadata call only; the archive is downloaded only if auto-update is on
// or the user confirms. Once an hour is frequent enough to pick up a
// release the same day and rare enough not to add load or interrupt a stream.
const streamerUpdateCheckInterval = 1 * time.Hour

// entitlementRetryInterval is how soon entitlementWatchdog retries after a
// FAILED recheck (network down, backend unreachable), instead of leaving
// whatever gap that failure caused sitting until the next full
// entitlementRecheckInterval tick. Matters most for a free-tier token: with
// no retry sooner than 6h, a machine that happens to be offline right when
// its cached free token's local expiry passes would sit downgraded to
// Sunshine for up to 6h even after connectivity comes back, purely because
// nothing asked the backend again sooner. Short enough to recover quickly,
// long enough not to hammer the backend while genuinely offline.
const entitlementRetryInterval = 5 * time.Minute

// entitlementWatchdog periodically re-verifies entitlement, downgrades to
// Sunshine the moment a license no longer holds up (a refund), and
// proactively renews whatever's cached (free or paid) so it never sits
// un-refreshed for the full 6h steady-state interval after a transient
// failure -- mirrors sunshineWatchdog's shape. Run also fires one immediate
// recheckEntitlement shortly after startup (not gated on this ticker), so a
// refund or a lapsed free token is caught quickly after a restart instead
// of waiting up to a full interval.
func (a *App) entitlementWatchdog(ctx context.Context) {
	ticker := time.NewTicker(entitlementRecheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// Keep retrying at entitlementRetryInterval (rather than only at
		// the next full 6h tick) until a recheck actually succeeds -- see
		// entitlementRetryInterval's own doc comment for why this matters
		// most for a free-tier token that's sitting downgraded because the
		// backend was briefly unreachable right when it needed renewing.
		for !a.recheckEntitlement(ctx) {
			select {
			case <-ctx.Done():
				return
			case <-time.After(entitlementRetryInterval):
			case <-ticker.C:
			}
		}
	}
}

// streamerUpdateWatchdog periodically checks whether a newer
// USBridge-streamer build is published. Separate from entitlementWatchdog
// so license re-verify stays at 6h while this stays at one hour
// (see streamerUpdateCheckInterval). Fires once immediately so a
// just-started agent doesn't wait a full interval to notice an already-
// published release.
func (a *App) streamerUpdateWatchdog(ctx context.Context) {
	a.tickStreamerUpdate(ctx)
	ticker := time.NewTicker(streamerUpdateCheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.tickStreamerUpdate(ctx)
		}
	}
}

func (a *App) tickStreamerUpdate(ctx context.Context) {
	a.checkPunktfunkUpdate(ctx)
	a.checkSunshineUpdate(ctx)
	a.checkVibepolloUpdate(ctx)
	token := a.cfg.EntitlementToken
	if strings.TrimSpace(token) == "" {
		return
	}
	// First-time StageRustShine still lives on ensureRustShineFresh
	// (purchase / 6h entitlement tick). This loop is only the cheap
	// "is there a newer tagged build?" check against an already-staged
	// binary -- running the full initial download every minute would be
	// the opposite of a light probe.
	if !a.rustshineStaged() {
		return
	}
	a.checkRustShineUpdate(ctx, token)
}

// turnCredentialsRefreshInterval is how often turnCredentialsWatchdog mints
// a fresh Cloudflare Realtime TURN credential (see usbridge-entitlement-
// backend's webrtcTurn.ts) and writes it for rust-shine to pick up.
// Deliberately much shorter than entitlementRecheckInterval (6h): a minted
// credential is only valid for usbridge-entitlement's own
// TURN_CREDENTIAL_TTL_SECONDS (1h) -- this refreshes comfortably before
// that expires, and stays well under the backend's 12-mints/hour rate limit
// per hardware id (this is the only caller of FetchTurnCredentials, ticking
// once per interval, so there's no risk of this loop alone exhausting it).
const turnCredentialsRefreshInterval = 50 * time.Minute

// turnCredentialsWatchdog periodically mints a fresh TURN credential for
// this hardware id and writes it to entitlement.TurnCredentialsFilePath,
// which rust-shine's own background poller picks up (see crates/webrtc-
// video's turn_credentials module) -- entirely additive to an ordinary
// WebRTC session: a free-tier install, one that's never linked, or a
// backend that's briefly unreachable all just mean "no TURN server offered
// this tick", never a hard failure. Mirrors streamerUpdateWatchdog's shape
// (fire once immediately, then on a ticker) so a freshly started agent's
// first WebRTC session doesn't wait out a full interval for TURN to become
// available.
func (a *App) turnCredentialsWatchdog(ctx context.Context) {
	a.tickTurnCredentials(ctx)
	ticker := time.NewTicker(turnCredentialsRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.tickTurnCredentials(ctx)
		}
	}
}

func (a *App) tickTurnCredentials(ctx context.Context) {
	// Cheap local pre-filter before ever making a network call: a hw_id
	// whose cached token doesn't even locally verify as pro/enterprise has
	// no chance of getting a credential (the backend's own live KV check is
	// still the actual authority -- see webrtcTurn.ts -- this just avoids a
	// wasted round trip for the common free-tier case). Also skips outright
	// if RustShine isn't even staged, same gate tickStreamerUpdate uses --
	// no WebRTC session exists to hand a TURN server to otherwise.
	if !a.rustshineStaged() {
		return
	}
	hwID, err := hwid.Get()
	if err != nil {
		return
	}
	claims, verifyErr := entitlement.VerifyForHardware(a.cfg.EntitlementToken, hwID)
	if verifyErr != nil || (claims.Tier != "pro" && claims.Tier != "enterprise") {
		// Not currently pro/enterprise (or no token at all yet) -- clear
		// any previously written credential rather than leaving a stale one
		// sitting there for up to its own remaining TTL after a downgrade.
		if err := entitlement.ClearTurnCredentialsFile(a.cfg.StateDir); err != nil {
			log.Printf("[app] warning: failed to clear turn credentials file: %v", err)
		}
		return
	}

	fetchedAt := time.Now()
	creds, err := entitlement.FetchTurnCredentials(ctx, hwID)
	if err != nil {
		if refused, ok := err.(*entitlement.TurnCredentialsRefused); ok && refused.Reason == "not_pro" {
			// Backend's own live tier check disagrees with our locally
			// cached claims (a lapsed subscription the local token hasn't
			// caught up to yet, e.g.) -- trust it, same as above.
			if err := entitlement.ClearTurnCredentialsFile(a.cfg.StateDir); err != nil {
				log.Printf("[app] warning: failed to clear turn credentials file: %v", err)
			}
			return
		}
		// rate_limited, network error, or a backend hiccup -- leave
		// whatever's already on disk alone (it may well still be fresh)
		// and just try again next tick.
		log.Printf("[app] turn credentials refresh failed (will retry next tick): %v", err)
		return
	}
	if err := entitlement.WriteTurnCredentialsFile(a.cfg.StateDir, creds, fetchedAt); err != nil {
		log.Printf("[app] warning: failed to write turn credentials file: %v", err)
	}
}

// deviceCertRegisterInterval is how often deviceCertWatchdog re-registers
// this machine's current LAN IP with the backend (see internal/devicecert)
// as a periodic heartbeat even when no IP change has been detected.
const deviceCertRegisterInterval = 5 * time.Minute

// deviceCertPendingRetry is how soon deviceCertWatchdog asks again after the
// backend answered "pending" (CSR queued for the external issuer, see
// devicecert.ErrPending).
const deviceCertPendingRetry = time.Minute

// deviceCertPollInterval is how frequently deviceCertWatchdog checks the local
// routing table for IP address / interface changes.
const deviceCertPollInterval = 3 * time.Second

// deviceCertDemandWindow: a device cert is requested (or renewed) only if a
// browser reached this machine's name within this window -- see
// tlshost.Manager's registeredHostname doc comment for why.
const deviceCertDemandWindow = 30 * 24 * time.Hour

// deviceCertTickOutcome decides how deviceCertWatchdog's runTick should
// update its retry bookkeeping after one attempt at ip, given the error (if
// any) tickDeviceCert returned. Pulled out as a pure function so this
// decision is unit-testable without a real 3-second ticker.
//
// Every outcome other than ErrPending backs off the full
// deviceCertRegisterInterval, success or not -- this function must ALWAYS
// return a non-zero registerTime and the attempted ip, never the zero
// values, on every path. An earlier version only did this for err == nil,
// ErrPending, and ErrRateLimited; any OTHER error (a network failure, a
// non-JSON response such as an edge/WAF block page, hwid unavailable, ...)
// fell through without updating either return value. Since the caller's
// outer ticker loop retries whenever registeredIP != the current IP OR
// registerTime is its zero value, leaving both untouched made that
// "never registered yet" condition permanently true for as long as the
// error kept recurring -- retrying on every single deviceCertPollInterval
// (3s) tick forever instead of backing off. That turned a transient
// backend problem into a self-reinforcing request storm: more failures (an
// account-wide rate limit, say) produced more non-JSON error responses,
// which produced more unthrottled retries, which kept the backend further
// over its limit. See usbridge-entitlement-backend's 2026-10-04 incident
// writeup. Fixed by making the fallback case identical to the success case.
func deviceCertTickOutcome(err error, ip string, now time.Time) (registeredIP string, registerTime time.Time) {
	if errors.Is(err, devicecert.ErrPending) {
		// Registered fine, cert just not issued yet: poll again in
		// ~deviceCertPendingRetry instead of waiting a full
		// deviceCertRegisterInterval.
		return ip, now.Add(deviceCertPendingRetry - deviceCertRegisterInterval)
	}
	return ip, now
}

// deviceCertWatchdog keeps this machine's <label>.device.usbridge.io DNS
// record and per-device TLS cert (see internal/tlshost,
// internal/devicecert) up to date -- what lets the browser-based web
// client (client/web, loaded from https://web.usbridge.io) reach this
// agent's HTTPS listener (a.tlsServer) at all.
//
// Actively monitors the local IP address of the egress network interface:
// when a DHCP lease change, Wi-Fi switch, or manual IP change occurs, the new
// LAN IP is immediately registered with the domain backend so the browser web
// client can connect without waiting for the 5-minute fallback heartbeat.
func (a *App) deviceCertWatchdog(ctx context.Context) {
	var lastRegisteredIP string
	var lastRegisterTime time.Time

	runTick := func() {
		ip := netutil.PreferredIPv4()
		if ip == "" {
			return
		}
		err := a.tickDeviceCert(ctx)
		if err == nil && lastRegisteredIP != "" && lastRegisteredIP != ip {
			log.Printf("🌐 [app] device-cert: local IP changed (%s -> %s), registered domain", lastRegisteredIP, ip)
		}
		lastRegisteredIP, lastRegisterTime = deviceCertTickOutcome(err, ip, time.Now())
	}

	runTick()
	ticker := time.NewTicker(deviceCertPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.tlsMgr.DemandC():
			// A browser asked for this machine's name: get the cert now
			// rather than at the next heartbeat (at most once a minute --
			// a browser retries its handshake).
			if _, needs := a.tlsMgr.DeviceCertStatus(); needs && time.Since(lastRegisterTime) >= time.Minute {
				runTick()
			}
		case <-ticker.C:
			currentIP := netutil.PreferredIPv4()
			if currentIP == "" {
				continue
			}
			// Trigger registration if:
			// 1. IP changed from last successfully registered IP
			// 2. OR registration hasn't succeeded yet (lastRegisterTime is zero)
			// 3. OR periodic heartbeat interval (5m) has elapsed
			if currentIP != lastRegisteredIP || lastRegisterTime.IsZero() || time.Since(lastRegisterTime) >= deviceCertRegisterInterval {
				runTick()
			}
		}
	}
}

// tickDeviceCert registers this machine's current LAN IP, then -- only if
// the hostname changed or the installed device cert is missing/expiring
// soon (tlshost.Manager.DeviceCertStatus, a cheap in-memory check) --
// fetches and installs a fresh cert. The common case is register-only: no
// cert fetch, since the device cert changes far less often than
// this ticks. Best-effort throughout: any failure here just leaves the
// self-signed fallback (or whatever device cert is already installed) in
// place until the next tick, never blocks or crashes the agent.
func (a *App) tickDeviceCert(ctx context.Context) error {
	hwID, err := hwid.Get()
	if err != nil {
		log.Printf("[app] device-cert: hwid unavailable: %v", err)
		return fmt.Errorf("hwid unavailable: %w", err)
	}
	ip := netutil.PreferredIPv4()
	if ip == "" {
		return fmt.Errorf("no LAN interface up")
	}

	regCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	hostname, err := devicecert.RegisterIP(regCtx, hwID, ip)
	cancel()
	if err != nil {
		log.Printf("[app] device-cert: register IP failed: %v", err)
		return fmt.Errorf("register IP failed: %w", err)
	}
	a.tlsMgr.SetRegisteredHostname(hostname)

	if a.tlsMgr != nil {
		selfSignedIPs := []net.IP{net.ParseIP("127.0.0.1")}
		if parsed := net.ParseIP(ip); parsed != nil {
			selfSignedIPs = append(selfSignedIPs, parsed)
		}
		if err := a.tlsMgr.EnsureSelfSigned(selfSignedIPs, nil); err != nil {
			log.Printf("[app] device-cert: update self-signed TLS cert: %v", err)
		}
	}

	currentHostname, needsRefresh := a.tlsMgr.DeviceCertStatus()
	if currentHostname == hostname && !needsRefresh {
		return nil
	}
	// No browser has come for this name lately: don't spend a certificate
	// from the shared Let's Encrypt budget on it.
	if !a.tlsMgr.DeviceCertDemanded(deviceCertDemandWindow) {
		return nil
	}

	csrDER, err := a.tlsMgr.DeviceCSR(hostname)
	if err != nil {
		log.Printf("[app] device-cert: generate CSR failed: %v", err)
		a.tlsMgr.SetDeviceCertError(err)
		return fmt.Errorf("generate CSR failed: %w", err)
	}

	certCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	cert, err := devicecert.RequestCert(certCtx, hwID, csrDER, strings.TrimSpace(a.cfg.AccountToken))
	cancel()
	if err != nil {
		log.Printf("[app] device-cert: request cert failed: %v", err)
		a.tlsMgr.SetDeviceCertError(err)
		return fmt.Errorf("request cert failed: %w", err)
	}
	if err := a.tlsMgr.InstallDeviceCert(hostname, cert.CertPEM); err != nil {
		log.Printf("[app] device-cert: install cert failed: %v", err)
		a.tlsMgr.SetDeviceCertError(err)
		return fmt.Errorf("install cert failed: %w", err)
	}
	return nil
}

// RetryDeviceCert re-runs tickDeviceCert immediately, outside its normal
// poll/heartbeat schedule -- what the Status UI's certificate-error retry
// button calls. Safe to call any time: tickDeviceCert itself is idempotent
// (it no-ops once a fresh device cert is already installed) and already
// runs concurrently with the watchdog's own ticks without any shared state
// beyond tlsMgr, which is already safe for concurrent use.
func (a *App) RetryDeviceCert() {
	if a.tlsMgr != nil {
		a.tlsMgr.NoteDeviceCertDemand()
	}
	go func() {
		if err := a.tickDeviceCert(context.Background()); err != nil {
			log.Printf("[app] device-cert: manual retry failed: %v", err)
		}
	}()
}

// recheckEntitlement re-verifies whatever's currently cached in
// cfg.EntitlementToken and downgrades to Sunshine if it no longer holds up.
// Returns true if this tick is "settled" (nothing more to usefully retry
// sooner than the next scheduled interval), false if it hit a transient
// failure entitlementWatchdog should retry at entitlementRetryInterval
// instead of waiting out the full entitlementRecheckInterval.
//
// BOTH a cached LICENSE token and a cached FREE/TRIAL token now get a real
// network re-check (RefreshLicense) every tick -- this used to skip the
// network call entirely for free/trial (a trial can't be revoked, so
// nothing was thought to change), but that missed the actual point of
// calling at all: RefreshLicense doesn't just re-confirm the same tier, it
// mints a FRESH token with a FRESH expiry. Skipping it for free meant a
// free-tier token only ever got renewed reactively, after it had already
// expired and downgraded the customer to Sunshine — see the 2026-09-14
// conversation this changed in. Renewing proactively, every interval,
// keeps a continuously-running free install from ever locally expiring in
// the first place, the same guarantee paid licenses already had.
//
// A transient failure on the network path (network down, backend
// unreachable) does NOT downgrade immediately by itself — but it also
// doesn't just do nothing: it falls back to checking the *cached* token's
// own embedded expiry locally (entitlement.VerifyForHardware, no network
// needed) and downgrades once *that* has passed. Without this fallback, a
// long-running agent process that's offline for longer than the token's
// TTL would keep trusting an already-locally-expired token forever
// (nothing else re-derives entStatus continuously while already running —
// only at the next restart, via New()'s own local check), silently
// breaking the "offline grace is bounded by the token's TTL" guarantee
// this whole scheme is supposed to provide. This closes that gap: the
// bound is enforced continuously, not just at the next restart -- and once
// that bound is hit, the immediate bootstrapFreeTier call below (rather
// than waiting for entitlementWatchdog's own next tick) means connectivity
// coming back is picked up within entitlementRetryInterval, not up to a
// full entitlementRecheckInterval later.
func (a *App) recheckEntitlement(ctx context.Context) bool {
	hwID, err := hwid.Get()
	if err != nil {
		log.Printf("[app] entitlement recheck: could not determine hardware id: %v", err)
		return true // not a transient/retryable failure -- a local config problem
	}

	if strings.TrimSpace(a.cfg.EntitlementToken) == "" {
		// Fresh install, never linked -- bootstrap today's unconditional
		// free tier right away rather than leaving the license dialog's
		// tier switcher with nothing to show until the user clicks
		// something first (there's no "start trial" step to wait for
		// anymore, see entitlement.StartTrial's doc comment). Deliberately
		// NOT applyIssuedToken (which also kicks off an immediate
		// RustShine download) -- staying on free tier with Sunshine active
		// shouldn't silently pull down a multi-MB RustShine build nobody
		// asked for yet; that still only happens once the user actually
		// picks RustShine in the dialog.
		return a.bootstrapFreeTier(ctx, hwID)
	}

	claims, verifyErr := entitlement.VerifyForHardware(a.cfg.EntitlementToken, hwID)
	if verifyErr != nil {
		log.Printf("[app] cached entitlement token is no longer valid locally — dropping it (free tier): %v", verifyErr)
		a.dropEntitlementToken()
		// Immediately try to bootstrap a fresh free token rather than
		// waiting for whatever's left of this watchdog's cadence, same
		// reasoning as entitlementRetryInterval itself.
		return a.bootstrapFreeTier(ctx, hwID)
	}

	res, err := entitlement.RefreshLicense(ctx, hwID)
	if err != nil {
		log.Printf("[app] entitlement recheck failed (will retry sooner than the next scheduled interval): %v", err)
		return false // cached token already verified locally above -- keep trusting it until it actually expires or a retry succeeds
	}

	if claims.Provider != entitlement.ProviderDesktopTrial && res.Status == "free" {
		// A previously PAID record is no longer paid (refund/cancellation):
		// the free token below replaces it, which turns RustShine's Pro
		// features (pen tablets, 4:4:4) off -- everything else carries on.
		log.Printf("[app] license no longer on record for this hardware (refunded/canceled?) — back to the free tier")
	}

	next := a.cfg
	next.EntitlementToken = res.Token
	if err := a.SaveConfig(next); err != nil {
		log.Printf("[app] warning: failed to persist refreshed entitlement: %v", err)
	}
	a.refreshLocalEntitlementStatus()
	a.ensureRustShineFresh(ctx, res.Token)
	a.ensureUSBBroker(ctx, res.Token)
	return true
}

// usbBrokerWatchdogInterval is how often usbBrokerWatchdog checks whether
// the usb-broker subprocess is actually alive and restarts it if not.
// ensureUSBBroker (below) only ever stages+starts it once, gated on
// Staged() -- which just checks the binary exists on disk, not that the
// process is actually running -- so a broker that crashes after a
// successful launch (fork succeeded, cmd.Start() returned nil, so Go never
// saw a staging failure) stayed dead forever until this watchdog existed.
// Confirmed live: an unrelated Windows process (WsToastNotification.exe)
// happened to be squatting on the broker's hardcoded URB port, so the
// broker's own bind() failed at startup and it exited immediately -- the
// agent kept relaying every USB-passthrough Hello into that *other*
// process's own HTTP listener on the same port instead, indefinitely, and
// every attach failed with a misleading "requires a license" error with no
// hint anywhere that the actual broker had died over an hour earlier.
// Mirrors sunshineWatchdog's exact shape: Start() is an idempotent no-op
// when the broker is already running (see Service.Start's own guard), so
// calling it unconditionally every tick is safe.
const usbBrokerWatchdogInterval = 15 * time.Second

// usbBrokerWatchdog periodically verifies the broker's control-plane port
// actually answers (Service.Status's BrokerAlive, a real dial+status-query
// against 127.0.0.1:<controlAddr>, not just "is a *os.Process handle set")
// and restarts it if not -- see this const's own doc comment for why
// ensureUSBBroker's one-shot staging call can't catch this on its own.
func (a *App) usbBrokerWatchdog(ctx context.Context) {
	if a.usbBroker == nil {
		return
	}
	ticker := time.NewTicker(usbBrokerWatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if !a.cfg.USBBrokerConsentGiven() || !a.usbBroker.Staged() {
			continue
		}
		if a.usbBroker.Status().BrokerAlive {
			continue
		}
		if err := a.usbBroker.Start(); err != nil {
			log.Printf("[usbpass] broker restart failed: %v", err)
		} else {
			log.Printf("[usbpass] broker was not alive -- restarted")
		}
	}
}

// ensureUSBBroker stages and starts the usb-broker regardless of which
// stream backend is active or which license tier is current: the broker is
// a separate process from RustShine (it only needs the entitlement token
// file, see usbpass.Start), and which specific *devices* a free-tier
// session may attach is decided per attach inside the closed broker itself
// (rust-shine's license_class/devlist_probe, see docs/USB_PASSTHROUGH.md in
// that repo) -- there is nothing left for Go to gate on tier here.
//
// What Go DOES still gate on is USBBrokerConsentGiven: this agent must run
// only open-source code until the user explicitly opts in (see the USB
// status row's button, ui.Window's usbBrokerRow, and App.EnableUSBBroker,
// the only place that flips the consent flag). No-op without that consent,
// or once the broker is already on disk.
func (a *App) ensureUSBBroker(ctx context.Context, token string) {
	if a.usbBroker == nil || !a.cfg.USBBrokerConsentGiven() || a.usbBroker.Staged() {
		return
	}
	log.Printf("[app] USB broker consent on record — staging usb-broker")
	if err := a.stageAndStartUSBBroker(ctx, token, nil); err != nil {
		log.Printf("[app] usb-broker not staged/started (will retry next interval): %v", err)
	}
}

// EnableUSBBroker records the user's one-time, explicit consent to run the
// closed usb-broker binary (see the USB status row's button in ui.Window)
// and immediately stages+starts it if entitled — the same staging path
// ensureUSBBroker's watchdog would otherwise only reach on its next tick.
// onProgress mirrors DownloadRustShine's identical threading contract.
func (a *App) EnableUSBBroker(onProgress entitlement.ProgressFunc) error {
	consent := true
	next := a.cfg
	next.USBBrokerConsent = &consent
	if err := a.SaveConfig(next); err != nil {
		return err
	}

	token := strings.TrimSpace(a.cfg.EntitlementToken)
	if token == "" {
		// Consent alone doesn't require entitlement -- ensureUSBBroker's own
		// next watchdog tick (or a subsequent EnableUSBBroker retry) picks
		// this up the moment a token exists. Not an error: recording "yes,
		// I want the proprietary broker enabled" is a valid, standalone
		// action even before/without ever linking a license.
		return nil
	}
	return a.stageAndStartUSBBroker(context.Background(), token, onProgress)
}

// stageAndStartUSBBroker is the "download the release, then launch it" pair
// both ensureUSBBroker's watchdog path and EnableUSBBroker's immediate path
// need -- factored out purely to keep that pairing in one place; callers
// still decide for themselves whether a failure here is fire-and-forget
// (logged, retried on the next tick) or something the caller should
// propagate to the user (EnableUSBBroker's return value, surfaced by the
// consent button).
func (a *App) stageAndStartUSBBroker(ctx context.Context, token string, onProgress entitlement.ProgressFunc) error {
	if a.usbBroker == nil {
		return fmt.Errorf("usb passthrough not available on this platform")
	}
	if err := entitlement.StageUSBBroker(ctx, a.cfg.StateDir, token, onProgress); err != nil {
		return err
	}
	return a.usbBroker.Start()
}

// ensureRustShineFresh makes sure a licensed/trialing customer always has
// the latest staged RustShine build, with no manual "Download RustShine"
// click required: called both right after a purchase/trial succeeds
// (applyIssuedToken, so a brand-new customer's download starts within
// seconds, not on whatever the next 6h watchdog tick happens to be) and
// from recheckEntitlement's own cadence afterward (so later releases keep
// reaching them). A no-op with no observable effect whenever
// entitlementToken is empty -- i.e. never entitled, or already downgraded
// to Sunshine -- so an unentitled install never downloads or updates
// RustShine at all, only ever runs Sunshine, exactly like
// DownloadRustShine's own explicit entitlement.VerifyForHardware gate.
func (a *App) ensureRustShineFresh(ctx context.Context, entitlementToken string) {
	if strings.TrimSpace(entitlementToken) == "" {
		return
	}
	if !a.rustshineStaged() {
		log.Printf("[app] entitlement linked — downloading rustshine")
		if err := a.stageRustShine(ctx, entitlementToken, nil); err != nil {
			// Non-fatal -- retried at the next watchdog interval (offline,
			// transient backend error, ...). Until it succeeds, this install
			// simply keeps running Sunshine.
			log.Printf("[app] rustshine initial download failed (will retry next interval): %v", err)
			return
		}
		log.Printf("[app] rustshine downloaded")
		a.entMu.Lock()
		a.entStatus.RustShineStaged = a.rustshineStaged()
		a.entMu.Unlock()
		// New() fell back to Sunshine because the binary wasn't staged yet;
		// honour a saved RustShine preference now that it is.
		if a.cfg.PreferredBackend == "rustshine" && a.streamKindView.Load() != "rustshine" {
			if err := a.SetStreamBackend("rustshine"); err != nil {
				log.Printf("[app] switching to preferred rustshine: %v", err)
			}
		}
		return
	}
	a.checkRustShineUpdate(ctx, entitlementToken)
}

// checkRustShineUpdate re-stages RustShine if the backend has published a
// newer build than whatever's currently staged -- called only via
// ensureRustShineFresh above, once RustShine is already known to be staged
// (a no-op otherwise, see CheckRustShineUpdate's own doc comment).
//
// This is what makes a RustShine-only fix (one that ships in a new
// gamestream-server release but not a new agent build -- e.g. the
// NV_KEYBOARD_PACKET modifiers-byte fix this mechanism was added for)
// actually reach an agent that already downloaded RustShine before that
// fix existed.
func (a *App) checkRustShineUpdate(ctx context.Context, entitlementToken string) {
	if strings.TrimSpace(entitlementToken) == "" {
		return
	}
	needsUpdate, version, err := entitlement.CheckRustShineUpdate(ctx, a.cfg.StateDir, entitlementToken)
	if err != nil {
		log.Printf("[app] rustshine update check failed (will retry next interval): %v", err)
		return
	}
	if !needsUpdate {
		a.entMu.Lock()
		a.pendingStreamerUpdate = ""
		a.entMu.Unlock()
		return
	}
	if !a.cfg.StreamerAutoUpdateEnabled() {
		a.entMu.Lock()
		a.pendingStreamerUpdate = version
		a.entMu.Unlock()
		log.Printf("[app] rustshine update available (%s) — auto-update off, waiting for the user", version)
		return
	}

	// Same reentrancy guard as CheckRustShineUpdateNow (see its doc comment)
	// -- this silent background tick and a manual "check for updates" click
	// share the same StageRustShine/stopRustShineForUpdate sequence and can
	// otherwise race each other just as easily as two clicks can.
	a.entMu.Lock()
	if a.entStatus.RustShineUpdateInProgress {
		a.entMu.Unlock()
		return
	}
	a.entStatus.RustShineUpdateInProgress = true
	a.entMu.Unlock()
	a.setRustShineUpdatePaused(true)
	defer func() {
		a.entMu.Lock()
		a.entStatus.RustShineUpdateInProgress = false
		a.entMu.Unlock()
		a.setRustShineUpdatePaused(false)
	}()

	if err := a.applyRustShineUpdate(ctx, entitlementToken, version); err != nil {
		log.Printf("[app] rustshine auto-update to %s failed (will retry next interval): %v", version, err)
	}
}

// applyRustShineUpdate downloads and stages a newer USBridge-streamer
// build, then hot-swaps the running backend if it's already rustshine.
// Callers must hold the RustShineUpdateInProgress / UpdatePauser pairing
// around this (checkRustShineUpdate and CheckRustShineUpdateNow).
func (a *App) applyRustShineUpdate(ctx context.Context, entitlementToken, version string) error {
	log.Printf("[app] rustshine update available (%s) — downloading", version)
	stopped := a.stopRustShineForUpdate()
	if err := a.stageRustShine(ctx, entitlementToken, nil); err != nil {
		if stopped {
			// The bundle on disk may have changed under the launcher
			// decision made before the update: re-verify first.
			a.syncSunshineCapExec()
			a.startSunshineNow()
		}
		return err
	}
	log.Printf("[app] rustshine updated to %s", version)
	// Only re-stage if the user already consented to running the broker at
	// all (see App.EnableUSBBroker/ensureUSBBroker's doc comments) -- an
	// update to RustShine must never be what silently pulls the closed
	// usb-broker binary down for the first time.
	if a.cfg.USBBrokerConsentGiven() {
		if err := entitlement.StageUSBBroker(ctx, a.cfg.StateDir, entitlementToken, nil); err != nil {
			log.Printf("[app] usb-broker not re-staged (USB passthrough unavailable): %v", err)
		} else if a.usbBroker != nil && a.usbBroker.Staged() {
			// The running broker keeps the old build until it restarts (on
			// Windows the new one waits beside it as a ".new"); the streamer
			// restarts below anyway, so its USB devices re-attach right after.
			if err := a.usbBroker.Restart(); err != nil {
				log.Printf("[app] usb-broker restart after its update: %v", err)
			} else {
				log.Printf("[app] usb-broker updated and restarted")
			}
		}
	}
	a.entMu.Lock()
	a.entStatus.RustShineStaged = a.rustshineStaged()
	a.pendingStreamerUpdate = ""
	a.entMu.Unlock()
	if strings.TrimSpace(a.cfg.StreamerUpdateSnoozed) != "" {
		next := a.cfg
		next.StreamerUpdateSnoozed = ""
		if err := a.SaveConfig(next); err != nil {
			log.Printf("[app] warning: could not clear snoozed streamer update: %v", err)
		}
	}
	a.restartRustShineIfActive()
	return nil
}

// setRustShineUpdatePaused tells the active backend (if it implements
// streamhost.UpdatePauser -- currently just rustshine) to hold off opening
// any new handle on its own executable for as long as an update is in
// flight. See that interface's doc comment for the exact
// --list-capture-devices race this closes; a no-op for any backend that
// doesn't implement it (Sunshine has no equivalent hot-replace-while-running
// concern).
func (a *App) setRustShineUpdatePaused(paused bool) {
	if up, ok := a.stream.(streamhost.UpdatePauser); ok {
		up.SetUpdateInProgress(paused)
	}
}

// stopRustShineForUpdate stops the active RustShine process before an
// update is staged, so entitlement.StageRustShine's os.Rename over the
// binary doesn't race a running process for the file. Only Windows actually
// needs this -- it locks a running .exe's image against rename/replace,
// while POSIX (Linux/macOS) allows renaming over an open file just fine, so
// staging there already hot-swaps cleanly without interrupting anything.
// Confirmed live as a real, self-perpetuating failure mode on Windows: a
// gamestream-server build that crashes instantly on every launch (e.g. a
// staged binary that predates a CLI flag the agent now always passes) keeps
// the exe open/locked essentially 100% of the time between respawns, so the
// very update that would fix the crash could never land either -- staging
// kept failing every single watchdog interval, forever.
//
// Returns whether it actually stopped anything, so the caller knows whether
// it's now responsible for relaunching (see both call sites).
func (a *App) stopRustShineForUpdate() bool {
	if runtime.GOOS != "windows" {
		return false
	}
	if a.currentStreamKind() != "rustshine" || a.stream == nil {
		return false
	}
	log.Printf("[app] stopping rustshine before staging update (Windows can't replace a running .exe)")
	_ = a.stream.Stop()
	// Stop() only kills the single instance this backend struct is tracking
	// (b.proc) -- confirmed live as insufficient on its own: a manual
	// StageRustShine run against a completely clean process list (zero
	// gamestream-server.exe alive) staged and renamed in ~1.3s every time,
	// while the exact same call from this update flow kept losing to
	// "Access is denied" for the *entire* 20s renameWithRetry budget,
	// despite Stop() reporting its tracked pid gone in milliseconds --
	// meaning some second, untracked gamestream-server.exe instance (this
	// backend struct never learned about it, so Stop() had nothing to kill
	// it with) was the one actually holding the file open. Root cause of
	// that second instance's existence not fully pinned down; this sweeps
	// unconditionally by name as a belt-and-suspenders guarantee that
	// nothing named gamestream-server.exe survives this point, regardless
	// of how it got there or whether this backend ever tracked it.
	killCmd := exec.Command("taskkill", "/F", "/IM", "usbridge-streamer.exe")
	maybeHideWindow(killCmd)
	_ = killCmd.Run()
	legacyKillCmd := exec.Command("taskkill", "/F", "/IM", "gamestream-server.exe")
	maybeHideWindow(legacyKillCmd)
	_ = legacyKillCmd.Run()
	// Stop() only signals termination; give the OS a moment to actually
	// release the exe's image-section file lock before the upcoming rename.
	time.Sleep(500 * time.Millisecond)
	// Do not escalate to a UAC-elevated taskkill. A consent prompt runs on
	// the secure desktop: the update flow has already stopped capture, so
	// video is gone, and a remote client cannot click Yes. Leftover
	// elevated copies (Autostart-at-Boot LocalSystem instance before
	// reboot, GPU clock-lock daemon using the same .exe) are left alone;
	// StageRustShine writes dest+".new" when dest is still locked.
	if processRunning("usbridge-streamer.exe") || processRunning("gamestream-server.exe") {
		log.Printf("[app] usbridge-streamer.exe still running after taskkill — not requesting UAC; staging will use a sidecar if the .exe stays locked")
	}
	return true
}

// processRunning reports whether any process named imageName (e.g.
// "gamestream-server.exe") is currently running, via `tasklist`'s own
// image-name filter -- used by stopRustShineForUpdate only to log that a
// leftover instance is still holding the .exe (staging then writes a
// sidecar instead of prompting UAC).
func processRunning(imageName string) bool {
	cmd := exec.Command("tasklist", "/NH", "/FI", "IMAGENAME eq "+imageName)
	maybeHideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(out)), strings.ToLower(imageName))
}

// stageRustShine downloads and stages RustShine. With the KMS launcher
// installed (Linux), the new build must pass that launcher's own
// verification before it replaces the current one -- so an update can
// never leave the next restart without CAP_SYS_ADMIN (and the remote user
// staring at a portal prompt they can't click). Without the launcher it's
// plain entitlement.StageRustShine.
func (a *App) stageRustShine(ctx context.Context, token string, onProgress entitlement.ProgressFunc) error {
	var verify entitlement.BundleVerifier
	if runtime.GOOS == "linux" && a.perms != nil && a.perms.KMSCaptureGranted(streamerlaunch.InstallPath) {
		verify = func(bundleDir string) error {
			_, err := a.perms.StreamerLauncherVerify(bundleDir)
			return err
		}
	}
	return entitlement.StageRustShineVerified(ctx, a.cfg.StateDir, token, onProgress, verify)
}

// restartRustShineIfActive re-execs the running RustShine subprocess (via
// the same generic RestartSunshine plumbing every other capability/config
// change already uses -- see that method's doc comment on why the name is
// legacy but the behavior isn't Sunshine-specific) so a version that was
// just staged onto disk actually takes effect immediately, instead of
// silently sitting there unused until the next unrelated restart. A no-op
// whenever Sunshine is the active backend -- the freshly staged binary
// isn't running yet either way, so there's nothing to hot-swap; it'll be
// picked up the next time something switches to RustShine (SetStreamBackend
// always re-resolves BinaryPath, it doesn't cache the old one).
//
// Called from both the silent background watchdog (checkRustShineUpdate
// above) and the GUI's explicit "Check for updates" button
// (CheckRustShineUpdateNow) -- same contract either way: whichever one
// staged a newer build, the active session should never keep running the
// old bytes just because nobody happened to flip backends or restart the
// whole agent afterward.
func (a *App) restartRustShineIfActive() {
	if a.currentStreamKind() != "rustshine" {
		return
	}
	// Re-check the launcher against the freshly staged bundle before the
	// restart rather than trusting the decision made for the old build.
	a.syncSunshineCapExec()
	log.Printf("[app] rustshine is the active backend — restarting it to pick up the update")
	if err := a.RestartSunshine(); err != nil {
		// Non-fatal: the newer binary is already staged and will be used
		// on the next restart regardless of how it's triggered (manual
		// backend toggle, agent restart, ...) -- this only means *this*
		// attempt at an immediate, no-agent-restart hot-swap didn't land.
		log.Printf("[app] restarting rustshine to apply the update failed (will still be picked up on the next restart): %v", err)
	}
}

// CheckRustShineUpdateNow is the GUI's explicit "Check for updates" button
// (as opposed to ensureRustShineFresh/checkRustShineUpdate's own silent
// background cadence) -- same entitlement.CheckRustShineUpdate +
// StageRustShine pair, but synchronous-enough to report a real error back
// to the click that triggered it, and unconditional on whether RustShine is
// currently staged at all (the very first "Download RustShine" click in the
// license dialog covers that case already; this one is a no-op, not an
// error, if nothing's staged yet -- see CheckRustShineUpdate's own doc
// comment). Hot-swaps in place via restartRustShineIfActive if an update
// was actually found and applied.
func (a *App) CheckRustShineUpdateNow() error {
	// Deliberately not a local entitlement.Verify call the way
	// DownloadRustShine's does one -- this button is conceptually "run the
	// same check checkRustShineUpdate's background watchdog already does,
	// right now instead of waiting for the next interval", and that
	// watchdog has never required local verification either (see its own
	// and ensureRustShineFresh's doc comments): ResolveDownload's backend
	// round-trip is the actual authority on whether this token still
	// grants access, same as it is for every other RustShine download
	// path. A locally well-formed-but-since-revoked token still gets a
	// real error here -- just from the backend response, one network
	// round-trip later, not from this local shortcut.
	token := a.cfg.EntitlementToken
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("not currently entitled: no entitlement token")
	}

	// Guard against two update attempts racing each other (a double-click on
	// this button, or overlapping with the silent background watchdog's own
	// checkRustShineUpdate tick). Confirmed live as a real failure mode: two
	// concurrent StageRustShine calls each stop/restart the process out from
	// under the other, so both "Access is denied" rename attempts land in
	// whatever brief window the other one's restart leaves open, and neither
	// ever succeeds. RustShineUpdateInProgress already existed for the UI to
	// show a spinner; it doubles as that lock here.
	a.entMu.Lock()
	if a.entStatus.RustShineUpdateInProgress {
		a.entMu.Unlock()
		return fmt.Errorf("rustshine update already in progress")
	}
	a.entStatus.RustShineUpdateInProgress = true
	a.entStatus.LastError = ""
	a.entMu.Unlock()
	// Same UpdatePauser guard checkRustShineUpdate's own background tick
	// takes (see setRustShineUpdatePaused's doc comment) -- this manual
	// button races the same --list-capture-devices helper the same way a
	// silent watchdog tick does, so it needs the same protection. Set/reset
	// here regardless of whether needsUpdate turns out true below: cheap
	// no-op either way, and keeps a single place responsible for the
	// pause/unpause pairing instead of threading a conditional through the
	// early-return path too.
	a.setRustShineUpdatePaused(true)
	defer func() {
		a.entMu.Lock()
		a.entStatus.RustShineUpdateInProgress = false
		a.entMu.Unlock()
		a.setRustShineUpdatePaused(false)
	}()

	ctx := context.Background()
	needsUpdate, version, err := entitlement.CheckRustShineUpdate(ctx, a.cfg.StateDir, token)
	if err != nil {
		a.setEntError(fmt.Sprintf("update check failed: %v", err))
		return err
	}
	if !needsUpdate {
		log.Printf("[app] rustshine is already up to date")
		a.entMu.Lock()
		a.pendingStreamerUpdate = ""
		a.entMu.Unlock()
		return nil
	}

	if err := a.applyRustShineUpdate(ctx, token, version); err != nil {
		a.setEntError(fmt.Sprintf("update failed: %v", err))
		return err
	}
	return nil
}

// waitForMonitorCorrelation blocks briefly for Sunshine's KMS/Wayland
// per-monitor CRTC-offset correlation (correlate_to_wayland in kmsgrab.cpp,
// see MonitorIndexByName's doc comment) to finish after a restart.
// WaitReady only confirms the HTTPS/NvHTTP listener is up, which binds well
// before that correlation completes — a client that reconnects (Launch) in
// that gap locks in a session using default/uncorrelated per-connector CRTC
// offsets, so absolute-mouse coordinates land on the wrong monitor or drift
// near its edges for that whole session. A *second*, later reconnect (e.g.
// triggered by an unrelated codec change) picks up the by-then-finished
// correlation and looks like it "fixed" the mouse — this closes that gap by
// making the restart itself wait for correlation instead.
//
// Also required on Windows, for a different reason: Sunshine's Windows
// display_device backend identifies monitors by a device_id GUID that only
// becomes knowable by parsing "Currently available display devices:" back
// out of Sunshine's own log (see sunshine_devices_windows.go) — there is no
// way to predict it in advance. Without this wait, a monitor pick made in
// the tiny window between Sunshine's admin port coming up (WaitReady) and
// that log block actually being appended falls back to capture.Service's
// generic display:N enumeration; videoSetDevice strips its prefix the same
// as a real winid:, so a bare numeric index (e.g. "0") gets written into
// output_name. Sunshine's Windows backend only accepts the GUID form, so it
// can't resolve a plain "0" to any device and silently falls back to
// auto-pick (== the primary/first monitor) from then on, regardless of what
// the client asks for — this is what made monitor switching look like a
// permanent no-op on Windows w/ Sunshine while the same flow worked fine on
// RustShine (whose "monitor_index" key genuinely does take a small numeric
// index).
//
// No-op on macOS: its Sunshine backend has no log-derived device list to
// wait for (ListCaptureDevices always returns nil there — see
// sunshine_devices_other.go), and a bounded poll would just time out doing
// nothing.
func (a *App) waitForMonitorCorrelation() {
	if a.stream == nil {
		return
	}
	switch runtime.GOOS {
	case "linux":
		if a.stream.CaptureMode() != "kms" {
			return
		}
	case "windows":
		// fall through to the poll below
	default:
		return
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(a.stream.ListCaptureDevices()) > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// kmsCaptureTarget returns what the Linux KMS-capture grant targets for
// the active backend -- a key permissions.KMSCaptureGranted/
// RequestKMSCapture understand, never a file that itself gets a setcap:
//
//   - RustShine: streamerlaunch.InstallPath. Granting installs the
//     root-owned usbridge-streamer-launch once; it runs only builds signed
//     by rust-shine's release key, so streamer updates no longer drop the
//     grant (a setcap directly on usbridge-streamer did, on every update,
//     since each update writes a new inode).
//   - Sunshine: the bundled Sunshine install-tree root. Granting copies it
//     into the root-owned streamerlaunch.SunshineDir, which the same
//     launcher execs. This replaced a user-writable sunshine_capexec that
//     gave CAP_SYS_ADMIN to any binary it was pointed at.
//
// Both need CAP_SYS_ADMIN in the first place for fullscreen games'
// direct-scanout buffers (confirmed live: plain KMS capture of the
// composited desktop worked without it, a fullscreen game didn't).
func (a *App) kmsCaptureTarget() string {
	if a.currentStreamKind() == "rustshine" {
		if a.stream != nil {
			return a.stream.CapExecPath()
		}
		return ""
	}
	return a.SunshineCapExecPath()
}

// removeLegacyKMSGrants deletes the capability-carrying files earlier
// builds left in the user-writable state dir:
//
//   - capexec-runtime/sunshine-capexec: cap_sys_admin on a launcher that
//     exec'd any path it was given -- CAP_SYS_ADMIN for any process of this
//     user. Removed unconditionally; nothing launches it anymore.
//   - a setcap directly on the staged usbridge-streamer, but only once the
//     root-owned launcher can actually take over -- installed AND
//     accepting the staged signed bundle (see shouldDropLegacySetcap). A
//     user can't drop a file capability in place (that needs
//     CAP_SETFCAP), but rewriting the file does: the kernel never copies
//     security.capability to a new inode.
func (a *App) removeLegacyKMSGrants() {
	if runtime.GOOS != "linux" || a.cfg.StateDir == "" {
		return
	}
	for _, name := range []string{"capexec-runtime", "rustshine-capexec-runtime"} {
		legacyCapexec := filepath.Join(a.cfg.StateDir, name)
		if _, err := os.Stat(legacyCapexec); err != nil {
			continue
		}
		if err := os.RemoveAll(legacyCapexec); err != nil {
			log.Printf("[app] could not remove legacy %s: %v", legacyCapexec, err)
		} else {
			log.Printf("[app] removed legacy capability launcher %s", legacyCapexec)
		}
	}
	if a.perms == nil {
		return
	}
	bin := entitlement.StagePath(a.cfg.StateDir)
	if !permissions.HasFileCapability(bin) {
		return
	}
	installed := a.perms.KMSCaptureGranted(streamerlaunch.InstallPath)
	var verifyErr error
	if installed {
		_, verifyErr = a.perms.StreamerLauncherVerify(streamerlaunch.BundleDir(a.rustshineStagedDir()))
	}
	if !shouldDropLegacySetcap(installed, verifyErr) {
		if installed {
			log.Printf("[app] keeping legacy setcap on %s until the launcher accepts a staged bundle: %v", bin, verifyErr)
		}
		return
	}
	if err := rewriteWithoutXattrs(bin); err != nil {
		log.Printf("[app] could not drop legacy setcap on %s: %v", bin, err)
		return
	}
	log.Printf("[app] dropped legacy setcap on %s (launcher %s is installed)", bin, streamerlaunch.InstallPath)
}

// shouldDropLegacySetcap: the per-binary setcap may only go once the
// launcher can run the staged build itself. Dropping it on "launcher
// installed" alone -- before any signed bundle had been staged (the
// backend didn't pass the manifest through yet) -- left RustShine with no
// CAP_SYS_ADMIN at all: confirmed live, KMS capture failed with
// "framebuffer has no exportable plane-0 handle" and clients got no video.
func shouldDropLegacySetcap(launcherInstalled bool, bundleVerifyErr error) bool {
	return launcherInstalled && bundleVerifyErr == nil
}

// rewriteWithoutXattrs replaces p with a fresh copy of its bytes and mode.
func rewriteWithoutXattrs(p string) error {
	st, err := os.Stat(p)
	if err != nil {
		return err
	}
	src, err := os.Open(p)
	if err != nil {
		return err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(filepath.Dir(p), ".rewrite-*")
	if err != nil {
		return err
	}
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Chmod(st.Mode().Perm()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// KMSCaptureTargetPath exposes kmsCaptureTarget to a GUI thin client (see
// runThinClientGUI), which runs the pkexec grant itself in its own session.
func (a *App) KMSCaptureTargetPath() string { return a.kmsCaptureTarget() }

// rustshineStagedDir is the directory holding the staged usbridge-streamer
// (and its signed release bundle, streamerlaunch.BundleDirName).
func (a *App) rustshineStagedDir() string {
	return filepath.Dir(entitlement.StagePath(a.cfg.StateDir))
}

// rustshineLauncherPath returns streamerlaunch.InstallPath when RustShine
// should launch through it: KMS capture mode, launcher installed, and the
// launcher accepts the staged bundle with its capability effective. Any
// failure falls back to "" (plain exec) and is logged -- never a hard
// failure that would take the stream down.
func (a *App) rustshineLauncherPathFor(b streamhost.Backend) string {
	if runtime.GOOS != "linux" || a.perms == nil || a.captureModeOf(b) != "kms" {
		return ""
	}
	if !a.perms.KMSCaptureGranted(streamerlaunch.InstallPath) {
		return ""
	}
	version, err := a.perms.StreamerLauncherVerify(streamerlaunch.BundleDir(a.rustshineStagedDir()))
	if err != nil {
		log.Printf("[app] rustshine: launcher installed but staged bundle not usable, plain exec: %v", err)
		return ""
	}
	log.Printf("[app] rustshine: launching %s via %s", version, streamerlaunch.InstallPath)
	return streamerlaunch.InstallPath
}

// KMSCaptureGranted reports whether the file KMS capture actually needs
// CAP_SYS_ADMIN on for the active backend (see kmsCaptureTarget) has it.
func (a *App) KMSCaptureGranted() bool {
	// A streamer that captures through the compositor (Punktfunk) has no
	// KMS grant to check; without this the row asked permissions.Service
	// about an empty launcher path and showed a Grant that did nothing.
	if ca, ok := a.compositorCapture(); ok {
		return ca.CaptureGranted()
	}
	if a.perms == nil {
		return false
	}
	// The launcher file alone is not enough: if it refuses the staged
	// bundle (e.g. after a streamer update), RustShine silently runs by
	// plain exec without CAP_SYS_ADMIN -- report that so the UI offers the
	// grant/refresh instead of showing it as done.
	if runtime.GOOS == "linux" && a.currentStreamKind() == "rustshine" && a.SunshineCaptureMode() == "kms" {
		if b, ok := a.stream.(interface{ LauncherActive() bool }); ok && !b.LauncherActive() {
			return false
		}
	}
	return a.perms.KMSCaptureGranted(a.kmsCaptureTarget())
}

// compositorCapture is the active backend's own capture-permission check,
// for the one that has it (see streamhost.CaptureAccess).
// Read without streamMu, like KMSCaptureGranted's own look at a.stream: the
// GUI asks on every refresh, and a backend switch holds that lock for as
// long as the switch takes.
func (a *App) compositorCapture() (streamhost.CaptureAccess, bool) {
	ca, ok := a.stream.(streamhost.CaptureAccess)
	return ca, ok
}

// RequestKMSCapture grants CAP_SYS_ADMIN to whichever file KMS capture
// actually needs it on for the active backend (see kmsCaptureTarget) —
// prompts for elevation via pkexec — then restarts the stream host so the
// newly-granted capability is actually picked up.
func (a *App) RequestKMSCapture() bool {
	if ca, ok := a.compositorCapture(); ok {
		if err := ca.RequestCapture(); err != nil {
			log.Printf("[app] screen capture for %s: %v", a.StreamerName(), err)
			return false
		}
		// A streamer that was refused at startup keeps that answer.
		if err := a.RestartSunshine(); err != nil {
			log.Printf("[app] failed to restart %s after granting screen capture: %v", a.StreamerName(), err)
		}
		return true
	}
	if a.perms == nil {
		return false
	}
	granted := a.perms.RequestKMSCapture(a.kmsCaptureTarget())
	if granted {
		a.syncSunshineCapExec()
		if err := a.RestartSunshine(); err != nil {
			log.Printf("[app] failed to restart Sunshine after granting KMS capability: %v", err)
		}
	}
	return granted
}

// RecheckKMSCapture re-syncs the capexec launcher's CAP_SYS_ADMIN capability
// from its current on-disk state and restarts Sunshine if it's now granted.
// Unlike RequestKMSCapture, this never runs pkexec itself — it exists for
// the case where something else already granted the capability on this
// exact path (a GUI thin client's own local pkexec call, see
// internal/adminapi.Server.handleKMSRecheck and cmd/usbridge_agent's
// runThinClientGUI) and this instance just needs to notice and pick it up.
func (a *App) RecheckKMSCapture() bool {
	a.syncSunshineCapExec()
	granted := a.KMSCaptureGranted()
	if granted {
		if err := a.RestartSunshine(); err != nil {
			log.Printf("[app] failed to restart Sunshine after KMS recheck: %v", err)
		}
	}
	return granted
}

// GPUClockLockSupported reports whether this platform's permissions backend
// can attempt an NVML GPU-clock lock at all -- true only on Windows (see
// internal/permissions/service_windows.go); every other platform's Service
// stubs this to false since there's no equivalent NVML-idle-stall problem
// documented there yet.
func (a *App) GPUClockLockSupported() bool {
	if a.perms == nil {
		return false
	}
	return a.perms.GPUClockLockSupported()
}

// NvidiaPowerPrefsSupported reports whether the NVIDIA power-mode/two-pass
// encoder preferences UI should be offered -- true on Windows and Linux
// (see internal/permissions's per-platform NvidiaPowerPrefsSupported for
// why this differs from GPUClockLockSupported above).
func (a *App) NvidiaPowerPrefsSupported() bool {
	if a.perms == nil {
		return false
	}
	return a.perms.NvidiaPowerPrefsSupported()
}

// LockGPUClocksEnabled returns the persisted "Lock GPU clocks" setting.
func (a *App) LockGPUClocksEnabled() bool {
	return a.cfg.LockGPUClocksEnabled
}

func (a *App) StreamerAutoUpdateEnabled() bool {
	return a.cfg.StreamerAutoUpdateEnabled()
}

func (a *App) SetStreamerAutoUpdate(enabled bool) error {
	next := a.cfg
	v := enabled
	next.StreamerAutoUpdate = &v
	if enabled {
		next.StreamerUpdateSnoozed = ""
	}
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if enabled {
		a.entMu.Lock()
		pending := a.pendingStreamerUpdate
		a.entMu.Unlock()
		if pending != "" {
			go func() {
				if err := a.CheckRustShineUpdateNow(); err != nil {
					log.Printf("[app] rustshine apply after enabling auto-update failed: %v", err)
				}
			}()
		}
	}
	return nil
}

func (a *App) SnoozeStreamerUpdate(version string) error {
	next := a.cfg
	next.StreamerUpdateSnoozed = strings.TrimSpace(version)
	return a.SaveConfig(next)
}

func (a *App) RemoteWindowLockEnabled() bool {
	return a.cfg.RemoteWindowLockEnabled()
}

func (a *App) SetRemoteWindowLock(enabled bool) error {
	next := a.cfg
	v := enabled
	next.RemoteWindowLock = &v
	return a.SaveConfig(next)
}

// SetLockGPUClocksEnabled persists the "Lock GPU clocks" setting and, if
// turning it on, immediately arms the lock (see applyGPUClockLock) instead of
// waiting for the next stream-host start. Turning it off does NOT tear down
// an already-armed lock daemon -- it only stops future arming; the daemon
// watches this agent's own PID (not the stream host's -- see
// applyGPUClockLock) and exits on its own once the agent process does, see
// permissions.Service.RequestGPUClockLock.
func (a *App) SetLockGPUClocksEnabled(enabled bool) error {
	next := a.cfg
	next.LockGPUClocksEnabled = enabled
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if enabled {
		a.applyGPUClockLock()
	}
	return nil
}

// applyGPUClockLock launches the elevated GPU-clock-lock daemon, if the
// setting is enabled and supported on this platform, watching *this agent
// process's own* PID rather than the stream host's. NVML's clock lock is
// GPU-wide, not scoped to whichever process the daemon happens to watch --
// watch-pid only exists so the daemon knows when to exit and release it --
// so tying it to the agent's own (long-lived, restart-free) PID instead of
// the stream host's means arming it once per agent run is enough: it keeps
// holding the lock across every later stream-host restart (sunshineWatchdog
// re-invoking startSunshine every 15s, or SetSunshineOutputName switching
// the captured monitor, both of which hand the stream host a brand new PID)
// without ever relaunching the elevated helper or popping a second UAC
// prompt. That matters because a UAC prompt runs on the secure desktop and
// can't be dismissed from a remote session -- switching monitors mid-stream
// used to hard-lock the session behind an unreachable local prompt. The
// trade-off: clocks now stay locked for as long as the agent runs with the
// setting on, not just while a stream host happens to be alive.
// gpuClockArmed guards against re-launching the helper (and prompting again)
// on every call; errors (including a declined UAC prompt) are logged only,
// never fatal to starting the stream, and deliberately leave gpuClockArmed
// unset so the next call retries instead of giving up silently forever.
//
// Superseded by the NVIDIA power mode "max" (the driver profile keeps the clocks
// up without elevation) and no longer offered in the UI: a lock turned on
// before only still applies while that preference is off.
func (a *App) applyGPUClockLock() {
	if !a.cfg.LockGPUClocksEnabled || a.cfg.NvidiaMaxPerformanceOK() || a.perms == nil || a.stream == nil {
		return
	}
	if !a.perms.GPUClockLockSupported() {
		return
	}
	binPath := a.stream.BinaryPath()
	if binPath == "" {
		return
	}
	a.gpuClockMu.Lock()
	alreadyArmed := a.gpuClockArmed
	a.gpuClockMu.Unlock()
	if alreadyArmed {
		return
	}
	if err := a.perms.RequestGPUClockLock(binPath, os.Getpid()); err != nil {
		log.Printf("[app] failed to lock GPU clocks: %v", err)
		return
	}
	a.gpuClockMu.Lock()
	a.gpuClockArmed = true
	a.gpuClockMu.Unlock()
}

// TailscaleStatus returns the current Tailscale status in the format expected by /api/auth/sync.
func (a *App) TailscaleStatus() *api.TailscaleStatusInfo {
	if a.ts == nil {
		return nil
	}
	status, err := a.ts.Status(context.Background())
	if err != nil || status == nil {
		return nil
	}
	return toTailscaleStatusInfo(status)
}

// RegisterTailscale authorizes this node on the tailnet, used by /api/auth/sync
// and /api/auth/tailscale/register. An empty authKey triggers interactive login
// and returns an AuthURL for the caller to open in a browser.
func (a *App) RegisterTailscale(ctx context.Context, authKey, hostname string) (*api.TailscaleStatusInfo, error) {
	if a.ts == nil {
		return nil, fmt.Errorf("tailscale service unavailable")
	}
	status, err := a.ts.Register(ctx, authKey, hostname)
	if err != nil {
		return nil, err
	}
	return toTailscaleStatusInfo(status), nil
}

func toTailscaleStatusInfo(status *tailscale.Status) *api.TailscaleStatusInfo {
	if status == nil {
		return nil
	}
	return &api.TailscaleStatusInfo{
		Running:  status.Running,
		LoggedIn: status.LoggedIn,
		Backend:  status.Backend,
		DNSName:  status.Self.DNSName,
		HostName: status.Self.HostName,
		IP4:      status.Self.IP4,
		AuthURL:  status.AuthURL,
	}
}

// QRLink returns the quick-connect deep link and the master key for the /api/auth/qr/link endpoint.
func (a *App) QRLink() (string, string) {
	masterKey := strings.TrimSpace(a.cfg.MasterKey)
	if masterKey == "" {
		return "", ""
	}
	internalHost := localIPv4()
	// Prefer the device's own <label>.device.usbridge.io hostname (see
	// internal/tlshost, internal/devicecert) over the bare LAN IP once
	// deviceCertWatchdog has registered+fetched one -- it re-resolves via
	// DNS on every connect instead of pinning a LAN IP that goes stale the
	// moment this machine's address changes (DHCP renewal, different
	// network), and a browser web client needs this exact hostname anyway
	// (SNI is never sent for an IP-literal connection, so
	// tlshost.Manager.GetCertificate can never select the trusted device
	// cert -- only the untrusted self-signed one -- for a bare-IP
	// connection). Falls back to the bare IP when no hostname is registered
	// yet, e.g. offline or still within the first tick.
	if deviceHost := a.DeviceHostname(); deviceHost != "" {
		internalHost = deviceHost
	}
	tailscaleHost := ""
	if a.ts != nil {
		if status, err := a.ts.Status(context.Background()); err == nil && status != nil && status.LoggedIn {
			switch {
			case strings.TrimSpace(status.Self.IP4) != "":
				tailscaleHost = strings.TrimSpace(status.Self.IP4)
			case strings.TrimSpace(status.Self.DNSName) != "":
				tailscaleHost = strings.TrimSpace(status.Self.DNSName)
			}
		}
	}
	// hwID (best-effort -- an empty string just omits hw_id from the link,
	// same "degrade gracefully" posture every other field here already has)
	// lets a browser client that later can't reach this agent directly
	// address the Cloudflare signaling relay for it (see
	// agent/internal/streamhost/webrtc_signal_relay.go and
	// client/internal/webrtcweb/client_wasm.go's postOffer). Not a new
	// exposure: master_key, the actual API credential, is already in this
	// same plaintext link -- hw_id alone only lets someone request a
	// free-tier entitlement token for it (see desktopLicense.ts's
	// documented trust model), no new capability against this device.
	hwID, _ := hwid.Get()
	link := buildQRLink(netutil.WithAPIPort(internalHost, a.cfg.HTTPPort), netutil.WithAPIPort(tailscaleHost, a.cfg.HTTPPort), masterKey, hwID)
	return link, masterKey
}

// applyStreamSharedSecret hands secret to stream if it implements the
// optional interface{ SetSharedSecret([]byte) } -- only rustshineBackend
// does today (see its own doc comment). Same optional-interface probe
// pattern the client GUI layer already uses for SetAPISecret/
// SetTailscaleService on service.VideoClient implementations; a no-op for
// sunshineBackend, which has no equivalent (real Sunshine's own protocol
// has nothing this key would authenticate).
func applyStreamSharedSecret(stream streamhost.Backend, secret []byte) {
	if setter, ok := stream.(interface{ SetSharedSecret([]byte) }); ok {
		setter.SetSharedSecret(secret)
	}
}

// applyStreamWebRTCEnabled hands enabled to stream if it implements the
// optional interface{ SetWebRTCEnabled(bool) } -- only rustshineBackend
// does today, same optional-interface probe pattern as
// applyStreamSharedSecret above; a no-op for sunshineBackend.
func applyStreamWebRTCEnabled(stream streamhost.Backend, enabled bool) {
	if setter, ok := stream.(interface{ SetWebRTCEnabled(bool) }); ok {
		setter.SetWebRTCEnabled(enabled)
	}
}

// applyStreamUSBPassBridgeAddr hands addr to stream if it implements the
// optional interface{ SetUSBPassBridgeAddr(string) } -- only rustshineBackend
// does today, same optional-interface probe pattern as
// applyStreamSharedSecret above; a no-op for sunshineBackend, which has no
// browser USB passthrough / WebRTC DataChannel path to bridge into. addr is
// api.Server.StartUSBPassBridge's own localhost listener address, started
// once at boot (see the app.New call site) and re-applied here every time
// SetStreamBackend swaps in a new rustshineBackend instance, since the
// bridge listener itself outlives any individual backend.
func applyStreamUSBPassBridgeAddr(stream streamhost.Backend, addr string) {
	if setter, ok := stream.(interface{ SetUSBPassBridgeAddr(string) }); ok {
		setter.SetUSBPassBridgeAddr(addr)
	}
}

// DeviceHostname returns this machine's <label>.device.usbridge.io
// hostname (see internal/tlshost, internal/devicecert) once
// deviceCertWatchdog has registered one, "" otherwise or when the HTTPS
// listener is disabled -- nothing answers on that name without it. Shared
// by QRLink and the GUI's token dialog (ui.TokenProvider) so both hand out
// the same link.
func (a *App) DeviceHostname() string {
	if a.tlsMgr == nil || !a.cfg.TLSEnabledOK() {
		return ""
	}
	// The registered name, cert or not: a browser that comes to it is what
	// gets the cert issued (tlshost.Manager.GetCertificate).
	return a.tlsMgr.RegisteredHostname()
}

// CertStatus reports the HTTPS listener's current certificate -- see
// tlshost.Manager.CertStatus. Zero value (no hostname, LetsEncrypt false)
// when HTTPS is disabled or the manager hasn't been created yet, which the
// Status UI's cert row reads as "HTTPS off".
func (a *App) CertStatus() tlshost.CertStatus {
	if a.tlsMgr == nil || !a.cfg.TLSEnabledOK() {
		return tlshost.CertStatus{}
	}
	return a.tlsMgr.CertStatus()
}

func buildQRLink(internalHost, tailscaleHost, masterKey, hwID string) string {
	if masterKey == "" {
		return ""
	}
	if internalHost == "" && tailscaleHost == "" {
		return ""
	}
	values := url.Values{}
	if internalHost != "" {
		values.Set("internal_host", internalHost)
	}
	if tailscaleHost != "" {
		values.Set("tailscale_host", tailscaleHost)
	}
	values.Set("master_key", masterKey)
	if hwID != "" {
		values.Set("hw_id", hwID)
	}
	return "usbridge://connect?" + values.Encode()
}

func localIPv4() string {
	return netutil.PreferredIPv4()
}

func (a *App) SaveConfig(cfg config.Config) error {
	if err := config.Save(a.cfgPath, cfg); err != nil {
		return err
	}
	a.cfg = cfg
	return nil
}

func (a *App) Status() api.SystemStatus {
	return api.SystemStatus{
		Service: api.ServiceStatus{
			Status:    "running",
			Timestamp: time.Now(),
			Uptime:    time.Since(a.state.startedAt).String(),
		},
		Timestamp:     time.Now(),
		OS:            runtime.GOOS,
		Streamer:      a.StreamerName(),
		AgentProtocol: a.EntitlementStatus().Protocol(),
	}
}

func (a *App) DeviceInfo() api.DeviceInfoResponse {
	a.state.mu.Lock()
	out := make([]api.DeviceInfo, len(a.state.devices))
	copy(out, a.state.devices)
	resp := api.DeviceInfoResponse{
		Devices:         out,
		Count:           len(out),
		MountInProgress: a.state.mountInProgress,
		LastMountError:  a.state.lastMountError,
		AgentOS:         capture.GetOSInfo(),
		AgentDisplay:    capture.GetDisplayServer(),
	}
	a.state.mu.Unlock()
	resp.AgentProtocol = a.EntitlementStatus().Protocol()
	return resp
}

func (a *App) ReplaceDevices(reqs []api.DeviceRequest) error {
	now := time.Now()
	devices := make([]api.DeviceInfo, 0, len(reqs))
	for _, req := range reqs {
		if req.Device == "rndis" {
			continue
		}
		deviceType := normalizeDeviceType(req)
		deviceName := strings.TrimSpace(req.ProductName)
		if deviceName == "" {
			deviceName = strings.TrimSpace(req.Server)
		}
		if deviceName == "" {
			deviceName = req.Device
		}

		devices = append(devices, api.DeviceInfo{
			ID:           len(devices) + 1,
			Device:       req.Device,
			Status:       "connected",
			VendorID:     req.VendorID,
			ProductID:    req.ProductID,
			ProductName:  req.ProductName,
			Manufacturer: req.Manufacturer,
			CreatedAt:    now,
			Server:       req.Server,
			Port:         req.Port,
			Type:         deviceType,
			Name:         deviceName,
		})
	}
	log.Printf("[app] devices active=%d", len(devices))
	a.state.mu.Lock()
	a.state.devices = devices
	a.state.mu.Unlock()
	return nil
}

func normalizeDeviceType(req api.DeviceRequest) string {
	switch req.Device {
	case "keyboard":
		return "keyboard"
	case "mouse":
		if strings.TrimSpace(req.Type) != "" {
			return req.Type
		}
		return "mouse"
	case "mtp":
		return "mtp"
	case "drive":
		if req.Port > 0 {
			return "nbd"
		}
		return "local"
	default:
		if strings.TrimSpace(req.Type) != "" {
			return req.Type
		}
		return req.Device
	}
}

func (a *App) ClearDevices() error {
	a.state.mu.Lock()
	a.state.devices = nil
	a.state.mountInProgress = false
	a.state.lastMountError = ""
	a.state.mu.Unlock()
	return nil
}

func (a *App) Input() interface {
	Key(uint8) error
	Combo(uint8, uint8) error
	Text(string) error
	MouseMove(int8, int8) error
	MouseClick(uint8) error
	MouseScroll(int8) error
	MouseAction(uint8, int8, int8, int8) error
	AbsoluteEvent(uint8, uint16, uint16, int8) error
} {
	return a.input
}

func (a *App) Screen() interface {
	Snapshot() (*api.ScreenSnapshot, error)
} {
	return a.screen
}

// Clipboard returns the agent's clipboard sync manager.
func (a *App) Clipboard() *clipboard.Manager {
	return a.clipboard
}

// ClipboardMaxBytes returns the configured per-transfer size cap for
// clipboard image/file payloads.
func (a *App) ClipboardMaxBytes() int64 {
	return a.cfg.ClipboardMaxBytes
}

// VideoDevices reports real display metadata (native resolution, supported
// FPS modes) — descriptive only, no capture process is spawned here.
// Sunshine does the actual capturing/encoding.
func (a *App) VideoDevices() []api.VideoDeviceInfo {
	return a.screen.Devices()
}

// AudioSinks enumerates real system audio output devices the client can
// choose for Sunshine to capture from.
func (a *App) AudioSinks() ([]api.AudioSink, error) {
	return audio.ListSinks()
}

// CurrentAudioSink returns the sink Sunshine is configured to use, falling
// back to the system default sink if Sunshine has no explicit override.
func (a *App) CurrentAudioSink() (string, error) {
	if runtime.GOOS == "windows" {
		// Every stream host captures the default playback device there (see
		// SetAudioSink), whatever audio_sink says.
		if sink, err := audio.DefaultSink(); err == nil {
			return sink, nil
		}
	}
	if a.stream != nil {
		if sink := a.stream.AudioSink(); sink != "" {
			return sink, nil
		}
	}
	return audio.DefaultSink()
}

// SunshineStreamHost returns the IP Sunshine advertises to Moonlight clients
// (external_ip from sunshine.conf, or Tailscale IP if not explicitly set).
func (a *App) SunshineStreamHost() string {
	if a.stream == nil {
		return ""
	}
	if ip := a.stream.ExternalIP(); ip != "" && ip != "0.0.0.0" {
		return ip
	}
	return ""
}

// SunshineAdminPort returns the Sunshine web admin / NvHTTP base port.
func (a *App) SunshineAdminPort() int {
	if a.cfg.SunshinePort > 0 {
		return a.cfg.SunshinePort
	}
	return 47990
}

// StreamerName returns a short human-readable label for which streaming
// host backend this build is actually running (e.g. "Sunshine (Open
// Source)" or "RustShine (Proprietary)") — display only, see
// streamhost.Identity.
func (a *App) StreamerName() string {
	if a.stream == nil {
		return "unknown"
	}
	return a.stream.DisplayName()
}

// RawHIDSupported reports whether the active streaming backend understands
// LiSendRawHidEvent (a Wacom tablet sent over the stream's control channel
// instead of USB/IP) -- see streamhost.CodecProbe's doc comment. Surfaced to
// the remote client via MasterSyncResponse.RawHIDSupported
// (agent/internal/api/sync.go), which gates DiskWidget.splitRawHID: without
// this check the client would switch a tablet into stream mode against a
// backend that cannot rebuild it, and the tablet would silently stop
// working (no USB/IP fallback once stream mode is chosen).
func (a *App) RawHIDSupported() bool {
	if a.stream == nil {
		return false
	}
	return a.stream.RawHIDSupported()
}

// StreamerRunning reports whether the active streaming host backend's own
// child process is currently alive -- for the GUI's status traffic light
// (see ui/window.go's streamerStatusDot), distinct from StreamerName (which
// backend it is) and entitlement.Status.RustShineStaged (whether it's
// downloaded at all, regardless of whether it's running right now).
func (a *App) StreamerRunning() bool {
	if a.stream == nil {
		return false
	}
	return a.stream.Running()
}

// SessionActive reports whether a Moonlight client is currently mid-stream
// (as opposed to merely paired, or the streamer process merely running
// idle) -- see streamhost.Backend.SessionActive. Used for the tray icon's
// status dot, which should track an actual video stream rather than pairing
// state (see ui.Window.updateTrayStatus).
func (a *App) SessionActive() bool {
	a.streamMu.Lock()
	stream := a.stream
	a.streamMu.Unlock()
	if stream == nil {
		return false
	}
	return stream.SessionActive()
}

// AdminUser returns the streaming host's admin-API username.
func (a *App) AdminUser() string {
	if a.stream == nil {
		return ""
	}
	return a.stream.AdminUser()
}

// AdminPass returns the streaming host's current session admin password.
func (a *App) AdminPass() string {
	if a.stream == nil {
		return ""
	}
	return a.stream.AdminPass()
}

// ListSunshineClients returns Moonlight clients currently paired with the
// bundled Sunshine instance.
func (a *App) ListSunshineClients() ([]streamhost.Client, error) {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil, nil
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	return a.stream.ListClients(port)
}

// CurrentVideoCodec returns the codec negotiated by the most recent stream,
// defaulting to "h264" if unable to determine.
func (a *App) CurrentVideoCodec() string {
	if a.stream != nil {
		return a.stream.CurrentVideoCodec()
	}
	return "h264"
}

// SupportedVideoCodecs returns which of h264/h265/av1 this host's hardware
// encoder can actually produce right now, per Sunshine's own live capability
// probe (its /serverinfo ServerCodecModeSupport field) — not a static list.
func (a *App) SupportedVideoCodecs() []string {
	if a.stream == nil {
		return []string{"h264"}
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	return a.stream.SupportedVideoCodecs(port)
}

// Color444Status reports the RustShine Pro color upgrade's state -- see
// Application interface's doc comment. (false, false) with no active
// backend, matching CurrentVideoCodec's own "nothing to report yet" default.
func (a *App) Color444Status() (active bool, available bool) {
	if a.stream == nil {
		return false, false
	}
	active, available = a.stream.Color444Status()
	// Sunshine has no license gate of its own: 4:4:4 is free, offered
	// whenever the host's encoder can actually produce a 4:4:4 format (the
	// client negotiates it via the codec-support flags, see client
	// moonlightVideoFormat).
	if !available {
		if sb, ok := a.stream.(interface{ Color444Supported(int) bool }); ok && sb.Color444Supported(a.SunshineAdminPort()) {
			available = true
		}
	}
	return active, available
}

// HdrStatus mirrors Color444Status exactly, for the RustShine HDR color
// upgrade -- see Application interface's doc comment.
func (a *App) HdrStatus() (active bool, available bool) {
	if a.stream == nil {
		return false, false
	}
	return a.stream.HdrStatus()
}

// PyroWaveColorStatus: whether 4:4:4 / HDR can be offered with PyroWave
// (streamhost.Backend.PyroWaveColorStatus; the license is already applied
// by rust-shine).
func (a *App) PyroWaveColorStatus() (color444 bool, hdr bool) {
	if a.stream == nil {
		return false, false
	}
	return a.stream.PyroWaveColorStatus()
}

// VirtualDisplayPrimary / SetVirtualDisplayPrimary: see
// streamhost.Backend.SetVirtualDisplayPrimary.
func (a *App) VirtualDisplayPrimary() bool {
	if a.stream == nil {
		return false
	}
	return a.stream.VirtualDisplayPrimary()
}

func (a *App) SetVirtualDisplayPrimary(primary bool) (live bool, err error) {
	if a.stream == nil {
		return false, fmt.Errorf("no stream backend")
	}
	live, err = a.stream.SetVirtualDisplayPrimary(primary)
	if err != nil || runtime.GOOS != "windows" {
		return live, err
	}
	// On Windows the virtual monitor is the agent's, up for a session
	// (virtual_others.go): the choice applies to it right away.
	state := a.virtualSessionStatePath()
	if displaypower.LoadState(state) == nil {
		return false, nil
	}
	res, herr := streamhost.RunDisplayHelper("primary", state, boolArg(primary))
	if herr != nil {
		return true, herr
	}
	if res.Error != "" {
		return true, errors.New(res.Error)
	}
	return true, nil
}

// SetVirtualDisplayExclusive saves "only the virtual monitor" in the agent's config:
// the agent switches the other monitors off for a session itself, for every stream
// host (virtualOthersWatchdog), so the stream host's own switch stays off.
func (a *App) SetVirtualDisplayExclusive(exclusive bool) error {
	a.cfg.VirtualDisplayExclusive = exclusive
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return err
	}
	if a.stream != nil {
		// A stream host that attaches its own virtual monitor (Punktfunk on
		// Windows) applies the choice itself; every other one keeps its own
		// switch off.
		if o, ok := a.stream.(virtualMonitorOwner); ok && o.OwnsVirtualMonitor() {
			return a.stream.SetVirtualDisplayExclusive(exclusive)
		}
		return a.stream.SetVirtualDisplayExclusive(false)
	}
	return nil
}

// VirtualDisplaySupported reports whether the current stream backend
// supports native virtual displays (without external physical monitors).
func (a *App) VirtualDisplaySupported() bool {
	if a.stream == nil {
		return false
	}
	return a.stream.VirtualDisplaySupported()
}

// VideoFec reports the streamer's FEC for the client's FEC slider: Sunshine
// and rust-shine read fec_percentage from their config; rust-shine also
// fec_auto (FEC follows measured loss, see rust-shine's
// pipeline::FEC_AUTO_*). ok is false for a backend with no such setting.
func (a *App) VideoFec() (percent int, auto, autoSupported, ok bool) {
	if a.stream == nil {
		return 0, false, false, false
	}
	kind := streamhost.BackendKind(a.stream)
	if kind != "sunshine" && kind != "rustshine" {
		return 0, false, false, false
	}
	percent = api.DefaultFecPercent
	if v, err := strconv.Atoi(strings.TrimSpace(a.stream.ConfigKey("fec_percentage"))); err == nil && v > 0 {
		percent = v
	}
	autoSupported = kind == "rustshine"
	auto = autoSupported && strings.EqualFold(strings.TrimSpace(a.stream.ConfigKey("fec_auto")), "true")
	return percent, auto, autoSupported, true
}

// SetVideoFec stores the client's FEC choice in the streamer's config and
// restarts the streamer when it changed (both read it only at startup).
func (a *App) SetVideoFec(percent int, auto bool) (changed bool, err error) {
	cur, curAuto, autoSupported, ok := a.VideoFec()
	if !ok {
		return false, fmt.Errorf("this stream backend has no FEC setting")
	}
	if percent < api.MinFecPercent || percent > api.MaxFecPercent {
		return false, fmt.Errorf("percent %d outside %d..%d", percent, api.MinFecPercent, api.MaxFecPercent)
	}
	if auto && !autoSupported {
		return false, fmt.Errorf("this stream backend has no auto FEC")
	}
	if cur == percent && curAuto == auto && a.stream.ConfigKey("fec_percentage") != "" {
		return false, nil
	}
	if err := a.stream.SetConfigKey("fec_percentage", strconv.Itoa(percent)); err != nil {
		return false, err
	}
	if autoSupported {
		if err := a.stream.SetConfigKey("fec_auto", strconv.FormatBool(auto)); err != nil {
			return false, err
		}
	}
	log.Printf("[app] streamer FEC percent=%d auto=%v -- restarting the streamer", percent, auto)
	return true, a.RestartSunshine()
}

// UnpairSunshineClient removes the Moonlight client identified by uniqueID
// (meaning depends on the active backend -- see streamhost's UnpairClient
// doc comments) from every stream-host backend's trust list, not just the
// currently active one -- see streamhost.RemoveTrustedClientEverywhere's
// doc comment for why a shared trust store means a client unpaired here
// must disappear from Sunshine's, rust-shine's, AND punktfunk's own files,
// or switching backends would silently resurrect it.
//
// The live admin-API call against the active backend runs first (so an
// already-running process drops the client immediately, in memory, not
// just on its next restart) but its failure is only logged, never
// returned: RemoveTrustedClientEverywhere's direct file rewrite is the
// authoritative removal and is what actually fixes the originally-reported
// bug (Sunshine's own admin-API unpair silently not persisting) -- a
// caller only needs to know whether the durable removal succeeded.
func (a *App) UnpairSunshineClient(uniqueID string) error {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	if err := a.stream.UnpairClient(port, uniqueID); err != nil {
		log.Printf("[app] live unpair against the active backend failed (will still remove it from the shared trust store): %v", err)
	}
	kind := streamhost.BackendKind(a.stream)
	if kind == "" || a.cfg.StateDir == "" {
		return nil
	}
	return streamhost.RemoveTrustedClientEverywhere(a.cfg.StateDir, kind, uniqueID)
}

// UpdateListenAddr updates the agent's HTTP listen host and port, persists the
// config, and hot-restarts the main HTTP server so the change takes effect immediately.
func (a *App) UpdateListenAddr(host string, port int) (config.Config, error) {
	a.cfg.ListenHost = host
	a.cfg.HTTPPort = port
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return a.cfg, err
	}
	go a.restartMainHTTP()
	return a.cfg, nil
}

// restartMainHTTP shuts down the current main HTTP server and starts a new one
// on the address currently in a.cfg. Used for hot-apply of listen address changes.
func (a *App) restartMainHTTP() {
	old := a.server
	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = old.Shutdown(ctx)
	}
	addr := fmt.Sprintf("%s:%d", a.cfg.EffectiveListenHost(), a.cfg.HTTPPort)
	next := &http.Server{
		Addr:              addr,
		Handler:           a.handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.server = next
	log.Printf("[app] http restarted on %s", addr)
	if err := next.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("[app] http server error: %v", err)
	}
}

// UpdateTLSAddr updates the agent's HTTPS listen port and "Enable HTTPS"
// flag, persists the config, and hot-restarts the TLS server so the change
// takes effect immediately -- mirrors UpdateListenAddr/restartMainHTTP
// exactly, see restartTLS.
func (a *App) UpdateTLSAddr(port int, enabled bool) (config.Config, error) {
	a.cfg.TLSPort = port
	a.cfg.TLSEnabled = &enabled
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return a.cfg, err
	}
	go a.restartTLS()
	return a.cfg, nil
}

// restartTLS shuts down the current TLS server (if one was running) and
// starts a new one on the host/port currently in a.cfg -- unless
// TLSEnabledOK is now false, in which case this only tears the old one
// down (used for both "port changed" and "HTTPS just got turned off").
// Mirrors restartMainHTTP's shape exactly; see that doc comment.
func (a *App) restartTLS() {
	old := a.tlsServer
	if old != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = old.Shutdown(ctx)
	}
	if !a.cfg.TLSEnabledOK() {
		log.Printf("[app] https disabled, not restarting")
		return
	}
	if a.tlsMgr == nil {
		return
	}
	addr := fmt.Sprintf("%s:%d", a.cfg.EffectiveListenHost(), a.cfg.TLSPort)
	next := &http.Server{
		Addr:              addr,
		Handler:           a.handler,
		TLSConfig:         &tls.Config{GetCertificate: a.tlsMgr.GetCertificate, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
	}
	a.tlsServer = next
	log.Printf("[app] https restarted on %s", addr)
	a.startDeviceCertWatchdogOnce()
	if err := next.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
		log.Printf("[app] https server error: %v", err)
	}
}

// startDeviceCertWatchdogOnce starts deviceCertWatchdog the first time
// HTTPS becomes active during this process's lifetime -- see
// tlsWatchdogOnce's doc comment. A no-op (not an error) if a.runCtx isn't
// set yet (restartTLS could theoretically race a UI call in before Run has
// reached the point that sets it; the initial Run() call site is the
// common path and always has it set by the time it calls this).
func (a *App) startDeviceCertWatchdogOnce() {
	if a.runCtx == nil {
		return
	}
	a.tlsWatchdogOnce.Do(func() { go a.deviceCertWatchdog(a.runCtx) })
}

// UpdateSunshinePort updates the Sunshine admin API port in agent config and
// in sunshine.conf, then restarts Sunshine so the change takes effect.
// port is the admin/web port (e.g. 47990); sunshine.conf receives port-1 (NvHTTP base).
func (a *App) UpdateSunshinePort(port int) (config.Config, error) {
	a.cfg.SunshinePort = port
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return a.cfg, err
	}
	// Sunshine's `port` key is the NvHTTP base port; admin is at base+1.
	// SunshinePort is the admin port, so write base = SunshinePort - 1.
	if a.stream != nil {
		_ = a.stream.SetConfigKey("port", strconv.Itoa(port-1))
	}
	_ = a.RestartSunshine()
	return a.cfg, nil
}

// UpdateSunshineStreamAddr sets the IP Sunshine advertises to Moonlight clients
// (external_ip in sunshine.conf) and the streaming port, then restarts Sunshine.
func (a *App) UpdateSunshineStreamAddr(host string, streamPort int) (config.Config, error) {
	webPort := streamPort + 1 // admin port = NvHTTP base + 1
	a.cfg.SunshinePort = webPort
	if err := config.Save(a.cfgPath, a.cfg); err != nil {
		return a.cfg, err
	}
	if a.stream != nil {
		_ = a.stream.SetExternalIP(host)
		// Only restrict Sunshine's kernel bind to this IP for real (LAN) hosts —
		// never for the agent's own tsnet IP, which has no kernel interface to
		// bind to. tsnet reachability is handled by restartStreamProxy instead.
		if !a.isTsnetSelfIP(host) {
			_ = a.stream.SetBindAddress(host)
		} else {
			_ = a.stream.SetBindAddress("")
		}
		// Write streamPort (NvHTTP base) to sunshine.conf, not webPort (admin port).
		_ = a.stream.SetConfigKey("port", strconv.Itoa(streamPort))
	}
	_ = a.RestartSunshine()
	return a.cfg, nil
}

// RelinquishEngine steps this process down from owning the engine so
// another process -- normally a --headless autostart launch that just
// started via launchctl/systemd/the Windows service right after the user
// checked "launch at login" -- can take over without needing a hard kill.
// Called over the admin socket by evictEngineLockHolder (see enginelock.go)
// as its first, cooperative attempt before ever falling back to killing the
// holder's PID outright.
//
// Stops the streamhost backend (killing whatever streamer is currently
// running -- the incoming owner starts its own fresh one right after
// acquiring the lock, so there is no point leaving this one up) and
// releases the engine lock so the waiting process's acquireEngineLock can
// succeed. If this process has a GUI window (it isn't headless), it also
// relaunches itself as a plain GUI process before exiting: that relaunch
// runs through Start()'s normal discovery, finds the new owner's admin
// socket already up, and attaches as a thin client -- so the user ends up
// looking at the same window in the same place, just backed by the new
// headless engine instead of running its own, rather than it simply
// vanishing.
//
// The actual process exit is deferred a moment (see the goroutine below) so
// the RPC caller gets a clean response before this process's admin socket
// (which is what's still carrying that very response) goes away.
func (a *App) RelinquishEngine() error {
	log.Printf("[app] relinquishing engine ownership (pid=%d) -- another process is taking over", os.Getpid())

	a.streamMu.Lock()
	if a.stream != nil {
		_ = a.stream.Stop()
	}
	a.streamMu.Unlock()

	if a.engineLock != nil {
		_ = a.engineLock.Close()
		a.engineLock = nil
	}

	if !a.headless {
		if exe, err := os.Executable(); err != nil {
			log.Printf("[app] warning: could not resolve own executable to relaunch as a thin client: %v", err)
		} else {
			cmd := exec.Command(exe)
			if err := cmd.Start(); err != nil {
				log.Printf("[app] warning: could not relaunch as a thin client: %v", err)
			} else {
				log.Printf("[app] relaunched pid=%d as a thin-client GUI against the incoming engine owner", cmd.Process.Pid)
			}
		}
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		os.Exit(0)
	}()
	return nil
}

// SubmitMoonlightPIN sends the PIN shown by a Moonlight client to Sunshine
// to complete the pairing handshake.
//
// Takes streamMu (matching SetStreamBackend, which holds it for its entire
// Stop-old/Start-new/WaitReady sequence) rather than reading a.stream
// unguarded -- a real race, not theoretical: a client's PIN submission
// landing in the middle of a backend switch used to read a.stream (or a
// stream host it points at) mid-teardown, sent against the old process
// after it had already been killed, and got rejected outright. The
// Moonlight client on the other end only tries once and falls back to
// manual PIN entry on any failure, so this used to surface as "switching
// streamers broke auto-pairing" -- confirmed live: session was active,
// switch was triggered, and the very next auto-pair attempt 401'd while
// the switch was still settling, a moment before it started working again
// on its own. Blocking here for the (bounded, ~5s worst case -- see
// SetStreamBackend's own WaitReady) duration of an in-flight switch is
// well inside the client's own 10s HTTP timeout for this call
// (submitPinToService in the client's moonlight_service.go), so this PIN
// now simply waits for the switch to finish and lands on the fresh,
// correctly-provisioned backend instead of racing it.
func (a *App) SubmitMoonlightPIN(pin string) error {
	a.streamMu.Lock()
	defer a.streamMu.Unlock()
	if a.stream == nil {
		return nil
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	kind := streamhost.BackendKind(a.stream)
	var before map[string]bool
	if kind != "" && a.cfg.StateDir != "" {
		before = streamhost.TrustedFingerprints(a.cfg.StateDir, kind)
	}
	if err := a.stream.SubmitPIN(port, pin); err != nil {
		return err
	}
	// Propagates the client that just paired to the other two backends'
	// trust lists, instead of waiting for either of them to next Start() --
	// see streamhost.AwaitPairingAndSync's doc comment for why this has to
	// wait for the client to finish the handshake first. Also the only
	// thing allowed to lift a prior tombstone (RemoveTrustedClientEverywhere),
	// since it runs in direct response to a real pairing ceremony, not a
	// passive file scan. In the background: the client is still mid-pairing
	// and must get this PIN response now.
	if before != nil {
		stateDir := a.cfg.StateDir
		go streamhost.AwaitPairingAndSync(stateDir, kind, before, 2*time.Minute)
	}
	return nil
}

// SetAudioSink points Sunshine at the given audio device (sunshine.conf's
// audio_sink) and restarts it so the change takes effect. If Sunshine is
// already running with this exact sink, it's left alone — every client
// session start used to unconditionally kill and relaunch Sunshine even
// when nothing changed, causing a needless restart (and brief capture
// interruption) on every connect.
func (a *App) SetAudioSink(sink string) error {
	if runtime.GOOS == "windows" && sink != "" {
		// RustShine's WASAPI loopback (and a stream host given no audio_sink)
		// captures the default playback device: picking one makes it the
		// default. audio_sink is still written for Sunshine/Vibepollo below.
		// "" (stop) leaves the default as it is.
		if err := audio.SetDefaultSink(sink); err != nil {
			return fmt.Errorf("make %s the default playback device: %w", sink, err)
		}
		log.Printf("[app] default playback device is now %s", sink)
	}
	if a.stream == nil {
		return nil
	}
	unchanged := a.stream.AudioSink() == sink && a.stream.Running()
	if err := a.stream.SetAudioSink(sink); err != nil {
		return fmt.Errorf("write sunshine.conf: %w", err)
	}
	if unchanged {
		return nil
	}
	return a.RestartSunshine()
}

// SunshineOutputName returns the monitor Sunshine is pinned to capture
// (Sunshine's own connected-output index, stringified), read from
// sunshine.conf if present, falling back to the persisted agent config.
func (a *App) SunshineOutputName() string {
	if a.stream != nil {
		if name := a.stream.OutputName(); name != "" {
			return name
		}
	}
	return a.cfg.SunshineOutputName
}

// reconcileOutputName snaps a persisted OutputName back onto a currently
// valid capture device when it no longer matches any device the backend
// currently reports. This is the fix for a real deadlock: a monitor
// (physical or virtual) that was selected and then torn down or
// disconnected leaves its old connector index behind in sunshine.conf, but
// Sunshine/RustShine's own connector indices aren't stable identifiers --
// see streamhost.CaptureDevice's own doc comment -- so removing e.g. a
// second, virtual monitor renumbers the remaining real one out from under
// the stale saved index (observed live: "Monitor 1 is DP-2" + "Monitor 0 is
// Virtual-1" before, "Monitor 0 is DP-2" after, with output_name still
// pinned to "1"). Sunshine then fails every encoder against a monitor index
// that no longer exists ("Couldn't find monitor [1]") and loops forever
// inside its own process without ever exiting, so nothing else in the
// agent notices or recovers automatically -- this call, made on every
// startSunshineNow (i.e. every sunshineWatchdog tick), is what breaks that
// loop.
//
// Deliberately left alone whenever more than one valid device remains:
// with two or more genuinely still-connected choices (which may well
// include a deliberately-configured virtual display), the stale value's
// intended target is ambiguous, and guessing wrong would silently override
// a manual pick instead of fixing one that's actually broken.
func (a *App) reconcileOutputName() {
	if a.stream == nil {
		return
	}
	current := a.stream.OutputName()
	if current == "" {
		return
	}
	if strings.HasPrefix(current, "virtual:") {
		// RustShine's virtual-display pick (see rustshineBackend.SetOutputName)
		// deliberately isn't a real KMS connector, so it never shows up in
		// ListCaptureDevices' real-monitor enumeration -- that's expected,
		// not staleness, and must never be "corrected" away from here.
		return
	}
	if fb, ok := disconnectedPinFallback(current); ok {
		log.Printf("[app] output_name %q points at a connector that is not connected; falling back to %q", current, fb)
		if err := a.SetSunshineOutputName(fb); err != nil {
			log.Printf("[app] failed to fall back to a connected output: %v", err)
		}
		return
	}
	devices := a.stream.ListCaptureDevices()
	if len(devices) == 0 {
		// No correlation data yet (fresh boot, or the backend hasn't
		// reported anything this session) -- nothing to validate against.
		return
	}
	for _, d := range devices {
		if d.OutputName == current {
			return
		}
	}
	if len(devices) != 1 {
		log.Printf("[app] output_name %q matches none of %d current capture devices; leaving it as-is (ambiguous -- needs a manual pick)", current, len(devices))
		return
	}
	only := devices[0]
	log.Printf("[app] output_name %q no longer matches any current capture device (stale/disconnected monitor); snapping to the only one currently reported: %q (%q)", current, only.Key, only.OutputName)
	if err := a.SetSunshineOutputName(only.OutputName); err != nil {
		log.Printf("[app] failed to auto-correct output_name: %v", err)
	}
}

// reconcileAudioSink clears a persisted audio_sink that no longer names any
// sink on this host (e.g. an Intel HDMI output left over from a hardware
// change: "alsa_output.pci-0000_00_1f.3.hdmi-stereo" on a box that now only
// has USB + NVIDIA HDMI). Sunshine doesn't fall back on its own: it fails
// "Couldn't set default-sink [...]: No such entity" and streams with no audio
// at all, while RustShine silently uses the default sink -- which is why the
// same stale value only ever broke audio under Sunshine. An empty audio_sink
// makes both backends capture the system default sink.
func (a *App) reconcileAudioSink() {
	if a.stream == nil {
		return
	}
	current := a.stream.AudioSink()
	if current == "" || strings.HasPrefix(current, "sink-sunshine-") {
		// Sunshine's own virtual sinks only exist while it runs, so their
		// absence right now isn't staleness.
		return
	}
	sinks, err := audio.ListSinks()
	if err != nil || len(sinks) == 0 {
		// PipeWire/PulseAudio not up yet (headless boot) -- nothing to
		// validate against.
		return
	}
	for _, s := range sinks {
		if s.Name == current {
			return
		}
	}
	log.Printf("[app] audio_sink %q matches none of %d current sinks; resetting to the system default sink", current, len(sinks))
	if err := a.SetAudioSink(""); err != nil {
		log.Printf("[app] failed to reset stale audio_sink: %v", err)
	}
}

// SetSunshineOutputName pins Sunshine's capture to the given monitor
// (Sunshine's connected-output index, stringified, or "" to auto-pick),
// persists it into both sunshine.conf and the agent config, and restarts
// Sunshine so the change takes effect.
func (a *App) SetSunshineOutputName(name string) error {
	if a.stream == nil {
		return nil
	}
	// Compares against OutputName() *after* SetOutputName runs, not the raw
	// `name` argument against the pre-write value -- RustShine's own
	// SetOutputName resolves a bare numeric index (what videoSetDevice
	// sends for a "drm:N" device path) into its real stored
	// "cardPath|connector" form internally (see rustshineBackend's own
	// doc comment), so a before/raw-`name` comparison could never match
	// that already-canonical stored string, even when the client is just
	// re-confirming the exact same monitor it already has selected.
	// Confirmed live: this made every /api/video/set_device call for an
	// unchanged device force a full Sunshine/RustShine restart -- and the
	// client calls it on every reconnect, so every reconnect was hard-
	// killing and relaunching the stream host from scratch, tearing down
	// whatever encode/packetize work (including a multi-threaded FEC
	// block build) happened to be mid-flight at that exact moment.
	before := a.stream.OutputName()
	if err := a.stream.SetOutputName(name); err != nil {
		return fmt.Errorf("write sunshine.conf: %w", err)
	}
	after := a.stream.OutputName()
	unchanged := before == after && a.stream.Running()
	next := a.cfg
	next.SunshineOutputName = after
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if unchanged {
		return nil
	}
	return a.RestartSunshine()
}

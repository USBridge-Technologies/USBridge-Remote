package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	"usbridge_agent/assets"
	"usbridge_agent/internal/adminapi"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/audio"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/clipboard"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/input"
	"usbridge_agent/internal/netutil"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/streamhost"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui"
	"usbridge_agent/internal/ui/design"
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

	state     *deviceState
	input     *input.Controller
	screen    *capture.Service
	perms     *permissions.Service
	ts        *tailscale.Service
	stream    streamhost.Backend
	tsProxy   *tailscale.StreamProxy
	server    *http.Server
	tsHTTP    *http.Server
	handler   http.Handler
	fyneApp   fyne.App
	clipboard *clipboard.Manager
	adminSrv  *adminapi.Server
}

// Start is the sole entry point from main(). It decides, based on mode and
// whether another instance's admin socket is already reachable, whether
// this process owns the engine (HTTP server, Sunshine, tsnet) or just
// attaches a GUI to one that's already running headless — see
// runThinClientGUI. This is what lets the same binary/AppImage work both as
// a `--headless` systemd/launchd/autostart service and as the normal GUI
// app without ever running two engines (and two Sunshine/tsnet instances)
// at once on the same machine.
func Start(headless bool) error {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}
	// EnsureState only creates cfg.StateDir if missing — needed up front so
	// the admin socket path is known, but otherwise side-effect-free (no
	// goroutines, no network binds), so probing before committing to owning
	// the engine is safe.
	if err := cfg.EnsureState(); err != nil {
		return err
	}

	socketPath := adminapi.SocketPath(cfg.StateDir)
	if client, dialErr := adminapi.Dial(socketPath); dialErr == nil {
		if headless {
			client.Close()
			return fmt.Errorf("usbridge-agent is already running (admin socket %s)", socketPath)
		}
		return runThinClientGUI(client)
	}

	instance, err := New()
	if err != nil {
		return err
	}
	return instance.Run(headless)
}

// runThinClientGUI shows the GUI backed by an already-running headless
// instance's admin socket instead of starting a second engine. Closing the
// window here does NOT stop the headless instance — only a process actually
// owning the engine (see App.Run/shutdownEngine) does that.
func runThinClientGUI(client *adminapi.Client) error {
	cfg, err := client.CurrentConfig()
	if err != nil {
		client.Close()
		return fmt.Errorf("fetch config from running instance: %w", err)
	}

	fyneApp := fyneapp.NewWithID("io.usbridge.agent")
	fyneApp.Settings().SetTheme(design.NewBrandTheme())
	fyneApp.SetIcon(assets.AppIcon)

	ui.NewWindow(fyneApp, cfg, client, client, client).ShowAndRun(func() {
		client.Close()
	})
	return nil
}

func New() (*App, error) {
	cfgPath := resolveConfigPath()
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, err
	}
	if err := cfg.EnsureState(); err != nil {
		return nil, err
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
	instance.stream = streamhost.NewDefault(resolveExeDir(), cfg.StateDir, filepath.Join(cfg.StateDir, "logs", "sunshine-stdout.log"))
	instance.screen = capture.New(instance.stream)
	instance.syncSunshineCapExec()
	handler := api.NewServerWithAuth(instance, masterKeyBytes, cfg.SunshinePort).Routes()
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
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if skipExeDir && homeCandidate != "" {
		return homeCandidate
	}
	return candidates[0]
}

// Run starts the engine (HTTP server, Sunshine, tsnet, admin socket) and
// then either blocks headlessly on ctx.Done() (headless==true — no Fyne
// driver ever touched, so no display connection is required) or shows the
// GUI window backed directly by this same in-process engine (headless==false).
func (a *App) Run(headless bool) error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.Printf("[app] starting http=%s:%d headless=%v", a.cfg.EffectiveListenHost(), a.cfg.HTTPPort, headless)

	a.startSunshine()
	go a.sunshineWatchdog(ctx)
	go func() { _ = a.server.ListenAndServe() }()
	if a.clipboard != nil {
		go a.clipboard.Run(ctx)
	}

	a.initTailscale(ctx)

	if srv, err := adminapi.NewServer(adminapi.SocketPath(a.cfg.StateDir), a, a.perms, a.ts, func() config.Config { return a.cfg }); err != nil {
		// Non-fatal: the engine itself works fine without it, it just means
		// no separate GUI process can attach to this instance later.
		log.Printf("[app] warning: admin socket unavailable: %v", err)
	} else {
		a.adminSrv = srv
		go func() {
			if err := srv.Serve(); err != nil {
				log.Printf("[app] admin socket server error: %v", err)
			}
		}()
	}

	if headless {
		<-ctx.Done()
		a.shutdownEngine()
		return nil
	}

	a.fyneApp = fyneapp.NewWithID("io.usbridge.agent")
	a.fyneApp.Settings().SetTheme(design.NewBrandTheme())
	a.fyneApp.SetIcon(assets.AppIcon)

	go a.handleShutdown(ctx, cancel)
	ui.NewWindow(a.fyneApp, a.cfg, a.perms, a.ts, a).ShowAndRun(cancel)
	return nil
}

func (a *App) startSunshine() {
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
	if err := a.stream.Start(a.cfg.SunshinePort); err != nil {
		log.Printf("[app] failed to start Sunshine: %v", err)
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
	a.tsProxy = a.ts.StartStreamProxy(basePort)
}

func (a *App) initTailscale(ctx context.Context) {
	if a.ts == nil {
		return
	}
	if a.cfg.TailscaleEnabled {
		go a.startTailscaleHTTP(ctx)
		go a.startSunshineTSNetForwarding()
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

// SunshineCapExecPath returns the path to the bundled sunshine_capexec
// launcher (Linux KMS capture only), or "" if not present.
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

// syncSunshineCapExec sets or clears the backend's sunshine_capexec launcher
// so Start launches Sunshine with CAP_SYS_ADMIN exactly when the capture
// mode is "kms" AND the capability is actually granted on that launcher —
// never based on mode alone, since sunshine_capexec exits with an error if
// asked to raise a capability it doesn't have, which would stop Sunshine
// from starting at all instead of gracefully running without KMS.
func (a *App) syncSunshineCapExec() {
	if a.stream == nil {
		return
	}
	capexecPath := a.SunshineCapExecPath()
	if a.SunshineCaptureMode() == "kms" && a.perms != nil && a.perms.KMSCaptureGranted(capexecPath) {
		a.stream.SetCapExecPath(capexecPath)
	} else {
		a.stream.SetCapExecPath("")
	}
}

// SunshineCaptureMode returns the configured Linux capture backend ("",
// "portal", or "kms"), read from sunshine.conf if present, falling back to
// the persisted agent config.
func (a *App) SunshineCaptureMode() string {
	if a.stream != nil {
		if mode := a.stream.CaptureMode(); mode != "" {
			return mode
		}
	}
	return a.cfg.SunshineCaptureMode
}

// SetSunshineCaptureMode persists the capture backend choice into both the
// agent config and Sunshine's own sunshine.conf, then restarts the bundled
// Sunshine instance so the change actually takes effect — a config edit
// alone is silently ignored by an already-running Sunshine process.
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

// RestartSunshine stops and relaunches the bundled Sunshine instance (if the
// agent owns its lifecycle) so a config or capability change takes effect.
func (a *App) RestartSunshine() error {
	if a.stream == nil {
		return nil
	}
	_ = a.stream.Stop()
	time.Sleep(time.Second)
	err := a.stream.Start(a.cfg.SunshinePort)
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
		a.stream.WaitReady(a.cfg.SunshinePort, 5*time.Second)
		a.waitForMonitorCorrelation()
	}
	a.restartStreamProxy()
	return err
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
// making the restart itself wait for correlation instead. Only meaningful
// for the Linux KMS capture backend; a bounded poll elsewhere just times out
// as a no-op.
func (a *App) waitForMonitorCorrelation() {
	if a.stream == nil || runtime.GOOS != "linux" || a.stream.CaptureMode() != "kms" {
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

// KMSCaptureGranted reports whether the bundled sunshine_capexec launcher
// has the CAP_SYS_ADMIN capability needed for KMS capture.
func (a *App) KMSCaptureGranted() bool {
	if a.perms == nil {
		return false
	}
	return a.perms.KMSCaptureGranted(a.SunshineCapExecPath())
}

// RequestKMSCapture grants CAP_SYS_ADMIN to the bundled sunshine_capexec
// launcher (prompts for elevation via pkexec) — never to Sunshine itself,
// which would break its bundled-library resolution, see
// internal/permissions.RequestKMSCapture — then restarts Sunshine so the
// newly-granted capability is actually picked up.
func (a *App) RequestKMSCapture() bool {
	if a.perms == nil {
		return false
	}
	granted := a.perms.RequestKMSCapture(a.SunshineCapExecPath())
	if granted {
		a.syncSunshineCapExec()
		if err := a.RestartSunshine(); err != nil {
			log.Printf("[app] failed to restart Sunshine after granting KMS capability: %v", err)
		}
	}
	return granted
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
	link := buildQRLink(internalHost, tailscaleHost, masterKey)
	return link, masterKey
}

func buildQRLink(internalHost, tailscaleHost, masterKey string) string {
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
		Timestamp: time.Now(),
		OS:        runtime.GOOS,
		Streamer:  a.StreamerName(),
	}
}

func (a *App) DeviceInfo() api.DeviceInfoResponse {
	a.state.mu.Lock()
	defer a.state.mu.Unlock()
	out := make([]api.DeviceInfo, len(a.state.devices))
	copy(out, a.state.devices)
	return api.DeviceInfoResponse{
		Devices:         out,
		Count:           len(out),
		MountInProgress: a.state.mountInProgress,
		LastMountError:  a.state.lastMountError,
		AgentOS:         capture.GetOSInfo(),
		AgentDisplay:    capture.GetDisplayServer(),
	}
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

// UnpairSunshineClient removes the Moonlight client with the given UUID from
// Sunshine's authorized client list.
func (a *App) UnpairSunshineClient(uniqueID string) error {
	if a.stream == nil {
		return nil
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	return a.stream.UnpairClient(port, uniqueID)
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

// SubmitMoonlightPIN sends the PIN shown by a Moonlight client to Sunshine
// to complete the pairing handshake.
func (a *App) SubmitMoonlightPIN(pin string) error {
	if a.stream == nil {
		return nil
	}
	port := a.cfg.SunshinePort
	if port == 0 {
		port = 47990
	}
	return a.stream.SubmitPIN(port, pin)
}

// SetAudioSink points Sunshine at the given audio device (sunshine.conf's
// audio_sink) and restarts it so the change takes effect. If Sunshine is
// already running with this exact sink, it's left alone — every client
// session start used to unconditionally kill and relaunch Sunshine even
// when nothing changed, causing a needless restart (and brief capture
// interruption) on every connect.
func (a *App) SetAudioSink(sink string) error {
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

// SetSunshineOutputName pins Sunshine's capture to the given monitor
// (Sunshine's connected-output index, stringified, or "" to auto-pick),
// persists it into both sunshine.conf and the agent config, and restarts
// Sunshine so the change takes effect.
func (a *App) SetSunshineOutputName(name string) error {
	if a.stream == nil {
		return nil
	}
	unchanged := a.stream.OutputName() == name && a.stream.Running()
	if err := a.stream.SetOutputName(name); err != nil {
		return fmt.Errorf("write sunshine.conf: %w", err)
	}
	next := a.cfg
	next.SunshineOutputName = name
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if unchanged {
		return nil
	}
	return a.RestartSunshine()
}

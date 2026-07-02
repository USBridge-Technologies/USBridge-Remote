package app

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	"usbridge_agent/assets"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/input"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/sunshine"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui"
	"usbridge_agent/internal/ui/design"
	"usbridge_agent/internal/video"
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

	state    *deviceState
	input    *input.Controller
	screen   *capture.Service
	video    *video.Manager
	perms    *permissions.Service
	ts       *tailscale.Service
	sunshine *sunshine.Process
	server   *http.Server
	tsHTTP   *http.Server
	fyneApp  fyne.App
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

	instance := &App{
		cfgPath: cfgPath,
		cfg:     cfg,
		state:   &deviceState{startedAt: time.Now()},
		input:   input.New(),
		screen:  capture.New(),
		perms:   permissions.New(),
		ts:      tailscale.New(cfg.TailscaleMode, cfg.StateDir),
		fyneApp: fyneapp.NewWithID("io.usbridge.agent"),
	}
	instance.fyneApp.Settings().SetTheme(design.NewBrandTheme())
	instance.fyneApp.SetIcon(assets.AppIcon)
	instance.video = video.New(cfg, instance.ts)
	instance.sunshine = sunshine.NewProcess(sunshine.LaunchPath(resolveExeDir()), filepath.Join(cfg.StateDir, "logs", "sunshine-stdout.log"))
	handler := api.NewServerWithAuth(instance, masterKeyBytes, cfg.SunshinePort).Routes()
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
	candidates := make([]string, 0, 6)
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates,
			filepath.Join(exeDir, "config.yaml"),
			filepath.Clean(filepath.Join(exeDir, "..", "..", "..", "config.yaml")),
		)
	}
	candidates = append(candidates, filepath.Join(".", "config.yaml"))
	if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
		candidates = append(candidates, filepath.Join(homeDir, ".config", "usbridge-agent", "config.yaml"))
	}

	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

func (a *App) Run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	captureMode := a.cfg.VideoCapture
	if runtime.GOOS == "linux" {
		mode := strings.ToLower(strings.TrimSpace(captureMode))
		if mode == "" || mode == "auto" || mode == "dxgi" || (mode == "x11grab" && capture.GetLinuxEnv() == "Wayland") {
			if capture.GetLinuxEnv() == "Wayland" {
				captureMode = "pipewire (auto)"
			} else {
				captureMode = "x11grab (auto)"
			}
		}
	}

	log.Printf("[app] starting http=%s:%d video_udp=%d capture=%s", a.cfg.EffectiveListenHost(), a.cfg.HTTPPort, a.cfg.VideoUDPPort, captureMode)
	log.Printf("[app] ffmpeg path=%s", a.cfg.FFmpegPath)
	if a.sunshine != nil {
		if err := a.sunshine.Start(a.cfg.SunshinePort); err != nil {
			log.Printf("[app] failed to start Sunshine: %v", err)
		}
	}
	go func() { _ = a.server.ListenAndServe() }()
	// Initialize Tailscale
	if a.ts != nil {
		err := a.ts.ApplyConfig(a.cfg.TailscaleMode == config.TailscaleModeUserspace, a.cfg.StateDir)
		if err != nil {
			log.Printf("[app] failed to apply tailscale config: %v", err)
		}
	}

	if a.cfg.TailscaleEnabled && a.ts != nil {
		go func() {
			if a.cfg.TailscaleMode == config.TailscaleModeUserspace {
				tsSrv, _ := a.ts.Server()
				if tsSrv != nil {
					ln, err := tsSrv.Listen("tcp", fmt.Sprintf(":%d", a.cfg.HTTPPort))
					if err != nil {
						log.Printf("[app] tsnet listen error: %v", err)
						return
					}
					log.Printf("[app] tsnet http listening on :%d", a.cfg.HTTPPort)
					if err := a.tsHTTP.Serve(ln); err != nil && err != http.ErrServerClosed {
						log.Printf("[app] tsnet http server error: %v", err)
					}
					return
				}
			}

			// Poll for tailscale IP more aggressively at start
			for i := 0; i < 30; i++ {
				if ctx.Err() != nil {
					return
				}
				if ip, err := a.ts.TailnetIPv4(ctx); err == nil && ip != "" {
					tsAddr := fmt.Sprintf("%s:%d", ip, a.cfg.HTTPPort)
					a.tsHTTP.Addr = tsAddr
					log.Printf("[app] tailscale http listening on %s", tsAddr)
					if err := a.tsHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
						log.Printf("[app] tailscale http server error: %v", err)
					}
					return
				}
				time.Sleep(1 * time.Second)
			}
			log.Printf("[app] tailscale enabled but tailnet IP could not be found after 30s")
		}()
	}
	go func() {
		<-ctx.Done()
		_ = a.server.Shutdown(context.Background())
		if a.tsHTTP != nil && a.tsHTTP.Addr != "" {
			_ = a.tsHTTP.Shutdown(context.Background())
		}
		_ = a.ts.Close()
		_ = a.video.Close()
		if a.sunshine != nil {
			_ = a.sunshine.Stop()
		}
		fyne.Do(func() {
			a.fyneApp.Quit()
		})
	}()
	ui.NewWindow(a.fyneApp, a.cfg, a.perms, a.ts, a).ShowAndRun(cancel)
	return nil
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
	path := sunshine.BinaryPath(resolveExeDir())
	if path == "" {
		return ""
	}
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// SunshineCaptureMode returns the configured Linux capture backend ("",
// "portal", or "kms"), read from sunshine.conf if present, falling back to
// the persisted agent config.
func (a *App) SunshineCaptureMode() string {
	if mode := sunshine.CaptureMode(); mode != "" {
		return mode
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
	if err := sunshine.SetCaptureMode(mode); err != nil {
		return fmt.Errorf("write sunshine.conf: %w", err)
	}
	next := a.cfg
	next.SunshineCaptureMode = mode
	if err := a.SaveConfig(next); err != nil {
		return err
	}
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
	if a.sunshine == nil {
		return nil
	}
	_ = a.sunshine.Stop()
	time.Sleep(time.Second)
	return a.sunshine.Start(a.cfg.SunshinePort)
}

// KMSCaptureGranted reports whether the bundled Sunshine binary has the
// CAP_SYS_ADMIN capability needed for KMS capture.
func (a *App) KMSCaptureGranted() bool {
	if a.perms == nil {
		return false
	}
	return a.perms.KMSCaptureGranted(a.SunshineBinaryPath())
}

// RequestKMSCapture grants CAP_SYS_ADMIN to the bundled Sunshine binary
// (prompts for elevation via pkexec), then restarts Sunshine so the
// newly-granted capability is actually picked up — an already-running
// process doesn't gain capabilities retroactively.
func (a *App) RequestKMSCapture() bool {
	if a.perms == nil {
		return false
	}
	granted := a.perms.RequestKMSCapture(a.SunshineBinaryPath())
	if granted {
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
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	type candidate struct{ name, ip string }
	var candidates []candidate
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip == nil {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() || (ip4[0] == 169 && ip4[1] == 254) {
				continue
			}
			candidates = append(candidates, candidate{iface.Name, ip4.String()})
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].name != candidates[j].name {
			return candidates[i].name < candidates[j].name
		}
		return candidates[i].ip < candidates[j].ip
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].ip
}

func (a *App) SaveConfig(cfg config.Config) error {
	if err := config.Save(a.cfgPath, cfg); err != nil {
		return err
	}
	a.cfg = cfg
	return nil
}

func (a *App) PortalPipeWire() (uint32, int) {
	return capture.GetPortalPipeWireNodeID(), capture.GetPortalPipeWireFD()
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

func (a *App) Video() interface {
	Start(api.VideoStartRequest) error
	Stop() error
	Info() map[string]interface{}
} {
	return a.video
}

func (a *App) VideoDevices() []api.VideoDeviceInfo {
	return a.screen.Devices()
}

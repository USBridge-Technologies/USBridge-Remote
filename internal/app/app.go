package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/frp"
	"usbridge_agent/internal/input"
	"usbridge_agent/internal/permissions"
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

	state   *deviceState
	input   *input.Controller
	screen  *capture.Service
	video   *video.Manager
	perms   *permissions.Service
	ts      *tailscale.Service
	frp     *frp.Manager
	server  *http.Server
	tsHTTP  *http.Server
	fyneApp fyne.App
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
	instance := &App{
		cfgPath: cfgPath,
		cfg:     cfg,
		state:   &deviceState{startedAt: time.Now()},
		input:   input.New(),
		screen:  capture.New(),
		perms:   permissions.New(),
		ts:      tailscale.New(),
		fyneApp: fyneapp.NewWithID("io.usbridge.agent"),
	}
	instance.fyneApp.Settings().SetTheme(design.NewBrandTheme())
	instance.frp = frp.New(cfg, cfg.HTTPPort, cfg.VideoUDPPort)
	instance.video = video.New(cfg, instance.frp, instance.ts)
	instance.server = &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.EffectiveListenHost(), cfg.HTTPPort),
		Handler:           api.NewServer(instance).Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	instance.tsHTTP = &http.Server{
		Handler:           instance.server.Handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return instance, nil
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

	log.Printf("[app] starting http=%s:%d frp_bind=%d video_udp=%d capture=%s", a.cfg.EffectiveListenHost(), a.cfg.HTTPPort, a.cfg.FRPBindPort, a.cfg.VideoUDPPort, a.cfg.VideoCapture)
	log.Printf("[app] ffmpeg path=%s", a.cfg.FFmpegPath)
	if err := a.frp.Start(ctx); err != nil {
		return err
	}
	go func() { _ = a.server.ListenAndServe() }()
	if a.cfg.TailscaleEnabled && a.ts != nil {
		if tsServer, err := a.ts.Server(); err != nil {
			log.Printf("[app] tailscale start failed: %v", err)
		} else if ln, err := tsServer.Listen("tcp", fmt.Sprintf(":%d", a.cfg.HTTPPort)); err != nil {
			log.Printf("[app] tailscale listen failed: %v", err)
		} else {
			log.Printf("[app] tailscale http listening on tailnet :%d", a.cfg.HTTPPort)
			go func() { _ = a.tsHTTP.Serve(ln) }()
		}
	}
	go func() {
		<-ctx.Done()
		_ = a.server.Shutdown(context.Background())
		_ = a.tsHTTP.Shutdown(context.Background())
		_ = a.ts.Close()
		_ = a.frp.Stop()
		_ = a.video.Stop()
		fyne.Do(func() {
			a.fyneApp.Quit()
		})
	}()
	ui.NewWindow(a.fyneApp, a.cfg, a.perms, a.ts, a).ShowAndRun(cancel)
	return nil
}

func (a *App) RegenerateFRPToken() (config.Config, error) {
	token, err := config.GenerateSecureToken()
	if err != nil {
		return a.cfg, fmt.Errorf("generate secure token: %w", err)
	}

	next := a.cfg
	next.FRPToken = token
	if err := config.Save(a.cfgPath, next); err != nil {
		return a.cfg, fmt.Errorf("save config: %w", err)
	}
	if a.frp != nil {
		if err := a.frp.UpdateToken(token); err != nil {
			return a.cfg, fmt.Errorf("reload frp token: %w", err)
		}
	}
	a.cfg = next
	return a.cfg, nil
}

func (a *App) Status() api.SystemStatus {
	return api.SystemStatus{
		Service: api.ServiceStatus{
			Status:    "running",
			Timestamp: time.Now(),
			Uptime:    time.Since(a.state.startedAt).String(),
		},
		Timestamp: time.Now(),
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
	}
}

func (a *App) ReplaceDevices(reqs []api.DeviceRequest) error {
	now := time.Now()
	devices := make([]api.DeviceInfo, 0, len(reqs))
	nbdPorts := make([]int, 0, len(reqs))
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
		if req.Device == "drive" && req.Port > 0 {
			nbdPorts = append(nbdPorts, req.Port)
		}
	}
	log.Printf("[app] devices active=%d nbd_visitors=%d", len(devices), len(nbdPorts))
	if err := a.frp.UpdateNBDVisitors(nbdPorts); err != nil {
		return err
	}
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
	if err := a.frp.UpdateNBDVisitors(nil); err != nil {
		return err
	}
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

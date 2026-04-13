package tailscale

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"

	"github.com/sirupsen/logrus"
)

type Status struct {
	Running   bool
	LoggedIn  bool
	Backend   string
	Userspace bool
	Self      Peer
}

type Peer struct {
	HostName  string
	DNSName   string
	IP4       string
	UserLogin string
	Online    bool
}

type Service struct {
	mu            sync.Mutex
	server        *tsnet.Server
	latestAuthURL string
}

func New() *Service {
	return &Service{}
}

func getTailscaleBinaryPath() string {
	name := "tailscale"
	if runtime.GOOS == "windows" {
		name = "tailscale.exe"
	}

	// 1. Check near the executable
	if exePath, err := os.Executable(); err == nil {
		localPath := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(localPath); err == nil {
			return localPath
		}
	}

	// 2. Check in PATH
	if path, err := exec.LookPath(name); err == nil {
		return path
	}

	// 3. Platform specific defaults
	if runtime.GOOS == "darwin" {
		macosPath := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
		if _, err := os.Stat(macosPath); err == nil {
			return macosPath
		}
	}

	return ""
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	tsPath := getTailscaleBinaryPath()
	if tsPath != "" {
		cmd := exec.Command(tsPath, "status", "--json")
		if out, err := cmd.Output(); err == nil {
			ip := s.GetSystemTailscaleIP()
			if ip != "" {
				return &Status{
					Running:   true,
					LoggedIn:  true,
					Backend:   "Running (System)",
					Userspace: false,
					Self: Peer{
						IP4: ip,
					},
				}, nil
			}
			statusStr := string(out)
			if strings.Contains(statusStr, "LoggedOut") || strings.Contains(statusStr, "NeedsLogin") {
				return &Status{
					Running:  true,
					LoggedIn: false,
					Backend:  "NeedsLogin (System)",
				}, nil
			}
		}
	}

	lc, err := s.localClient()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	state, err := lc.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}

	out := &Status{
		Running:   strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn:  state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:   strings.TrimSpace(state.BackendState),
		Userspace: !state.TUN,
	}
	if state.Self != nil {
		out.Self = Peer{
			HostName:  strings.TrimSpace(state.Self.HostName),
			DNSName:   trimDotSuffix(state.Self.DNSName),
			IP4:       firstAddr(state.Self.TailscaleIPs),
			UserLogin: userLogin(state, state.Self.UserID),
			Online:    state.Self.Online,
		}
	}
	return out, nil
}

func (s *Service) TailnetIPv4(ctx context.Context) (string, error) {
	if runtime.GOOS == "linux" {
		if ip := s.GetSystemTailscaleIP(); ip != "" {
			return ip, nil
		}
	}
	status, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status.Self.IP4) == "" {
		return "", fmt.Errorf("tailscale IPv4 address unavailable")
	}
	return status.Self.IP4, nil
}

func (s *Service) IsUserspace(ctx context.Context) (bool, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return false, err
	}
	return status.Userspace, nil
}

func (s *Service) StartLogin(ctx context.Context) (string, error) {
	tsPath := getTailscaleBinaryPath()
	if tsPath != "" {
		logrus.Infof("🚀 [Tailscale] Starting system login via %s", tsPath)
		var cmd *exec.Cmd
		if runtime.GOOS == "linux" {
			cmd = exec.Command("pkexec", tsPath, "up", "--accept-dns=false")
		} else if runtime.GOOS == "darwin" {
			cmd = exec.Command(tsPath, "up", "--accept-dns=false")
		} else {
			cmd = exec.Command(tsPath, "up", "--accept-dns=false")
		}
		
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		
		if err := cmd.Start(); err != nil {
			if runtime.GOOS == "darwin" {
				// Try with osascript if direct start failed
				script := fmt.Sprintf("do shell script \"%s up --accept-dns=false\" with administrator privileges", tsPath)
				cmd = exec.Command("osascript", "-e", script)
				out, err2 := cmd.CombinedOutput()
				if err2 == nil {
					output := string(out)
					lines := strings.Split(output, "\n")
					for _, line := range lines {
						if strings.Contains(line, "https://login.tailscale.com") {
							for _, p := range strings.Fields(line) {
								if strings.HasPrefix(p, "https://") {
									return p, nil
								}
							}
						}
					}
				}
			}
			logrus.Warnf("⚠️ [Tailscale] System login failed: %v", err)
		} else {
			urlChan := make(chan string, 1)
			go func() {
				scanner := bufio.NewScanner(io.MultiReader(stdout, stderr))
				for scanner.Scan() {
					line := scanner.Text()
					if strings.Contains(line, "https://login.tailscale.com") {
						for _, w := range strings.Fields(line) {
							if strings.HasPrefix(w, "https://") {
								urlChan <- w
								return
							}
						}
					}
				}
			}()
			select {
			case foundURL := <-urlChan:
				return foundURL, nil
			case <-time.After(15 * time.Second):
				logrus.Warn("⚠️ [Tailscale] No link from system Tailscale in 15s")
			}
		}
	}

	logrus.Info("tailscale agent: StartLogin begin (tsnet)")
	lc, err := s.localClient()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
	}

	if err := lc.StartLoginInteractive(ctx); err != nil {
		logrus.WithError(err).Error("tailscale agent: StartLoginInteractive failed")
	}

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		return "", err
	}
	defer watcher.Close()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, err := lc.Status(ctx)
		if err == nil && status != nil {
			if url := strings.TrimSpace(status.AuthURL); url != "" {
				return url, nil
			}
			if strings.TrimSpace(status.BackendState) == "Running" {
				return "", nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("auth URL timeout")
}

func (s *Service) Logout(ctx context.Context) error {
	tsPath := getTailscaleBinaryPath()
	if tsPath != "" {
		logrus.Infof("🛑 [Tailscale] System CLI logout via %s", tsPath)
		if runtime.GOOS == "linux" {
			_ = exec.Command("pkexec", tsPath, "logout").Run()
		} else {
			_ = exec.Command(tsPath, "logout").Run()
		}
		return nil
	}

	lc, err := s.localClient()
	if err != nil {
		return err
	}
	return lc.Logout(ctx)
}

func (s *Service) Server() (*tsnet.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return s.server, nil
	}

	s.server = &tsnet.Server{
		Dir:      tailscaleStateDir("usbridge-agent"),
		Hostname: "usbridge-agent",
		UserLogf: s.handleUserLogf,
	}

	if err := s.server.Start(); err != nil {
		s.server = nil
		return nil, fmt.Errorf("tailscale start: %w", err)
	}
	return s.server, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server == nil {
		return nil
	}
	err := s.server.Close()
	s.server = nil
	if err != nil {
		return fmt.Errorf("tailscale close: %w", err)
	}
	return nil
}

func (s *Service) localClient() (*local.Client, error) {
	server, err := s.Server()
	if err != nil {
		return nil, err
	}
	lc, err := server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("tailscale local client: %w", err)
	}
	return lc, nil
}

func (s *Service) handleUserLogf(format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	logrus.Infof("tailscale agent tsnet: %s", message)
	const marker = "or go to: "
	if idx := strings.LastIndex(message, marker); idx >= 0 {
		url := strings.TrimSpace(message[idx+len(marker):])
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			s.setLatestAuthURL(url)
			logrus.Infof("tailscale agent: captured AuthURL from tsnet log %s", url)
		}
	}
}

func (s *Service) setLatestAuthURL(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestAuthURL = strings.TrimSpace(raw)
}

func (s *Service) getLatestAuthURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.latestAuthURL
}

func tailscaleStateDir(appName string) string {
	if base, err := os.UserConfigDir(); err == nil && strings.TrimSpace(base) != "" {
		return filepath.Join(base, appName, "tailscale")
	}
	return filepath.Join(os.TempDir(), appName, "tailscale")
}

func trimDotSuffix(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
}

func firstAddr(values []netip.Addr) string {
	for _, v := range values {
		if v.Is4() {
			return v.String()
		}
	}
	return ""
}

func userLogin(state *ipnstate.Status, userID tailcfg.UserID) string {
	if state == nil || state.User == nil {
		return ""
	}
	user, ok := state.User[userID]
	if !ok {
		return ""
	}
	return strings.TrimSpace(user.LoginName)
}

func (s *Service) GetSystemTailscaleIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		if !strings.Contains(name, "tailscale") && !strings.Contains(name, "wg") && !strings.Contains(name, "tun") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
				ip := ipnet.IP.To4()
				if ip != nil && ip[0] == 100 {
					return ip.String()
				}
			}
		}
	}
	return ""
}

package tailscale

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

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
	mu sync.Mutex
}

func New() *Service {
	return &Service{}
}

// JSON structures for tailscale status --json
type tsStatus struct {
	BackendState string                `json:"BackendState"`
	AuthURL      string                `json:"AuthURL"`
	TUN          bool                  `json:"TUN"`
	Self         *tsPeerStatus         `json:"Self"`
	User         map[string]tsUserInfo `json:"User"` // JSON map keys are always strings
}

type tsPeerStatus struct {
	DNSName      string      `json:"DNSName"`
	HostName     string      `json:"HostName"`
	TailscaleIPs []string    `json:"TailscaleIPs"`
	UserID       interface{} `json:"UserID"` // Can be int or string depending on version
	Online       bool        `json:"Online"`
}

type tsUserInfo struct {
	LoginName string `json:"LoginName"`
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	tsPath := s.getTailscalePath()
	if tsPath == "" {
		return &Status{Backend: "Tailscale not found"}, nil
	}

	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, tsPath, "status", "--json")
	out, err := cmd.Output()
	if err != nil {
		// If tailscale is not running, status might return error
		return &Status{Running: false, LoggedIn: false, Backend: "Not Running"}, nil
	}

	var raw tsStatus
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse tailscale status: %w", err)
	}

	loggedIn := raw.BackendState == "Running" || raw.BackendState == "Starting" || (raw.BackendState != "NeedsLogin" && raw.BackendState != "NoState" && raw.BackendState != "LoggedOut" && raw.BackendState != "")

	res := &Status{
		Running:   raw.BackendState == "Running",
		LoggedIn:  loggedIn,
		Backend:   raw.BackendState,
		Userspace: !raw.TUN,
	}

	if raw.Self != nil {
		var ip4 string
		for _, ip := range raw.Self.TailscaleIPs {
			if strings.Contains(ip, ".") {
				ip4 = ip
				break
			}
		}

		login := ""
		if raw.User != nil {
			var userIDStr string
			switch v := raw.Self.UserID.(type) {
			case float64:
				userIDStr = fmt.Sprintf("%.0f", v)
			case string:
				userIDStr = v
			}
			if u, ok := raw.User[userIDStr]; ok {
				login = u.LoginName
			}
		}

		res.Self = Peer{
			HostName:  raw.Self.HostName,
			DNSName:   strings.TrimSuffix(raw.Self.DNSName, "."),
			IP4:       ip4,
			UserLogin: login,
			Online:    raw.Self.Online,
		}
	}

	// Double check IP if not found in JSON (sometimes it happens in some states)
	if res.Self.IP4 == "" && res.Running {
		res.Self.IP4 = s.GetSystemTailscaleIP()
	}

	return res, nil
}

func (s *Service) IsUserspace(ctx context.Context) (bool, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return false, err
	}
	return status.Userspace, nil
}

func (s *Service) TailnetIPv4(ctx context.Context) (string, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	if status.Self.IP4 == "" {
		return "", fmt.Errorf("tailscale IPv4 address unavailable")
	}
	return status.Self.IP4, nil
}

func (s *Service) StartLogin(ctx context.Context) (string, error) {
	tsPath := s.getTailscalePath()
	if tsPath == "" {
		return "", fmt.Errorf("tailscale binary not found")
	}

	logrus.Infof("🚀 [Tailscale] Starting system login via %s", tsPath)
	
	args := s.upArgs()
	
	// Prepare command
	cmd := s.prepareUpCommand(tsPath, args)
	
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	
	if err := cmd.Start(); err != nil {
		return s.handleUpStartError(tsPath, args, err)
	}

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
		// Check status again, maybe it's already logged in
		status, _ := s.Status(ctx)
		if status != nil && status.LoggedIn {
			return "", nil
		}
	}
	
	return "", fmt.Errorf("login URL not found in tailscale output")
}

func (s *Service) Logout(ctx context.Context) error {
	tsPath := s.getTailscalePath()
	if tsPath == "" {
		return fmt.Errorf("tailscale binary not found")
	}

	logrus.Infof("🛑 [Tailscale] System CLI logout via %s", tsPath)
	return s.runLogoutCommand(tsPath)
}

// Close is a no-op for system tailscale
func (s *Service) Close() error {
	return nil
}

// Server is a no-op for system tailscale (returns nil, nil)
func (s *Service) Server() (any, error) {
	return nil, nil
}

func (s *Service) GetSystemTailscaleIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		// Check for common tailscale interface names
		if !strings.Contains(name, "tailscale") && !strings.Contains(name, "utun") && !strings.Contains(name, "wg") {
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

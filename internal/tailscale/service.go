package tailscale

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"tailscale.com/client/local"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"

	"usbridge_agent/internal/config"
)

type Status struct {
	Running   bool
	LoggedIn  bool
	Backend   string
	Userspace bool
	Self      Peer
	Peers     []Peer
}

type Peer struct {
	HostName  string
	DNSName   string
	IP4       string
	UserLogin string
	Online    bool
	Active    bool
	Relay     string
	CurAddr   string
}

type Service struct {
	mu            sync.Mutex
	userspace     bool
	server        *tsnet.Server
	stateDir      string
	latestAuthURL string
	ctx           context.Context
	cancel        context.CancelFunc
}

func New(mode config.TailscaleMode, stateDir string) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		userspace: mode == config.TailscaleModeUserspace,
		stateDir:  stateDir,
		ctx:       ctx,
		cancel:    cancel,
	}
	go s.monitorLoop()
	return s
}

func (s *Service) monitorLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastState string
	var lastPeers = make(map[string]string) // Peer IP -> Connection Type (Direct/Relay)

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			status, err := s.Status(s.ctx)
			if err != nil {
				continue
			}

			if status.Backend != lastState {
				logrus.Infof("🛰️ [Tailscale] Backend state changed: %s -> %s", lastState, status.Backend)
				lastState = status.Backend
			}

			if !status.Running {
				continue
			}

			currentPeers := make(map[string]string)
			for _, p := range status.Peers {
				if !p.Online {
					continue
				}
				
				connType := "Direct"
				if p.Relay != "" {
					connType = fmt.Sprintf("Relay (%s)", p.Relay)
				}
				
				// Identify connection change for ACTIVE peers
				if p.Active {
					prevType, exists := lastPeers[p.IP4]
					if !exists || prevType != connType {
						if p.Relay != "" {
							logrus.Warnf("⚠️ [Tailscale] Connection to %s (%s) is via RELAY (%s). NAT traversal failed or is in progress.", p.HostName, p.IP4, p.Relay)
						} else {
							logrus.Infof("🎯 [Tailscale] Connection to %s (%s) is DIRECT (NAT punch successful!)", p.HostName, p.IP4)
						}
					}
					
					if p.CurAddr != "" {
						// Optionally log the current endpoint address
						// logrus.Debugf("📡 [Tailscale] Peer %s endpoint: %s", p.HostName, p.CurAddr)
					}
				}
				currentPeers[p.IP4] = connType
			}
			lastPeers = currentPeers
		}
	}
}

func (s *Service) ApplyConfig(userspace bool, stateDir string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.userspace == userspace && s.stateDir == stateDir {
		return nil
	}

	logrus.Infof("🛰️ [Tailscale] Mode changing: userspace=%v -> %v", s.userspace, userspace)

	if s.server != nil {
		_ = s.server.Close()
		s.server = nil
	}

	s.userspace = userspace
	s.stateDir = stateDir
	s.latestAuthURL = ""

	return nil
}

// JSON structures for system tailscale status --json
type tsStatus struct {
	BackendState string                   `json:"BackendState"`
	AuthURL      string                   `json:"AuthURL"`
	TUN          bool                     `json:"TUN"`
	Self         *tsPeerStatus            `json:"Self"`
	Peer         map[string]*tsPeerStatus `json:"Peer"`
	User         map[string]tsUserInfo    `json:"User"`
}

type tsPeerStatus struct {
	DNSName      string      `json:"DNSName"`
	HostName     string      `json:"HostName"`
	TailscaleIPs []string    `json:"TailscaleIPs"`
	UserID       interface{} `json:"UserID"`
	Online       bool        `json:"Online"`
	Active       bool        `json:"Active"`
	Relay        string      `json:"Relay"`
	CurAddr      string      `json:"CurAddr"`
}

type tsUserInfo struct {
	LoginName string `json:"LoginName"`
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	if userspace {
		return s.statusUserspace(ctx)
	}
	return s.statusSystem(ctx)
}

func (s *Service) statusUserspace(ctx context.Context) (*Status, error) {
	lc, err := s.localClient()
	if err != nil {
		return &Status{Running: false, Backend: "Initializing"}, nil
	}

	state, err := lc.Status(ctx)
	if err != nil {
		return &Status{Running: false, Backend: "Not Running"}, nil
	}

	out := &Status{
		Running:   strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn:  state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:   strings.TrimSpace(state.BackendState),
		Userspace: true,
	}

	if state.Self != nil {
		out.Self = s.mapIpnPeer(state, state.Self)
	}

	for _, key := range state.Peers() {
		peer := state.Peer[key]
		if peer == nil {
			continue
		}
		out.Peers = append(out.Peers, s.mapIpnPeer(state, peer))
	}

	return out, nil
}

func (s *Service) statusSystem(ctx context.Context) (*Status, error) {
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
	s.configureCommand(cmd)
	out, err := cmd.Output()
	if err != nil {
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
		res.Self = s.mapTsPeer(raw.Self, raw.User)
	}

	for _, p := range raw.Peer {
		res.Peers = append(res.Peers, s.mapTsPeer(p, raw.User))
	}

	if res.Self.IP4 == "" && res.Running {
		res.Self.IP4 = s.GetSystemTailscaleIP()
	}

	return res, nil
}

func (s *Service) mapIpnPeer(st *ipnstate.Status, p *ipnstate.PeerStatus) Peer {
	return Peer{
		HostName:  strings.TrimSpace(p.HostName),
		DNSName:   strings.TrimSuffix(p.DNSName, "."),
		IP4:       s.firstAddr(p.TailscaleIPs),
		UserLogin: s.userLogin(st, p.UserID),
		Online:    p.Online,
		Active:    p.Active,
		Relay:     p.Relay,
		CurAddr:   p.CurAddr,
	}
}

func (s *Service) mapTsPeer(p *tsPeerStatus, users map[string]tsUserInfo) Peer {
	var ip4 string
	for _, ip := range p.TailscaleIPs {
		if strings.Contains(ip, ".") {
			ip4 = ip
			break
		}
	}

	login := ""
	if users != nil {
		var userIDStr string
		switch v := p.UserID.(type) {
		case float64:
			userIDStr = fmt.Sprintf("%.0f", v)
		case string:
			userIDStr = v
		}
		if u, ok := users[userIDStr]; ok {
			login = u.LoginName
		}
	}

	return Peer{
		HostName:  p.HostName,
		DNSName:   strings.TrimSuffix(p.DNSName, "."),
		IP4:       ip4,
		UserLogin: login,
		Online:    p.Online,
		Active:    p.Active,
		Relay:     p.Relay,
		CurAddr:   p.CurAddr,
	}
}

func (s *Service) IsUserspace(ctx context.Context) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userspace, nil
}

func (s *Service) TailnetIPv4(ctx context.Context) (string, error) {
	status, err := s.Status(ctx)
	if err == nil && status.Self.IP4 != "" {
		return status.Self.IP4, nil
	}

	if ip := s.GetSystemTailscaleIP(); ip != "" {
		return ip, nil
	}

	if err != nil {
		return "", err
	}
	return "", fmt.Errorf("tailscale IPv4 address unavailable")
}

func (s *Service) StartLogin(ctx context.Context) (string, error) {
	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	if userspace {
		return s.startLoginUserspace(ctx)
	}
	return s.startLoginSystem(ctx)
}

func (s *Service) startLoginUserspace(ctx context.Context) (string, error) {
	lc, err := s.localClient()
	if err != nil {
		return "", err
	}

	_ = lc.StartLoginInteractive(ctx)
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

func (s *Service) startLoginSystem(ctx context.Context) (string, error) {
	tsPath := s.getTailscalePath()
	if tsPath == "" {
		return "", fmt.Errorf("tailscale binary not found")
	}

	logrus.Infof("🚀 [Tailscale] Starting system login via %s", tsPath)

	args := s.upArgs()
	cmd := s.prepareUpCommand(tsPath, args)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return s.handleUpStartError(tsPath, args, err)
	}

	urlChan := make(chan string, 1)

	scanFunc := func(r io.Reader, name string) {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			logrus.Infof("📡 [Tailscale/%s] %s", name, line)
			if url := s.extractURL(line); url != "" {
				select {
				case urlChan <- url:
				default:
				}
				return
			}
		}
	}

	go scanFunc(stdout, "stdout")
	go scanFunc(stderr, "stderr")

	doneChan := make(chan error, 1)
	go func() {
		doneChan <- cmd.Wait()
	}()

	select {
	case foundURL := <-urlChan:
		return foundURL, nil
	case err := <-doneChan:
		logrus.Warnf("⚠️ [Tailscale] Command finished (err=%v), checking result...", err)
		if err != nil {
			return s.handleUpStartError(tsPath, args, err)
		}
	case <-time.After(20 * time.Second):
		logrus.Warn("⚠️ [Tailscale] No link captured in 20s")
	}

	status, _ := s.Status(ctx)
	if status != nil && status.LoggedIn {
		return "", nil
	}

	return "", fmt.Errorf("login URL not found in tailscale output")
}

func (s *Service) extractURL(text string) string {
	text = strings.TrimSpace(text)
	if strings.Contains(text, "https://login.tailscale.com") {
		for _, w := range strings.Fields(text) {
			if strings.HasPrefix(w, "https://") {
				return w
			}
		}
	}
	return ""
}

func (s *Service) Logout(ctx context.Context) error {
	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	if userspace {
		lc, err := s.localClient()
		if err != nil {
			return err
		}
		return lc.Logout(ctx)
	}

	tsPath := s.getTailscalePath()
	if tsPath == "" {
		return fmt.Errorf("tailscale binary not found")
	}

	logrus.Infof("🛑 [Tailscale] System CLI logout via %s", tsPath)
	return s.runLogoutCommand(tsPath)
}

func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		err := s.server.Close()
		s.server = nil
		return err
	}
	return nil
}

func (s *Service) Server() (*tsnet.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return s.server, nil
	}

	stateDir := s.stateDir
	if stateDir == "" {
		base, _ := os.UserConfigDir()
		stateDir = filepath.Join(base, "usbridge-agent", "tailscale")
	} else {
		stateDir = filepath.Join(stateDir, "tailscale")
	}

	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return nil, err
	}

	s.server = &tsnet.Server{
		Dir:       stateDir,
		Hostname:  "usbridge-agent",
		UserLogf:  s.handleUserLogf,
		Ephemeral: true,
	}

	if err := s.server.Start(); err != nil {
		return nil, err
	}

	return s.server, nil
}

func (s *Service) handleUserLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	logrus.Infof("📡 [Tailscale/User] %s", msg)
}

func (s *Service) localClient() (*local.Client, error) {
	srv, err := s.Server()
	if err != nil {
		return nil, err
	}
	return srv.LocalClient()
}

func (s *Service) firstAddr(values []netip.Addr) string {
	for _, v := range values {
		if v.Is4() {
			return v.String()
		}
	}
	return ""
}

func (s *Service) userLogin(st *ipnstate.Status, id tailcfg.UserID) string {
	if st == nil || st.User == nil {
		return ""
	}
	if u, ok := st.User[id]; ok {
		return u.LoginName
	}
	return ""
}

func (s *Service) GetSystemTailscaleIP() string {
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
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


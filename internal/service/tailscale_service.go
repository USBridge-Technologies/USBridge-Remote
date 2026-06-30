package service

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
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

type TailscaleStatus struct {
	Running   bool
	LoggedIn  bool
	Backend   string
	Self      TailscalePeer
	Peers     []TailscalePeer
	Userspace bool
}

type TailscalePeer struct {
	ID        string
	HostName  string
	DNSName   string
	OS        string
	Online    bool
	Active    bool
	IP4       string
	UserLogin string
	Relay     string
	CurAddr   string
	Addrs     []string
}

type TailscaleService struct {
	mu                 sync.Mutex
	server             *tsnet.Server
	userspace          bool
	latestAuthURL      string
	videoRelayConn     net.PacketConn
	videoRelayCancel   context.CancelFunc
	videoRelayPort     int
	videoRelayTailAddr string
	videoRelayTraceID  string
	videoRelayPackets  uint64
	videoRelayLastFrom string
	videoRelayLastAt   time.Time
	lastBackendState   string
}

func NewTailscaleService(userspace bool) *TailscaleService {
	return &TailscaleService{
		userspace: userspace,
	}
}

func (s *TailscaleService) SetUserspace(userspace bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.userspace == userspace {
		return
	}
	s.userspace = userspace
	logrus.Infof("🛰️ [Tailscale] Mode changed to userspace=%v", userspace)
}

func (s *TailscaleService) IsUserspace() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.userspace
}

func (s *TailscaleService) IsSystemTailscaleAvailable() bool {
	return getTailscaleBinaryPath() != ""
}

// CheckSystemTailscaleStatus returns system Tailscale status without starting tsnet.
// Returns nil if system Tailscale is unavailable or not in a connected state.
func (s *TailscaleService) CheckSystemTailscaleStatus() *TailscaleStatus {
	// On Android there is no system Tailscale binary — skip the exec.Command to
	// avoid blocking ~2s waiting for a non-existent binary.
	// On macOS/Windows/Linux we always check the system binary, even when tsnet
	// is configured as the preferred mode. This lets initTailscaleMode correctly
	// detect a running system Tailscale daemon and switch to kernel mode so that
	// tsnet is NOT started alongside system TS (two WireGuard stacks on the same
	// machine disrupt macOS LAN routing, causing EHOSTUNREACH for LAN addresses).
	if runtime.GOOS == "android" {
		return nil
	}
	tsPath := getTailscaleBinaryPath()
	if tsPath == "" {
		return nil
	}
	cmd := exec.Command(tsPath, "status", "--json")
	maybeHideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var st ipnstate.Status
	if err := json.Unmarshal(out, &st); err != nil {
		return nil
	}
	if st.BackendState == "" || st.BackendState == "NeedsLogin" || st.BackendState == "LoggedOut" || st.BackendState == "NoState" {
		return nil
	}
	result := &TailscaleStatus{
		Running:   strings.TrimSpace(st.BackendState) == "Running",
		LoggedIn:  true,
		Backend:   strings.TrimSpace(st.BackendState),
		Userspace: false,
	}
	if st.Self != nil {
		result.Self = s.convertPeer(&st, st.Self)
	}
	for _, key := range st.Peers() {
		p := st.Peer[key]
		if p == nil {
			continue
		}
		result.Peers = append(result.Peers, s.convertPeer(&st, p))
	}
	return result
}

// LogSystemTailscalePeers logs system Tailscale peer routing for diagnostic purposes.
func (s *TailscaleService) LogSystemTailscalePeers() {
	sysSt := s.CheckSystemTailscaleStatus()
	if sysSt == nil {
		logrus.Info("🛰️ [sys-ts] CheckSystemTailscaleStatus: not available (binary missing or not running)")
		return
	}
	logrus.Infof("🛰️ [sys-ts] backend=%s peers=%d", sysSt.Backend, len(sysSt.Peers))
	for _, p := range sysSt.Peers {
		logrus.Infof("🛰️ [sys-ts]   peer ip4=%-18s curAddr=%-28s relay=%s hostname=%s", p.IP4, p.CurAddr, p.Relay, p.HostName)
	}
}

func (s *TailscaleService) Start(ctx context.Context) error {
	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	if userspace {
		_, err := s.serverInstance()
		return err
	}
	return nil
}

func (s *TailscaleService) Stop() error {
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.mu.Unlock()

	_ = s.StopVideoUDPRelay()

	if server != nil {
		logrus.Info("🛰️ [Tailscale] Stopping tsnet server...")
		// Attempt a clean logout before closing so the coordination server
		// immediately deregisters this node instead of waiting for a timeout.
		// This prevents the system Tailscale daemon from entering a confused
		// state when tsnet ran on the same account.
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			lc, err := server.LocalClient()
			if err == nil {
				_ = lc.Logout(ctx)
			}
		}()
		return server.Close()
	}
	return nil
}

func (s *TailscaleService) Status(ctx context.Context) (status *TailscaleStatus, err error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in TailscaleService.Status: %v", r)
			err = fmt.Errorf("panic in tailscale status: %v", r)
		}
	}()

	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	var state *ipnstate.Status

	if !userspace {
		tsPath := getTailscaleBinaryPath()
		if tsPath != "" {
			cmd := exec.Command(tsPath, "status", "--json")
			maybeHideWindow(cmd)
			if out, err := cmd.Output(); err == nil {
				var st ipnstate.Status
				if err := json.Unmarshal(out, &st); err == nil {
					state = &st
					if state.BackendState == "NeedsLogin" || state.BackendState == "LoggedOut" || state.BackendState == "NoState" {
						state = nil
					}
				}
			}
		}
	}

	if state == nil {
		// Don't auto-start the tsnet server during a status check.
		// Only query the local client if the server was already explicitly started.
		s.mu.Lock()
		serverStarted := s.server != nil
		s.mu.Unlock()
		if !serverStarted {
			return &TailscaleStatus{Running: false}, nil
		}

		refreshAndroidDefaultRouteInterface()
		lc, err := s.localClient()
		if err != nil {
			return &TailscaleStatus{Running: false}, nil
		}
		if ctx == nil {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
		}
		state, err = lc.Status(ctx)
		if err != nil {
			return &TailscaleStatus{Running: false}, nil
		}
	}

	if state == nil {
		return &TailscaleStatus{Running: false}, nil
	}

	out := &TailscaleStatus{
		Running:   strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn:  state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:   strings.TrimSpace(state.BackendState),
		Userspace: !state.TUN,
	}

	s.mu.Lock()
	if s.lastBackendState != out.Backend {
		logrus.Infof("🛰️ [Tailscale] State transition: %s -> %s", s.lastBackendState, out.Backend)
		s.lastBackendState = out.Backend
	}
	s.mu.Unlock()

	if state.Self != nil {
		out.Self = s.convertPeer(state, state.Self)
	}

	for _, key := range state.Peers() {
		peer := state.Peer[key]
		if peer == nil {
			continue
		}
		out.Peers = append(out.Peers, s.convertPeer(state, peer))
	}

	return out, nil
}

func (s *TailscaleService) convertPeer(st *ipnstate.Status, p *ipnstate.PeerStatus) TailscalePeer {
	return TailscalePeer{
		ID:        string(p.ID),
		HostName:  strings.TrimSpace(p.HostName),
		DNSName:   trimDotSuffix(p.DNSName),
		OS:        strings.TrimSpace(p.OS),
		Online:    p.Online,
		Active:    p.Active,
		IP4:       firstAddr(p.TailscaleIPs),
		UserLogin: userLogin(st, p.UserID),
		Relay:     p.Relay,
		CurAddr:   p.CurAddr,
		Addrs:     p.Addrs,
	}
}

func (s *TailscaleService) StartLogin(ctx context.Context) (string, error) {
	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	if !userspace {
		tsPath := getTailscaleBinaryPath()
		if tsPath != "" {
			logrus.Infof("🚀 [Tailscale] Starting system login via %s", tsPath)
			var cmd *exec.Cmd
			if runtime.GOOS == "linux" {
				cmd = exec.Command("pkexec", tsPath, "up", "--accept-dns=false")
			} else {
				cmd = exec.Command(tsPath, "up", "--accept-dns=false")
			}
			maybeHideWindow(cmd)

			stdout, _ := cmd.StdoutPipe()
			stderr, _ := cmd.StderrPipe()
			if err := cmd.Start(); err != nil {
				if runtime.GOOS == "darwin" {
					script := fmt.Sprintf("do shell script \"%s up --accept-dns=false\" with administrator privileges", tsPath)
					cmd = exec.Command("osascript", "-e", script)
					maybeHideWindow(cmd)
					out, err2 := cmd.CombinedOutput()
					if err2 == nil {
						return s.extractURL(string(out)), nil
					}
				}
				logrus.Warnf("⚠️ [Tailscale] System login failed: %v", err)
			} else {
				urlChan := make(chan string, 1)
				scanFunc := func(r io.Reader) {
					scanner := bufio.NewScanner(r)
					for scanner.Scan() {
						line := scanner.Text()
						if url := s.extractURL(line); url != "" {
							urlChan <- url
							return
						}
					}
				}
				go scanFunc(stdout)
				go scanFunc(stderr)
				select {
				case foundURL := <-urlChan:
					return foundURL, nil
				case <-time.After(15 * time.Second):
					logrus.Warn("⚠️ [Tailscale] No link from system Tailscale in 15s")
				}
			}
		}
	}

	refreshAndroidDefaultRouteInterface()
	lc, err := s.localClient()
	if err != nil { return "", err }
	_ = lc.StartLoginInteractive(ctx)
	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil { return "", err }
	defer watcher.Close()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		status, err := lc.Status(ctx)
		if err == nil && status != nil {
			if url := strings.TrimSpace(status.AuthURL); url != "" { return url, nil }
			if strings.TrimSpace(status.BackendState) == "Running" { return "", nil }
		}
		time.Sleep(500 * time.Millisecond)
	}
	return "", fmt.Errorf("auth URL timeout")
}

func (s *TailscaleService) Logout(ctx context.Context) error {
	s.mu.Lock()
	userspace := s.userspace
	s.mu.Unlock()

	if !userspace {
		tsPath := getTailscaleBinaryPath()
		if tsPath != "" {
			logrus.Infof("🛑 [Tailscale] System CLI logout via %s", tsPath)
			var cmd *exec.Cmd
			if runtime.GOOS == "linux" {
				cmd = exec.Command("pkexec", tsPath, "logout")
			} else {
				cmd = exec.Command(tsPath, "logout")
			}
			maybeHideWindow(cmd)
			_ = cmd.Run()
			return nil
		}
	}

	lc, err := s.localClient()
	if err != nil { return err }
	return lc.Logout(ctx)
}

func (s *TailscaleService) HTTPClient() (*http.Client, error) {
	// When system Tailscale is running it handles 100.x routing in the kernel —
	// use the plain OS dialer so Moonlight HTTP doesn't go through tsnet (which
	// may not have established a WireGuard session with the peer yet).
	if s.GetSystemTailscaleIP() != "" {
		return &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				Proxy:        nil,
				TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
			},
		}, nil
	}
	// Android / no system Tailscale: all traffic must go through tsnet.
	srv, err := s.serverInstance()
	if err != nil { return nil, err }
	
	client := srv.HTTPClient()
	if t, ok := client.Transport.(*http.Transport); ok {
		t.Proxy = nil
		t.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	}
	return client, nil
}

// WarmUpPeer proactively dials a Tailscale peer via tsnet to trigger the
// WireGuard handshake before Moonlight tries to use it for HTTP.
// No-op when system Tailscale is running (it handles routing transparently).
func (s *TailscaleService) WarmUpPeer(tailscaleIP string) {
	if s.GetSystemTailscaleIP() != "" {
		return
	}
	srv, err := s.serverInstance()
	if err != nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
		defer cancel()
		conn, err := srv.Dial(ctx, "tcp", net.JoinHostPort(tailscaleIP, "47989"))
		if err == nil {
			conn.Close()
		}
		logrus.Debugf("🛰️ [tsnet] peer warmup %s:47989 done (err=%v)", tailscaleIP, err)
	}()
}

// WaitUntilReady blocks until the tsnet node is online (Running state) or
// the context expires. On kernel Tailscale or non-userspace builds this is a no-op.
func (s *TailscaleService) WaitUntilReady(ctx context.Context) error {
	if !s.IsUserspace() {
		return nil
	}
	srv, err := s.serverInstance()
	if err != nil {
		return err
	}
	_, err = srv.Up(ctx)
	return err
}

func (s *TailscaleService) ValidateAddress(raw string) error { return nil }

func (s *TailscaleService) TailnetIPv4(ctx context.Context) (ip string, err error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in TailscaleService.TailnetIPv4: %v", r)
			err = fmt.Errorf("panic in tailnet ipv4: %v", r)
		}
	}()

	if !s.IsUserspace() {
		if sysIP := s.GetSystemTailscaleIP(); sysIP != "" {
			return sysIP, nil
		}
	}

	lc, err := s.localClient()
	if err != nil { return "", err }
	if ctx == nil { ctx = context.Background() }
	st, err := lc.Status(ctx)
	if err != nil || st.Self == nil { return "", fmt.Errorf("status failed") }
	return firstAddr(st.Self.TailscaleIPs), nil
}

func (s *TailscaleService) RefreshNetwork() {
	logrus.Info("🛰️ [Tailscale] Refreshing network state...")
	refreshAndroidDefaultRouteInterface()
	
	s.mu.Lock()
	srv := s.server
	s.mu.Unlock()

	if srv != nil {
		logrus.Debug("🛰️ [Tailscale] tsnet server re-discovery triggered via netmon update")
	}
}

func (s *TailscaleService) EnsureVideoUDPRelay(port int) (int, error) {
	if port <= 0 {
		port = 55000
	}

	if !s.IsUserspace() && s.GetSystemTailscaleIP() != "" {
		return port, nil
	}

	_ = s.StopVideoUDPRelay()

	srv, err := s.serverInstance()
	if err != nil { return 0, err }
	tailIP, err := s.TailnetIPv4(context.Background())
	if err != nil { return 0, err }

	var pc net.PacketConn
	tailAddr := net.JoinHostPort(tailIP, fmt.Sprintf("%d", port))
	pc, err = srv.ListenPacket("udp", tailAddr)
	if err != nil {
		logrus.Warnf("⚠️ [Tailscale] Port %d is busy in netstack, trying dynamic port...", port)
		tailAddr = net.JoinHostPort(tailIP, "0")
		pc, err = srv.ListenPacket("udp", tailAddr)
		if err != nil {
			return 0, fmt.Errorf("failed to listen on tailnet: %w", err)
		}
	}

	actualAddr := pc.LocalAddr().String()
	_, portStr, _ := net.SplitHostPort(actualAddr)
	actualPort, _ := strconv.Atoi(portStr)

	logrus.Infof("📡 [Tailscale] Relay listening on %s (requested %d)", actualAddr, port)

	localTarget, _ := net.ResolveUDPAddr("udp4", "127.0.0.1:"+fmt.Sprintf("%d", actualPort))
	localConn, err := net.DialUDP("udp4", nil, localTarget)
	if err != nil {
		pc.Close()
		return 0, fmt.Errorf("failed to dial local UDP: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.videoRelayConn = pc
	s.videoRelayCancel = cancel
	s.mu.Unlock()
	go s.runVideoUDPRelay(ctx, pc, localConn, actualPort, actualAddr)
	return actualPort, nil
}

func (s *TailscaleService) SetVideoRelayTraceID(traceID string) {
	s.mu.Lock(); defer s.mu.Unlock(); s.videoRelayTraceID = traceID
}

// VideoRelayDebugInfo returns a human-readable string describing how video
// traffic reaches the bridge. bridgeHost is the bridge's Tailscale IP.
// It checks system Tailscale first (more accurate for actual UDP path),
// then falls back to tsnet peer status.
func (s *TailscaleService) VideoRelayDebugInfo(bridgeHost string) string {
	s.mu.Lock()
	state := "inactive"
	if s.videoRelayConn != nil {
		state = "userspace-relay"
	} else if !s.userspace && s.GetSystemTailscaleIP() != "" {
		state = "system-stack"
	}
	s.mu.Unlock()

	if bridgeHost == "" {
		return state
	}
	cleanBridge := strings.Split(bridgeHost, ":")[0]

	// Check system Tailscale first — it has the actual WireGuard peer endpoints
	// and reflects the real path that UDP video traffic takes.
	if sysSt := s.CheckSystemTailscaleStatus(); sysSt != nil {
		for _, peer := range sysSt.Peers {
			if peer.IP4 == cleanBridge || strings.Contains(peer.DNSName, cleanBridge) {
				if peer.CurAddr != "" {
					return fmt.Sprintf("%s (system-ts: direct via %s)", state, peer.CurAddr)
				} else if peer.Relay != "" {
					return fmt.Sprintf("%s (system-ts: DERP via %s)", state, peer.Relay)
				}
			}
		}
	}

	// Fall back to tsnet peer status.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if st, err := s.Status(ctx); err == nil && st != nil {
		for _, peer := range st.Peers {
			if peer.IP4 == cleanBridge || strings.Contains(peer.DNSName, cleanBridge) || peer.HostName == cleanBridge {
				if peer.CurAddr != "" {
					return fmt.Sprintf("%s (tsnet: direct via %s)", state, peer.CurAddr)
				} else if peer.Relay != "" {
					return fmt.Sprintf("%s (tsnet: DERP via %s)", state, peer.Relay)
				}
				return fmt.Sprintf("%s (tsnet: routing unknown)", state)
			}
		}
	}

	return state
}

func (s *TailscaleService) PunchVideoHole(targetHost string, port int) {
	s.mu.Lock()
	pc := s.videoRelayConn
	traceID := s.videoRelayTraceID
	userspace := s.userspace
	s.mu.Unlock()

	host := strings.Split(targetHost, ":")[0]

	var puncher func(addr *net.UDPAddr)
	if pc != nil {
		puncher = func(addr *net.UDPAddr) {
			_, _ = pc.WriteTo([]byte("PUNCH"), addr)
		}
	} else if !userspace && s.GetSystemTailscaleIP() != "" {
		s.pingSystemTailscale(host)

		conn, err := net.ListenUDP("udp4", nil)
		if err == nil {
			defer conn.Close()
			puncher = func(addr *net.UDPAddr) {
				_, _ = conn.WriteTo([]byte("PUNCH"), addr)
			}
		}
	}

	if puncher == nil || targetHost == "" {
		return
	}

	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		logrus.Infof("🎯 [%s] Sending PUNCH to target direct IP %v (primary path)...", traceID, addr)
		puncher(addr)
	} else {
		logrus.Warnf("⚠️ [%s] Failed to resolve target host %s: %v", traceID, host, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if st, err := s.Status(ctx); err == nil && st != nil {
		foundPeer := false
		for _, peer := range st.Peers {
			if peer.IP4 == host || strings.Contains(peer.DNSName, host) || peer.HostName == host {
				foundPeer = true
				logrus.Infof("🎯 [%s] Found Tailscale peer %s (online=%v, active=%v, relay=%s)", traceID, peer.HostName, peer.Online, peer.Active, peer.Relay)
				
				if peer.CurAddr != "" {
					if ra, err := net.ResolveUDPAddr("udp4", peer.CurAddr); err == nil {
						videoAddr := &net.UDPAddr{IP: ra.IP, Port: port}
						logrus.Infof("🎯 [%s] Sending PUNCH to current peer active endpoint %v...", traceID, videoAddr)
						puncher(videoAddr)
					}
				}
				for _, addrStr := range peer.Addrs {
					if addrStr == peer.CurAddr {
						continue
					}
					if ra, err := net.ResolveUDPAddr("udp4", addrStr); err == nil {
						videoAddr := &net.UDPAddr{IP: ra.IP, Port: port}
						logrus.Infof("🎯 [%s] Sending PUNCH to potential peer endpoint %v...", traceID, videoAddr)
						puncher(videoAddr)
					}
				}
				break
			}
		}
		if !foundPeer {
			logrus.Warnf("⚠️ [%s] Target host %s not found in Tailscale peer list", traceID, host)
		}
	} else {
		logrus.Errorf("⚠️ [%s] Failed to get Tailscale status for punching: %v", traceID, err)
	}
}

func (s *TailscaleService) pingSystemTailscale(host string) {
	tsPath := getTailscaleBinaryPath()
	if tsPath == "" {
		return
	}

	go func() {
		logrus.Infof("🎯 [Tailscale] Running system ping to %s to force P2P...", host)
		cmd := exec.Command(tsPath, "ping", "--timeout=5s", "--c=5", "--verbose", host)
		maybeHideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			logrus.Warnf("⚠️ [Tailscale] System ping to %s failed: %v (out: %s)", host, err, string(out))
		} else {
			logrus.Infof("✅ [Tailscale] System ping to %s completed: %s", host, string(out))
		}

		statusCmd := exec.Command(tsPath, "status")
		maybeHideWindow(statusCmd)
		_ = statusCmd.Run()
	}()
}

// GetPeerDirectIP returns the non-Tailscale (LAN/direct) IP of a peer when
// both nodes are on the same local network. Returns "" when the peer is only
// reachable via DERP relay (different networks).
// Used on macOS/Android with userspace tsnet where C-level sockets bypass tsnet
// and can only reach LAN IPs through the kernel network stack.
func (s *TailscaleService) GetPeerDirectIP(tailscaleIP string) string {
	// Prefer system Tailscale status — it discovers the correct LAN endpoint
	// via proper STUN/NAT traversal, while tsnet may see a Docker/VM interface.
	if sysSt := s.CheckSystemTailscaleStatus(); sysSt != nil {
		for _, peer := range sysSt.Peers {
			if peer.IP4 != tailscaleIP {
				continue
			}
			if peer.CurAddr != "" {
				if host, _, err := net.SplitHostPort(peer.CurAddr); err == nil {
					if !strings.HasPrefix(host, "100.") && net.ParseIP(host) != nil {
						logrus.Infof("🎯 [Tailscale] system-ts direct IP for peer %s: %s (tsnet may differ)", tailscaleIP, host)
						return host
					}
				}
			}
		}
	}

	// Fall back to tsnet peer status.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	st, err := s.Status(ctx)
	if err != nil || st == nil {
		return ""
	}
	for _, peer := range st.Peers {
		if peer.IP4 != tailscaleIP {
			continue
		}

		var candidates []string

		// CurAddr is the active endpoint, e.g. "192.168.1.108:41641"
		if peer.CurAddr != "" {
			if host, _, err := net.SplitHostPort(peer.CurAddr); err == nil {
				candidates = append(candidates, host)
			}
		}
		// Also look in Addrs list
		for _, addr := range peer.Addrs {
			if host, _, err := net.SplitHostPort(addr); err == nil {
				candidates = append(candidates, host)
			}
		}
		logrus.Debugf("🎯 [Tailscale] tsnet peer %s candidates: %v", tailscaleIP, candidates)

		// Filter and prioritize candidates
		var bestIP string
		maxPriority := -1

		for _, ipStr := range candidates {
			ip := net.ParseIP(ipStr)
			if ip == nil || ip.IsLoopback() || strings.HasPrefix(ipStr, "100.") || strings.Contains(ipStr, ":") {
				continue
			}

			priority := 0
			if strings.HasPrefix(ipStr, "192.168.") {
				priority = 10
			} else if strings.HasPrefix(ipStr, "10.") {
				priority = 8
			} else if strings.HasPrefix(ipStr, "172.") {
				// 172.16.x.x to 172.31.x.x is private, but let's just prefer others
				priority = 5
			}

			if priority > maxPriority {
				maxPriority = priority
				bestIP = ipStr
			}
		}

		if bestIP != "" {
			logrus.Infof("🎯 [Tailscale] Selected best direct IP %s for peer %s (priority %d)", bestIP, tailscaleIP, maxPriority)
			return bestIP
		}
		break
	}
	return ""
}

func (s *TailscaleService) PeerConnectionMode(targetHost string) string {
	if targetHost == "" {
		return "unknown"
	}
	cleanTarget := strings.Split(targetHost, ":")[0]

	// Check system Tailscale first — it reflects the real UDP path.
	if sysSt := s.CheckSystemTailscaleStatus(); sysSt != nil {
		for _, peer := range sysSt.Peers {
			if peer.IP4 == cleanTarget || strings.HasPrefix(peer.DNSName, cleanTarget) {
				if peer.CurAddr != "" {
					return "direct:" + peer.CurAddr
				}
				if peer.Relay != "" {
					return "derp:" + peer.Relay
				}
			}
		}
	}

	// Fall back to tsnet.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	st, err := s.Status(ctx)
	if err != nil || st == nil {
		return "unknown"
	}
	for _, peer := range st.Peers {
		if peer.IP4 == cleanTarget || strings.HasPrefix(peer.DNSName, cleanTarget) {
			if peer.CurAddr != "" {
				return "direct:" + peer.CurAddr
			}
			if peer.Relay != "" {
				return "derp:" + peer.Relay
			}
			return "unknown"
		}
	}
	return "unknown"
}

func (s *TailscaleService) StopVideoUDPRelay() error {
	s.mu.Lock()
	cancel, pc := s.videoRelayCancel, s.videoRelayConn
	s.videoRelayCancel, s.videoRelayConn = nil, nil
	s.mu.Unlock()
	if cancel != nil { cancel() }
	if pc != nil { pc.Close() }
	return nil
}

func (s *TailscaleService) RespondToVideoProbe(systemIP string, port int, timeout time.Duration) error {
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("0.0.0.0:%d", port))
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("listen error: %w", err)
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1024)
	n, remoteAddr, err := conn.ReadFrom(buf)
	if err != nil {
		return fmt.Errorf("read error: %w", err)
	}
	if strings.Contains(string(buf[:n]), "RELAY_PROBE") {
		_, _ = conn.WriteTo([]byte(videoRelayAckPayload), remoteAddr)
		logrus.Infof("✅ [Tailscale/SystemStack] Responded to probe from %v", remoteAddr)
		return nil
	}
	return fmt.Errorf("unexpected packet")
}

func (s *TailscaleService) extractURL(text string) string {
	if strings.Contains(text, "https://login.tailscale.com") {
		for _, w := range strings.Fields(text) {
			if strings.HasPrefix(w, "https://") {
				return w
			}
		}
	}
	return ""
}

func (s *TailscaleService) GetBindHost() string { return "0.0.0.0" }

func (s *TailscaleService) serverInstance() (*tsnet.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil { return s.server, nil }

	initAndroidTailscaleUserspace()

	if runtime.GOOS == "android" {
		logrus.Info("🛰️ [Tailscale] Requesting Android VPN permissions (just in case)")
		_ = requestAndroidVpnPermission()
	}

	stateDir := tailscaleStateDir("usbridge-client")
	logrus.Infof("🛰️ [Tailscale] State directory: %s", stateDir)
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		logrus.WithError(err).Errorf("🛰️ [Tailscale] Failed to create state directory: %s", stateDir)
	}

	s.server = &tsnet.Server{
		Dir:      stateDir,
		Hostname: "usbridge-client",
		UserLogf: s.handleUserLogf,
		// Ephemeral=true: the node is automatically removed from the Tailscale
		// coordination server when it disconnects. This prevents zombie "offline"
		// entries from accumulating in the peer list and causing system Tailscale
		// to repeatedly retry failed WireGuard handshakes, which was disrupting
		// LAN routing for bundle-launched apps on macOS. Auth credentials are
		// preserved in the state directory and re-used on the next connection.
		Ephemeral: true,
	}

	logrus.Info("🛰️ [Tailscale] Starting tsnet server...")
	if err := s.server.Start(); err != nil {
		logrus.WithError(err).Error("🛰️ [Tailscale] Failed to start tsnet server")
		return nil, err
	}
	logrus.Info("🛰️ [Tailscale] tsnet server started successfully")
	return s.server, nil
}

func (s *TailscaleService) localClient() (*tsnetLocalClient, error) {
	srv, err := s.serverInstance()
	if err != nil { return nil, err }
	lc, err := srv.LocalClient()
	if err != nil { return nil, err }
	return &tsnetLocalClient{Client: lc}, nil
}

type tsnetLocalClient struct { *local.Client }

func (s *TailscaleService) handleUserLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "https://login.tailscale.com") {
		for _, p := range strings.Fields(msg) { if strings.HasPrefix(p, "https://") { s.setLatestAuthURL(p) } }
	}
	logrus.Infof("📡 [Tailscale/User] %s", msg)
}

func (s *TailscaleService) setLatestAuthURL(u string) {
	s.mu.Lock()
	alreadyHave := s.latestAuthURL == u
	s.latestAuthURL = u
	s.mu.Unlock()

	if !alreadyHave && u != "" {
		logrus.Infof("🔗 [Tailscale] New AuthURL: %s", u)
		if runtime.GOOS == "android" {
			logrus.Info("🌐 [Tailscale/Android] Attempting to open AuthURL automatically")
			opened, err := openAndroidExternalUrl(u)
			if err != nil {
				logrus.WithError(err).Error("❌ [Tailscale/Android] Failed to open AuthURL via JNI")
			} else if opened {
				logrus.Info("✅ [Tailscale/Android] AuthURL opened successfully")
			}
		}
	}
}

func (s *TailscaleService) getLatestAuthURL() string {
	s.mu.Lock(); defer s.mu.Unlock(); return s.latestAuthURL
}

func tailscaleStateDir(appName string) string {
	if runtime.GOOS == "android" {
		dir, err := getAndroidFilesDir()
		if err == nil && dir != "" {
			return filepath.Join(dir, appName, "tailscale")
		}
		logrus.Warnf("tailscale android: getAndroidFilesDir() failed or empty, using fallback: %v", err)
	}
	base, _ := os.UserConfigDir()
	return filepath.Join(base, appName, "tailscale")
}
func firstAddr(values []netip.Addr) string {
	for _, v := range values { if v.Is4() { return v.String() } }
	return ""
}

func trimDotSuffix(v string) string { return strings.TrimSuffix(v, ".") }

func userLogin(st *ipnstate.Status, id tailcfg.UserID) string {
	if st == nil || st.User == nil { return "" }
	if u, ok := st.User[id]; ok { return u.LoginName }
	return ""
}

func (s *TailscaleService) GetSystemTailscaleIP() string {
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
				if ip != nil && ip[0] == 100 { return ip.String() }
			}
		}
	}
	return ""
}

func (s *TailscaleService) runVideoUDPRelay(ctx context.Context, pc net.PacketConn, localConn *net.UDPConn, port int, tailAddr string) {
	s.mu.Lock()
	traceID := s.videoRelayTraceID
	s.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 [%s] PANIC in runVideoUDPRelay: %v", traceID, r)
		}
		pc.Close()
		localConn.Close()
		logrus.Infof("📡 [%s] Relay stopped for %s", traceID, tailAddr)
	}()

	const bufferSize = 8 * 1024 * 1024
	if s_pc, ok := pc.(interface{ SetReadBuffer(int) error }); ok {
		_ = s_pc.SetReadBuffer(bufferSize)
	}
	_ = localConn.SetWriteBuffer(bufferSize)

	buf := make([]byte, 65536)
	packetCount := uint64(0)
	lastReportCount := uint64(0)
	lastReportTime := time.Now()
	lastPacketTime := time.Now()

	logrus.Infof("📡 [%s] Relay started: %s -> 127.0.0.1:%d", traceID, tailAddr, port)

	for {
		select {
		case <-ctx.Done():
			logrus.Infof("📡 [%s] Relay context cancelled, total packets: %d", traceID, packetCount)
			return
		default:
			_ = pc.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
			n, remoteAddr, err := pc.ReadFrom(buf)
			
			if err != nil {
				if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
					if time.Since(lastPacketTime) > 2*time.Second {
						logrus.Warnf("📡 [%s] Relay SILENCE: no packets for %.1fs from %s", traceID, time.Since(lastPacketTime).Seconds(), tailAddr)
						lastPacketTime = time.Now().Add(-1500 * time.Millisecond) // Log every 0.5s of silence after first 2s
					}
					continue
				}
				logrus.Errorf("📡 [%s] Relay read error: %v", traceID, err)
				return
			}

			if n > 0 {
				now := time.Now()
				if packetCount == 0 {
					logrus.Infof("📡 [%s] FIRST PACKET received at relay from %v (%d bytes)", traceID, remoteAddr, n)
				}
				
				packetCount++
				s.mu.Lock()
				s.videoRelayPackets = packetCount
				s.videoRelayLastAt = now
				s.videoRelayLastFrom = remoteAddr.String()
				s.mu.Unlock()
				
				if time.Since(lastPacketTime) > 1*time.Second && packetCount > 1 {
					logrus.Infof("📡 [%s] Relay RESUMED: packets started flowing again after %.1fs", traceID, time.Since(lastPacketTime).Seconds())
				}
				lastPacketTime = now

				if time.Since(lastReportTime) > 5*time.Second {
					diff := packetCount - lastReportCount
					logrus.Infof("📡 [%s] Relay status: %d pkts in last 5s (total: %d), last from %v", traceID, diff, packetCount, remoteAddr)
					lastReportCount = packetCount
					lastReportTime = now
				}

				_, err = localConn.Write(buf[:n])
				if err != nil {
					logrus.Warnf("⚠️ [%s] Relay local write error: %v", traceID, err)
				}
			}
		}
	}
}

const (
	videoRelayProbePayload = "USBRIDGE_VIDEO_RELAY_PROBE_V1"
	videoRelayAckPayload   = "USBRIDGE_VIDEO_RELAY_ACK_V1"
)

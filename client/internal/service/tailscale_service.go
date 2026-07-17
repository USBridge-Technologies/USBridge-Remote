package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
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

// TailscaleService always runs Tailscale in userspace (tsnet) mode — a Go-only
// WireGuard/netstack embedded in the app, no kernel TUN or system daemon
// required on any platform.
type TailscaleService struct {
	mu               sync.Mutex
	server           *tsnet.Server
	latestAuthURL    string
	authURLHandler   func(string)
	lastBackendState string
}

func NewTailscaleService() *TailscaleService {
	return &TailscaleService{}
}

func (s *TailscaleService) Start(ctx context.Context) error {
	_, err := s.serverInstance()
	return err
}

func (s *TailscaleService) Stop() error {
	s.mu.Lock()
	server := s.server
	s.server = nil
	s.mu.Unlock()

	if server != nil {
		logrus.Info("🛰️ [Tailscale] Stopping tsnet server...")
		// Attempt a clean logout before closing so the coordination server
		// immediately deregisters this node instead of waiting for a timeout.
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
	state, err := lc.Status(ctx)
	if err != nil || state == nil {
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
	refreshAndroidDefaultRouteInterface()
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

func (s *TailscaleService) Logout(ctx context.Context) error {
	lc, err := s.localClient()
	if err != nil {
		return err
	}
	return lc.Logout(ctx)
}

func (s *TailscaleService) HTTPClient() (*http.Client, error) {
	srv, err := s.serverInstance()
	if err != nil {
		return nil, err
	}

	client := srv.HTTPClient()
	if t, ok := client.Transport.(*http.Transport); ok {
		t.Proxy = http.ProxyURL(nil)
		t.TLSNextProto = make(map[string]func(authority string, c *tls.Conn) http.RoundTripper)
	}
	return client, nil
}

// WarmUpPeer proactively dials a Tailscale peer via tsnet to trigger the
// WireGuard handshake before Moonlight tries to use it for HTTP.
func (s *TailscaleService) WarmUpPeer(tailscaleIP string) {
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
// the context expires.
func (s *TailscaleService) WaitUntilReady(ctx context.Context) error {
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

	lc, err := s.localClient()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	st, err := lc.Status(ctx)
	if err != nil || st.Self == nil {
		return "", fmt.Errorf("status failed")
	}
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

// VideoRelayDebugInfo returns a human-readable string describing the tsnet
// peer path for bridgeHost, for diagnostic logging when no video frames arrive.
func (s *TailscaleService) VideoRelayDebugInfo(bridgeHost string) string {
	if bridgeHost == "" {
		return "tsnet"
	}
	cleanBridge := strings.Split(bridgeHost, ":")[0]

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if st, err := s.Status(ctx); err == nil && st != nil {
		for _, peer := range st.Peers {
			if peer.IP4 == cleanBridge || strings.Contains(peer.DNSName, cleanBridge) || peer.HostName == cleanBridge {
				if peer.CurAddr != "" {
					return fmt.Sprintf("tsnet: direct via %s", peer.CurAddr)
				} else if peer.Relay != "" {
					return fmt.Sprintf("tsnet: DERP via %s", peer.Relay)
				}
				return "tsnet: routing unknown"
			}
		}
	}

	return "tsnet"
}

// GetPeerDirectIP returns the non-Tailscale (LAN/direct) IP of a peer when
// both nodes are on the same local network, as seen via the tsnet peer list.
// Returns "" when the peer is only reachable via DERP relay (different networks).
// Used so C-level sockets (which bypass tsnet's netstack) can reach the peer
// directly over the LAN instead of needing a tsnet proxy.
func (s *TailscaleService) GetPeerDirectIP(tailscaleIP string) string {
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

func (s *TailscaleService) serverInstance() (*tsnet.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.server != nil {
		return s.server, nil
	}

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
		// Logf is required for tsnet's own internal diagnostics (netstack,
		// magicsock, wgengine, and — critically — the compiled ACL packet
		// filter's accept/drop decisions on outbound dials). Without it,
		// an ACL denying a specific destination port is indistinguishable
		// from any other "connection refused" in the app's own logs.
		Logf: s.handleInternalLogf,
		// Ephemeral=true: the node is automatically removed from the Tailscale
		// coordination server when it disconnects. This prevents zombie "offline"
		// entries from accumulating in the peer list and causing repeated failed
		// WireGuard handshake retries. Auth credentials are preserved in the
		// state directory and re-used on the next connection.
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
	if err != nil {
		return nil, err
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return nil, err
	}
	return &tsnetLocalClient{Client: lc}, nil
}

type tsnetLocalClient struct{ *local.Client }

func (s *TailscaleService) handleUserLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "https://login.tailscale.com") {
		for _, p := range strings.Fields(msg) {
			if strings.HasPrefix(p, "https://") {
				s.setLatestAuthURL(p)
			}
		}
	}
	logrus.Infof("📡 [Tailscale/User] %s", msg)
}

// SetAuthURLHandler registers a callback invoked whenever tsnet produces a new
// interactive-login AuthURL, regardless of which caller (the explicit Sign-In
// button, or an incidental first touch of the server by WarmUpPeer/
// WaitUntilReady/HTTPClient/TailnetIPv4) triggered it. tsnet only auto-starts
// an interactive login once, on the very first Start() of the server's
// lifetime — whichever of those callers happens to get there first "wins" and
// silently owns that one attempt. Centralizing the browser-open here, keyed
// purely off "a genuinely new URL appeared," is what makes the login surface
// reliably instead of depending on which caller's own polling loop (if any)
// happened to be watching at the right moment.
func (s *TailscaleService) SetAuthURLHandler(fn func(string)) {
	s.mu.Lock()
	s.authURLHandler = fn
	s.mu.Unlock()
}

func (s *TailscaleService) setLatestAuthURL(u string) {
	s.mu.Lock()
	alreadyHave := s.latestAuthURL == u
	s.latestAuthURL = u
	handler := s.authURLHandler
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
		if handler != nil {
			handler(u)
		}
	}
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
	for _, v := range values {
		if v.Is4() {
			return v.String()
		}
	}
	return ""
}

func trimDotSuffix(v string) string { return strings.TrimSuffix(v, ".") }

func userLogin(st *ipnstate.Status, id tailcfg.UserID) string {
	if st == nil || st.User == nil {
		return ""
	}
	if u, ok := st.User[id]; ok {
		return u.LoginName
	}
	return ""
}

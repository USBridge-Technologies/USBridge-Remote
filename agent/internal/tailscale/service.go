package tailscale

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"
)

type Status struct {
	Running  bool
	LoggedIn bool
	Backend  string
	AuthURL  string
	Self     Peer
	Peers    []Peer
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

// Service always runs Tailscale in userspace (tsnet) mode — a Go-only
// WireGuard/netstack embedded in the agent, no kernel TUN or system daemon
// required on any platform.
type Service struct {
	mu             sync.Mutex
	server         *tsnet.Server
	stateDir       string
	authURLHandler func(string)
	ctx            context.Context
	cancel         context.CancelFunc
}

func New(stateDir string) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Service{
		stateDir: stateDir,
		ctx:      ctx,
		cancel:   cancel,
	}
	go s.monitorLoop()
	return s
}

// SetAuthURLHandler registers a callback invoked every time tsnet reports a
// pending interactive-login AuthURL, regardless of which caller (local UI, a
// remote client's sync/register request, or tsnet's own first-boot
// auto-login) triggered it — and repeatedly for as long as the node stays in
// NeedsLogin, since tsnet's printAuthURLLoop re-announces the *same* URL every
// 5s (see tsnet.Server.printAuthURLLoop) regardless of whether anyone asked
// for it. Not deduplicated by URL value: an earlier attempt deduped here, but
// that meant the very first automatic announcement (which usually happens
// before any UI button click, or before this handler is even registered)
// permanently consumed the "new URL" signal, so a later genuine button click
// would call StartLogin and then wait forever for a callback that was never
// coming — tsnet only reuses the cached AuthURL and never re-logs it via
// UserLogf outside this loop. Callers that need "open the browser at most
// once per click" semantics (e.g. the UI's Sign In button) must gate that
// themselves, e.g. with a CompareAndSwap flag set right before calling
// StartLogin.
func (s *Service) SetAuthURLHandler(fn func(string)) {
	s.mu.Lock()
	s.authURLHandler = fn
	s.mu.Unlock()
}

func (s *Service) monitorLoop() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	var lastState string
	var lastAuthURL string
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

			// Backstop AuthURL broadcast, independent of tsnet's own
			// printAuthURLLoop (which SetAuthURLHandler's other caller,
			// handleUserLogf, depends on). That loop is started once per
			// tsnet.Server and exits for good the moment login first succeeds,
			// so it never fires again for a later re-login within the same
			// process (see StartLogin's doc comment). Polling Status here
			// instead catches every AuthURL regardless of that loop's state,
			// at the cost of up to one ticker interval of latency versus a
			// caller that can poll faster itself (e.g. StartLogin).
			if url := strings.TrimSpace(status.AuthURL); url != "" && url != lastAuthURL {
				lastAuthURL = url
				s.setLatestAuthURL(url)
			}

			if !status.Running {
				continue
			}

			currentPeers := make(map[string]string)
			for _, p := range status.Peers {
				if !p.Online {
					continue
				}

				// Relay is the peer's home DERP region and stays set even once a
				// direct path is established — CurAddr (the endpoint currently in
				// use) is what actually tells direct from relayed, matching how
				// upstream `tailscale status` classifies it.
				isRelayed := p.Relay != "" && p.CurAddr == ""
				connType := "Direct"
				if isRelayed {
					connType = fmt.Sprintf("Relay (%s)", p.Relay)
				}

				// Identify connection change for ACTIVE peers
				if p.Active {
					prevType, exists := lastPeers[p.IP4]
					if !exists || prevType != connType {
						if isRelayed {
							logrus.Warnf("⚠️ [Tailscale] Connection to %s (%s) is via RELAY (%s). NAT traversal failed or is in progress.", p.HostName, p.IP4, p.Relay)
						} else {
							logrus.Infof("🎯 [Tailscale] Connection to %s (%s) is DIRECT (NAT punch successful!)", p.HostName, p.IP4)
						}
					}
				}
				currentPeers[p.IP4] = connType
			}
			lastPeers = currentPeers
		}
	}
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
	lc, err := s.localClient()
	if err != nil {
		return &Status{Running: false, Backend: "Initializing"}, nil
	}

	state, err := lc.Status(ctx)
	if err != nil {
		return &Status{Running: false, Backend: "Not Running"}, nil
	}

	out := &Status{
		Running:  strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn: state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:  strings.TrimSpace(state.BackendState),
		AuthURL:  strings.TrimSpace(state.AuthURL),
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

// StartLogin triggers (or nudges) an interactive login and waits briefly for
// the resulting AuthURL by polling status every 500ms for up to 30s. Callers
// driven by a direct user action (e.g. a UI button) should open the browser
// from this return value themselves — it doesn't depend on tsnet's
// printAuthURLLoop (see SetAuthURLHandler's doc comment), so unlike that
// handler it keeps working across repeated Sign Out + Sign In cycles within
// the same process. AuthURLHandler still needs to stay registered separately
// for triggers that never call StartLogin (first boot, a remote client's
// sync/register request) and so have no return value to open from.
func (s *Service) StartLogin(ctx context.Context) (string, error) {
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

// Register authorizes this node on the tailnet. With a non-empty authKey it
// registers unattended (no browser approval needed) — used when the sync
// payload carries a pre-issued auth key. With an empty authKey it behaves
// like StartLogin, triggering an interactive login and returning the AuthURL
// for the caller (a remote client) to open in a browser.
//
// When the node is already logged in and Running, an empty-authKey call is a
// no-op that just returns the current status instead of touching login state
// at all. Without this short-circuit, /api/auth/sync's TailscaleRegister path
// (see api/sync.go) calls this on every client reconnect regardless of
// whether Tailscale is already up -- and lc.Start + StartLoginInteractive on
// an already-Running backend isn't a harmless no-op on tsnet's side: it can
// interrupt the backend's in-flight control-plane cycle ("context canceled"
// fetching the control key) and, once in that state, tsnet decides the node
// needs to re-register from scratch and generates a *brand new node key* --
// confirmed live: RegisterReq logging a fresh node key mid-session, followed
// by the connected client's WireGuard endpoint spending the next couple
// minutes silently retrying handshakes against the now-stale old key
// ("Handshake did not complete after 5 seconds, retrying") because nothing
// told it the peer's identity just changed underneath it. From the outside
// that looks exactly like the agent hanging or losing connectivity after a
// few connect/disconnect cycles, even though the process itself is fine.
// An explicit authKey still always goes through Start (a caller passing one
// means they intend an unattended re-register, e.g. a fresh auth key from
// the sync payload), and this only short-circuits the no-authKey path.
func (s *Service) Register(ctx context.Context, authKey, hostname string) (*Status, error) {
	srv, err := s.Server()
	if err != nil {
		return nil, err
	}
	hostname = strings.TrimSpace(hostname)
	if hostname != "" {
		srv.Hostname = hostname
	}
	authKey = strings.TrimSpace(authKey)

	if status, err := s.Status(ctx); err == nil && status != nil && skipRegister(authKey, status) {
		return status, nil
	}

	lc, err := srv.LocalClient()
	if err != nil {
		return nil, err
	}

	opts := ipn.Options{AuthKey: authKey}
	if hostname != "" {
		opts.UpdatePrefs = &ipn.Prefs{Hostname: hostname, WantRunning: true}
	}
	if err := lc.Start(ctx, opts); err != nil {
		return nil, fmt.Errorf("tsnet start: %w", err)
	}

	if authKey == "" {
		// No auth key: fall back to interactive login so the caller gets an AuthURL.
		if _, err := s.StartLogin(ctx); err != nil {
			logrus.Warnf("⚠️ [Tailscale] register: interactive login failed: %v", err)
		}
	}

	return s.Status(ctx)
}

// skipRegister reports whether Register can skip touching login state
// entirely and just hand back the current status. Only applies to the
// no-authKey (interactive) path: an explicit authKey is the caller
// deliberately asking for an unattended re-register (e.g. a fresh key from
// the sync payload), which must always go through regardless of current
// state. See Register's doc comment for why re-running Start/StartLogin on
// an already-Running node is unsafe rather than merely redundant.
func skipRegister(authKey string, status *Status) bool {
	return authKey == "" && status.LoggedIn && status.Running
}

func (s *Service) Logout(ctx context.Context) error {
	lc, err := s.localClient()
	if err != nil {
		return err
	}
	return lc.Logout(ctx)
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

	// Not ephemeral: an ephemeral node is removed from the tailnet by the
	// control server shortly after it goes offline, so on the next launch
	// there is no identity left to resume — tsnet has to register as a brand
	// new device (fresh node key, fresh 100.x IP) and re-prompt for login.
	// Keeping the node persistent lets it resume the same identity/IP from
	// the state stored in Dir across restarts without any user action.
	s.server = &tsnet.Server{
		Dir:      stateDir,
		Hostname: "usbridge-agent",
		UserLogf: s.handleUserLogf,
		// Logf is required for tsnet's own internal diagnostics (notably
		// netstack's forward-to-localhost path for unclaimed ports, e.g. the
		// Sunshine streaming ports) — netstack.Create's logger is a no-op
		// whenever Logf is nil, so forwarding failures were being silently
		// dropped instead of explaining why a Moonlight client got connection
		// refused reaching Sunshine over Tailscale.
		Logf: s.handleInternalLogf,
		// Ephemeral must stay false here: the agent is the permanently-dialable
		// server side of the connection, not a throwaway client. An ephemeral
		// node gets deleted from the tailnet by the coordination server once it
		// spends a while without an active control connection (laptop sleep,
		// wifi roam, brief network drop) — after that the agent silently drops
		// out of the tailnet and no client can reach it again without a fresh
		// interactive re-auth, even though the process is still running. This
		// matches the "works at first, then stops connecting after a while"
		// reports — do not copy the client's Ephemeral:true here.
	}

	if err := s.server.Start(); err != nil {
		return nil, err
	}

	return s.server, nil
}

func (s *Service) handleUserLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if url := s.extractURL(msg); url != "" {
		s.setLatestAuthURL(url)
	}
	logrus.Infof("📡 [Tailscale/User] %s", msg)
}

// handleInternalLogf receives tsnet's own internal diagnostics (netstack,
// magicsock, wgengine, ...) — notably netstack's forward-to-localhost path
// logs its dial errors only here, which is why a Sunshine streaming port
// being unreachable over Tailscale previously left no trace in app.log at
// all.
func (s *Service) handleInternalLogf(format string, args ...any) {
	logrus.Infof("🛰️ [Tailscale/Internal] %s", fmt.Sprintf(format, args...))
}

func (s *Service) extractURL(text string) string {
	if strings.Contains(text, "https://login.tailscale.com") {
		for _, w := range strings.Fields(text) {
			if strings.HasPrefix(w, "https://") {
				return w
			}
		}
	}
	return ""
}

func (s *Service) setLatestAuthURL(u string) {
	if u == "" {
		return
	}
	s.mu.Lock()
	handler := s.authURLHandler
	s.mu.Unlock()

	if handler != nil {
		handler(u)
	}
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

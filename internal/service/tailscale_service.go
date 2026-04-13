package service

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
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
}

type TailscaleService struct {
	mu                 sync.Mutex
	server             *tsnet.Server
	latestAuthURL      string
	videoRelayConn     net.PacketConn
	videoRelayCancel   context.CancelFunc
	videoRelayPort     int
	videoRelayTailAddr string
	videoRelayTraceID  string
	videoRelayPackets  uint64
	videoRelayLastFrom string
	videoRelayLastAt   time.Time
}

func NewTailscaleService() *TailscaleService {
	return &TailscaleService{}
}

func (s *TailscaleService) Status(ctx context.Context) (*TailscaleStatus, error) {
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("tailscale"); err == nil {
			cmd := exec.Command("tailscale", "status", "--json")
			if out, err := cmd.Output(); err == nil {
				ip := s.GetSystemTailscaleIP()
				if ip != "" {
					return &TailscaleStatus{
						Running:   true,
						LoggedIn:  true,
						Backend:   "Running (System)",
						Userspace: false,
						Self: TailscalePeer{
							IP4: ip,
							OS:  "linux",
						},
					}, nil
				}
				statusStr := string(out)
				if strings.Contains(statusStr, "LoggedOut") || strings.Contains(statusStr, "NeedsLogin") {
					return &TailscaleStatus{
						Running:  true,
						LoggedIn: false,
						Backend:  "NeedsLogin (System)",
					}, nil
				}
			}
		}
	}

	refreshAndroidDefaultRouteInterface()
	lc, err := s.localClient()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx, _ = context.WithTimeout(context.Background(), 5*time.Second)
	}

	state, err := lc.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("tailscale status: %w", err)
	}

	out := &TailscaleStatus{
		Running:   strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn:  state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:   strings.TrimSpace(state.BackendState),
		Userspace: !state.TUN,
	}
	if state.Self != nil {
		out.Self = TailscalePeer{
			ID:        string(state.Self.ID),
			HostName:  strings.TrimSpace(state.Self.HostName),
			DNSName:   trimDotSuffix(state.Self.DNSName),
			OS:        strings.TrimSpace(state.Self.OS),
			Online:    state.Self.Online,
			Active:    state.Self.Active,
			IP4:       firstAddr(state.Self.TailscaleIPs),
			UserLogin: userLogin(state, state.Self.UserID),
		}
	}
	for _, key := range state.Peers() {
		peer := state.Peer[key]
		if peer == nil { continue }
		out.Peers = append(out.Peers, TailscalePeer{
			ID:        string(peer.ID),
			HostName:  strings.TrimSpace(peer.HostName),
			DNSName:   trimDotSuffix(peer.DNSName),
			OS:        strings.TrimSpace(peer.OS),
			Online:    peer.Online,
			Active:    peer.Active,
			IP4:       firstAddr(peer.TailscaleIPs),
			UserLogin: userLogin(state, peer.UserID),
		})
	}
	return out, nil
}

func (s *TailscaleService) StartLogin(ctx context.Context) (string, error) {
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("tailscale"); err == nil {
			logrus.Info("🚀 [Tailscale] Starting elevated login process")
			
			cmd := exec.Command("pkexec", "tailscale", "up", "--accept-dns=false")
			stdout, _ := cmd.StdoutPipe()
			stderr, _ := cmd.StderrPipe()
			
			if err := cmd.Start(); err == nil {
				urlChan := make(chan string, 1)
				scanFunc := func(r io.Reader) {
					scanner := bufio.NewScanner(r)
					for scanner.Scan() {
						line := scanner.Text()
						logrus.Infof("📡 [Tailscale/CLI] %s", line)
						if strings.Contains(line, "https://login.tailscale.com") {
							words := strings.Fields(line)
							for _, w := range words {
								if strings.HasPrefix(w, "https://") {
									urlChan <- w
									return
								}
							}
						}
					}
				}
				go scanFunc(stdout)
				go scanFunc(stderr)

				select {
				case foundURL := <-urlChan:
					logrus.Infof("🔗 [Tailscale] Captured system AuthURL: %s", foundURL)
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
	
	if err := lc.StartLoginInteractive(ctx); err != nil {
		logrus.WithError(err).Error("tailscale client: StartLoginInteractive failed")
	}

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
	if runtime.GOOS == "linux" {
		if _, err := exec.LookPath("tailscale"); err == nil {
			logrus.Info("🛑 [Tailscale] System CLI logout")
			_ = exec.Command("pkexec", "tailscale", "logout").Run()
			return nil
		}
	}
	lc, err := s.localClient()
	if err != nil { return err }
	return lc.Logout(ctx)
}

func (s *TailscaleService) HTTPClient() (*http.Client, error) {
	if runtime.GOOS == "linux" {
		if s.GetSystemTailscaleIP() != "" {
			logrus.Debug("🌐 [Tailscale] Using system HTTP client")
			return &http.Client{
				Timeout: 10 * time.Second,
			}, nil
		}
	}
	srv, err := s.serverInstance()
	if err != nil { return nil, err }
	return srv.HTTPClient(), nil
}

func (s *TailscaleService) ValidateAddress(raw string) error { return nil }

func (s *TailscaleService) TailnetIPv4(ctx context.Context) (string, error) {
	if runtime.GOOS == "linux" {
		if ip := s.GetSystemTailscaleIP(); ip != "" { return ip, nil }
	}
	lc, err := s.localClient()
	if err != nil { return "", err }
	st, err := lc.Status(ctx)
	if err != nil || st.Self == nil { return "", fmt.Errorf("status failed") }
	return firstAddr(st.Self.TailscaleIPs), nil
}

func (s *TailscaleService) EnsureVideoUDPRelay(port int) error {
	if port <= 0 { return fmt.Errorf("invalid port") }
	if runtime.GOOS == "linux" && s.GetSystemTailscaleIP() != "" { return nil }

	srv, err := s.serverInstance()
	if err != nil { return err }
	tailIP, err := s.TailnetIPv4(nil)
	if err != nil { return err }
	tailAddr := net.JoinHostPort(tailIP, fmt.Sprintf("%d", port))

	pc, err := srv.ListenPacket("udp", tailAddr)
	if err != nil { return err }
	
	localTarget, _ := net.ResolveUDPAddr("udp4", "127.0.0.1:"+fmt.Sprintf("%d", port))
	localConn, _ := net.DialUDP("udp4", nil, localTarget)

	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.videoRelayConn = pc
	s.videoRelayCancel = cancel
	s.mu.Unlock()

	go s.runVideoUDPRelay(ctx, pc, localConn, port, tailAddr)
	return nil
}

func (s *TailscaleService) SetVideoRelayTraceID(traceID string) {
	s.mu.Lock(); defer s.mu.Unlock(); s.videoRelayTraceID = traceID
}

func (s *TailscaleService) VideoRelayDebugInfo() string { return "relay-active" }

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
	addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", systemIP, port))
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil { return err }
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 1024)
	n, remoteAddr, err := conn.ReadFrom(buf)
	if err != nil { return err }
	if strings.Contains(string(buf[:n]), "RELAY_PROBE") {
		_, _ = conn.WriteTo([]byte(videoRelayAckPayload), remoteAddr)
		return nil
	}
	return fmt.Errorf("unexpected packet")
}

func (s *TailscaleService) GetBindHost() string { return "0.0.0.0" }

func (s *TailscaleService) serverInstance() (*tsnet.Server, error) {
	s.mu.Lock(); defer s.mu.Unlock()
	if s.server != nil { return s.server, nil }
	s.server = &tsnet.Server{
		Dir: tailscaleStateDir("usbridge-client"),
		Hostname: "usbridge-client",
		UserLogf: s.handleUserLogf,
	}
	if err := s.server.Start(); err != nil { return nil, err }
	return s.server, nil
}

func (s *TailscaleService) localClient() (*tsnetLocalClient, error) {
	srv, err := s.serverInstance()
	if err != nil { return nil, err }
	lc, err := srv.LocalClient()
	return &tsnetLocalClient{Client: lc}, nil
}

type tsnetLocalClient struct { *local.Client }

func (s *TailscaleService) handleUserLogf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if strings.Contains(msg, "https://login.tailscale.com") {
		parts := strings.Fields(msg)
		for _, p := range parts { if strings.HasPrefix(p, "https://") { s.setLatestAuthURL(p) } }
	}
}

func (s *TailscaleService) setLatestAuthURL(u string) {
	s.mu.Lock(); defer s.mu.Unlock(); s.latestAuthURL = u
}

func (s *TailscaleService) getLatestAuthURL() string {
	s.mu.Lock(); defer s.mu.Unlock(); return s.latestAuthURL
}

func tailscaleStateDir(appName string) string {
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
	const bufferSize = 2 * 1024 * 1024
	if s_pc, ok := pc.(interface{ SetReadBuffer(int) error }); ok { _ = s_pc.SetReadBuffer(bufferSize) }
	_ = localConn.SetWriteBuffer(bufferSize)
	packetCh := make(chan []byte, 4096)
	go func() {
		buf := make([]byte, 2048)
		for {
			n, _, err := pc.ReadFrom(buf)
			if err != nil { return }
			if n > 0 {
				pkt := make([]byte, n)
				copy(pkt, buf[:n])
				select { case packetCh <- pkt: default: }
			}
		}
	}()
	for {
		select {
		case <-ctx.Done(): return
		case b := <-packetCh: _, _ = localConn.Write(b)
		}
	}
}

const (
	videoRelayProbePayload = "USBRIDGE_VIDEO_RELAY_PROBE_V1"
	videoRelayAckPayload   = "USBRIDGE_VIDEO_RELAY_ACK_V1"
)

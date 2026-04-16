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

func getTailscaleBinaryPath() string {
	name := "tailscale"
	if runtime.GOOS == "windows" {
		name = "tailscale.exe"
	}
	if runtime.GOOS == "android" {
		// On Android, we look in the app's native library directory
		// The binary is bundled as libtailscale.so to be extracted by Android
		libDir, err := getAndroidNativeLibraryDir()
		if err == nil && libDir != "" {
			localPath := filepath.Join(libDir, "libtailscale.so")
			if _, err := os.Stat(localPath); err == nil {
				return localPath
			}
		}
	}
	if exePath, err := os.Executable(); err == nil {
		localPath := filepath.Join(filepath.Dir(exePath), name)
		if _, err := os.Stat(localPath); err == nil {
			return localPath
		}
	}
	if path, err := exec.LookPath(name); err == nil {
		return path
	}
	if runtime.GOOS == "darwin" {
		macosPath := "/Applications/Tailscale.app/Contents/MacOS/Tailscale"
		if _, err := os.Stat(macosPath); err == nil {
			return macosPath
		}
	}
	return ""
}

func (s *TailscaleService) Status(ctx context.Context) (status *TailscaleStatus, err error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in TailscaleService.Status: %v", r)
			err = fmt.Errorf("panic in tailscale status: %v", r)
		}
	}()

	var state *ipnstate.Status

	// 1. Пытаемся получить статус от системного Tailscale (System Stack)
	tsPath := getTailscaleBinaryPath()
	if tsPath != "" {
		cmd := exec.Command(tsPath, "status", "--json")
		maybeHideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			var st ipnstate.Status
			if err := json.Unmarshal(out, &st); err == nil {
				state = &st
				// Если системный Tailscale не залогинен, сбрасываем state и идем дальше
				if state.BackendState == "NeedsLogin" || state.BackendState == "LoggedOut" {
					state = nil
				}
			}
		}
	}

	// 2. Если системного нет или он не залогинен, пробуем userspace (tsnet)
	if state == nil {
		refreshAndroidDefaultRouteInterface()
		lc, err := s.localClient()
		if err != nil {
			return nil, err
		}
		if ctx == nil {
			ctx, _ = context.WithTimeout(context.Background(), 5*time.Second)
		}
		state, err = lc.Status(ctx)
		if err != nil {
			return nil, fmt.Errorf("tailscale status: %w", err)
		}
	}

	if state == nil {
		return &TailscaleStatus{Running: false}, nil
	}

	// 3. Конвертируем ipnstate.Status в наш внутренний формат
	out := &TailscaleStatus{
		Running:   strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn:  state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:   strings.TrimSpace(state.BackendState),
		Userspace: !state.TUN,
	}

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
		maybeHideWindow(cmd)
		
		stdout, _ := cmd.StdoutPipe()
		stderr, _ := cmd.StderrPipe()
		if err := cmd.Start(); err != nil {
			if runtime.GOOS == "darwin" {
				// Try with osascript if direct start failed
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
					logrus.Infof("📡 [Tailscale/CLI] %s", line)
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
				logrus.Infof("🔗 [Tailscale] Captured system AuthURL: %s", foundURL)
				return foundURL, nil
			case <-time.After(15 * time.Second):
				logrus.Warn("⚠️ [Tailscale] No link from system Tailscale in 15s")
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
	tsPath := getTailscaleBinaryPath()
	if tsPath != "" {
		logrus.Infof("🛑 [Tailscale] System CLI logout via %s", tsPath)
		if runtime.GOOS == "linux" {
			cmd := exec.Command("pkexec", tsPath, "logout")
			maybeHideWindow(cmd)
			_ = cmd.Run()
		} else {
			cmd := exec.Command(tsPath, "logout")
			maybeHideWindow(cmd)
			_ = cmd.Run()
		}
		return nil
	}
	lc, err := s.localClient()
	if err != nil { return err }
	return lc.Logout(ctx)
}

func (s *TailscaleService) HTTPClient() (*http.Client, error) {
	if s.GetSystemTailscaleIP() != "" {
		return &http.Client{Timeout: 10 * time.Second}, nil
	}
	srv, err := s.serverInstance()
	if err != nil { return nil, err }
	return srv.HTTPClient(), nil
}

func (s *TailscaleService) ValidateAddress(raw string) error { return nil }

func (s *TailscaleService) TailnetIPv4(ctx context.Context) (ip string, err error) {
	defer func() {
		if r := recover(); r != nil {
			logrus.Errorf("🔥 PANIC in TailscaleService.TailnetIPv4: %v", r)
			err = fmt.Errorf("panic in tailnet ipv4: %v", r)
		}
	}()
	if sysIP := s.GetSystemTailscaleIP(); sysIP != "" { return sysIP, nil }
	lc, err := s.localClient()
	if err != nil { return "", err }
	if ctx == nil { ctx = context.Background() }
	st, err := lc.Status(ctx)
	if err != nil || st.Self == nil { return "", fmt.Errorf("status failed") }
	return firstAddr(st.Self.TailscaleIPs), nil
}

func (s *TailscaleService) EnsureVideoUDPRelay(port int) (int, error) {
	if port <= 0 { port = 55000 }
	if s.GetSystemTailscaleIP() != "" { return port, nil }

	// Обязательно останавливаем старый релей перед запуском нового на том же или другом порту
	_ = s.StopVideoUDPRelay()

	srv, err := s.serverInstance()
	if err != nil { return 0, err }
	tailIP, err := s.TailnetIPv4(context.Background())
	if err != nil { return 0, err }

	// Пробуем занять основной порт, если не выходит — любой свободный
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

	// Локальная цель (GStreamer). Используем ТОТ ЖЕ порт на 127.0.0.1.
	// GStreamer должен слушать на 127.0.0.1:actualPort
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

func (s *TailscaleService) VideoRelayDebugInfo(targetHost string) string {
	s.mu.Lock()
	state := "inactive"
	if s.videoRelayConn != nil {
		state = "userspace-relay"
	} else if s.GetSystemTailscaleIP() != "" {
		state = "system-stack"
	}
	s.mu.Unlock()

	routingInfo := ""
	if targetHost != "" {
		// Убираем порт и нормализуем хост для поиска
		cleanTarget := strings.Split(targetHost, ":")[0]
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if st, err := s.Status(ctx); err == nil && st != nil {
			for _, peer := range st.Peers {
				if peer.IP4 == cleanTarget || strings.Contains(peer.DNSName, cleanTarget) || peer.HostName == cleanTarget {
					if peer.CurAddr != "" {
						routingInfo = fmt.Sprintf(" (Direct via %s)", peer.CurAddr)
					} else if peer.Relay != "" {
						routingInfo = fmt.Sprintf(" (DERP via %s)", peer.Relay)
					} else {
						routingInfo = " (routing unknown)"
					}
					break
				}
			}
		}
	}

	return state + routingInfo
}

func (s *TailscaleService) PunchVideoHole(targetHost string, port int) {
	s.mu.Lock()
	pc := s.videoRelayConn
	traceID := s.videoRelayTraceID
	s.mu.Unlock()

	host := strings.Split(targetHost, ":")[0]

	// Определяем функцию для отправки PUNCH-пакета
	var puncher func(addr *net.UDPAddr)
	if pc != nil {
		// Режим userspace (tsnet relay)
		puncher = func(addr *net.UDPAddr) {
			_, _ = pc.WriteTo([]byte("PUNCH"), addr)
		}
	} else if s.GetSystemTailscaleIP() != "" {
		// Режим системного стека (system tailscaled)
		s.pingSystemTailscale(host)

		// Создаем временный UDP сокет для "пробивки"
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

	// 1. Сначала пробиваем на основной Target (Tailscale IP или DNS)
	addr, err := net.ResolveUDPAddr("udp4", net.JoinHostPort(host, strconv.Itoa(port)))
	if err == nil {
		logrus.Infof("🎯 [%s] Sending PUNCH to target %v...", traceID, addr)
		puncher(addr)
	}

	// 2. ДОПОЛНИТЕЛЬНО: Пробиваем на все известные эндпоинты этого пира через статус
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if st, err := s.Status(ctx); err == nil && st != nil {
		for _, peer := range st.Peers {
			if peer.IP4 == host || strings.Contains(peer.DNSName, host) || peer.HostName == host {
				// Пробиваем текущий адрес
				if peer.CurAddr != "" {
					if ra, err := net.ResolveUDPAddr("udp4", peer.CurAddr); err == nil {
						videoAddr := &net.UDPAddr{IP: ra.IP, Port: port}
						logrus.Infof("🎯 [%s] Sending PUNCH to current peer address %v...", traceID, videoAddr)
						puncher(videoAddr)
					}
				}
				// ПРОБИВАЕМ ВСЕ ИЗВЕСТНЫЕ ЭНДПОИНТЫ (критично для P2P в локальной сети при наличии нескольких NAT)
				for _, addrStr := range peer.Addrs {
					if addrStr == peer.CurAddr {
						continue
					}
					if ra, err := net.ResolveUDPAddr("udp4", addrStr); err == nil {
						// Мы пробуем достучаться до видео-порта на всех IP пира
						videoAddr := &net.UDPAddr{IP: ra.IP, Port: port}
						logrus.Infof("🎯 [%s] Sending PUNCH to potential peer endpoint %v...", traceID, videoAddr)
						puncher(videoAddr)
					}
				}
				break
			}
		}
	}
}

func (s *TailscaleService) pingSystemTailscale(host string) {
	tsPath := getTailscaleBinaryPath()
	if tsPath == "" {
		return
	}

	// Запускаем пинг в фоне, чтобы не блокировать UI.
	// Пинг заставляет системный tailscaled искать прямой путь (P2P).
	go func() {
		logrus.Infof("🎯 [Tailscale] Running system ping to %s to force P2P...", host)
		// Используем --timeout и --c для быстрого завершения
		cmd := exec.Command(tsPath, "ping", "--timeout=3s", "--c=3", host)
		maybeHideWindow(cmd)
		out, err := cmd.CombinedOutput()
		if err != nil {
			logrus.Warnf("⚠️ [Tailscale] System ping to %s failed: %v (out: %s)", host, err, string(out))
		} else {
			logrus.Infof("✅ [Tailscale] System ping to %s completed: %s", host, string(out))
		}
	}()
}

// PeerConnectionMode returns whether the connection to targetHost is direct P2P or via DERP relay.
// Returns "direct:<addr>", "derp:<relay>", or "unknown".
func (s *TailscaleService) PeerConnectionMode(targetHost string) string {
	if targetHost == "" {
		return "unknown"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	st, err := s.Status(ctx)
	if err != nil || st == nil {
		return "unknown"
	}
	for _, peer := range st.Peers {
		if peer.IP4 == targetHost || strings.HasPrefix(peer.DNSName, targetHost) {
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
	// Listen on all interfaces to be sure
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
	s.mu.Lock(); defer s.mu.Unlock()
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
		Ephemeral: true, // Не захламляем список устройств в консоли Tailscale
	}

	if runtime.GOOS == "android" {
		// Use unix socket for local API on Android
		// NOTE: LocalListenAddr is only available in newer Tailscale versions (v1.76+)
		// sockPath := filepath.Join(stateDir, "tailscaled.sock")
		// s.server.LocalListenAddr = "unix:" + sockPath
		// logrus.Infof("🛰️ [Tailscale] Local API socket: %s", sockPath)
		logrus.Infof("🛰️ [Tailscale] Android initialization (userspace)")
	} else if runtime.GOOS == "linux" {
		// On Linux we can also use a socket in the state dir if running as userspace
		// s.server.LocalListenAddr = "unix:" + filepath.Join(stateDir, "tailscaled.sock")
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

	// Используем один буфер для чтения и немедленной записи.
	// В UDP это максимально быстро (минимум контекст-свитчей).
	buf := make([]byte, 65536)
	packetCount := 0

	for {
		n, remoteAddr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}

		if n > 0 {
			packetCount++
			if packetCount <= 10 || packetCount%1000 == 0 {
				logrus.Infof("📡 [%s] Relay received packet #%d (%d bytes) from %v", traceID, packetCount, n, remoteAddr)
			}

			// Прямая пересылка на 127.0.0.1 (GStreamer)
			_, err = localConn.Write(buf[:n])
			if err != nil {
				logrus.Warnf("⚠️ [%s] Relay local write error: %v", traceID, err)
			}
		}
	}
}

const (
	videoRelayProbePayload = "USBRIDGE_VIDEO_RELAY_PROBE_V1"
	videoRelayAckPayload   = "USBRIDGE_VIDEO_RELAY_ACK_V1"
)

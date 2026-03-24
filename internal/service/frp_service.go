package service

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/fatedier/frp/client"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
)

// FRPService manages FRP client connection with QUIC support
type FRPService struct {
	service    *client.Service
	ctx        context.Context
	cancel     context.CancelFunc
	isRunning  bool
	serverAddr string
	serverPort int
	authToken  string
	httpPort   int // локальный порт HTTP visitor (клиент подключается сюда)
	videoPort  int // порт proxy video_sudp (GStreamer udpsrc)
}

// NewFRPService creates a new FRP service instance
func NewFRPService(serverAddr string, serverPort int, authToken string) *FRPService {
	return &FRPService{
		serverAddr: serverAddr,
		serverPort: serverPort,
		authToken:  authToken,
		isRunning:  false,
	}
}

// Connect establishes QUIC encrypted connection to FRP server and sets up tunnels
func (f *FRPService) Connect(httpPort, nbdPort, videoPort int) error {
	if f.isRunning {
		return fmt.Errorf("FRP service already running")
	}

	// Create context for FRP service
	f.ctx, f.cancel = context.WithCancel(context.Background())

	localHTTPPort, err := findAvailableLocalPort(httpPort)
	if err != nil {
		return fmt.Errorf("failed to allocate local HTTP visitor port: %w", err)
	}

	// Configure common client settings
	common := &v1.ClientCommonConfig{
		ServerAddr: f.serverAddr,
		ServerPort: f.serverPort,
		Auth: v1.AuthClientConfig{
			Method: "token",
			Token:  f.authToken,
		},
		Transport: v1.ClientTransportConfig{
			Protocol: "quic", // Use QUIC protocol for encrypted connection
			QUIC: v1.QUICOptions{
				KeepalivePeriod:    10,    // Keepalive каждые 10 с — агрессивно держим QUIC alive, сбрасываем idle-таймер
				MaxIdleTimeout:     86400, // 24 часа — NBD-сессии долгоживущие, QUIC не должен их рвать (ВАЖНО: frps тоже должен иметь >= 86400)
				MaxIncomingStreams: 100000,
			},
			TLS: v1.TLSClientConfig{
				Enable: lo.ToPtr(true),
			},
			DialServerTimeout:   10,
			DialServerKeepAlive: 7200,
			TCPMux:              lo.ToPtr(true),
			HeartbeatInterval:   3,  // Частые heartbeat — сервер иначе кикает каждые ~5 сек (tcpMuxKeepaliveInterval)
			HeartbeatTimeout:    10, // Время на ответ
		},
		LoginFailExit: lo.ToPtr(true),
	}

	// ═══════════════════════════════════════════════════════════════════════════
	// FRP STCP: Конфигурация должна совпадать с Bridge (frpc.yaml).
	// Bridge visitors: nbd_from_client1-16, serverName=nbd_srv1-16, secretKey=sk-nbd
	// Наши proxy: nbd_srv1-16, LocalPort=10809-10824, secretKey=sk-nbd
	// Цепочка: mountdrive->127.0.0.1:10810 (Bridge)->visitor nbd_from_client2->frps->наш proxy nbd_srv2->127.0.0.1:10810 (Mac)
	// ═══════════════════════════════════════════════════════════════════════════

	proxies := []v1.ProxyConfigurer{}

	// Создаем все 16 NBD proxy — имена и secretKey должны совпадать с Bridge visitors
	for i := 1; i <= 5; i++ {
		port := 10808 + i // 10809..10824
		proxyName := fmt.Sprintf("nbd_srv%d", i)

		nbdProxy := &v1.STCPProxyConfig{
			ProxyBaseConfig: v1.ProxyBaseConfig{
				Name: proxyName,
				Type: "stcp",
				ProxyBackend: v1.ProxyBackend{
					LocalIP:   "127.0.0.1",
					LocalPort: port,
				},
			},
			Secretkey: "sk-nbd", // Должен совпадать с Bridge visitor secretKey
		}
		proxies = append(proxies, nbdProxy)
	}
	logrus.Debugf("   📤 NBD proxies: nbd_srv1-16 -> 127.0.0.1:10809-10824 (secretKey=sk-nbd)")

	// Video SUDP: Bridge visitor serverName=video_sudp, secretKey=sk-video
	vp := videoPort
	if vp <= 0 {
		vp = models.DefaultVideoUDPPort
	}
	videoProxy := &v1.SUDPProxyConfig{
		ProxyBaseConfig: v1.ProxyBaseConfig{
			Name: "video_sudp",
			Type: "sudp",
			ProxyBackend: v1.ProxyBackend{
				LocalIP:   "127.0.0.1",
				LocalPort: vp,
			},
		},
		Secretkey: "sk-video",
	}
	proxies = append(proxies, videoProxy)
	logrus.Debugf("   📤 Video proxy: video_sudp -> 127.0.0.1:%d (secretKey=sk-video)", vp)

	// Тянем радксовый HTTP как локальный порт на клиенте
	visitors := []v1.VisitorConfigurer{}

	// HTTP visitor: клиент слушает 127.0.0.1:8080 -> QUIC -> сервер http_srv -> 127.0.0.1:8080
	httpVisitor := &v1.STCPVisitorConfig{
		VisitorBaseConfig: v1.VisitorBaseConfig{
			Name:       "http_from_server",
			Type:       "stcp",
			ServerName: "http_srv",  // Имя STCP прокси на сервере
			SecretKey:  "sk-http",   // Общий секрет
			BindAddr:   "127.0.0.1", // Клиент слушает локально
			BindPort:   localHTTPPort,
		},
	}
	visitors = append(visitors, httpVisitor)

	// Create FRP client service
	svc, err := client.NewService(client.ServiceOptions{
		Common:      common,
		ProxyCfgs:   proxies,
		VisitorCfgs: visitors,
	})
	if err != nil {
		return fmt.Errorf("failed to create FRP service: %v", err)
	}

	f.service = svc

	// Start FRP service in background
	errChan := make(chan error, 1)
	go func() {
		logrus.Info("🔐 Starting FRP QUIC encrypted tunnel...")
		if err := svc.Run(f.ctx); err != nil {
			logrus.Errorf("❌ FRP service error: %v", err)
			errChan <- err
		}
	}()

	// Wait for connection to establish or fail
	// Нужно дать FRP время на полную регистрацию прокси в frps (nbd_srv1-16, video_sudp)
	select {
	case err := <-errChan:
		return fmt.Errorf("FRP connection failed: %v", err)
	case <-time.After(2 * time.Second):
		// 2 секунды — базовое время на стабилизацию QUIC и регистрацию всех proxy в frps.
		// Но для WireGuard bootstrap этого мало: нужно дождаться, пока локальный visitor реально
		// начнет слушать localhost:httpPort, иначе первый POST упрется в connect refused.
		if err := waitForLocalListener("127.0.0.1", localHTTPPort, 5*time.Second); err != nil {
			if f.cancel != nil {
				f.cancel()
			}
			return fmt.Errorf("FRP HTTP visitor localhost:%d is not ready: %w", localHTTPPort, err)
		}

		f.isRunning = true
		f.httpPort = localHTTPPort
		logrus.Info("✅ FRP QUIC tunnel established successfully")
		f.videoPort = vp
		logrus.Debugf("   📥 HTTP API: localhost:%d -> QUIC -> %s:8080", f.httpPort, f.serverAddr)
		logrus.Debugf("   📤 Video UDP: localhost:%d (proxy) <- Bridge visitor -> QUIC -> %s", vp, f.serverAddr)
		logrus.Debugf("   📤 NBD Servers: localhost:10809-10824 -> QUIC -> Bridge visitors (16 портов) -> %s", f.serverAddr)
		logrus.Debugf("   💡 Приложение работает через localhost:%d", f.httpPort)
		return nil
	}
}

// Disconnect stops the FRP service
func (f *FRPService) Disconnect() error {
	if !f.isRunning {
		return nil
	}

	logrus.Info("🛑 Stopping FRP QUIC tunnel...")

	if f.cancel != nil {
		f.cancel()
	}

	f.isRunning = false
	f.service = nil
	f.httpPort = 0

	logrus.Info("✅ FRP tunnel disconnected")
	return nil
}

// IsRunning returns true if FRP service is running
func (f *FRPService) IsRunning() bool {
	return f.isRunning
}

// GetServerPorts returns the local ports exposed via FRP tunnel
// videoPort — порт proxy (GStreamer udpsrc слушает здесь, Bridge visitor шлёт сюда)
func (f *FRPService) GetServerPorts() (httpPort, videoPort, nbdPort int) {
	hp := 8080
	if f.httpPort > 0 {
		hp = f.httpPort
	}

	vp := models.DefaultVideoUDPPort
	if f.videoPort > 0 {
		vp = f.videoPort
	}
	return hp, vp, 10809
}

func findAvailableLocalPort(preferred int) (int, error) {
	if preferred > 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", preferred))
		if err == nil {
			_ = ln.Close()
			return preferred, nil
		}
		logrus.Warnf("⚠️ Локальный порт %d занят, выбираем свободный порт для FRP HTTP visitor", preferred)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	addr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected local addr type: %T", ln.Addr())
	}
	return addr.Port, nil
}

func waitForLocalListener(host string, port int, timeout time.Duration) error {
	address := fmt.Sprintf("%s:%d", host, port)
	deadline := time.Now().Add(timeout)
	var lastErr error

	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", address, 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("timeout")
	}
	return lastErr
}

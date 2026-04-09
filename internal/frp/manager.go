package frp

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/fatedier/frp/client"
	"github.com/fatedier/frp/pkg/config/source"
	v1 "github.com/fatedier/frp/pkg/config/v1"
	"github.com/fatedier/frp/server"
	"github.com/samber/lo"

	"usbridge_agent/internal/config"
)

type Manager struct {
	cfg      config.Config
	httpPort int
	videoUDP int

	rootCtx    context.Context
	mu         sync.Mutex
	server     *server.Service
	client     *client.Service
	agg        *source.Aggregator
	clientCtx  context.Context
	clientStop context.CancelFunc
	nbdPorts   []int
}

func New(cfg config.Config, httpPort, videoUDP int) *Manager {
	return &Manager{cfg: cfg, httpPort: httpPort, videoUDP: videoUDP}
}

func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.server != nil {
		return nil
	}
	m.rootCtx = ctx
	return m.startLocked(ctx)
}

func (m *Manager) startLocked(ctx context.Context) error {
	srvCfg := &v1.ServerConfig{
		BindAddr:     m.cfg.FRPBindHost,
		BindPort:     m.cfg.FRPBindPort,
		QUICBindPort: m.cfg.FRPBindPort,
		Auth: v1.AuthServerConfig{
			Method: "token",
			Token:  m.cfg.FRPToken,
		},
		Transport: v1.ServerTransportConfig{
			TCPMux: lo.ToPtr(true),
			QUIC: v1.QUICOptions{
				KeepalivePeriod:    10,
				MaxIdleTimeout:     86400,
				MaxIncomingStreams: 100000,
			},
			TLS: v1.TLSServerConfig{
				Force: true,
				TLSConfig: v1.TLSConfig{
					CertFile: m.cfg.FRPTLSCertFile,
					KeyFile:  m.cfg.FRPTLSKeyFile,
				},
			},
			HeartbeatTimeout: 10,
		},
		UserConnTimeout: 86400,
	}
	if err := srvCfg.Complete(); err != nil {
		return err
	}
	svc, err := server.NewService(srvCfg)
	if err != nil {
		return fmt.Errorf("create frps: %w", err)
	}
	m.server = svc
	go svc.Run(ctx)

	common := &v1.ClientCommonConfig{
		ServerAddr: "127.0.0.1",
		ServerPort: m.cfg.FRPBindPort,
		Auth: v1.AuthClientConfig{
			Method: "token",
			Token:  m.cfg.FRPToken,
		},
		Transport: v1.ClientTransportConfig{
			Protocol: "quic",
			QUIC: v1.QUICOptions{
				KeepalivePeriod:    10,
				MaxIdleTimeout:     86400,
				MaxIncomingStreams: 100000,
			},
			TLS: v1.TLSClientConfig{
				Enable: lo.ToPtr(true),
			},
			HeartbeatInterval: 3,
			HeartbeatTimeout:  10,
			TCPMux:            lo.ToPtr(true),
		},
		LoginFailExit: lo.ToPtr(true),
	}
	cfgSource := source.NewConfigSource()
	if err := cfgSource.ReplaceAll(m.baseProxies(), m.baseVisitors()); err != nil {
		_ = m.server.Close()
		m.server = nil
		return err
	}
	m.agg = source.NewAggregator(cfgSource)
	cli, err := client.NewService(client.ServiceOptions{
		Common:                 common,
		ConfigSourceAggregator: m.agg,
	})
	if err != nil {
		_ = m.server.Close()
		m.server = nil
		return fmt.Errorf("create frpc: %w", err)
	}
	m.client = cli
	m.clientCtx, m.clientStop = context.WithCancel(ctx)
	go func() { _ = cli.Run(m.clientCtx) }()
	time.Sleep(1500 * time.Millisecond)
	return nil
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopLocked()
	return nil
}

func (m *Manager) stopLocked() {
	if m.clientStop != nil {
		m.clientStop()
	}
	if m.server != nil {
		_ = m.server.Close()
	}
	m.client = nil
	m.server = nil
	m.agg = nil
	m.clientStop = nil
	m.clientCtx = nil
}

func (m *Manager) UpdateToken(token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cfg.FRPToken = token
	if m.server == nil && m.client == nil {
		return nil
	}

	ctx := m.rootCtx
	if ctx == nil {
		ctx = context.Background()
	}
	m.stopLocked()
	return m.startLocked(ctx)
}

func (m *Manager) UpdateNBDVisitors(ports []int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nbdPorts = append([]int(nil), ports...)
	if m.client == nil {
		return nil
	}
	return m.client.UpdateAllConfigurer(m.baseProxies(), m.baseVisitors())
}

func (m *Manager) UpdateVideoVisitor(port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if port <= 0 {
		port = m.cfg.VideoUDPPort
	}
	m.videoUDP = port
	if m.client == nil {
		return nil
	}
	return m.client.UpdateAllConfigurer(m.baseProxies(), m.baseVisitors())
}

func (m *Manager) baseProxies() []v1.ProxyConfigurer {
	return []v1.ProxyConfigurer{
		&v1.STCPProxyConfig{
			ProxyBaseConfig: v1.ProxyBaseConfig{
				Name: "http_srv",
				Type: "stcp",
				ProxyBackend: v1.ProxyBackend{
					LocalIP:   "127.0.0.1",
					LocalPort: m.httpPort,
				},
			},
			Secretkey: "sk-http",
		},
	}
}

func (m *Manager) baseVisitors() []v1.VisitorConfigurer {
	visitors := []v1.VisitorConfigurer{
		&v1.SUDPVisitorConfig{
			VisitorBaseConfig: v1.VisitorBaseConfig{
				Name:       "video_visitor",
				Type:       "sudp",
				ServerName: "video_sudp",
				SecretKey:  "sk-video",
				BindAddr:   "127.0.0.1",
				BindPort:   m.videoUDP,
			},
		},
	}
	for i, port := range m.nbdPorts {
		visitors = append(visitors, &v1.STCPVisitorConfig{
			VisitorBaseConfig: v1.VisitorBaseConfig{
				Name:       fmt.Sprintf("nbd_from_client%d", i+1),
				Type:       "stcp",
				ServerName: fmt.Sprintf("nbd_srv%d", i+1),
				SecretKey:  "sk-nbd",
				BindAddr:   "127.0.0.1",
				BindPort:   port,
			},
		})
	}
	return visitors
}

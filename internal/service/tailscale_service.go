package service

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
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
	mu            sync.Mutex
	server        *tsnet.Server
	latestAuthURL string
}

func NewTailscaleService() *TailscaleService {
	return &TailscaleService{}
}

func (s *TailscaleService) Status(ctx context.Context) (*TailscaleStatus, error) {
	refreshAndroidDefaultRouteInterface()
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
		if peer == nil {
			continue
		}
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
	refreshAndroidDefaultRouteInterface()
	logrus.Info("tailscale client: StartLogin begin")
	lc, err := s.localClient()
	if err != nil {
		logrus.WithError(err).Error("tailscale client: localClient failed")
		return "", err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
	}

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		logrus.WithError(err).Error("tailscale client: WatchIPNBus failed")
		return "", fmt.Errorf("tailscale watch bus: %w", err)
	}
	defer watcher.Close()

	status, err := lc.Status(ctx)
	if err == nil && status != nil {
		logrus.Infof("tailscale client: initial status backend=%s authURL=%t tun=%v", strings.TrimSpace(status.BackendState), strings.TrimSpace(status.AuthURL) != "", status.TUN)
		if url := strings.TrimSpace(status.AuthURL); url != "" {
			s.setLatestAuthURL(url)
			logrus.Infof("tailscale client: returning initial AuthURL %s", url)
			return url, nil
		}
		state := strings.TrimSpace(status.BackendState)
		if state == "" || state == "NoState" {
			logrus.Infof("tailscale client: starting backend from state=%q", state)
			if err := lc.Start(ctx, ipn.Options{}); err != nil {
				logrus.WithError(err).Error("tailscale client: lc.Start failed")
				return "", fmt.Errorf("tailscale start: %w", err)
			}
		}
	} else if err != nil {
		logrus.WithError(err).Warn("tailscale client: initial Status failed")
	}

	if err := lc.StartLoginInteractive(ctx); err != nil {
		logrus.WithError(err).Error("tailscale client: StartLoginInteractive failed")
		return "", fmt.Errorf("tailscale interactive login: %w", err)
	}
	logrus.Info("tailscale client: StartLoginInteractive requested")

	deadline := time.Now().Add(45 * time.Second)
	type watchResult struct {
		url string
		err error
	}
	watchCh := make(chan watchResult, 1)
	go func() {
		for {
			n, err := watcher.Next()
			if err != nil {
				logrus.WithError(err).Warn("tailscale client: watcher.Next ended")
				watchCh <- watchResult{err: err}
				return
			}
			if n.BrowseToURL != nil && strings.TrimSpace(*n.BrowseToURL) != "" {
				logrus.Infof("tailscale client: BrowseToURL received %s", strings.TrimSpace(*n.BrowseToURL))
				watchCh <- watchResult{url: strings.TrimSpace(*n.BrowseToURL)}
				return
			}
			if n.ErrMessage != nil && strings.TrimSpace(*n.ErrMessage) != "" {
				logrus.Warnf("tailscale client: watcher ErrMessage=%s", strings.TrimSpace(*n.ErrMessage))
				watchCh <- watchResult{err: fmt.Errorf("%s", strings.TrimSpace(*n.ErrMessage))}
				return
			}
			if n.LoginFinished != nil {
				logrus.Info("tailscale client: LoginFinished notification received")
				watchCh <- watchResult{}
				return
			}
		}
	}()

	ticker := time.NewTicker(400 * time.Millisecond)
	defer ticker.Stop()
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case result := <-watchCh:
			if strings.TrimSpace(result.url) != "" {
				s.setLatestAuthURL(result.url)
				return result.url, nil
			}
			if result.err != nil {
				if url := strings.TrimSpace(s.getLatestAuthURL()); url != "" {
					return url, nil
				}
			}
		case <-ticker.C:
		}

		status, err := lc.Status(ctx)
		if err == nil && status != nil {
			if url := strings.TrimSpace(status.AuthURL); url != "" {
				s.setLatestAuthURL(url)
				logrus.Infof("tailscale client: polled AuthURL %s", url)
				return url, nil
			}
			if strings.TrimSpace(status.BackendState) == "Running" {
				logrus.Info("tailscale client: backend reached Running without auth URL")
				return "", nil
			}
		} else if err != nil {
			logrus.WithError(err).Debug("tailscale client: poll Status failed")
		}
		if url := strings.TrimSpace(s.getLatestAuthURL()); url != "" {
			logrus.Infof("tailscale client: using cached AuthURL %s", url)
			return url, nil
		}
	}
	logrus.Warn("tailscale client: auth URL was not produced before timeout")
	return "", fmt.Errorf("tailscale auth URL was not produced")
}

func (s *TailscaleService) HTTPClient() (*http.Client, error) {
	srv, err := s.serverInstance()
	if err != nil {
		return nil, err
	}
	return srv.HTTPClient(), nil
}

func (s *TailscaleService) ValidateAddress(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("tailscale host is empty")
	}
	if strings.Contains(raw, "://") {
		parsed, err := url.Parse(raw)
		if err != nil {
			return err
		}
		if strings.TrimSpace(parsed.Host) == "" {
			return fmt.Errorf("tailscale host is empty")
		}
	}
	return nil
}

func (s *TailscaleService) localClient() (*tsnetLocalClient, error) {
	srv, err := s.serverInstance()
	if err != nil {
		return nil, err
	}
	lc, err := srv.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("tailscale local client: %w", err)
	}
	return &tsnetLocalClient{Client: lc}, nil
}

func (s *TailscaleService) serverInstance() (*tsnet.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return s.server, nil
	}
	initAndroidTailscaleUserspace()
	s.server = &tsnet.Server{
		Dir:      tailscaleStateDir("usbridge-client"),
		Hostname: "usbridge-client",
		UserLogf: s.handleUserLogf,
	}
	if err := s.server.Start(); err != nil {
		s.server = nil
		return nil, fmt.Errorf("tailscale start: %w", err)
	}
	return s.server, nil
}

type tsnetLocalClient struct {
	*local.Client
}

func (s *TailscaleService) handleUserLogf(format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	logrus.Infof("tailscale client tsnet: %s", message)
	const marker = "or go to: "
	if idx := strings.LastIndex(message, marker); idx >= 0 {
		url := strings.TrimSpace(message[idx+len(marker):])
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			s.setLatestAuthURL(url)
			logrus.Infof("tailscale client: captured AuthURL from tsnet log %s", url)
		}
	}
}

func (s *TailscaleService) setLatestAuthURL(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestAuthURL = strings.TrimSpace(raw)
}

func (s *TailscaleService) getLatestAuthURL() string {
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

func firstAddr(values []netip.Addr) string {
	for _, value := range values {
		text := value.String()
		if strings.Count(text, ".") == 3 {
			return text
		}
	}
	if len(values) > 0 {
		return values[0].String()
	}
	return ""
}

func trimDotSuffix(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
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

func (s *TailscaleService) Close() error {
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

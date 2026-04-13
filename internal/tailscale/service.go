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

	"tailscale.com/client/local"
	"tailscale.com/ipn"
	"tailscale.com/ipn/ipnstate"
	"tailscale.com/tailcfg"
	"tailscale.com/tsnet"

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
	mu            sync.Mutex
	server        *tsnet.Server
	latestAuthURL string
}

func New() *Service {
	return &Service{}
}

func (s *Service) Status(ctx context.Context) (*Status, error) {
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

	out := &Status{
		Running:   strings.TrimSpace(state.BackendState) == "Running",
		LoggedIn:  state.BackendState != "" && state.BackendState != "NeedsLogin" && state.BackendState != "NoState",
		Backend:   strings.TrimSpace(state.BackendState),
		Userspace: !state.TUN,
	}
	if state.Self != nil {
		out.Self = Peer{
			HostName:  strings.TrimSpace(state.Self.HostName),
			DNSName:   trimDotSuffix(state.Self.DNSName),
			IP4:       firstAddr(state.Self.TailscaleIPs),
			UserLogin: userLogin(state, state.Self.UserID),
			Online:    state.Self.Online,
		}
	}
	return out, nil
}

func (s *Service) TailnetIPv4(ctx context.Context) (string, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status.Self.IP4) == "" {
		return "", fmt.Errorf("tailscale IPv4 address unavailable")
	}
	return status.Self.IP4, nil
}

func (s *Service) IsUserspace(ctx context.Context) (bool, error) {
	status, err := s.Status(ctx)
	if err != nil {
		return false, err
	}
	return status.Userspace, nil
}

func (s *Service) StartLogin(ctx context.Context) (string, error) {
	logrus.Info("tailscale agent: StartLogin begin")
	lc, err := s.localClient()
	if err != nil {
		logrus.WithError(err).Error("tailscale agent: localClient failed")
		return "", err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
	}

	watcher, err := lc.WatchIPNBus(ctx, ipn.NotifyInitialState)
	if err != nil {
		logrus.WithError(err).Error("tailscale agent: WatchIPNBus failed")
		return "", fmt.Errorf("tailscale watch bus: %w", err)
	}
	defer watcher.Close()

	status, err := lc.Status(ctx)
	if err == nil && status != nil {
		logrus.Infof("tailscale agent: initial status backend=%s authURL=%t tun=%v", strings.TrimSpace(status.BackendState), strings.TrimSpace(status.AuthURL) != "", status.TUN)
		if url := strings.TrimSpace(status.AuthURL); url != "" {
			s.setLatestAuthURL(url)
			logrus.Infof("tailscale agent: returning initial AuthURL %s", url)
			return url, nil
		}
		state := strings.TrimSpace(status.BackendState)
		if state == "" || state == "NoState" {
			logrus.Infof("tailscale agent: starting backend from state=%q", state)
			if err := lc.Start(ctx, ipn.Options{}); err != nil {
				logrus.WithError(err).Error("tailscale agent: lc.Start failed")
				return "", fmt.Errorf("tailscale start: %w", err)
			}
		}
	} else if err != nil {
		logrus.WithError(err).Warn("tailscale agent: initial Status failed")
	}

	if err := lc.StartLoginInteractive(ctx); err != nil {
		logrus.WithError(err).Error("tailscale agent: StartLoginInteractive failed")
		return "", fmt.Errorf("tailscale interactive login: %w", err)
	}
	logrus.Info("tailscale agent: StartLoginInteractive requested")

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
				logrus.WithError(err).Warn("tailscale agent: watcher.Next ended")
				watchCh <- watchResult{err: err}
				return
			}
			if n.BrowseToURL != nil && strings.TrimSpace(*n.BrowseToURL) != "" {
				logrus.Infof("tailscale agent: BrowseToURL received %s", strings.TrimSpace(*n.BrowseToURL))
				watchCh <- watchResult{url: strings.TrimSpace(*n.BrowseToURL)}
				return
			}
			if n.ErrMessage != nil && strings.TrimSpace(*n.ErrMessage) != "" {
				logrus.Warnf("tailscale agent: watcher ErrMessage=%s", strings.TrimSpace(*n.ErrMessage))
				watchCh <- watchResult{err: fmt.Errorf("%s", strings.TrimSpace(*n.ErrMessage))}
				return
			}
			if n.LoginFinished != nil {
				logrus.Info("tailscale agent: LoginFinished notification received")
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
				logrus.Infof("tailscale agent: polled AuthURL %s", url)
				return url, nil
			}
			if strings.TrimSpace(status.BackendState) == "Running" {
				logrus.Info("tailscale agent: backend reached Running without auth URL")
				return "", nil
			}
		} else if err != nil {
			logrus.WithError(err).Debug("tailscale agent: poll Status failed")
		}
		if url := strings.TrimSpace(s.getLatestAuthURL()); url != "" {
			logrus.Infof("tailscale agent: using cached AuthURL %s", url)
			return url, nil
		}
	}
	logrus.Warn("tailscale agent: auth URL was not produced before timeout")
	return "", fmt.Errorf("tailscale auth URL was not produced")
}

func (s *Service) Logout(ctx context.Context) error {
	lc, err := s.localClient()
	if err != nil {
		return err
	}
	if ctx == nil {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
	}
	if err := lc.Logout(ctx); err != nil {
		return fmt.Errorf("tailscale logout: %w", err)
	}
	return nil
}

func (s *Service) Server() (*tsnet.Server, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.server != nil {
		return s.server, nil
	}

	// On Linux, try to use Kernel mode (TUN) for better video performance
	// This requires CAP_NET_ADMIN permissions.
	userspace := true
	if os.Getenv("TS_USERSPACE") == "false" || (os.Getenv("TS_USERSPACE") == "" && os.Getuid() == 0) {
		userspace = false
		logrus.Info("🚀 [Tailscale] Attempting to enable Kernel Mode (TUN) for Agent")
	}

	s.server = &tsnet.Server{
		Dir:       tailscaleStateDir("usbridge-agent"),
		Hostname:  "usbridge-agent",
		UserLogf:  s.handleUserLogf,
		Userspace: userspace,
	}

	if err := s.server.Start(); err != nil {
		// Fallback to userspace if TUN fails
		if !userspace {
			logrus.Warnf("⚠️ [Tailscale] Kernel Mode failed (%v), falling back to Userspace", err)
			s.server = &tsnet.Server{
				Dir:       tailscaleStateDir("usbridge-agent"),
				Hostname:  "usbridge-agent",
				UserLogf:  s.handleUserLogf,
				Userspace: true,
			}
			if err := s.server.Start(); err != nil {
				s.server = nil
				return nil, fmt.Errorf("tailscale fallback start: %w", err)
			}
			return s.server, nil
		}
		s.server = nil
		return nil, fmt.Errorf("tailscale start: %w", err)
	}
	return s.server, nil
}

func (s *Service) Close() error {
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

func (s *Service) localClient() (*local.Client, error) {
	server, err := s.Server()
	if err != nil {
		return nil, err
	}
	lc, err := server.LocalClient()
	if err != nil {
		return nil, fmt.Errorf("tailscale local client: %w", err)
	}
	return lc, nil
}

func (s *Service) handleUserLogf(format string, args ...any) {
	message := strings.TrimSpace(fmt.Sprintf(format, args...))
	logrus.Infof("tailscale agent tsnet: %s", message)
	const marker = "or go to: "
	if idx := strings.LastIndex(message, marker); idx >= 0 {
		url := strings.TrimSpace(message[idx+len(marker):])
		if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
			s.setLatestAuthURL(url)
			logrus.Infof("tailscale agent: captured AuthURL from tsnet log %s", url)
		}
	}
}

func (s *Service) setLatestAuthURL(raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.latestAuthURL = strings.TrimSpace(raw)
}

func (s *Service) getLatestAuthURL() string {
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

func trimDotSuffix(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), ".")
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

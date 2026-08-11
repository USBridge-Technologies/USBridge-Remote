//go:build js && wasm

package service

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// TailscaleService is a no-op stand-in for the embedded-Tailscale (tsnet)
// backend on wasm/browser builds. A browser sandbox cannot run tsnet's
// userspace WireGuard/netstack (no raw UDP sockets, no dialing arbitrary
// hosts), so the web client does not implement Tailscale itself: users who
// want tailnet access from the browser run a normal Tailscale client on
// their OS and simply browse to the tailnet IP like any other address.
// This type exists purely so internal/gui keeps compiling unmodified across
// platforms; every method reports "unavailable" and never blocks.
type TailscaleService struct{}

// TailscaleStatus mirrors the shape read by internal/gui — always reports a
// signed-out, not-running state on wasm.
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

func NewTailscaleService() *TailscaleService { return &TailscaleService{} }

var errTailscaleUnsupported = fmt.Errorf("tailscale is not available in the browser — install a Tailscale client on this OS and browse to the tailnet address directly")

func (s *TailscaleService) Start(ctx context.Context) error { return errTailscaleUnsupported }

func (s *TailscaleService) Stop() error { return nil }

func (s *TailscaleService) Status(ctx context.Context) (*TailscaleStatus, error) {
	return &TailscaleStatus{Running: false, LoggedIn: false}, nil
}

func (s *TailscaleService) WaitForPeerReachable(ctx context.Context, tailscaleIP, port string, maxWait time.Duration) {
}

func (s *TailscaleService) StartLogin(ctx context.Context) (string, error) {
	return "", errTailscaleUnsupported
}

func (s *TailscaleService) Logout(ctx context.Context) error { return nil }

func (s *TailscaleService) HTTPClient() (*http.Client, error) {
	return nil, errTailscaleUnsupported
}

func (s *TailscaleService) WarmUpPeer(tailscaleIP string) {}

func (s *TailscaleService) WaitUntilReady(ctx context.Context) error {
	return errTailscaleUnsupported
}

func (s *TailscaleService) ValidateAddress(raw string) error { return nil }

func (s *TailscaleService) TailnetIPv4(ctx context.Context) (string, error) {
	return "", errTailscaleUnsupported
}

func (s *TailscaleService) RefreshNetwork() {}

func (s *TailscaleService) VideoRelayDebugInfo(bridgeHost string) string {
	return "tailscale: n/a (browser)"
}

func (s *TailscaleService) GetPeerDirectIP(tailscaleIP string) string { return "" }

func (s *TailscaleService) SetAuthURLHandler(fn func(string)) {}

//go:build !(js && wasm)

package gui

import (
	"context"
	"fmt"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// dialTailscaleTarget establishes a client connection to a Tailscale peer
// via the embedded tsnet userspace WireGuard stack (desktop/Android — see
// main_window_connection_tailscale_wasm.go for the browser build's plain-
// HTTP counterpart, which has no tsnet to speak of).
func (mw *MainWindow) dialTailscaleTarget(ctx context.Context, target string) (*api.USBClient, error) {
	if !mw.config.TailscaleEnabled {
		return nil, fmt.Errorf("Tailscale disabled in config")
	}
	if mw.tailscaleService == nil {
		mw.tailscaleService = service.NewTailscaleService()
	}

	status, err := mw.tailscaleService.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("tailscale is not ready: %w", err)
	}
	if !status.LoggedIn {
		return nil, fmt.Errorf("tailscale is signed out, use Google login in Connection Manager first")
	}

	if err := mw.tailscaleService.ValidateAddress(target); err != nil {
		return nil, err
	}
	httpClient, err := mw.tailscaleService.HTTPClient()
	if err != nil {
		return nil, err
	}

	tsClient := api.NewUSBClientWithHTTPClient(target, mw.config.USBPort, mw.config.APITimeout, httpClient)

	// On Android userspace Tailscale (tsnet), the first request can fail
	// until tsnet has established a route to the peer.
	var connErr error
	const maxConnAttempts = 6
	for attempt := 1; attempt <= maxConnAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		connErr = tsClient.TestConnectionWithContext(ctx)
		if connErr == nil {
			break
		}
		if attempt < maxConnAttempts {
			pause := time.Duration(attempt*attempt) * time.Second
			if pause > 10*time.Second {
				pause = 10 * time.Second
			}
			select {
			case <-time.After(pause):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
	}
	if connErr != nil {
		return nil, fmt.Errorf("bridge unreachable at %s: %w", target, connErr)
	}

	// Pre-warm the tsnet WireGuard session to the peer so it's established
	// before Moonlight HTTP calls (pairing, serverinfo) happen a moment later.
	mw.tailscaleService.WarmUpPeer(target)

	logrus.Debugf("🛰️ [TS] Dialed %s via tsnet", target)
	return tsClient, nil
}

// tailscaleRegisterSupported reports whether the "register bridge in
// Tailscale" flow is available on this platform — see
// main_window_connection_tailscale_wasm.go's counterpart for why it isn't
// in the browser.
func tailscaleRegisterSupported() bool { return true }

// usesTsnetTransport reports whether this platform routes Tailscale
// connections through the embedded tsnet userspace WireGuard stack — see
// main_window_connection_tailscale_wasm.go's counterpart for the browser
// build, which does not.
func usesTsnetTransport() bool { return true }

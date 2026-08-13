//go:build js && wasm

package gui

import (
	"context"

	"usbridge-client/internal/api"
)

// dialTailscaleTarget in the browser build treats a Tailscale address
// exactly like a plain LAN address: the wasm sandbox cannot run tsnet (see
// service/tailscale_service_wasm.go's doc comment — no raw UDP sockets, no
// dialing arbitrary hosts from inside a browser tab), so there is no
// separate Tailscale transport to speak of here. Whatever the user put in
// the Tailscale field (a 100.x.x.x address or a *.ts.net MagicDNS name) is
// just an ordinary hostname from the browser's point of view — it resolves
// and routes exactly like any other address, PROVIDED a native Tailscale
// client is already running on this OS. So this dials it the same way the
// "direct"/LAN path does: a plain HTTP request, no tsnet status/login
// checks, no WarmUpPeer.
func (mw *MainWindow) dialTailscaleTarget(ctx context.Context, target string) (*api.USBClient, error) {
	tempClient := api.NewDirectUSBClient(target, mw.config.USBPort, mw.config.APITimeout)
	if err := testConnectionWithRetry(ctx, tempClient, target); err != nil {
		return nil, err
	}
	return tempClient, nil
}

// tailscaleRegisterSupported reports whether the "register bridge in
// Tailscale" flow (agent-side `tailscale up`, polled for from the client
// then followed by an automatic LAN→Tailscale reconnect) makes sense on
// this platform. It does not in the browser: there's no separate Tailscale
// transport to switch to afterward (see dialTailscaleTarget above), so
// attempting it would just disconnect and silently reconnect the exact same
// way — a pointless reconnect with no payoff, not a feature.
func tailscaleRegisterSupported() bool { return false }

// usesTsnetTransport reports whether this platform routes Tailscale
// connections through the embedded tsnet userspace WireGuard stack. The
// browser build does not (see dialTailscaleTarget above) — so there's
// nothing here for doConnect's pre-connect "wait for tsnet Running" step to
// usefully wait on.
func usesTsnetTransport() bool { return false }

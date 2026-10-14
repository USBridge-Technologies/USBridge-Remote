package controller

import (
	"usbridge-client/internal/gui/view"

	"github.com/sirupsen/logrus"
)

// showPairingPINDialog displays the Moonlight pairing PIN for manual entry on
// the host, for use against any host that isn't this project's own agent (a
// stock Sunshine or real NVIDIA GameStream host has no endpoint to auto-submit
// the PIN to -- see MoonlightService.SetOnPairingPINRequired). Closing the
// overlay (X, Cancel, or the dim) aborts the in-flight Pair() request so the
// rest of the client stays usable; a successful/failed Pair dismisses it via
// dismissPairingPINDialog.
func (vw *VideoWidget) showPairingPINDialog(pin string) {
	if vw.parentWindow == nil {
		return
	}
	vw.dismissPairingPINDialog()
	vw.pairingPINDialog = view.ShowPairingPINDialog(vw.parentWindow, pin, func() {
		logrus.Info("🔐 [VideoWidget] pairing PIN dialog cancelled by user")
		vw.MarkUserStopped()
		go func() {
			if vw.videoClient != nil {
				_ = vw.videoClient.Disconnect()
			}
		}()
	})
}

// dismissPairingPINDialog hides the dialog raised by showPairingPINDialog, if
// one is currently showing. Safe to call even when none was ever shown (the
// common case: most hosts are this project's own agent and never need this
// fallback at all).
func (vw *VideoWidget) dismissPairingPINDialog() {
	if vw.pairingPINDialog == nil {
		return
	}
	vw.pairingPINDialog.Hide()
	vw.pairingPINDialog = nil
}

// SetGameStreamOnly marks the current connection as a plain Sunshine/
// GameStream host with no usbridge agent API behind it (connected with an
// empty master key). Starting video then skips the agent-only steps -- the
// video-info query that seeds the start dialog and the HID gadget
// auto-connect -- which would only fail (or 401) against such a host; input
// goes over the Moonlight stream itself.
func (vw *VideoWidget) SetGameStreamOnly(enabled bool) {
	vw.gameStreamOnly.Store(enabled)
}

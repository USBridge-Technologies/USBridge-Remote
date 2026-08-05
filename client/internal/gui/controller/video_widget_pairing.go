package controller

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

var pairingPINDialogSize = fyne.NewSize(360, 260)

// showPairingPINDialog displays the Moonlight pairing PIN for manual entry on
// the host, for use against any host that isn't this project's own agent (a
// stock Sunshine or real NVIDIA GameStream host has no endpoint to auto-submit
// the PIN to -- see MoonlightService.SetOnPairingPINRequired). The dialog has
// no buttons: it's dismissed automatically via dismissPairingPINDialog once
// the pairing HTTP request this PIN belongs to returns, whether the host
// accepted it or the attempt timed out/failed.
func (vw *VideoWidget) showPairingPINDialog(pin string) {
	if vw.parentWindow == nil {
		return
	}
	if vw.pairingPINDialog != nil {
		vw.pairingPINDialog.Hide()
	}

	pinText := canvas.NewText(pin, nil)
	pinText.TextStyle.Bold = true
	pinText.TextSize = 48
	pinText.Alignment = fyne.TextAlignCenter

	content := container.NewVBox(
		widget.NewLabel(i18n.Current.PairingPINMessage),
		widget.NewLabel(""),
		container.NewCenter(pinText),
		widget.NewLabel(""),
		widget.NewProgressBarInfinite(),
		widget.NewLabel(i18n.Current.PairingPINWaiting),
	)

	d := dialog.NewCustomWithoutButtons(i18n.Current.PairingPINTitle, content, vw.parentWindow)
	d.Resize(pairingPINDialogSize)
	vw.pairingPINDialog = d
	d.Show()
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

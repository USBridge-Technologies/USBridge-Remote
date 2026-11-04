package gui

import (
	"errors"
	"strings"
	"sync/atomic"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"
)

// The user's own control server (a self-hosted Headscale) for this app's
// Tailscale node. Niche: opened by a right click / long press on the header's
// Tailscale chip, nothing on screen points at it; Tailscale is the default.

const tailscaleControlURLPrefKey = "tailscale_control_url"

var tailscaleServerOpen atomic.Bool

// restoreTailscaleControlURL applies the saved choice before the node first
// starts.
func restoreTailscaleControlURL(app fyne.App) {
	if app == nil || app.Preferences() == nil {
		return
	}
	if u, err := service.NormalizeControlURL(app.Preferences().String(tailscaleControlURLPrefKey)); err == nil {
		service.SetOwnControlURL(u)
	}
}

func (mw *MainWindow) showTailscaleServerDialog() {
	if mw.app == nil || !tailscaleServerOpen.CompareAndSwap(false, true) {
		return
	}
	tr := i18n.Current
	current := service.OwnControlURL()

	entry := view.NewStyledEntry()
	entry.SetPlaceHolder("https://headscale.example.com")
	entry.SetText(current)
	headscale := container.NewVBox(
		view.NewDialogField(tr.TailscaleServerAddress, entry),
		view.NewDialogVSpace(6),
		view.NewDialogHint(tr.TailscaleServerHint, 0),
	)
	selected := tr.TailscaleServerTailscale
	if current != "" {
		selected = tr.TailscaleServerHeadscale
	} else {
		headscale.Hide()
	}
	picker := view.NewDialogPicker([]string{tr.TailscaleServerTailscale, tr.TailscaleServerHeadscale}, selected, func(sel string) {
		if sel == tr.TailscaleServerHeadscale {
			headscale.Show()
		} else {
			headscale.Hide()
		}
	})
	body := container.NewVBox(view.NewDialogField(tr.TailscaleServerTitle, picker), view.NewDialogVSpace(8), headscale)

	apply := func() {
		tailscaleServerOpen.Store(false)
		value := ""
		if picker.Selected == tr.TailscaleServerHeadscale {
			value = strings.TrimSpace(entry.Text)
			if value == "" {
				dialog.ShowError(errors.New(tr.TailscaleServerNeedURL), mw.window)
				return
			}
		}
		u, err := service.NormalizeControlURL(value)
		if err != nil {
			dialog.ShowError(err, mw.window)
			return
		}
		if u == service.OwnControlURL() {
			return
		}
		mw.app.Preferences().SetString(tailscaleControlURLPrefKey, u)
		service.SetOwnControlURL(u)
		// The node restarts on the new server at its next use (connect,
		// sign-in); the old server's login stays in its own state dir.
		// (During a guest session the owner's node is parked in
		// ownerTailscale; Reset is a no-op on the guest node.)
		for _, ts := range []*service.TailscaleService{mw.tailscaleService, mw.ownerTailscale} {
			if ts != nil {
				go ts.Reset()
			}
		}
	}
	view.ShowBrandFormDialog(view.BrandFormDialogSpec{
		Parent:     mw.window,
		Title:      tr.TailscaleServerTitle,
		Body:       body,
		CancelText: tr.Cancel,
		ApplyText:  tr.Apply,
		OnCancel:   func() { tailscaleServerOpen.Store(false) },
		OnApply:    apply,
		MinWidth:   clientSettingsWidth,
	})
}

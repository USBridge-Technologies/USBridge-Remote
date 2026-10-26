package gui

import (
	"time"

	"usbridge-client/internal/platform"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/sirupsen/logrus"
)

const (
	urlSchemeDeclinedKey = "url_scheme_declined_at"
	urlSchemeAskAgain    = 7 * 24 * time.Hour
)

// offerURLSchemeRegistration asks, once the window is up, to let this app open
// usbridge:// links (invitation e-mails, connect links) when the OS does not
// know it yet -- a portable copy that no installer registered. Declining
// silences the question for a week; nothing is registered without the answer.
func (mw *MainWindow) offerURLSchemeRegistration() {
	if mw.app == nil || platform.IsURLSchemeRegistered() {
		return
	}
	if at := mw.app.Preferences().Int(urlSchemeDeclinedKey); at > 0 && time.Since(time.Unix(int64(at), 0)) < urlSchemeAskAgain {
		return
	}
	fyne.Do(func() {
		dialog.ShowConfirm("Open links from e-mail in USBridge?",
			"Allow USBridge to open usbridge:// links, such as the invitation to a shared computer. This changes only your own user settings.",
			func(ok bool) {
				if !ok {
					mw.app.Preferences().SetInt(urlSchemeDeclinedKey, int(time.Now().Unix()))
					return
				}
				if err := platform.RegisterURLScheme(); err != nil {
					logrus.Warnf("registering the usbridge:// scheme: %v", err)
					dialog.ShowError(err, mw.window)
					return
				}
				mw.app.Preferences().SetInt(urlSchemeDeclinedKey, 0)
			}, mw.window)
	})
}

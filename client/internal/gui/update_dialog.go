package gui

import (
	"fmt"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
)

// ShowUpdateAvailableDialog asks the user whether to install a newer,
// already signature-verified build before applying it — replacing a forced
// silent update, which is jarring for a window the user is actively
// looking at. onResult is called with the user's answer once they respond;
// it always runs on the UI goroutine (via fyne.Do, mirroring every other
// dialog in this package).
//
// Safe to call from any goroutine — internal/update's Check runs a network
// request, so callers are expected to invoke this from a background
// goroutine rather than blocking startup.
func (mw *MainWindow) ShowUpdateAvailableDialog(newVersion, currentVersion string, onResult func(confirmed bool)) {
	fyne.Do(func() {
		if mw == nil || mw.window == nil {
			return
		}
		d := dialog.NewConfirm(
			i18n.Current.UpdateAvailableTitle,
			fmt.Sprintf(i18n.Current.UpdateAvailableMessage, newVersion, currentVersion),
			onResult,
			mw.window,
		)
		d.SetConfirmText(i18n.Current.UpdateNowButton)
		d.SetDismissText(i18n.Current.UpdateLaterButton)
		d.Show()
	})
}

package gui

import (
	"fmt"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
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

// UpdateProgress is a live handle to an in-flight update's progress dialog,
// returned by ShowUpdateProgressDialog — added because a self-update that
// silently sits there with no visible activity (as this looked before) is
// indistinguishable from having frozen. Every method is safe to call from
// any goroutine (they hop to the UI thread themselves via fyne.Do) and safe
// to call on a nil receiver, so callers never need to nil-check before
// using one (e.g. when the window wasn't available to show it against).
type UpdateProgress struct {
	dialog *dialog.CustomDialog
	bar    *widget.ProgressBar
}

// ShowUpdateProgressDialog shows a determinate progress dialog for an
// in-flight update download, anchored to the main window. Pass the
// returned value's Update method as internal/update's ProgressFunc, and
// call Close once the download (and, on desktop, the apply/relaunch
// attempt) finishes.
func (mw *MainWindow) ShowUpdateProgressDialog(version string) *UpdateProgress {
	if mw == nil || mw.window == nil {
		return nil
	}
	up := &UpdateProgress{bar: widget.NewProgressBar()}
	fyne.Do(func() {
		content := container.NewVBox(
			widget.NewLabel(fmt.Sprintf(i18n.Current.UpdateDownloadingMessage, version)),
			up.bar,
		)
		up.dialog = dialog.NewCustomWithoutButtons(i18n.Current.UpdateDownloadingTitle, content, mw.window)
		up.dialog.Resize(fyne.NewSize(360, 120))
		up.dialog.Show()
	})
	return up
}

// Update sets the progress bar's fraction from downloaded/total bytes —
// intended to be passed directly as internal/update's ProgressFunc.
func (up *UpdateProgress) Update(downloaded, total int64) {
	if up == nil || up.bar == nil || total <= 0 {
		return
	}
	fraction := float64(downloaded) / float64(total)
	fyne.Do(func() { up.bar.SetValue(fraction) })
}

// Close dismisses the progress dialog. A successful desktop apply
// relaunches the whole app before this would ever run, but the Android and
// error paths need it to avoid leaving a stuck-looking dialog on screen.
func (up *UpdateProgress) Close() {
	if up == nil || up.dialog == nil {
		return
	}
	fyne.Do(func() { up.dialog.Hide() })
}

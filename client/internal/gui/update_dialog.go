package gui

import (
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
)

// debugPreviewUpdateDialog, when true, opens the update-available overlay on
// launch with dummy versions so the card can be tuned without waiting for a
// real GitHub release. Same idea as debugForceAllStatusIndicators. Flip
// back to false before shipping.
const debugPreviewUpdateDialog = false

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
		view.ShowUpdateAvailableDialog(mw.window, newVersion, currentVersion, onResult)
	})
}

// previewUpdateAvailableDialog opens the styled update card with dummy
// versions — flip debugPreviewUpdateDialog to true to see it on launch.
func (mw *MainWindow) previewUpdateAvailableDialog() {
	current := view.AppVersion()
	if current == "" {
		current = "2.4.46"
	}
	mw.ShowUpdateAvailableDialog("2.4.99", current, nil)
}

// UpdateProgress is a live handle to an in-flight update's progress dialog,
// returned by ShowUpdateProgressDialog — added because a self-update that
// silently sits there with no visible activity (as this looked before) is
// indistinguishable from having frozen. Every method is safe to call from
// any goroutine (they hop to the UI thread themselves via fyne.Do) and safe
// to call on a nil receiver, so callers never need to nil-check before
// using one (e.g. when the window wasn't available to show it against).
type UpdateProgress struct {
	inner *view.UpdateProgress
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
	// Callers (the confirm onResult) already run on the UI thread. Building
	// here avoids a fyne.Do race where the first download ticks land before
	// inner exists.
	return &UpdateProgress{inner: view.ShowUpdateProgressDialog(mw.window, version)}
}

// Update sets the progress bar's fraction from downloaded/total bytes —
// intended to be passed directly as internal/update's ProgressFunc.
func (up *UpdateProgress) Update(downloaded, total int64) {
	if up == nil || up.inner == nil || total <= 0 {
		return
	}
	fraction := float64(downloaded) / float64(total)
	fyne.Do(func() { up.inner.SetFraction(fraction) })
}

// Close dismisses the progress dialog. A successful desktop apply
// relaunches the whole app before this would ever run, but the Android and
// error paths need it to avoid leaving a stuck-looking dialog on screen.
func (up *UpdateProgress) Close() {
	if up == nil || up.inner == nil {
		return
	}
	fyne.Do(func() { up.inner.Close() })
}

// ShowUpdateFailedDialog surfaces a failed DownloadAndApply to the user.
// Without this, a failure after the progress dialog closes (e.g. the
// install directory wasn't writable) looked identical to nothing having
// happened at all — the download visibly ran, then silence, with only a
// log line nobody but a developer would see.
func (mw *MainWindow) ShowUpdateFailedDialog(err error) {
	if mw == nil || mw.window == nil || err == nil {
		return
	}
	fyne.Do(func() { view.ShowErrorDialog(err, mw.window) })
}

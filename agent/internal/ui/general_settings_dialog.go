package ui

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge_agent/internal/remotelock"
)

const generalSettingsDialogWidth float32 = 360

// showGeneralSettingsDialog is Settings → General Settings: shared agent
// preferences. Auto-update is persisted on the engine (config.yaml) so a
// headless service honors it too.
func (w *Window) showGeneralSettingsDialog(parent fyne.Window) {
	if parent == nil {
		parent = w.guiWin
	}
	if parent == nil {
		return
	}

	enabled := true
	if w.token != nil {
		enabled = w.token.StreamerAutoUpdateEnabled()
	}

	var check *styledCheck
	check = newStyledCheck("", enabled, func(on bool) {
		if w.token == nil {
			return
		}
		if err := w.token.SetStreamerAutoUpdate(on); err != nil {
			logrus.WithError(err).Warn("could not save streamer auto-update")
			check.SetChecked(!on)
		}
	})
	rows := []fyne.CanvasObject{newPermToggleRow(loc().AgentAutoUpdate, check)}

	// Injected-input filter is Windows-only (SendInput sets INJECTED).
	if runtime.GOOS == "windows" {
		lockOn := false
		if w.token != nil {
			lockOn = w.token.RemoteWindowLockEnabled()
		}
		var lockCheck *styledCheck
		lockCheck = newStyledCheck("", lockOn, func(on bool) {
			if w.token == nil {
				return
			}
			if err := w.token.SetRemoteWindowLock(on); err != nil {
				logrus.WithError(err).Warn("could not save remote window lock")
				lockCheck.SetChecked(!on)
				return
			}
			remotelock.SetEnabled(on)
		})
		rows = append(rows, newPermToggleRow(loc().RemoteWindowLock, lockCheck))
	}

	body := container.New(&tightVBoxLayout{gap: 8}, rows...)

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	panel := newBrandedDialogPanelChrome(loc().GeneralSettings, loc().GeneralSettingsSubtitle, generalSettingsDialogWidth, 20, 0, body, nil, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

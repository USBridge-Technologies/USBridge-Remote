package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const generalSettingsDialogWidth float32 = 380

// Shared left/right inset so General Settings rows keep labels and
// checkboxes in one column.
const generalSettingsRowInset float32 = 8

// showGeneralSettingsDialog is Settings → General Settings: shared agent
// preferences. Auto-update is persisted on the engine (config.yaml) so a
// headless service honors it too.
//
// The remote-window-lock toggle is intentionally hidden for now (Linux
// path still needs polish before release); config/API remain for later.
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
	body := container.New(&tightVBoxLayout{gap: 10},
		newExactInset(newPermToggleRow(loc().AgentAutoUpdate, check), generalSettingsRowInset, generalSettingsRowInset, 0, 0),
	)

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	panel := newBrandedDialogPanelChrome(loc().GeneralSettings, loc().GeneralSettingsSubtitle, generalSettingsDialogWidth, 20, 8, body, nil, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

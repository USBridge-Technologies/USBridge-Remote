package ui

import (
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge_agent/internal/remotelock"
	"usbridge_agent/internal/ui/design"
)

const generalSettingsDialogWidth float32 = 380

// Shared left/right inset so the auto-update row and the framed lock row
// keep labels and checkboxes in one column.
const generalSettingsRowInset float32 = 8

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
	rows := []fyne.CanvasObject{newExactInset(newPermToggleRow(loc().AgentAutoUpdate, check), generalSettingsRowInset, generalSettingsRowInset, 0, 0)}

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
		if on && runtime.GOOS == "darwin" && !remotelock.HookInstalled() {
			logrus.Warn("remote window lock needs Accessibility for this agent")
			lockCheck.SetChecked(false)
			if err := w.token.SetRemoteWindowLock(false); err != nil {
				logrus.WithError(err).Warn("could not revert remote window lock")
			}
		}
	})
	hint := widget.NewLabel(loc().RemoteWindowLockHint)
	hint.Wrapping = fyne.TextWrapWord
	hint.Alignment = fyne.TextAlignLeading
	lockInner := container.New(&tightVBoxLayout{gap: 4},
		newPermToggleRow(loc().RemoteWindowLock, lockCheck),
		wrapDialogLabel(hint, 8, design.ColorMutedOlive),
	)
	rows = append(rows, wrapGeneralSettingsLock(lockInner))

	body := container.New(&tightVBoxLayout{gap: 10}, rows...)

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	panel := newBrandedDialogPanelChrome(loc().GeneralSettings, loc().GeneralSettingsSubtitle, generalSettingsDialogWidth, 20, 8, body, nil, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func wrapGeneralSettingsLock(inner fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, newExactInset(inner, generalSettingsRowInset, generalSettingsRowInset, 8, 8))
}

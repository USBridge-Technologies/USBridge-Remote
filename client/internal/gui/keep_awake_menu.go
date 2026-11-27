package gui

import (
	"fmt"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
)

// Keep awake (controller/keep_awake.go): the Control footer's cup icon opens
// a menu of two switches -- move the mouse, press Shift -- that keep the
// host from going idle while nobody uses the mouse or keyboard here, and how
// long the idle wait is. Kept across restarts; the icon turns teal while a
// switch is on.
const (
	keepAwakeMousePref = "keep_awake_mouse"
	keepAwakeShiftPref = "keep_awake_shift"
	keepAwakeEveryPref = "keep_awake_every_s"
)

func loadKeepAwake(prefs fyne.Preferences) {
	every := time.Duration(prefs.IntWithFallback(keepAwakeEveryPref, int(controller.DefaultKeepAwakeEvery/time.Second))) * time.Second
	controller.SetKeepAwake(prefs.BoolWithFallback(keepAwakeMousePref, false), prefs.BoolWithFallback(keepAwakeShiftPref, false), every)
}

func (mw *MainWindow) setKeepAwake(mouse, shift bool, every time.Duration) {
	prefs := mw.app.Preferences()
	prefs.SetBool(keepAwakeMousePref, mouse)
	prefs.SetBool(keepAwakeShiftPref, shift)
	prefs.SetInt(keepAwakeEveryPref, int(every/time.Second))
	controller.SetKeepAwake(mouse, shift, every)
	mw.refreshKeepAwakeIcon()
}

// refreshKeepAwakeIcon shows whether keep-awake is on.
func (mw *MainWindow) refreshKeepAwakeIcon() {
	if mw.keepAwakeIcon == nil {
		return
	}
	mouse, shift, _ := controller.KeepAwake()
	if mouse || shift {
		mw.keepAwakeIcon.SetIcon(assets.KeepAwakeIconOn)
		mw.keepAwakeIcon.SetHoverIcon(assets.KeepAwakeIconOn)
	} else {
		mw.keepAwakeIcon.SetIcon(assets.KeepAwakeIcon)
		mw.keepAwakeIcon.SetHoverIcon(assets.KeepAwakeIconFooterHover)
	}
	mw.keepAwakeIcon.Refresh()
}

func keepAwakeIntervalLabel(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf(i18n.Current.KeepAwakeAfterSeconds, int(d/time.Second))
	}
	return fmt.Sprintf(i18n.Current.KeepAwakeAfterMinutes, int(d/time.Minute))
}

// showKeepAwakeMenu: the two switches, then the idle wait (one of
// controller.KeepAwakeIntervals).
func (mw *MainWindow) showKeepAwakeMenu() {
	if mw.keepAwakeIcon == nil {
		return
	}
	mouse, shift, every := controller.KeepAwake()
	items := []view.StyledMenuItem{
		{
			Label:    i18n.Current.KeepAwakeMouse,
			Selected: mouse,
			OnTap:    func() { mw.setKeepAwake(!mouse, shift, every) },
		},
		{
			Label:    i18n.Current.KeepAwakeShift,
			Selected: shift,
			OnTap:    func() { mw.setKeepAwake(mouse, !shift, every) },
		},
	}
	for _, d := range controller.KeepAwakeIntervals {
		items = append(items, view.StyledMenuItem{
			Label:    keepAwakeIntervalLabel(d),
			Selected: d == every,
			OnTap:    func() { mw.setKeepAwake(mouse, shift, d) },
		})
	}
	view.ShowStyledMenuTealAbove(mw.keepAwakeIcon, items)
}

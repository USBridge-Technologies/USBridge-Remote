package view

import (
	"fyne.io/fyne/v2"

	"usbridge-client/internal/service"
)

const scrollSpeedLevelPrefKey = "scroll_speed_level"

// RestoreScrollSpeedPreference loads the two-finger scroll speed. Safe to
// call with no fyne app (tests): it is then a no-op.
func RestoreScrollSpeedPreference() {
	app := fyne.CurrentApp()
	if app == nil || app.Preferences() == nil {
		return
	}
	service.SetScrollSpeedLevel(app.Preferences().IntWithFallback(scrollSpeedLevelPrefKey, service.ScrollSpeedDefaultLevel))
}

// SaveScrollSpeedPreference stores the level the dialog just applied.
func SaveScrollSpeedPreference(level int) {
	app := fyne.CurrentApp()
	if app == nil || app.Preferences() == nil {
		return
	}
	app.Preferences().SetInt(scrollSpeedLevelPrefKey, level)
}

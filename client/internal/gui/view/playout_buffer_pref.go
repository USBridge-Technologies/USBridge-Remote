package view

import (
	"fyne.io/fyne/v2"

	"usbridge-client/internal/service"
)

const playoutBufferLevelPrefKey = "playout_buffer_level"

// RestorePlayoutBufferPreference loads the last latency/smoothness slider
// level before the first stream. Safe to call with no fyne app (tests): it
// is then a no-op.
func RestorePlayoutBufferPreference() {
	app := fyne.CurrentApp()
	if app == nil || app.Preferences() == nil {
		return
	}
	service.SetPlayoutBufferLevel(app.Preferences().IntWithFallback(playoutBufferLevelPrefKey, service.PlayoutBufferLevel()))
}

// SavePlayoutBufferPreference stores the level the dialog just applied.
func SavePlayoutBufferPreference(level int) {
	app := fyne.CurrentApp()
	if app == nil || app.Preferences() == nil {
		return
	}
	app.Preferences().SetInt(playoutBufferLevelPrefKey, level)
}

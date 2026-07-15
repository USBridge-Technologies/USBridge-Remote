//go:build windows

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
)

// На Windows не используем app.NewWithID, чтобы не восстанавливать потенциально
// "битые" сохранённые координаты окна ( off‑screen / невидимое окно ).
// Но без ID не будут сохраняться настройки, поэтому мы обязаны использовать NewWithID.
func newFyneApp() fyne.App {
	a := app.NewWithID("com.usbridge.client")
	a.SetIcon(assets.AppIcon)
	a.Settings().SetTheme(design.NewBrandTheme())
	return a
}

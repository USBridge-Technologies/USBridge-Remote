//go:build windows

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
)

// On Windows we don't use app.NewWithID, to avoid restoring potentially
// "broken" saved window coordinates (off-screen / invisible window).
// But without an ID, settings won't be persisted, so we have to use NewWithID.
func newFyneApp() fyne.App {
	a := app.NewWithID("com.usbridge.client")
	a.SetIcon(assets.AppIcon)
	a.Settings().SetTheme(design.NewBrandTheme())
	return a
}

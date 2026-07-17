//go:build ios || android
// +build ios android

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
)

// newFyneApp creates a Fyne app with a persistent ID (mobile),
// so that preferences and other things work.
//
// On mobile, the platform driver may already have registered fyne.CurrentApp()
// by the time this runs (e.g. on app resume), so reuse it instead of creating
// a second instance.
func newFyneApp() fyne.App {
	if a := fyne.CurrentApp(); a != nil {
		return a
	}
	a := app.NewWithID("usbridge-client")
	a.SetIcon(assets.AppIcon)
	a.Settings().SetTheme(design.NewBrandTheme())
	return a
}

//go:build !windows && !ios && !android
// +build !windows,!ios,!android

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
)

// newFyneApp creates a Fyne app with a persistent ID (desktop),
// so that preferences and other things work.
//
// Unlike the mobile variant (app_mobile.go), this always creates a fresh
// app rather than reusing fyne.CurrentApp(): on desktop, fyne.io/fyne/v2/test
// gets linked in transitively (glfw driver -> driver/software -> test), and
// that package's init() unconditionally registers a dummy test app as
// CurrentApp. Reusing CurrentApp() here would silently pick up that fake
// app instead of a real window driver, so ShowAndRun() returns instantly
// with no window ever appearing.
func newFyneApp() fyne.App {
	a := app.NewWithID("usbridge-client")
	a.SetIcon(assets.AppIcon)
	a.Settings().SetTheme(design.NewBrandTheme())
	return a
}

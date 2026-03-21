//go:build !windows
// +build !windows

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"usbridge-client/internal/gui/design"
)

// newFyneApp создаёт приложение Fyne с постоянным ID ( desktop / mobile ),
// чтобы работали предпочтения и прочее.
func newFyneApp() fyne.App {
	a := app.NewWithID("usbridge-client")
	a.Settings().SetTheme(design.NewBrandTheme())
	return a
}

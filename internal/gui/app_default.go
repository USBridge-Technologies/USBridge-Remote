//go:build !windows
// +build !windows

package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
)

// newFyneApp создаёт приложение Fyne с постоянным ID ( desktop / mobile ),
// чтобы работали предпочтения и прочее.
func newFyneApp() fyne.App {
	return app.NewWithID("usbridge-client")
}

package app

import (
	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
)

// newFyneApp creates the agent's Fyne app, declaring the fyneDo migration:
// the agent touches its UI only from the Fyne goroutine (fyne.Do), so
// Fyne's per-call "is this the main goroutine?" check is pure overhead --
// it captures a stack trace (runtime.Stack) for every canvas refresh, and
// every Rectangle/Text resize refreshes. In the agent's deep card layouts
// that was ~75% of the CPU of a window resize, which on Wayland (where the
// compositor waits for the client to redraw at the new size) made dragging
// the window edge visibly lag. The only off-goroutine calls the check ever
// logged were status refreshes landing after the main loop had stopped,
// on shutdown -- harmless either way.
func newFyneApp() fyne.App {
	id := fyneAppID()
	fyneapp.SetMetadata(fyne.AppMetadata{
		ID:         id,
		Name:       "USBridge Agent",
		Migrations: map[string]bool{"fyneDo": true},
	})
	return fyneapp.NewWithID(id)
}

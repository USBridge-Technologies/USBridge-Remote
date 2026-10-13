//go:build windows

package app

import (
	"image"

	"usbridge_agent/internal/input"
)

// wireStreamedBounds makes the absolute mouse scale over the monitor the
// streamer captures instead of display 0. Reads a.screen lazily so a backend
// swap (SetStreamBackend) is picked up.
func (a *App) wireStreamedBounds() {
	input.SetStreamedBoundsProvider(func() (image.Rectangle, bool) {
		if a.screen == nil {
			return image.Rectangle{}, false
		}
		return a.screen.StreamedBounds()
	})
}

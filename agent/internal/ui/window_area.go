package ui

import "fyne.io/fyne/v2"

// fitWindowSizeToScreen keeps a restored / default size inside the screen
// the window opens on (see availableWindowArea).
func fitWindowSizeToScreen(win fyne.Window, size fyne.Size) fyne.Size {
	if win == nil {
		return size
	}
	area := availableWindowArea(win.Canvas())
	if area.Width > 0 && size.Width > area.Width {
		size.Width = area.Width
	}
	if area.Height > 0 && size.Height > area.Height {
		size.Height = area.Height
	}
	return size
}

package view

import (
	"sync/atomic"

	"fyne.io/fyne/v2"
)

// CompactLayoutWidth is the canvas-width breakpoint below which the app
// switches to its compact/mobile layout (narrower dialogs, top-anchored
// popups, footer QR row, etc.) regardless of what device it thinks it's
// running on. Matches common small-viewport breakpoints (Material Design's
// "compact" window class cuts off at 600dp).
const CompactLayoutWidth float32 = 600

// IsMobile is true on a real phone/tablet, or when ForceMobileDesign is on
// so the desktop build can preview the mobile layout.
func IsMobile() bool {
	return ForceMobileDesign || fyne.CurrentDevice().IsMobile()
}

// landscapeCanvas is 1 when the last noted window canvas is wider than tall.
// Updated from MainWindow's resize guard so connected chrome can reflow
// without a full UI reload.
var landscapeCanvas atomic.Bool

// IsLandscape is true when the window canvas is wider than it is tall
// (phone rotated, or a landscape phone-preview frame).
func IsLandscape() bool {
	return landscapeCanvas.Load()
}

// NoteCanvasSize records the latest window/canvas size for IsLandscape.
// Returns whether the landscape bit changed.
func NoteCanvasSize(size fyne.Size) (changed bool) {
	next := size.Width > size.Height && size.Width > 0 && size.Height > 0
	prev := landscapeCanvas.Swap(next)
	return prev != next
}

// UseCompactLayout reports whether the UI should use its compact/mobile
// layout: ForceMobileDesign, a real mobile device, or a narrow canvas
// (below CompactLayoutWidth). Canvas width still matters for a desktop
// browser resized small, where Fyne's wasm IsMobile() is a one-time
// user-agent sniff and never updates.
func UseCompactLayout(canvasWidth float32) bool {
	return IsMobile() || canvasWidth < CompactLayoutWidth
}

package view

import "fyne.io/fyne/v2"

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

// UseCompactLayout reports whether the UI should use its compact/mobile
// layout: ForceMobileDesign, a real mobile device, or a narrow canvas
// (below CompactLayoutWidth). Canvas width still matters for a desktop
// browser resized small, where Fyne's wasm IsMobile() is a one-time
// user-agent sniff and never updates.
func UseCompactLayout(canvasWidth float32) bool {
	return IsMobile() || canvasWidth < CompactLayoutWidth
}

package view

import "fyne.io/fyne/v2"

// CompactLayoutWidth is the canvas-width breakpoint below which the app
// switches to its compact/mobile layout (narrower dialogs, top-anchored
// popups, footer QR row, etc.) regardless of what device it thinks it's
// running on. Matches common small-viewport breakpoints (Material Design's
// "compact" window class cuts off at 600dp).
const CompactLayoutWidth float32 = 600

// UseCompactLayout reports whether the UI should use its compact/mobile
// layout: true on an actual mobile device (fyne.CurrentDevice().IsMobile(),
// which is what every existing IsMobile()-gated call site already checks),
// OR whenever the available canvas width is narrow — regardless of device
// class.
//
// The device check alone isn't enough for the browser build: Fyne's wasm
// driver derives IsMobile() from a one-time user-agent sniff at process
// start (see fyne's internal/driver/glfw/device_wasm.go), so it's accurate
// for a phone's browser but never updates for a desktop browser window that
// gets resized narrow, or (the inverse gap) a phone browser opened in
// desktop-site/landscape-tablet mode. Checking canvas width directly covers
// both: it's the actual signal the layout code needs ("do I have room for
// the wide layout"), device class is just a proxy for it that happens to be
// right most of the time on native builds where window width tracks
// physical screen size much more closely.
func UseCompactLayout(canvasWidth float32) bool {
	return fyne.CurrentDevice().IsMobile() || canvasWidth < CompactLayoutWidth
}

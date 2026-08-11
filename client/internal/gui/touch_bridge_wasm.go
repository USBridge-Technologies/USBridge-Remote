//go:build js && wasm

package gui

import "usbridge-client/internal/gui/controller"

// InitTouchGestureBridge attaches the browser's own raw touchstart/
// touchmove/touchend listeners that drive VideoWidget's mouse/pan/zoom
// handling under wasm -- see controller.InitTouchGestureBridge's own doc
// comment (video_gestures_wasm.go) for why this exists at all: Fyne's
// wasm driver never dispatches real multi-touch gestures, and browsers
// don't synthesize mouse events from them either, so without this bridge
// no touch/swipe/pinch input reaches the video widget at all. Exported
// from package controller (not implemented here directly) since it needs
// VideoWidget/TouchpadWrapper's own unexported fields and methods.
func InitTouchGestureBridge() {
	controller.InitTouchGestureBridge()
}

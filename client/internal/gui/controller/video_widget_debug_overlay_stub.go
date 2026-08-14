//go:build !(js && wasm)

// Non-wasm platforms have no on-screen HUD to update -- see
// video_widget_debug_overlay_wasm.go's own doc comment for why this
// exists at all.
package controller

func (vw *VideoWidget) debugLogViewport(tag string) {}

func (vw *VideoWidget) debugLogGesture(scaleFactor, focusX, focusY, panDx, panDy float32) {}

func (vw *VideoWidget) debugLogSpinner(tag string) {}

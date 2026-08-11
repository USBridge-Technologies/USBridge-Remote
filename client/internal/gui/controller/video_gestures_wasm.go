//go:build js && wasm

// The browser counterpart of video_gestures_android.go/video_gestures_ios.go:
// those drive VideoWidget's touch/gesture handling from a real native
// platform gesture recognizer (Android's ScaleGestureDetector/
// GestureDetector, iOS's UIPinchGestureRecognizer, delivered via cgo).
// There is no such thing under GOOS=js -- Fyne's own wasm driver
// (fyne-io/glfw-js) only ever dispatches desktop-style events synthesized
// from the browser's own mouse-event compatibility layer (mousedown/
// mousemove/mouseup), which covers simple single-finger taps on a
// touchscreen but never fires for genuine multi-touch gestures at all
// (browsers don't synthesize mouse events from 2-finger touches, and
// there is no touchstart/touchmove/touchend listening anywhere in Fyne's
// wasm driver to translate them into its own mobile.Touchable/Draggable
// dispatch either) -- confirmed live: with zero code here, not a single
// input message ever reached the server from a real phone browser
// session, for taps or swipes alike.
//
// Earlier attempts attached these listeners directly to Fyne's shared
// <canvas> element and filtered in Go (bounds-checking against the video
// wrapper's on-screen rect, later also requiring vw.IsStreaming()).
// Confirmed live, twice, on a real device: that approach kept breaking
// ordinary taps everywhere on the page, including the connection manager
// screen before any video session existed -- because *any* non-passive
// touchstart listener on the same element Fyne's own click synthesis
// depends on is enough to interfere with it, independent of whether our
// handler actually claims (preventDefault's) the event or not, and
// independent of whatever Go-side bug might also be at fault.
//
// This version instead creates its own transparent DOM overlay <div>,
// entirely separate from the canvas, sized and positioned (via inline
// CSS) to exactly cover the video widget's on-screen rect only while a
// session is actually streaming -- otherwise it's 0x0 and
// pointer-events:none. Touch listeners live on *that* element. Because
// browsers hit-test DOM elements natively, a tap on the Connect button or
// any other UI chrel simply never reaches this element at all: there is
// no Go-side filtering left to get wrong, and the canvas itself never has
// a competing listener on it.
package controller

import (
	"math"
	"strconv"
	"syscall/js"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/mobile"
	"github.com/sirupsen/logrus"
)

// recoverTouchPanic must be deferred at the top of every js.FuncOf callback
// in this file. An unrecovered panic inside a js.FuncOf callback is fatal
// to the *entire* wasm program, not just that one call -- Go's wasm runtime
// has no isolation between the goroutine a JS callback runs on and the rest
// of the program, so a single bad touch event anywhere (a nil pointer off
// a widget that isn't laid out yet, a stale wrapper mid-teardown, etc.)
// silently kills every future callback, including totally unrelated ones
// (button clicks, taps on other screens). Recovering here trades a crashed
// app for a dropped gesture.
func recoverTouchPanic(where string) {
	if r := recover(); r != nil {
		logrus.Errorf("🖐️ recovered panic in %s: %v", where, r)
	}
}

// webTouchState tracks the handful of values needed to turn a raw
// touchstart/touchmove/touchend sequence into the same
// TouchDown/Dragged/TouchUp (single finger) or
// applyViewportGesture/scroll (two fingers) calls the native gesture
// bridges make. Package-level, not per-VideoWidget: there's only one
// live touch surface in a wasm build (a single browser tab/overlay), same
// assumption activeMobileGestureTarget already makes.
var webTouch struct {
	activeID  int
	haveTouch bool
	lastX     float32
	lastY     float32

	twoFinger bool
	prevDist  float32
	prevMidX  float32
	prevMidY  float32
}

// touchOverlayEl is the lazily-created transparent <div> touch listeners
// are attached to -- see this file's top doc comment for why it exists
// instead of listening on Fyne's own canvas.
var touchOverlayEl js.Value

// InitTouchGestureBridge creates the touch overlay div, attaches listeners
// to it, and starts the geometry-sync loop that keeps it positioned over
// the video widget (and only over the video widget) while streaming.
// Called once at wasm startup (see cmd/wasm/main.go), same convention as
// InitIMEBridge.
func InitTouchGestureBridge() {
	doc := js.Global().Get("document")
	body := doc.Get("body")
	if body.IsNull() || body.IsUndefined() {
		js.Global().Call("setTimeout", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			defer recoverTouchPanic("InitTouchGestureBridge retry")
			InitTouchGestureBridge()
			return nil
		}), 200)
		return
	}

	el := doc.Call("createElement", "div")
	el.Set("id", "usbridge-touch-overlay")
	style := el.Get("style")
	style.Set("position", "fixed")
	style.Set("left", "0px")
	style.Set("top", "0px")
	style.Set("width", "0px")
	style.Set("height", "0px")
	style.Set("pointerEvents", "none")
	style.Set("touchAction", "none")
	style.Set("zIndex", "10")
	style.Set("background", "transparent")
	body.Call("appendChild", el)
	touchOverlayEl = el

	el.Call("addEventListener", "touchstart", js.FuncOf(onTouchStart), map[string]interface{}{"passive": false})
	el.Call("addEventListener", "touchmove", js.FuncOf(onTouchMove), map[string]interface{}{"passive": false})
	el.Call("addEventListener", "touchend", js.FuncOf(onTouchEnd), map[string]interface{}{"passive": false})
	el.Call("addEventListener", "touchcancel", js.FuncOf(onTouchEnd), map[string]interface{}{"passive": false})

	js.Global().Call("setInterval", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer recoverTouchPanic("syncTouchOverlay")
		syncTouchOverlay()
		// Also keep the virtual-cursor dot (video_widget_cursor_wasm.go)
		// positioned at rest, not just while actively dragging -- e.g.
		// right after switching into cursor mode, before any drag has
		// happened yet.
		syncCursorDot(activeGestureVideoWidget())
		return nil
	}), 150)
}

// syncTouchOverlay repositions/resizes the overlay to exactly match the
// active viewport wrapper's on-screen rect while a session is actually
// streaming, and collapses it to nothing (0x0, pointer-events:none)
// otherwise -- the only thing keeping this element from ever intercepting
// a tap meant for the rest of the UI.
func syncTouchOverlay() {
	if touchOverlayEl.IsUndefined() || touchOverlayEl.IsNull() {
		return
	}
	style := touchOverlayEl.Get("style")

	vw := activeGestureVideoWidget()
	if vw == nil || !vw.IsStreaming() {
		style.Set("pointerEvents", "none")
		style.Set("width", "0px")
		style.Set("height", "0px")
		return
	}
	wrapper := vw.activeViewportWrapper()
	if wrapper == nil || !wrapper.Visible() {
		style.Set("pointerEvents", "none")
		style.Set("width", "0px")
		style.Set("height", "0px")
		return
	}
	size := wrapper.Size()
	if size.Width <= 0 || size.Height <= 0 {
		style.Set("pointerEvents", "none")
		style.Set("width", "0px")
		style.Set("height", "0px")
		return
	}
	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
	style.Set("left", pxf(abs.X))
	style.Set("top", pxf(abs.Y))
	style.Set("width", pxf(size.Width))
	style.Set("height", pxf(size.Height))
	style.Set("pointerEvents", "auto")
}

// pxf formats a dp value as a CSS px string; sub-pixel precision doesn't
// matter for a touch hit-test overlay.
func pxf(v float32) string {
	return strconv.Itoa(int(v)) + "px"
}

// jsTouch reads one JS Touch object's page coordinates -- clientX/clientY
// are already in CSS pixels, the same space Fyne's own dp coordinates use
// under wasm.
func jsTouch(t js.Value) (id int, x, y float32) {
	return t.Get("identifier").Int(), float32(t.Get("clientX").Float()), float32(t.Get("clientY").Float())
}

// wrapperLocalPos converts page coordinates into the active viewport
// wrapper's own local coordinate space, matching what Fyne's real pointer
// dispatch would have handed TouchDown/Dragged directly.
func wrapperLocalPos(vw *VideoWidget, pageX, pageY float32) (fyne.Position, bool) {
	wrapper := vw.activeViewportWrapper()
	if wrapper == nil {
		return fyne.Position{}, false
	}
	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
	return fyne.NewPos(pageX-abs.X, pageY-abs.Y), true
}

func onTouchStart(this js.Value, args []js.Value) interface{} {
	defer recoverTouchPanic("onTouchStart")
	event := args[0]
	touches := event.Get("touches")
	count := touches.Length()
	if count == 0 {
		return nil
	}

	vw := activeGestureVideoWidget()
	if vw == nil {
		return nil
	}
	// The overlay only exists (pointer-events:auto, nonzero size) over the
	// video rect while streaming -- see syncTouchOverlay -- so reaching
	// this handler at all already means the touch is where it should be.
	// preventDefault stops the browser's own scroll/zoom/refresh gestures
	// on this element; safe to call unconditionally here since nothing
	// outside the video area can ever dispatch through this listener.
	event.Call("preventDefault")

	t0 := touches.Index(0)
	_, x0, y0 := jsTouch(t0)

	if count >= 2 {
		if webTouch.haveTouch {
			webTouch.haveTouch = false
		}
		vw.multiTouchActive = true
		vw.lastMultiTouchAt = time.Now()
		vw.cancelLocalTouchState()

		t1 := touches.Index(1)
		_, x1, y1 := jsTouch(t1)
		webTouch.twoFinger = true
		webTouch.prevDist = dist(x0, y0, x1, y1)
		webTouch.prevMidX = (x0 + x1) / 2
		webTouch.prevMidY = (y0 + y1) / 2
		return nil
	}

	id, _, _ := jsTouch(t0)
	local, ok := wrapperLocalPos(vw, x0, y0)
	if !ok {
		return nil
	}
	webTouch.activeID = id
	webTouch.haveTouch = true
	webTouch.lastX = local.X
	webTouch.lastY = local.Y

	wrapper := vw.activeViewportWrapper()
	if wrapper != nil {
		fyne.Do(func() {
			wrapper.TouchDown(&mobile.TouchEvent{PointEvent: fyne.PointEvent{Position: local, AbsolutePosition: fyne.NewPos(x0, y0)}})
			vw.updateNativeViewportAndCursor()
		})
	}
	return nil
}

func onTouchMove(this js.Value, args []js.Value) interface{} {
	defer recoverTouchPanic("onTouchMove")
	event := args[0]
	touches := event.Get("touches")
	count := touches.Length()

	vw := activeGestureVideoWidget()
	if vw == nil {
		return nil
	}
	event.Call("preventDefault")

	if count >= 2 {
		t0 := touches.Index(0)
		t1 := touches.Index(1)
		_, x0, y0 := jsTouch(t0)
		_, x1, y1 := jsTouch(t1)
		curDist := dist(x0, y0, x1, y1)
		curMidX := (x0 + x1) / 2
		curMidY := (y0 + y1) / 2

		if !webTouch.twoFinger || webTouch.prevDist <= 0 {
			webTouch.twoFinger = true
			webTouch.prevDist = curDist
			webTouch.prevMidX = curMidX
			webTouch.prevMidY = curMidY
			return nil
		}

		scaleFactor := float32(1)
		if webTouch.prevDist > 0 {
			scaleFactor = curDist / webTouch.prevDist
		}
		panDx := curMidX - webTouch.prevMidX
		panDy := curMidY - webTouch.prevMidY
		webTouch.prevDist = curDist
		webTouch.prevMidX = curMidX
		webTouch.prevMidY = curMidY

		wrapper := vw.activeViewportWrapper()
		if wrapper == nil {
			return nil
		}
		wrapperSize := wrapper.Size()
		if wrapperSize.Width <= 0 || wrapperSize.Height <= 0 {
			return nil
		}
		local, ok := wrapperLocalPos(vw, curMidX, curMidY)
		if !ok {
			return nil
		}

		fyne.Do(func() {
			vw.UpdateTouchpadAndContentRect(wrapperSize.Width, wrapperSize.Height, vw.GetCurrentFrame())
			vw.applyViewportGesture(scaleFactor, local.X, local.Y, panDx, panDy)
			vw.updateNativeViewportAndCursor()
			vw.refreshViewportViews()
		})
		return nil
	}

	if webTouch.twoFinger || !webTouch.haveTouch {
		return nil
	}
	t := touches.Index(0)
	id, x, y := jsTouch(t)
	if id != webTouch.activeID {
		return nil
	}
	local, ok := wrapperLocalPos(vw, x, y)
	if !ok {
		return nil
	}
	dx := local.X - webTouch.lastX
	dy := local.Y - webTouch.lastY
	webTouch.lastX = local.X
	webTouch.lastY = local.Y
	if dx == 0 && dy == 0 {
		return nil
	}

	wrapper := vw.activeViewportWrapper()
	if wrapper != nil {
		fyne.Do(func() {
			wrapper.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: local, AbsolutePosition: fyne.NewPos(x, y)}, Dragged: fyne.Delta{DX: dx, DY: dy}})
			// Dragged() (video_mouse_handler.go) updates
			// vw.virtualCursorU/V synchronously in cursor mode via
			// handleVirtualCursorMove, but nothing in that call chain
			// refreshes the on-screen dot or re-centers the viewport --
			// on Android that happens because the Vulkan render thread
			// polls the shared cursor state every frame on its own.
			// Without an equivalent render loop here, the dot/viewport
			// were only ever catching up on video_gestures_wasm.go's
			// 150ms poll tick, which reads as a stutter: a 60ms glide to
			// the last known position, then a ~90ms stall waiting for
			// the next tick, repeating for as long as the finger moves.
			// Driving it from every touchmove instead removes that
			// stall -- this now updates at the same rate the browser
			// delivers touch events, same as Android's Vulkan thread
			// updates every render frame.
			vw.updateNativeViewportAndCursor()
		})
	}
	return nil
}

func onTouchEnd(this js.Value, args []js.Value) interface{} {
	defer recoverTouchPanic("onTouchEnd")
	event := args[0]
	remaining := event.Get("touches").Length()

	vw := activeGestureVideoWidget()
	if vw == nil {
		return nil
	}
	event.Call("preventDefault")

	if webTouch.twoFinger {
		if remaining < 2 {
			webTouch.twoFinger = false
			vw.multiTouchActive = false
			vw.lastMultiTouchAt = time.Now()
			vw.cancelLocalTouchState()
		}
		return nil
	}

	if webTouch.haveTouch && remaining == 0 {
		webTouch.haveTouch = false
		wrapper := vw.activeViewportWrapper()
		if wrapper != nil {
			local := fyne.NewPos(webTouch.lastX, webTouch.lastY)
			wrapper.TouchUp(&mobile.TouchEvent{PointEvent: fyne.PointEvent{Position: local, AbsolutePosition: local}})
		}
	}
	return nil
}

func dist(x0, y0, x1, y1 float32) float32 {
	dx := float64(x1 - x0)
	dy := float64(y1 - y0)
	return float32(math.Sqrt(dx*dx + dy*dy))
}

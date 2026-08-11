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
// This file attaches its own raw touchstart/touchmove/touchend/touchcancel
// listeners directly to the canvas element via syscall/js, entirely
// bypassing Fyne's own event dispatch, and drives exactly the same
// TouchpadWrapper (mobile.Touchable/fyne.Draggable) methods and
// VideoWidget.applyViewportGesture the native platforms' own gesture
// bridges do -- same target API, same semantics, different (and for this
// platform, the only available) event source.
package controller

import (
	"math"
	"syscall/js"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/mobile"
)

// webTouchState tracks the handful of values needed to turn a raw
// touchstart/touchmove/touchend sequence into the same
// TouchDown/Dragged/TouchUp (single finger) or
// applyViewportGesture/scroll (two fingers) calls the native gesture
// bridges make. Package-level, not per-VideoWidget: there's only one
// live touch surface in a wasm build (a single browser tab/canvas), same
// assumption activeMobileGestureTarget already makes.
var webTouch struct {
	// Single-finger drag tracking (browser touch identifier -> last
	// position), mirroring what TouchDown/Dragged need to compute deltas
	// between consecutive move events -- Fyne's own DragEvent carries a
	// delta, not an absolute position, unlike TouchEvent.
	activeID  int
	haveTouch bool
	lastX     float32
	lastY     float32

	// Two-finger pinch/pan tracking, mirroring the Android/iOS native
	// recognizers' own running state between gesture update callbacks.
	twoFinger bool
	prevDist  float32
	prevMidX  float32
	prevMidY  float32
}

// InitTouchGestureBridge attaches the real DOM touch listeners. Called
// once at wasm startup (see cmd/wasm/main.go), same convention as
// InitIMEBridge.
func InitTouchGestureBridge() {
	doc := js.Global().Get("document")
	canvasEl := doc.Call("querySelector", "canvas")
	if canvasEl.IsNull() || canvasEl.IsUndefined() {
		// Fyne creates its single canvas element lazily on window creation
		// (fyne-io/glfw-js's CreateWindow) -- if this runs before that,
		// retry shortly rather than silently never attaching at all.
		js.Global().Call("setTimeout", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			InitTouchGestureBridge()
			return nil
		}), 200)
		return
	}

	canvasEl.Call("addEventListener", "touchstart", js.FuncOf(onTouchStart), map[string]interface{}{"passive": false})
	canvasEl.Call("addEventListener", "touchmove", js.FuncOf(onTouchMove), map[string]interface{}{"passive": false})
	canvasEl.Call("addEventListener", "touchend", js.FuncOf(onTouchEnd), map[string]interface{}{"passive": false})
	canvasEl.Call("addEventListener", "touchcancel", js.FuncOf(onTouchEnd), map[string]interface{}{"passive": false})
}

// jsTouch reads one JS Touch object's page coordinates -- clientX/clientY
// are already in CSS pixels, the same space Fyne's own dp coordinates use
// under wasm (unlike Android's raw MotionEvent, which reports physical
// pixels and needs dividing by canvas.Scale() -- see
// deliverViewportGestureUpdateFromJNI's doc comment for that platform's
// equivalent conversion, not needed here).
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

// touchStartsInsideWrapper reports whether (pageX, pageY) falls within the
// active viewport wrapper's actual on-screen rect. Required in
// onTouchStart before claiming a touch at all: activeGestureVideoWidget()
// returns non-nil from the moment createInterface() runs (app startup,
// see platformRegisterGestureTarget's own doc comment), not just while a
// session is connected -- and this listener is attached to the single
// canvas element covering the *entire* app, not scoped to the video area.
// Without this check every touch anywhere (the Connect button, tabs,
// dialogs) was being swallowed via preventDefault and misrouted into
// TouchDown/Dragged on the touchpad wrapper instead of reaching its real
// target -- confirmed live: after adding the raw touch bridge, tapping the
// Connect button (well outside any video widget, since no session was
// even active yet) stopped working entirely.
func touchStartsInsideWrapper(vw *VideoWidget, pageX, pageY float32) bool {
	wrapper := vw.activeViewportWrapper()
	if wrapper == nil {
		return false
	}
	size := wrapper.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return false
	}
	abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(wrapper)
	return pageX >= abs.X && pageX < abs.X+size.Width && pageY >= abs.Y && pageY < abs.Y+size.Height
}

func onTouchStart(this js.Value, args []js.Value) interface{} {
	event := args[0]
	touches := event.Get("touches")
	count := touches.Length()

	vw := activeGestureVideoWidget()
	if vw == nil {
		return nil
	}
	t0 := touches.Index(0)
	_, x0check, y0check := jsTouch(t0)
	if !touchStartsInsideWrapper(vw, x0check, y0check) {
		// Outside the video widget's actual rect (a UI button, tab, or
		// dialog elsewhere on the page) -- don't claim it, see
		// touchStartsInsideWrapper's own doc comment.
		return nil
	}
	// Only claim the event (preventDefault, stopping the browser's own
	// scroll/zoom/refresh gestures on this element) once we know it's
	// actually going to a live session -- an unclaimed touch still reaches
	// any real DOM controls (buttons, dialogs) normally.
	event.Call("preventDefault")

	if count >= 2 {
		// Transitioning into (or continuing) a multi-touch gesture --
		// cancel any single-finger state in progress, matching
		// deliverViewportGestureStateFromJNI(active=true)'s own doc
		// comment on why.
		if webTouch.haveTouch {
			webTouch.haveTouch = false
		}
		vw.multiTouchActive = true
		vw.lastMultiTouchAt = time.Now()
		vw.cancelLocalTouchState()

		t1 := touches.Index(1)
		_, x1, y1 := jsTouch(t1)
		webTouch.twoFinger = true
		webTouch.prevDist = dist(x0check, y0check, x1, y1)
		webTouch.prevMidX = (x0check + x1) / 2
		webTouch.prevMidY = (y0check + y1) / 2
		return nil
	}

	// First finger down, not (yet) a multi-touch gesture.
	id, _, _ := jsTouch(t0)
	x, y := x0check, y0check
	local, ok := wrapperLocalPos(vw, x, y)
	if !ok {
		return nil
	}
	webTouch.activeID = id
	webTouch.haveTouch = true
	webTouch.lastX = local.X
	webTouch.lastY = local.Y

	wrapper := vw.activeViewportWrapper()
	if wrapper != nil {
		wrapper.TouchDown(&mobile.TouchEvent{PointEvent: fyne.PointEvent{Position: local, AbsolutePosition: fyne.NewPos(x, y)}})
	}
	return nil
}

func onTouchMove(this js.Value, args []js.Value) interface{} {
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
			// Gained a second finger without going through onTouchStart's
			// own >=2 branch (e.g. a third finger landing while two were
			// already down) -- just (re)baseline instead of computing a
			// bogus scaleFactor off a zero/missing previous distance.
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

	// Single finger drag -- only meaningful if we're not mid-multi-touch
	// (a lifted-back-to-one-finger move after a pinch shouldn't suddenly
	// start dragging).
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
		wrapper.Dragged(&fyne.DragEvent{PointEvent: fyne.PointEvent{Position: local, AbsolutePosition: fyne.NewPos(x, y)}, Dragged: fyne.Delta{DX: dx, DY: dy}})
	}
	return nil
}

func onTouchEnd(this js.Value, args []js.Value) interface{} {
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

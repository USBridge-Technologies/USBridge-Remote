//go:build js && wasm

// Touch-to-scroll bridge for popup lists (header_dropdown.go's
// showStyledMenu -- the resolution/FPS pickers in video_start_dialog.go
// route through it too). Fyne's own wasm driver never translates a touch
// drag into a container.Scroll's Scrolled call or an offset drag the way
// native Android/iOS/desktop dispatch does (see video_gestures_wasm.go's
// package doc for the same underlying gap on the video widget itself, and
// why a listener can't just be added to Fyne's own shared <canvas> to fix
// it generally: any non-passive touchstart listener there is enough to
// break native tap/click synthesis for every unrelated element on the
// page). Confirmed live: a resolution list taller than its popup could be
// tapped (single-finger taps still worked, since those are covered by the
// browser's own touch-to-mouse-event compatibility shim) but never
// swiped -- no scrollbar drag, no touch-scroll, nothing.
//
// Mirrors video_gestures_wasm.go's own fix for the same class of problem:
// a small, separate DOM overlay <div> scoped to exactly the open popup's
// list rect (not Fyne's shared canvas), created when a scrollable popup
// opens and torn down when it closes.
package view

import (
	"strconv"
	"syscall/js"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"github.com/sirupsen/logrus"
)

// scrollTouchDragThreshold is how many CSS px of vertical movement a touch
// needs before it counts as a drag (scroll) rather than a tap (select
// row) -- see onScrollTouchEnd.
const scrollTouchDragThreshold = 8

// scrollTouchEl is the lazily-created, reused overlay div touch listeners
// are attached to -- see attachTouchScroll.
var scrollTouchEl js.Value

// scrollTouchState tracks the one active touch-scroll gesture. Package-
// level like video_gestures_wasm.go's webTouch: only one popup can
// meaningfully be open at a time under wasm (a single browser tab), so
// there's nothing to key by widget.
var scrollTouchState struct {
	scroll    *container.Scroll
	rows      []fyne.CanvasObject
	callbacks []func()
	startX    float32
	startY    float32
	lastY     float32
	dragging  bool
}

func ensureScrollTouchEl() {
	if !scrollTouchEl.IsUndefined() && !scrollTouchEl.IsNull() {
		return
	}
	doc := js.Global().Get("document")
	body := doc.Get("body")
	if body.IsNull() || body.IsUndefined() {
		return
	}

	el := doc.Call("createElement", "div")
	el.Set("id", "usbridge-popup-scroll-overlay")
	style := el.Get("style")
	style.Set("position", "fixed")
	style.Set("left", "0px")
	style.Set("top", "0px")
	style.Set("width", "0px")
	style.Set("height", "0px")
	style.Set("pointerEvents", "none")
	style.Set("touchAction", "none")
	style.Set("zIndex", "10001")
	style.Set("background", "transparent")
	body.Call("appendChild", el)
	scrollTouchEl = el

	el.Call("addEventListener", "touchstart", js.FuncOf(onScrollTouchStart), map[string]interface{}{"passive": false})
	el.Call("addEventListener", "touchmove", js.FuncOf(onScrollTouchMove), map[string]interface{}{"passive": false})
	el.Call("addEventListener", "touchend", js.FuncOf(onScrollTouchEnd), map[string]interface{}{"passive": false})
	el.Call("addEventListener", "touchcancel", js.FuncOf(onScrollTouchCancel), map[string]interface{}{"passive": false})
}

// attachTouchScroll positions a transparent touch-catching overlay exactly
// over scroll's on-screen rect and wires it to swipe-scroll scroll, while
// still routing a touch that doesn't move past scrollTouchDragThreshold to
// the matching row's own callback -- so items stay tappable exactly as
// before this file existed, only genuine swipes are new behavior. Call
// once right after showing a popup whose content actually overflows
// (showStyledMenu only builds a container.Scroll in that case); pair with
// detachTouchScroll from the popup's onDismiss.
func attachTouchScroll(scroll *container.Scroll, rows []fyne.CanvasObject, callbacks []func(), pos fyne.Position, size fyne.Size) {
	ensureScrollTouchEl()
	if scrollTouchEl.IsUndefined() || scrollTouchEl.IsNull() {
		return
	}
	scrollTouchState.scroll = scroll
	scrollTouchState.rows = rows
	scrollTouchState.callbacks = callbacks
	scrollTouchState.dragging = false

	style := scrollTouchEl.Get("style")
	style.Set("left", pxf(pos.X))
	style.Set("top", pxf(pos.Y))
	style.Set("width", pxf(size.Width))
	style.Set("height", pxf(size.Height))
	style.Set("pointerEvents", "auto")
}

// detachTouchScroll releases the overlay -- safe to call even when
// attachTouchScroll was never called for the current popup (the plain,
// non-scrolling case), which is why showStyledMenu passes it
// unconditionally as every popup's onDismiss.
func detachTouchScroll() {
	scrollTouchState.scroll = nil
	scrollTouchState.rows = nil
	scrollTouchState.callbacks = nil
	scrollTouchState.dragging = false
	if scrollTouchEl.IsUndefined() || scrollTouchEl.IsNull() {
		return
	}
	style := scrollTouchEl.Get("style")
	style.Set("pointerEvents", "none")
	style.Set("width", "0px")
	style.Set("height", "0px")
}

func onScrollTouchStart(this js.Value, args []js.Value) interface{} {
	defer recoverScrollTouchPanic("onScrollTouchStart")
	if len(args) == 0 {
		return nil
	}
	event := args[0]
	touches := event.Get("touches")
	if touches.Length() == 0 {
		return nil
	}
	t := touches.Index(0)
	x, y := float32(t.Get("clientX").Float()), float32(t.Get("clientY").Float())
	scrollTouchState.startX = x
	scrollTouchState.startY = y
	scrollTouchState.lastY = y
	scrollTouchState.dragging = false
	return nil
}

func onScrollTouchMove(this js.Value, args []js.Value) interface{} {
	defer recoverScrollTouchPanic("onScrollTouchMove")
	if len(args) == 0 || scrollTouchState.scroll == nil {
		return nil
	}
	event := args[0]
	touches := event.Get("touches")
	if touches.Length() == 0 {
		return nil
	}
	t := touches.Index(0)
	y := float32(t.Get("clientY").Float())
	dy := y - scrollTouchState.lastY

	if !scrollTouchState.dragging {
		total := y - scrollTouchState.startY
		if total < 0 {
			total = -total
		}
		if total < scrollTouchDragThreshold {
			return nil
		}
		scrollTouchState.dragging = true
	}
	event.Call("preventDefault")
	scrollTouchState.lastY = y

	scroll := scrollTouchState.scroll
	fyne.Do(func() {
		if scroll == nil || scroll.Content == nil {
			return
		}
		offset := scroll.Offset
		offset.Y -= dy
		maxY := scroll.Content.MinSize().Height - scroll.Size().Height
		if maxY < 0 {
			maxY = 0
		}
		if offset.Y < 0 {
			offset.Y = 0
		}
		if offset.Y > maxY {
			offset.Y = maxY
		}
		scroll.ScrollToOffset(offset)
	})
	return nil
}

func onScrollTouchEnd(this js.Value, args []js.Value) interface{} {
	defer recoverScrollTouchPanic("onScrollTouchEnd")
	wasDragging := scrollTouchState.dragging
	scrollTouchState.dragging = false
	if wasDragging {
		return nil
	}

	// Not a drag -- treat it as a tap on whatever row is under the touch
	// point, the same row a plain click would have hit if this overlay
	// weren't sitting on top of it. Hit-tested by absolute position
	// rather than delegated to the row widget's own Tapped -- these rows
	// never receive a synthetic click at all while the overlay covers
	// them (that's the whole reason it needs to exist), so there's no
	// Fyne dispatch path left to fall back to here.
	x, y := scrollTouchState.startX, scrollTouchState.startY
	rows := scrollTouchState.rows
	callbacks := scrollTouchState.callbacks
	fyne.Do(func() {
		for i, row := range rows {
			if row == nil {
				continue
			}
			abs := fyne.CurrentApp().Driver().AbsolutePositionForObject(row)
			size := row.Size()
			if x < abs.X || x > abs.X+size.Width || y < abs.Y || y > abs.Y+size.Height {
				continue
			}
			if i < len(callbacks) && callbacks[i] != nil {
				callbacks[i]()
			}
			break
		}
	})
	return nil
}

func onScrollTouchCancel(this js.Value, args []js.Value) interface{} {
	scrollTouchState.dragging = false
	return nil
}

func recoverScrollTouchPanic(where string) {
	if r := recover(); r != nil {
		logrus.Errorf("🖐️ recovered panic in %s: %v", where, r)
	}
}

func pxf(v float32) string {
	return strconv.Itoa(int(v)) + "px"
}

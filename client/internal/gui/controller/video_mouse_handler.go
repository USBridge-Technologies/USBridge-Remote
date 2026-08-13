package controller

import (
	"image"
	"image/color"
	"math"
	"runtime"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// touchMouseCheckPending prevents concurrent checkMouseConnected calls from touch events.
// On mobile there is no overlay event dispatch to trigger checkMouseConnected automatically,
// so TouchDown/Dragged do it on first touch when isMouseConnected is false.
var touchMouseCheckPending int32

// Check that TouchpadWrapper implements all the necessary interfaces
var (
	_ fyne.Tappable          = (*TouchpadWrapper)(nil)
	_ fyne.SecondaryTappable = (*TouchpadWrapper)(nil)
	_ fyne.Scrollable        = (*TouchpadWrapper)(nil)
	_ fyne.Draggable         = (*TouchpadWrapper)(nil)
	_ fyne.Focusable         = (*TouchpadWrapper)(nil)
	_ desktop.Keyable        = (*TouchpadWrapper)(nil)
	_ desktop.Cursorable     = (*TouchpadWrapper)(nil)
	_ desktop.Mouseable      = (*TouchpadWrapper)(nil)
	_ desktop.Hoverable      = (*TouchpadWrapper)(nil)
	_ mobile.Touchable       = (*TouchpadWrapper)(nil)
)

// TouchpadWrapper wraps videoCanvas with mouse and touchscreen support
type TouchpadWrapper struct {
	widget.BaseWidget
	videoWidget *VideoWidget
	image       *canvas.Image
	clip        *container.Clip
	cursor      *canvas.Image
	hScrollBar  *canvas.Rectangle
	vScrollBar  *canvas.Rectangle

	// Keyboard handlers (for macOS, where Canvas.SetOnTypedKey may not work)
	window            fyne.Window
	windowFocusTarget fyne.Focusable
	onKeyDown         func(*fyne.KeyEvent)
	onKeyUp           func(*fyne.KeyEvent)
	onKeyPress        func(*fyne.KeyEvent)
	onRunePress       func(rune)

	// skipWindowFocus: when set, requestFocus skips window.RequestFocus() to avoid
	// disrupting the Win32 z-order when a native overlay (Vulkan) is the topmost window.
	skipWindowFocus atomic.Bool
}

// NewTouchpadWrapper creates a wrapper for the touchpad
func NewTouchpadWrapper(videoWidget *VideoWidget) *TouchpadWrapper {
	wrapper := &TouchpadWrapper{
		videoWidget: videoWidget,
		image:       videoWidget.videoCanvas,
		cursor:      canvas.NewImageFromImage(newOverlayCursorImage()),
		hScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
		vScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
	}
	if wrapper.image != nil {
		wrapper.clip = container.NewClip(wrapper.image)
	}
	wrapper.hScrollBar.Hide()
	wrapper.vScrollBar.Hide()
	wrapper.cursor.Hide()
	wrapper.ExtendBaseWidget(wrapper)
	return wrapper
}

// NewTouchpadWrapperWithImage creates a wrapper for the touchpad with a given image
func NewTouchpadWrapperWithImage(videoWidget *VideoWidget, image *canvas.Image) *TouchpadWrapper {
	wrapper := &TouchpadWrapper{
		videoWidget: videoWidget,
		image:       image,
		cursor:      canvas.NewImageFromImage(newOverlayCursorImage()),
		hScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
		vScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
	}
	if wrapper.image != nil {
		wrapper.clip = container.NewClip(wrapper.image)
	}
	wrapper.hScrollBar.Hide()
	wrapper.vScrollBar.Hide()
	wrapper.cursor.Hide()
	wrapper.ExtendBaseWidget(wrapper)
	return wrapper
}

func (t *TouchpadWrapper) SetImage(image *canvas.Image) {
	t.image = image
	if image != nil {
		t.clip = container.NewClip(image)
	} else {
		t.clip = nil
	}
	t.Refresh()
}

func (t *TouchpadWrapper) UpdateCursorOverlay() {
	if t.cursor == nil {
		return
	}
	if !t.videoWidget.ShouldRenderCursorOverlay() || !t.videoWidget.cursorOverlayShown {
		t.cursor.Hide()
		canvas.Refresh(t.cursor)
		return
	}

	const hotspotX = float32(1)
	const hotspotY = float32(1)
	size := fyne.NewSize(18, 24)
	t.cursor.Resize(size)
	t.cursor.Move(fyne.NewPos(t.videoWidget.cursorOverlayX-hotspotX, t.videoWidget.cursorOverlayY-hotspotY))
	t.cursor.Show()
	canvas.Refresh(t.cursor)
}

// SetKeyHandlers sets the keyboard handlers (for macOS, where Canvas.SetOnTypedKey is unreliable)
func (t *TouchpadWrapper) SetKeyHandlers(onKeyDown func(*fyne.KeyEvent), onKeyUp func(*fyne.KeyEvent), onKey func(*fyne.KeyEvent), onRune func(rune)) {
	t.onKeyDown = onKeyDown
	t.onKeyUp = onKeyUp
	t.onKeyPress = onKey
	t.onRunePress = onRune
}

// SetWindowForFocus sets the window used to request focus on click (desktop)
func (t *TouchpadWrapper) SetWindowForFocus(w fyne.Window) {
	t.window = w
}

func (t *TouchpadWrapper) SetWindowFocusTarget(target fyne.Focusable) {
	t.windowFocusTarget = target
}

// SetSkipWindowFocus controls whether requestFocus skips window.RequestFocus().
// Set to true when a native overlay owns the z-order (e.g. Vulkan fullscreen on Windows)
// so that MouseDown events don't trigger SetForegroundWindow and disrupt z-order.
func (t *TouchpadWrapper) SetSkipWindowFocus(skip bool) {
	t.skipWindowFocus.Store(skip)
}

// FocusGained implements fyne.Focusable
func (t *TouchpadWrapper) FocusGained() {}

// FocusLost implements fyne.Focusable
func (t *TouchpadWrapper) FocusLost() {}

func (t *TouchpadWrapper) Cursor() desktop.Cursor { return desktop.DefaultCursor }

// CreateRenderer creates the renderer for the widget
func (t *TouchpadWrapper) CreateRenderer() fyne.WidgetRenderer {
	return &touchpadRenderer{
		wrapper: t,
	}
}

// touchpadRenderer is the renderer for TouchpadWrapper
type touchpadRenderer struct {
	wrapper *TouchpadWrapper
}

func (r *touchpadRenderer) Layout(size fyne.Size) {
	if r.wrapper.clip != nil {
		r.wrapper.clip.Resize(size)
	}
	if r.wrapper.image != nil {
		r.wrapper.wrapperLayoutImage(size)
	}
	r.wrapper.UpdateCursorOverlay()
}

func (r *touchpadRenderer) MinSize() fyne.Size {
	return fyne.NewSize(320, 240)
}

func (r *touchpadRenderer) Refresh() {
	if r.wrapper.clip != nil {
		r.wrapper.wrapperLayoutImage(r.wrapper.Size())
		canvas.Refresh(r.wrapper.clip)
	}
	if r.wrapper.cursor != nil {
		canvas.Refresh(r.wrapper.cursor)
	}
	if r.wrapper.image != nil {
		canvas.Refresh(r.wrapper.image)
	}
}

func (r *touchpadRenderer) Objects() []fyne.CanvasObject {
	if r.wrapper.clip == nil {
		return []fyne.CanvasObject{r.wrapper.cursor, r.wrapper.hScrollBar, r.wrapper.vScrollBar}
	}
	return []fyne.CanvasObject{r.wrapper.clip, r.wrapper.cursor, r.wrapper.hScrollBar, r.wrapper.vScrollBar}
}

func (r *touchpadRenderer) Destroy() {
}

// updateTouchpadSize updates the input area size and the video rectangle (for correct coordinate translation into 0..4095).
func (t *TouchpadWrapper) updateTouchpadSize() {
	s := t.Size()
	if s.Width > 0 && s.Height > 0 {
		var frame image.Image
		if t.image != nil {
			frame = t.image.Image
		}
		t.videoWidget.UpdateTouchpadAndContentRect(s.Width, s.Height, frame)
	}
}

func (t *TouchpadWrapper) wrapperLayoutImage(size fyne.Size) {
	if t.image == nil {
		return
	}
	// Explicitly set the clip container size so the video doesn't spill past the widget bounds
	if t.clip != nil {
		t.clip.Resize(size)
		t.clip.Move(fyne.NewPos(0, 0))
	}
	t.videoWidget.UpdateTouchpadAndContentRect(size.Width, size.Height, t.image.Image)
	x, y, w, h := t.videoWidget.GetViewportRect()
	t.image.Move(fyne.NewPos(x, y))
	t.image.Resize(fyne.NewSize(w, h))
	t.updateScrollIndicators(size, x, y, w, h)
}

func (t *TouchpadWrapper) updateScrollIndicators(size fyne.Size, x, y, w, h float32) {
	if t.hScrollBar == nil || t.vScrollBar == nil {
		return
	}

	const thickness = float32(3)
	const margin = float32(6)
	const minThumb = float32(28)

	if w > size.Width {
		thumbW := clampFloat((size.Width/w)*size.Width, minThumb, size.Width-(margin*2))
		progress := clampFloat(-x/(w-size.Width), 0, 1)
		trackW := size.Width - (margin * 2) - thumbW
		t.hScrollBar.Show()
		// Move to the top: use just margin instead of size.Height-margin-thickness
		t.hScrollBar.Move(fyne.NewPos(margin+(trackW*progress), margin))
		t.hScrollBar.Resize(fyne.NewSize(thumbW, thickness))
	} else {
		t.hScrollBar.Hide()
	}

	if h > size.Height {
		thumbH := clampFloat((size.Height/h)*size.Height, minThumb, size.Height-(margin*2))
		progress := clampFloat(-y/(h-size.Height), 0, 1)
		trackH := size.Height - (margin * 2) - thumbH
		t.vScrollBar.Show()
		t.vScrollBar.Move(fyne.NewPos(size.Width-margin-thickness, margin+(trackH*progress)))
		t.vScrollBar.Resize(fyne.NewSize(thickness, thumbH))
	} else {
		t.vScrollBar.Hide()
	}
}

func (t *TouchpadWrapper) beginScrollbarDrag(pos fyne.Position) bool {
	axis := t.scrollbarAxisAt(pos)
	if axis == "" {
		return false
	}
	t.videoWidget.scrollDragAxis = axis
	t.videoWidget.scrollDragLastX = pos.X
	t.videoWidget.scrollDragLastY = pos.Y
	return true
}

func (t *TouchpadWrapper) updateScrollbarDrag(pos fyne.Position) bool {
	switch t.videoWidget.scrollDragAxis {
	case "horizontal":
		dx := pos.X - t.videoWidget.scrollDragLastX
		t.videoWidget.scrollDragLastX = pos.X
		t.videoWidget.scrollDragLastY = pos.Y
		t.applyScrollbarDelta("horizontal", dx)
		return true
	case "vertical":
		dy := pos.Y - t.videoWidget.scrollDragLastY
		t.videoWidget.scrollDragLastX = pos.X
		t.videoWidget.scrollDragLastY = pos.Y
		t.applyScrollbarDelta("vertical", dy)
		return true
	default:
		return false
	}
}

func (t *TouchpadWrapper) endScrollbarDrag() bool {
	if t.videoWidget.scrollDragAxis == "" {
		return false
	}
	t.videoWidget.scrollDragAxis = ""
	return true
}

func (t *TouchpadWrapper) applyScrollbarDelta(axis string, delta float32) {
	size := t.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return
	}
	_, _, w, h := t.videoWidget.GetViewportRect()
	switch axis {
	case "horizontal":
		if w <= size.Width {
			return
		}
		scrollRange := w - size.Width
		trackRange := size.Width - clampFloat((size.Width/w)*size.Width, 28, size.Width-12)
		if trackRange <= 0 {
			return
		}
		viewportDelta := (delta / trackRange) * scrollRange
		t.videoWidget.panOffsetX -= viewportDelta
	case "vertical":
		if h <= size.Height {
			return
		}
		scrollRange := h - size.Height
		trackRange := size.Height - clampFloat((size.Height/h)*size.Height, 28, size.Height-12)
		if trackRange <= 0 {
			return
		}
		viewportDelta := (delta / trackRange) * scrollRange
		t.videoWidget.panOffsetY -= viewportDelta
	default:
		return
	}
	t.videoWidget.recalculateViewport()
	t.Refresh()
}

func (t *TouchpadWrapper) scrollbarAxisAt(pos fyne.Position) string {
	size := t.Size()
	if size.Width <= 0 || size.Height <= 0 {
		return ""
	}
	x, y, w, h := t.videoWidget.GetViewportRect()
	const touchBand = float32(22)
	const margin = float32(6)
	const minThumb = float32(28)

	if w > size.Width {
		thumbW := clampFloat((size.Width/w)*size.Width, minThumb, size.Width-(margin*2))
		progress := clampFloat(-x/(w-size.Width), 0, 1)
		trackW := size.Width - (margin * 2) - thumbW
		thumbX := margin + (trackW * progress)
		// Check for the top of the screen (pos.Y <= touchBand)
		if pos.Y <= touchBand && pos.X >= thumbX-touchBand/2 && pos.X <= thumbX+thumbW+touchBand/2 {
			return "horizontal"
		}
	}

	if h > size.Height {
		thumbH := clampFloat((size.Height/h)*size.Height, minThumb, size.Height-(margin*2))
		progress := clampFloat(-y/(h-size.Height), 0, 1)
		trackH := size.Height - (margin * 2) - thumbH
		thumbY := margin + (trackH * progress)
		if pos.X >= size.Width-touchBand && pos.Y >= thumbY-touchBand/2 && pos.Y <= thumbY+thumbH+touchBand/2 {
			return "vertical"
		}
	}

	return ""
}

// Tapped is called on a full click (pressed and released on the widget). In Fyne MouseUp is delivered
// to the widget under the cursor on release, so we won't get MouseUp when releasing outside the widget.
// Tapped, however, only fires on a completed click on the widget — we use it for tap detection.
func (t *TouchpadWrapper) Tapped(ev *fyne.PointEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	if fyne.CurrentDevice().IsMobile() && t.videoWidget.shouldIgnoreTouchInput() {
		return
	}
	t.updateTouchpadSize()
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		t.videoWidget.touchActive = false
		t.videoWidget.dragButton = 0
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.enqueueTouchTap(x, y)
		return
	}
	// Absolute: the click itself already comes through MouseDown/MouseUp or TouchDown/TouchUp.
	// Here we only sync the position so we don't duplicate the click.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, true)
		return
	}
	logrus.Debugf("🖱️ Tapped at: %v (mouse mode, using TouchUp)", ev.Position)
}

// TappedSecondary — right click. In touch mode: only update the position without a touch (tip=false),
// then a single right-button click. A touch_position(down)+touch_position(up) pair is often interpreted
// by the host as a left click, which resulted in a "double left" instead of a right click.
func (t *TouchpadWrapper) TappedSecondary(ev *fyne.PointEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	if fyne.CurrentDevice().IsMobile() && t.videoWidget.shouldIgnoreTouchInput() {
		return
	}
	t.updateTouchpadSize()
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		t.videoWidget.touchActive = false
		t.videoWidget.dragButton = 0
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.enqueueSecondaryTouchTap(x, y)
		return
	}
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, true)
		return
	}
	t.videoWidget.enqueueMouseClick(2)
}

// MouseDown handles a mouse button press (desktop)
func (t *TouchpadWrapper) MouseDown(ev *desktop.MouseEvent) {
	t.requestFocus()
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()

	t.videoWidget.touchStartX = ev.Position.X
	t.videoWidget.touchStartY = ev.Position.Y
	t.videoWidget.touchStartTime = time.Now()
	t.videoWidget.resetRelativeMoveAccumulator()
	t.videoWidget.lastMouseX = ev.Position.X
	t.videoWidget.lastMouseY = ev.Position.Y
	t.videoWidget.currentMouseX = ev.Position.X
	t.videoWidget.currentMouseY = ev.Position.Y
	t.videoWidget.isDragging = false

	var btn int
	switch ev.Button {
	case desktop.MouseButtonPrimary:
		t.videoWidget.dragButton = 1
		btn = 1
	case desktop.MouseButtonSecondary:
		t.videoWidget.dragButton = 2
		btn = 2
	case desktop.MouseButtonTertiary:
		t.videoWidget.dragButton = 3
		btn = 3
	default:
		t.videoWidget.dragButton = 0
		btn = 0
	}

	// Touchscreen: deferred touch(down). For the left button — touch+BTN_LEFT, for the right — only position (click later).
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.StartTouchDownDelay(x, y, btn)
	}
	// Absolute: sync the position immediately on press and atomically update the button.
	// Only forward the press when the cursor is within the actual video content area —
	// clicks in letterbox/pillarbox regions or outside the widget must not reach the
	// remote, otherwise both the local OS and the remote system register the click.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		if !t.videoWidget.isPositionInContentRect(ev.Position.X, ev.Position.Y) {
			t.videoWidget.dragButton = 0
			return
		}
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		logrus.Infof("🖱️ [Drag] MouseDown btn=%d pos=(%.0f,%.0f) abs=(%d,%d)", btn, ev.Position.X, ev.Position.Y, x, y)
		t.videoWidget.PressAbsoluteButton(btn, x, y)
	}
}

// MouseUp handles a mouse button release (desktop)
func (t *TouchpadWrapper) MouseUp(ev *desktop.MouseEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}

	dx := math.Abs(float64(ev.Position.X - t.videoWidget.touchStartX))
	dy := math.Abs(float64(ev.Position.Y - t.videoWidget.touchStartY))
	duration := time.Since(t.videoWidget.touchStartTime)

	// Touchscreen: release (tip=false) — only when touch is active (timer fired / was a drag)
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		if t.videoWidget.touchActive {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			t.videoWidget.lastTouchX = x
			t.videoWidget.lastTouchY = y
			t.videoWidget.touchActive = false
			button := t.videoWidget.dragButton
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
			if button == 2 {
				t.videoWidget.enqueueSecondaryTouchTap(x, y)
			} else {
				t.videoWidget.enqueueTouch(x, y, false)
			}
		} else {
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
		}
		return
	}

	if t.videoWidget.IsAbsoluteLikeInputMode() {
		button := t.videoWidget.dragButton
		t.videoWidget.dragButton = 0
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		if button == 0 {
			// MouseDown was suppressed (click was outside content rect) — nothing to release.
			return
		}
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		logrus.Infof("🖱️ [Drag] MouseUp btn=%d pos=(%.0f,%.0f) abs=(%d,%d) dx=%.0f dy=%.0f dur=%v", button, ev.Position.X, ev.Position.Y, x, y, dx, dy, duration)
		t.videoWidget.ReleaseAbsoluteButton(button, x, y)
		return
	}

	// Mouse mode: as before
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		t.videoWidget.dragButton = 0
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	if dx < 10 && dy < 10 && duration < 300*time.Millisecond {
		button := t.videoWidget.dragButton
		t.videoWidget.dragButton = 0
		t.videoWidget.enqueueMouseClick(button)
	} else if duration >= 500*time.Millisecond && dx < 20 && dy < 20 {
		if t.videoWidget.dragButton == 1 {
			t.videoWidget.dragButton = 0
			t.videoWidget.enqueueMouseClick(2)
		} else {
			button := t.videoWidget.dragButton
			t.videoWidget.dragButton = 0
			t.videoWidget.enqueueMouseClick(button)
		}
	} else {
		t.videoWidget.dragButton = 0
	}
	t.videoWidget.isDragging = false
	t.videoWidget.resetRelativeMoveAccumulator()
}

// MouseMoved handles mouse movement (desktop)
func (t *TouchpadWrapper) MouseMoved(ev *desktop.MouseEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, true)

	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		// Touchscreen: update the position during a drag. Left — SendTouch, right — position only (SendTouchPositionOnly).
		if t.videoWidget.touchActive && t.videoWidget.dragButton != 0 {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			if x != t.videoWidget.lastTouchX || y != t.videoWidget.lastTouchY {
				t.videoWidget.lastTouchX = x
				t.videoWidget.lastTouchY = y
				if t.videoWidget.dragButton == 2 {
					t.videoWidget.enqueueTouchPositionOnly(x, y, true)
				} else {
					t.videoWidget.enqueueTouch(x, y, true)
				}
			}
		}
	}

	// Absolute: cursor positioning via absolute tablet.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, false)
	}

	t.videoWidget.currentMouseX = ev.Position.X
	t.videoWidget.currentMouseY = ev.Position.Y
}

// MouseIn handles the cursor entering the area (desktop)
func (t *TouchpadWrapper) MouseIn(ev *desktop.MouseEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, true)
	t.videoWidget.lastMouseX = ev.Position.X
	t.videoWidget.lastMouseY = ev.Position.Y
	t.videoWidget.currentMouseX = ev.Position.X
	t.videoWidget.currentMouseY = ev.Position.Y
	t.videoWidget.resetRelativeMoveAccumulator()
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, true)
	}
}

// ForceReleaseStuckButton unconditionally clears any held mouse/touch
// button state and tells the remote to release it too, regardless of
// whether normal event delivery (MouseUp) is expected to still arrive.
//
// This is deliberately NOT the same thing MouseOut does for
// IsAbsoluteLikeInputMode: MouseOut skips releasing while dragButton != 0
// because it trusts Fyne's implicit mouse capture to still deliver the
// real MouseUp to this widget later, even once the cursor is outside its
// bounds. That assumption breaks under wasm on a right-click: the
// browser's native context menu (see InitTouchGestureBridge's
// "contextmenu" listener) can swallow the DOM mouseup event for the
// button that opened it entirely, so Fyne's driver never calls MouseUp at
// all -- dragButton/absButtons then stay stuck "held" forever, and every
// later click looks like it's happening on top of an already-down right
// button. Confirmed live: right-click on the video would show the
// browser's own context menu and the stream would stop responding to any
// further input until the page was reloaded.
//
// `reason` is only for the log line (e.g. "contextmenu", "window-blur") --
// this same safety net is reused for more than one trigger.
func (t *TouchpadWrapper) ForceReleaseStuckButton(reason string) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		if t.videoWidget.touchActive {
			t.videoWidget.touchActive = false
			x, y := t.videoWidget.lastTouchX, t.videoWidget.lastTouchY
			button := t.videoWidget.dragButton
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
			if button == 2 {
				t.videoWidget.enqueueTouchPositionOnly(x, y, false)
			} else {
				t.videoWidget.enqueueTouch(x, y, false)
			}
		} else {
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
		}
		return
	}
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		if t.videoWidget.dragButton == 0 && t.videoWidget.absButtons == 0 {
			return
		}
		logrus.Infof("🖱️ [Drag] %s: force-releasing stuck button(s) dragButton=%d absButtons=%d", reason, t.videoWidget.dragButton, t.videoWidget.absButtons)
		x, y := t.videoWidget.lastAbsX, t.videoWidget.lastAbsY
		t.videoWidget.dragButton = 0
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		t.videoWidget.ReleaseAllAbsoluteButtons(x, y)
		return
	}
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		t.videoWidget.dragButton = 0
		t.videoWidget.resetRelativeMoveAccumulator()
		t.videoWidget.enqueueMouseAction(0, 0, 0, 0)
	}
}

// MouseOut handles the cursor leaving the area (desktop)
func (t *TouchpadWrapper) MouseOut() {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.videoWidget.UpdateCursorOverlayFromLocalInput(0, 0, false)
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		if t.videoWidget.touchActive {
			t.videoWidget.touchActive = false
			x, y := t.videoWidget.lastTouchX, t.videoWidget.lastTouchY
			button := t.videoWidget.dragButton
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
			if button == 2 {
				t.videoWidget.enqueueTouchPositionOnly(x, y, false)
			} else {
				t.videoWidget.enqueueTouch(x, y, false)
			}
		} else {
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
		}
		return
	}
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		// Don't release during an active drag — Fyne delivers MouseUp to the original
		// widget even outside its bounds (implicit mouse capture), so the button will be
		// properly released there. Releasing here would cause phantom button-up events
		// while the physical button is still held.
		if t.videoWidget.dragButton != 0 {
			logrus.Infof("🖱️ [Drag] MouseOut skipped release (dragButton=%d still held)", t.videoWidget.dragButton)
			return
		}
		if t.videoWidget.absButtons != 0 {
			logrus.Infof("🖱️ [Drag] MouseOut releasing stuck absButtons=%d", t.videoWidget.absButtons)
			x, y := t.videoWidget.lastAbsX, t.videoWidget.lastAbsY
			t.videoWidget.isDragging = false
			t.videoWidget.resetRelativeMoveAccumulator()
			t.videoWidget.ReleaseAllAbsoluteButtons(x, y)
		}
		return
	}
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		t.videoWidget.dragButton = 0
		t.videoWidget.resetRelativeMoveAccumulator()
		t.videoWidget.enqueueMouseAction(0, 0, 0, 0)
	}
}

// Scrolled handles mouse wheel scrolling (desktop). In touch mode scroll is also sent to the host.
func (t *TouchpadWrapper) Scrolled(ev *fyne.ScrollEvent) {
	logrus.Debugf("🖱️ Scrolled: %v", ev.Scrolled)

	if !t.videoWidget.isMouseConnected {
		return
	}

	scroll := int(ev.Scrolled.DY / 10)
	scroll = clamp(scroll, -127, 127)
	if scroll == 0 {
		return
	}
	// Absolute: sync the absolute position before scrolling.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		// Take the current cursor position within the widget (last known).
		// MouseMoved/TouchMove update lastAbsX/Y, but on some platforms scroll can arrive without movement.
		x, y := t.videoWidget.lastAbsX, t.videoWidget.lastAbsY
		t.videoWidget.SendAbsoluteEvent(x, y, scroll, true)
		return
	}
	t.videoWidget.enqueueMouseScroll(scroll)
}

// TouchDown handles the start of a touch (mobile)
func (t *TouchpadWrapper) TouchDown(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		// On mobile there is no overlay event dispatch to trigger checkMouseConnected.
		// Retry it here on the first touch so the move worker starts promptly.
		if atomic.CompareAndSwapInt32(&touchMouseCheckPending, 0, 1) {
			go func() {
				t.videoWidget.checkMouseConnected()
				atomic.StoreInt32(&touchMouseCheckPending, 0)
			}()
		}
		return
	}
	if t.videoWidget.shouldIgnoreTouchInput() {
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, true)
	if t.beginScrollbarDrag(ev.Position) {
		return
	}
	t.videoWidget.touchStartX = ev.Position.X
	t.videoWidget.touchStartY = ev.Position.Y
	t.videoWidget.touchStartTime = time.Now()
	t.videoWidget.resetRelativeMoveAccumulator()
	t.videoWidget.lastMouseX = ev.Position.X
	t.videoWidget.lastMouseY = ev.Position.Y
	t.videoWidget.isDragging = false

	if isVirtualCursorLikeMode(t.videoWidget.GetMouseInputMode()) {
		// If a quick tap just fired and second finger comes down within 600ms → potential LMB hold.
		// On Android: arm a 200ms timer — if finger stays (hold) activate lmbHeld; if lifts quickly (double-click) send Down2+Up2.
		if !t.videoWidget.lmbHeld &&
			!t.videoWidget.lmbPendingHold &&
			!t.videoWidget.lastVirtualTapAt.IsZero() &&
			time.Since(t.videoWidget.lastVirtualTapAt) < 600*time.Millisecond {
			t.videoWidget.lastVirtualTapAt = time.Time{}

			if runtime.GOOS == "android" {
				vw := t.videoWidget
				if vw.lmbHoldTimer != nil {
					vw.lmbHoldTimer.Stop()
				}
				vw.lmbPendingHold = true
				vw.lmbHoldTimer = time.AfterFunc(150*time.Millisecond, func() {
					fyne.Do(func() {
						if !vw.lmbPendingHold {
							return
						}
						vw.lmbPendingHold = false
						vw.lmbHeld = true
						if vw.lmbUpTimer != nil {
							// Up1 not yet sent (timer still running) — cancel it; Down1 stays held.
							vw.lmbUpTimer.Stop()
							vw.lmbUpTimer = nil
						} else if !vw.lmbUp1Sent {
							// lmbUpTimer fired but deferred Up1 because lmbPendingHold was set.
							// Down1 is still held — instant hold, nothing to do.
						} else {
							// Up1 already sent — the first tap is complete and the new
							// touch is an independent gesture (e.g. cursor move). Cancel
							// drag mode so the swipe doesn't trigger drag-select.
							vw.lmbHeld = false
						}
					})
				})
			} else {
				t.videoWidget.lmbHeld = true
				t.videoWidget.enqueueMouseButtonDown(1)
			}
		}
		// Arm 1-second haptic timer: fires when long-press RMB threshold is reached,
		// giving tactile confirmation that the finger can now be released for RMB.
		vw := t.videoWidget
		if vw.rmbHapticTimer != nil {
			vw.rmbHapticTimer.Stop()
		}
		vw.rmbHapticTimer = time.AfterFunc(time.Second, func() {
			fyne.Do(func() {
				vw.rmbHapticTimer = nil
				if !isVirtualCursorLikeMode(vw.GetMouseInputMode()) {
					return
				}
				// Only vibrate if the finger hasn't moved enough to disqualify long-press.
				if vw.isDragging {
					return
				}
				triggerRmbHaptic()
			})
		})
		return
	}
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		if !t.videoWidget.TryRecordTouchDown(x, y) {
			return
		}
		t.videoWidget.touchActive = true
		t.videoWidget.enqueueTouch(x, y, true)
	}
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, true)
	}
}

// TouchUp handles the end of a touch (mobile)
func (t *TouchpadWrapper) TouchUp(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	// Release held LMB unconditionally — must not be blocked by shouldIgnoreTouchInput
	// or any other early return, otherwise LMB gets stuck pressed.
	if t.videoWidget.lmbHeld {
		t.videoWidget.lmbHeld = false
		if t.videoWidget.lmbUpTimer != nil {
			t.videoWidget.lmbUpTimer.Stop()
			t.videoWidget.lmbUpTimer = nil
		}
		t.videoWidget.enqueueMouseButtonUp(1)
		t.videoWidget.lastVirtualTapAt = time.Time{}
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	// Pending hold: second finger lifted quickly — treat as double-click.
	if t.videoWidget.lmbPendingHold {
		t.videoWidget.lmbPendingHold = false
		if t.videoWidget.lmbHoldTimer != nil {
			t.videoWidget.lmbHoldTimer.Stop()
			t.videoWidget.lmbHoldTimer = nil
		}
		if t.videoWidget.lmbUpTimer != nil {
			// Up1 not yet sent (timer still running) — flag it to complete the double-click after Up1.
			t.videoWidget.lmbPendingDoubleClick = true
		} else if !t.videoWidget.lmbUp1Sent {
			// lmbUpTimer fired but deferred Up1 (Down1 still held) — send Up1 then Down2+Up2 with gap.
			t.videoWidget.lmbUp1Sent = true
			t.videoWidget.enqueueMouseButtonUp(1)
			t.videoWidget.lastVirtualTapAt = time.Time{}
			vw := t.videoWidget
			go func() {
				time.Sleep(50 * time.Millisecond)
				fyne.Do(func() {
					vw.enqueueMouseButtonDown(1)
					vw.enqueueMouseButtonUp(1)
				})
			}()
		} else {
			// Up1 already sent — send Down2+Up2 immediately (Up1 was long ago, no merge risk).
			t.videoWidget.lastVirtualTapAt = time.Time{}
			t.videoWidget.enqueueMouseButtonDown(1)
			t.videoWidget.enqueueMouseButtonUp(1)
		}
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	if t.videoWidget.shouldIgnoreTouchInput() {
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, false)
	if t.endScrollbarDrag() {
		return
	}
	dx := math.Abs(float64(ev.Position.X - t.videoWidget.touchStartX))
	dy := math.Abs(float64(ev.Position.Y - t.videoWidget.touchStartY))
	duration := time.Since(t.videoWidget.touchStartTime)

	mode := t.videoWidget.GetMouseInputMode()

	// Virtual cursor: tap = left click; long-tap = right click; tap-then-hold = LMB drag.
	// Note: lmbHeld case is handled unconditionally above (before shouldIgnoreTouchInput).
	if isVirtualCursorLikeMode(mode) {
		// Cancel the RMB haptic timer — touch ended so confirmation is no longer needed.
		if t.videoWidget.rmbHapticTimer != nil {
			t.videoWidget.rmbHapticTimer.Stop()
			t.videoWidget.rmbHapticTimer = nil
		}
		// Use physical distance from touch start only — don't check isDragging because
		// Dragged sets it even on stationary touches (touch sensor noise).
		if dx < 15 && dy < 15 {
			if duration >= time.Second {
				t.videoWidget.enqueueMouseClick(2) // right click (long press)
			} else if duration < 500*time.Millisecond {
				// Quick tap → left click, arm the drag window (600ms to place second finger).
				// On Android: send Down immediately and delay Up by 100ms so that a quick
				// second touch can cancel the Up and keep Down held (instant LMB drag, no
				// gap between the two events that would trigger a double-click on the target).
				now := time.Now()
				t.videoWidget.lastVirtualTapAt = now
				if runtime.GOOS == "android" {
					t.videoWidget.enqueueMouseButtonDown(1)
					t.videoWidget.lmbTapAt = now
					if t.videoWidget.lmbUpTimer != nil {
						t.videoWidget.lmbUpTimer.Stop()
					}
					vw := t.videoWidget
					t.videoWidget.lmbUp1Sent = false
					t.videoWidget.lmbUpTimer = time.AfterFunc(150*time.Millisecond, func() {
						fyne.Do(func() {
							if vw.lmbUpTimer != nil {
								vw.lmbUpTimer = nil
								if vw.lmbPendingHold {
									// Second finger is still down; lmbHoldTimer will decide hold vs double-click.
									// Do NOT send Up1 — Down1 stays pressed. lmbUp1Sent stays false.
									return
								}
								vw.lmbUp1Sent = true
								vw.enqueueMouseButtonUp(1)
								// Second finger lifted quickly while Up1 was still pending → complete double-click.
								// Delay Down2 by 50ms: without the gap Up1+Down2+Up2 arrive simultaneously
								// and the OS merges them into a triple-click burst.
								if vw.lmbPendingDoubleClick {
									vw.lmbPendingDoubleClick = false
									vw.lastVirtualTapAt = time.Time{}
									go func() {
										time.Sleep(50 * time.Millisecond)
										fyne.Do(func() {
											vw.enqueueMouseButtonDown(1)
											vw.enqueueMouseButtonUp(1)
										})
									}()
								}
							}
						})
					})
				} else {
					t.videoWidget.enqueueMouseClick(1)
				}
			}
		}
		// Finger lifted — flush final position regardless of rate limiter.
		t.flushVirtualCursorPosition()
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}

	// Touchscreen: release (tip=false)
	if mode == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.lastTouchX = x
		t.videoWidget.lastTouchY = y
		t.videoWidget.touchActive = false
		t.videoWidget.enqueueTouch(x, y, false)
		t.videoWidget.isDragging = false
		return
	}
	// Mouse mode: as before
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	if dx < 10 && dy < 10 && duration < 300*time.Millisecond {
		if t.videoWidget.IsAbsoluteLikeInputMode() {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			t.videoWidget.ClickAbsoluteButton(1, x, y)
		} else {
			t.videoWidget.enqueueMouseClick(1)
		}
	} else if duration >= 1*time.Second && dx < 20 && dy < 20 {
		if t.videoWidget.IsAbsoluteLikeInputMode() {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			t.videoWidget.ClickAbsoluteButton(2, x, y)
		} else {
			t.videoWidget.enqueueMouseClick(2)
		}
	}
	t.videoWidget.isDragging = false
	t.videoWidget.resetRelativeMoveAccumulator()
}

// TouchMove handles touch movement (mobile)
func (t *TouchpadWrapper) TouchMove(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	if t.videoWidget.shouldIgnoreTouchInput() {
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, true)
	if t.updateScrollbarDrag(ev.Position) {
		return
	}

	mode := t.videoWidget.GetMouseInputMode()

	if mode == "touchscreen" {
		if t.videoWidget.touchActive {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			if x != t.videoWidget.lastTouchX || y != t.videoWidget.lastTouchY {
				t.videoWidget.lastTouchX = x
				t.videoWidget.lastTouchY = y
				t.videoWidget.enqueueTouch(x, y, true)
			}
		}
		t.videoWidget.lastMouseX = ev.Position.X
		t.videoWidget.lastMouseY = ev.Position.Y
		return
	}

	if isVirtualCursorLikeMode(mode) {
		// Only track reference position here. Dragged handles the actual delta
		// via ev.Dragged.DX/DY to avoid pointer-ID confusion when both TouchMove
		// and Dragged fire for the same Android event with different positions.
		t.videoWidget.lastMouseX = ev.Position.X
		t.videoWidget.lastMouseY = ev.Position.Y
		return
	}

	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, false)
		t.videoWidget.lastMouseX = ev.Position.X
		t.videoWidget.lastMouseY = ev.Position.Y
		return
	}

	// Mouse: relative movement
	rawDx := ev.Position.X - t.videoWidget.lastMouseX
	rawDy := ev.Position.Y - t.videoWidget.lastMouseY
	t.videoWidget.lastMouseX = ev.Position.X
	t.videoWidget.lastMouseY = ev.Position.Y
	if !t.videoWidget.isDragging {
		t.videoWidget.isDragging = true
	}
	const touchpadSensitivity = 2.0
	dx, dy := t.videoWidget.accumulateRelativeMove(rawDx, rawDy, touchpadSensitivity)
	if dx == 0 && dy == 0 {
		return
	}
	t.videoWidget.enqueueMouseMove(dx, dy)
}

// handleVirtualCursorMove moves the virtual cursor by the given delta and
// forwards the absolute position to Moonlight at most every 8ms (same rate as
// Mac absolute mode). The local Vulkan cursor is always updated immediately.
// rawDx/rawDy are already the relative delta — do NOT pass absolute positions.
func (t *TouchpadWrapper) handleVirtualCursorMove(rawDx, rawDy float32) {
	vw := t.videoWidget

	cw := vw.contentRectW
	ch := vw.contentRectH
	vw.vcMu.Lock()

	frameX, frameY, frameW, frameH := vw.getFrameContentRect()
	minU, maxU := float32(0), float32(1)
	minV, maxV := float32(0), float32(1)
	if frameW > 0 && frameH > 0 {
		minU = frameX
		maxU = frameX + frameW
		minV = frameY
		maxV = frameY + frameH
	}

	if cw > 0 {
		vw.virtualCursorU = clampFloat(vw.virtualCursorU+rawDx/cw, minU, maxU)
	}
	if ch > 0 {
		vw.virtualCursorV = clampFloat(vw.virtualCursorV+rawDy/ch, minV, maxV)
	}
	vw.vcMu.Unlock()
	// Only mark as dragging after significant movement so that touch noise
	// doesn't prevent tap detection in TouchUp.
	if math.Abs(float64(rawDx)) >= 3 || math.Abs(float64(rawDy)) >= 3 {
		vw.isDragging = true
		// Movement while pending hold → user is dragging, not tapping. Commit to hold immediately
		// without waiting for lmbHoldTimer so the drag feels instantaneous.
		if vw.lmbPendingHold {
			vw.lmbPendingHold = false
			if vw.lmbHoldTimer != nil {
				vw.lmbHoldTimer.Stop()
				vw.lmbHoldTimer = nil
			}
			vw.lmbHeld = true
			if vw.lmbUpTimer != nil {
				// Up1 not yet sent (timer still running) — cancel it; Down1 stays held.
				vw.lmbUpTimer.Stop()
				vw.lmbUpTimer = nil
			} else if !vw.lmbUp1Sent {
				// lmbUpTimer fired but deferred Up1 because lmbPendingHold was set.
				// Down1 is still held — instant hold, nothing to do.
			} else {
				// Up1 already sent: tap is complete, movement is a new independent
				// gesture. Cancel drag mode — do not send Down2.
				vw.lmbHeld = false
				if vw.lmbHoldTimer != nil {
					vw.lmbHoldTimer.Stop()
					vw.lmbHoldTimer = nil
				}
			}
		} else if vw.lmbUpTimer != nil {
			// User is swiping within the tap-capture window (no second finger).
			// Release LMB immediately so the cursor moves without drag-selecting.
			vw.lmbUpTimer.Stop()
			vw.lmbUpTimer = nil
			vw.lmbPendingDoubleClick = false
			vw.lmbUp1Sent = true
			vw.enqueueMouseButtonUp(1)
		}
	}

	// Update viewport and cursor state immediately (before logging so the log
	// shows the POST-update contentRectX — the actual value sent to Vulkan).
	if vw.isNativeVideoActive() {
		vw.updateNativeViewportAndCursor()
	} else {
		vw.updateNativeViewportAndCursor()
		vw.refreshViewportViews()
	}

	// Rate-limit network sends to 8ms so ENet doesn't pile up stale reliable
	// packets faster than they can be ACKed. When the finger stops,
	// flushVirtualCursorPosition() sends the final position unconditionally.
	const minInterval = 8 * time.Millisecond
	if !vw.lastVirtualCursorSentTime.IsZero() && time.Since(vw.lastVirtualCursorSentTime) < minInterval {
		return
	}
	vw.sendVirtualCursorToHost()
}

// sendVirtualCursorToHost computes and sends the current virtualCursorU/V as an
// absolute Moonlight position.
func (vw *VideoWidget) sendVirtualCursorToHost() {
	vw.vcMu.Lock()
	u := vw.virtualCursorU
	v := vw.virtualCursorV
	vw.vcMu.Unlock()

	frameX, frameY, frameW, frameH := vw.getFrameContentRect()
	hostU := u
	hostV := v
	if frameW > 0 && frameH > 0 {
		hostU = (u - frameX) / frameW
		hostV = (v - frameY) / frameH
	}

	hostU = clampFloat(hostU, 0, 1)
	hostV = clampFloat(hostV, 0, 1)

	absX := int16(math.Round(float64(hostU * 32767)))
	absY := int16(math.Round(float64(hostV * 32767)))
	mi := vw.moonlightInput()
	if mi != nil && mi.IsInputActive() {
		vw.lastVirtualCursorSentTime = time.Now()
		// Off the calling (Fyne UI/touch-handler) goroutine, same reason
		// every other mouse send already goes through enqueueSend: this
		// was previously a direct, synchronous call right here, blocking
		// whatever touchmove callback invoked it (handleVirtualCursorMove
		// calls this, then rate-limits to at most once per 8ms) for
		// however long the underlying send takes -- a WebRTC DataChannel
		// send under wasm, an ENet reliable send elsewhere. Since the same
		// call also drives updateNativeViewportAndCursor()'s local visual
		// cursor/viewport update just before this in
		// handleVirtualCursorMove, any latency here directly stalled the
		// on-screen cursor too, reading as "the local cursor waits for
		// network round-trips" instead of moving with the finger
		// immediately. Decoupling the network send from the local visual
		// update is what actually fixes that: the UI updates instantly
		// off vw.virtualCursorU/V regardless of how the send is queued.
		vw.enqueueSend(func() {
			mi.SendMoonlightMousePosition(absX, absY, 32767, 32767)
		})
	} else {
		connected := vw.videoClient != nil && vw.videoClient.IsConnected()
		logrus.Warnf("🖱️ [VC] DROPPED send: mi_nil=%v input_active=%v client_nil=%v client_connected=%v",
			mi == nil, mi != nil && mi.IsInputActive(), vw.videoClient == nil, connected)
	}
}

// flushVirtualCursorPosition sends the current cursor position to the host
// unconditionally — call this when the finger lifts so the final position
// is always delivered regardless of the rate limiter.
func (t *TouchpadWrapper) flushVirtualCursorPosition() {
	t.videoWidget.sendVirtualCursorToHost()
}

// TouchCancel handles a canceled touch (mobile)
func (t *TouchpadWrapper) TouchCancel(ev *mobile.TouchEvent) {
	t.endScrollbarDrag()
	if t.videoWidget.rmbHapticTimer != nil {
		t.videoWidget.rmbHapticTimer.Stop()
		t.videoWidget.rmbHapticTimer = nil
	}
	if t.videoWidget.lmbHoldTimer != nil {
		t.videoWidget.lmbHoldTimer.Stop()
		t.videoWidget.lmbHoldTimer = nil
	}
	t.videoWidget.lmbPendingHold = false
	t.videoWidget.lmbPendingDoubleClick = false
	t.videoWidget.lmbUp1Sent = false
	if t.videoWidget.lmbUpTimer != nil {
		t.videoWidget.lmbUpTimer.Stop()
		t.videoWidget.lmbUpTimer = nil
	}
	if t.videoWidget.lmbHeld {
		t.videoWidget.lmbHeld = false
		t.videoWidget.enqueueMouseButtonUp(1)
	}
	t.videoWidget.isDragging = false
	t.videoWidget.resetRelativeMoveAccumulator()
}

// Dragged handles a drag (implementation of fyne.Draggable)
// On desktop — updates the position for polling. On Android — the primary way finger movement is tracked.
func (t *TouchpadWrapper) Dragged(ev *fyne.DragEvent) {
	if !t.videoWidget.isMouseConnected {
		if atomic.CompareAndSwapInt32(&touchMouseCheckPending, 0, 1) {
			go func() {
				t.videoWidget.checkMouseConnected()
				atomic.StoreInt32(&touchMouseCheckPending, 0)
			}()
		}
		return
	}
	if t.videoWidget.shouldIgnoreTouchInput() {
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, true)
	if t.updateScrollbarDrag(ev.Position) {
		return
	}

	isAndroid := fyne.CurrentDevice().IsMobile()

	if isAndroid {
		mode := t.videoWidget.GetMouseInputMode()
		if mode == "touchscreen" {
			if t.videoWidget.touchActive {
				x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
				if x != t.videoWidget.lastTouchX || y != t.videoWidget.lastTouchY {
					t.videoWidget.lastTouchX = x
					t.videoWidget.lastTouchY = y
					t.videoWidget.enqueueTouch(x, y, true)
				}
			}
		} else if isVirtualCursorLikeMode(mode) {
			// Raw finger delta felt too "slippery"/fast for precise pointing —
			// scale it down before it reaches handleVirtualCursorMove's
			// rawDx/cw mapping (which is otherwise a direct 1:1 finger-to-cursor
			// fraction of the video width/height).
			const virtualCursorTouchSensitivity = 0.6
			t.handleVirtualCursorMove(ev.Dragged.DX*virtualCursorTouchSensitivity, ev.Dragged.DY*virtualCursorTouchSensitivity)
		} else if t.videoWidget.IsAbsoluteLikeInputMode() {
			vw := t.videoWidget
			px, py := ev.Position.X, ev.Position.Y
			x, y := vw.PositionToAbsolute(px, py)
			logrus.Debugf("🖱️ [AbsCursor] touch=(%.1f,%.1f) abs=(%d,%d) contentRect=(%.1f,%.1f,%.1f,%.1f) touchpad=(%.0f,%.0f) pan=(%.1f,%.1f) zoom=%.2f",
				px, py, x, y,
				vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH,
				vw.touchpadSizeW, vw.touchpadSizeH,
				vw.panOffsetX, vw.panOffsetY, vw.zoomScale)
			vw.SendAbsolutePosition(x, y, false)
			vw.lastMouseX = px
			vw.lastMouseY = py
		} else {
			// Touchpad: relative movement
			rawDx := ev.Position.X - t.videoWidget.lastMouseX
			rawDy := ev.Position.Y - t.videoWidget.lastMouseY
			t.videoWidget.lastMouseX = ev.Position.X
			t.videoWidget.lastMouseY = ev.Position.Y
			if !t.videoWidget.isDragging {
				t.videoWidget.isDragging = true
			}
			const touchpadSensitivity = 2.0
			dx, dy := t.videoWidget.accumulateRelativeMove(rawDx, rawDy, touchpadSensitivity)
			if dx == 0 && dy == 0 {
				return
			}
			t.videoWidget.enqueueMouseMove(dx, dy)
		}
	} else {
		if t.videoWidget.IsAbsoluteLikeInputMode() {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			t.videoWidget.SendAbsolutePosition(x, y, false)
		}
		t.videoWidget.currentMouseX = ev.Position.X
		t.videoWidget.currentMouseY = ev.Position.Y
	}
}

// DragEnd handles the end of a drag (implementation of fyne.Draggable)
// IMPORTANT: On desktop this method is NOT used (MouseUp is used instead)
// IMPORTANT: On Android it is called after a swipe finishes
func (t *TouchpadWrapper) DragEnd() {
	isAndroid := fyne.CurrentDevice().IsMobile()

	if isAndroid {
		if t.endScrollbarDrag() {
			return
		}
		logrus.Infof("🖱️ [DRAGGED] Android: DragEnd called, isDragging=%v lmbHeld=%v", t.videoWidget.isDragging, t.videoWidget.lmbHeld)
		// Cancel RMB haptic timer — finger lifted.
		if t.videoWidget.rmbHapticTimer != nil {
			t.videoWidget.rmbHapticTimer.Stop()
			t.videoWidget.rmbHapticTimer = nil
		}
		// On Android, DragEnd fires when the finger is lifted after a drag — Fyne may
		// not fire TouchUp in this case.  Release held LMB here so it doesn't stay stuck.
		if t.videoWidget.lmbPendingHold {
			t.videoWidget.lmbPendingHold = false
			if t.videoWidget.lmbHoldTimer != nil {
				t.videoWidget.lmbHoldTimer.Stop()
				t.videoWidget.lmbHoldTimer = nil
			}
		}
		if t.videoWidget.lmbHeld {
			t.videoWidget.lmbHeld = false
			if t.videoWidget.lmbUpTimer != nil {
				t.videoWidget.lmbUpTimer.Stop()
				t.videoWidget.lmbUpTimer = nil
			}
			t.videoWidget.enqueueMouseButtonUp(1)
			t.videoWidget.lastVirtualTapAt = time.Time{}
			t.videoWidget.isDragging = false
			t.videoWidget.resetRelativeMoveAccumulator()
			return
		}
		if t.videoWidget.isDragging {
			logrus.Info("🖱️ [DRAGGED] Android: Swipe completed")
			if isVirtualCursorLikeMode(t.videoWidget.GetMouseInputMode()) {
				t.flushVirtualCursorPosition()
			}
			t.videoWidget.isDragging = false
			t.videoWidget.resetRelativeMoveAccumulator()
		}
	}
	// On desktop we do nothing - MouseUp is used instead
}

// TypedKey handles a key press
// On macOS Canvas.SetOnTypedKey is unreliable, so we forward via the focused widget
func (t *TouchpadWrapper) TypedKey(key *fyne.KeyEvent) {
	if key != nil && key.Name == fyne.KeyF11 && t.videoWidget != nil && t.videoWidget.isStreaming {
		logrus.Info("⌨️ [TOUCHPAD][PRESS] opening fullscreen")
		t.videoWidget.ShowFullscreen()
		return
	}
	if t.onKeyPress != nil {
		t.onKeyPress(key)
	}
}

// TypedRune handles character input
func (t *TouchpadWrapper) TypedRune(r rune) {
	if t.onRunePress != nil {
		t.onRunePress(r)
	}
}

func (t *TouchpadWrapper) KeyDown(key *fyne.KeyEvent) {
	if t.onKeyDown != nil {
		t.onKeyDown(key)
	}
}

func (t *TouchpadWrapper) KeyUp(key *fyne.KeyEvent) {
	if t.onKeyUp != nil {
		t.onKeyUp(key)
	}
}

// requestFocus requests focus for the widget (desktop only, when a window is set)
func (t *TouchpadWrapper) requestFocus() {
	if t.window == nil || fyne.CurrentDevice().IsMobile() {
		return
	}
	target := fyne.Focusable(t)
	if t.windowFocusTarget != nil {
		target = t.windowFocusTarget
	}
	// Skip window.RequestFocus() when a native overlay owns the z-order — calling
	// SetForegroundWindow via GLFW would promote the Fyne TOPMOST window above the
	// Vulkan TOPMOST overlay. Canvas focus is still updated for Fyne's internal state.
	if !t.skipWindowFocus.Load() {
		t.window.RequestFocus()
	}
	t.window.Canvas().Focus(target)
}

// clamp restricts a value to the min..max range
func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

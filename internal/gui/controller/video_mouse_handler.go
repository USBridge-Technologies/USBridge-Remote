package controller

import (
	"image"
	"image/color"
	"math"
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

// Проверяем что TouchpadWrapper реализует все необходимые интерфейсы
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

// TouchpadWrapper обертка для videoCanvas с поддержкой мыши и тачскрина
type TouchpadWrapper struct {
	widget.BaseWidget
	videoWidget *VideoWidget
	image       *canvas.Image
	clip        *container.Clip
	cursor      *canvas.Image
	hScrollBar  *canvas.Rectangle
	vScrollBar  *canvas.Rectangle

	// Обработчики клавиатуры (для macOS, где Canvas.SetOnTypedKey может не работать)
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

// NewTouchpadWrapper создает обертку для тачпада
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

// NewTouchpadWrapperWithImage создает обертку для тачпада с заданным изображением
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

// SetKeyHandlers устанавливает обработчики клавиатуры (для macOS, где Canvas.SetOnTypedKey ненадёжен)
func (t *TouchpadWrapper) SetKeyHandlers(onKeyDown func(*fyne.KeyEvent), onKeyUp func(*fyne.KeyEvent), onKey func(*fyne.KeyEvent), onRune func(rune)) {
	t.onKeyDown = onKeyDown
	t.onKeyUp = onKeyUp
	t.onKeyPress = onKey
	t.onRunePress = onRune
}

// SetWindowForFocus устанавливает окно для запроса фокуса при клике (desktop)
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

// FocusGained реализация fyne.Focusable
func (t *TouchpadWrapper) FocusGained() {}

// FocusLost реализация fyne.Focusable
func (t *TouchpadWrapper) FocusLost() {}

func (t *TouchpadWrapper) Cursor() desktop.Cursor { return desktop.DefaultCursor }

// CreateRenderer создает renderer для виджета
func (t *TouchpadWrapper) CreateRenderer() fyne.WidgetRenderer {
	return &touchpadRenderer{
		wrapper: t,
	}
}

// touchpadRenderer рендерер для TouchpadWrapper
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

// updateTouchpadSize обновляет размер области ввода и прямоугольник видео (для корректного перевода координат в 0..4095).
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
	// Явно задаем размер контейнеру обрезки, чтобы видео не вылезало за границы виджета
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
		// Перемещаем наверх: вместо size.Height-margin-thickness используем просто margin
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
		// Проверка для верхней части экрана (pos.Y <= touchBand)
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

// Tapped вызывается при полном клике (нажали и отпустили на виджете). В Fyne MouseUp приходит виджету под курсором при отпускании,
// поэтому при отпускании вне виджета MouseUp мы не получим. Tapped же приходит только при завершённом клике по виджету — используем для тапа.
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
	// Absolute: сам клик уже приходит через MouseDown/MouseUp или TouchDown/TouchUp.
	// Здесь только синхронизируем позицию, чтобы не удваивать нажатие.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, true)
		return
	}
	logrus.Debugf("🖱️ Tapped at: %v (mouse mode, using TouchUp)", ev.Position)
}

// TappedSecondary — правый клик. В режиме тача: только обновляем позицию без касания (tip=false),
// затем один клик правой кнопкой. Пара touch_position(down)+touch_position(up) хост часто трактует
// как левый клик, из-за чего получалось «двойной левый» вместо правого.
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

// MouseDown обрабатывает нажатие кнопки мыши (desktop)
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

	// Тачскрин: отложенный touch(down). Для левой — touch+BTN_LEFT, для правой — только позиция (клик потом).
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.StartTouchDownDelay(x, y, btn)
	}
	// Absolute: синхронизируем позицию сразу при нажатии и атомарно обновляем кнопку.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		logrus.Infof("🖱️ [Drag] MouseDown btn=%d pos=(%.0f,%.0f) abs=(%d,%d)", btn, ev.Position.X, ev.Position.Y, x, y)
		t.videoWidget.PressAbsoluteButton(btn, x, y)
	}
}

// MouseUp обрабатывает отпускание кнопки мыши (desktop)
func (t *TouchpadWrapper) MouseUp(ev *desktop.MouseEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}

	dx := math.Abs(float64(ev.Position.X - t.videoWidget.touchStartX))
	dy := math.Abs(float64(ev.Position.Y - t.videoWidget.touchStartY))
	duration := time.Since(t.videoWidget.touchStartTime)

	// Тачскрин: отпускание (tip=false) — только при активном touch (таймер успел сработать / был драг)
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
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		button := t.videoWidget.dragButton
		logrus.Infof("🖱️ [Drag] MouseUp btn=%d pos=(%.0f,%.0f) abs=(%d,%d) dx=%.0f dy=%.0f dur=%v", button, ev.Position.X, ev.Position.Y, x, y, dx, dy, duration)
		t.videoWidget.dragButton = 0
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		t.videoWidget.ReleaseAbsoluteButton(button, x, y)
		return
	}

	// Режим мыши: как раньше
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

// MouseMoved обрабатывает перемещение мыши (desktop)
func (t *TouchpadWrapper) MouseMoved(ev *desktop.MouseEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.UpdateCursorOverlayFromLocalInput(ev.Position.X, ev.Position.Y, true)

	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		// Тачскрин: обновление позиции при драге. Левая — SendTouch, правая — только позиция (SendTouchPositionOnly).
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

	// Absolute: позиционирование курсора через absolute tablet.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, false)
	}

	t.videoWidget.currentMouseX = ev.Position.X
	t.videoWidget.currentMouseY = ev.Position.Y
}

// MouseIn обрабатывает вход курсора в область (desktop)
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

// MouseOut обрабатывает выход курсора из области (desktop)
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

// Scrolled обрабатывает прокрутку колеса мыши (desktop). В режиме тача скролл тоже отправляется на хост.
func (t *TouchpadWrapper) Scrolled(ev *fyne.ScrollEvent) {
	logrus.Debugf("🖱️ Scrolled: %v", ev.Scrolled)

	if !t.videoWidget.isMouseConnected {
		return
	}

	scroll := int(-ev.Scrolled.DY / 10)
	scroll = clamp(scroll, -127, 127)
	if scroll == 0 {
		return
	}
	// Absolute: перед скроллом синхронизируем абсолютную позицию.
	if t.videoWidget.IsAbsoluteLikeInputMode() {
		// Берем текущую позицию курсора внутри виджета (последнее известное).
		// MouseMoved/TouchMove обновляют lastAbsX/Y, но на некоторых платформах скролл может прийти без движения.
		x, y := t.videoWidget.lastAbsX, t.videoWidget.lastAbsY
		t.videoWidget.SendAbsoluteEvent(x, y, scroll, true)
		return
	}
	t.videoWidget.enqueueMouseScroll(scroll)
}

// TouchDown обрабатывает начало касания (mobile)
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

	if t.videoWidget.GetMouseInputMode() == mouseModeVirtualCursor {
		// If a quick tap just fired and second finger comes down within 600ms → hold LMB.
		if !t.videoWidget.lmbHeld &&
			!t.videoWidget.lastVirtualTapAt.IsZero() &&
			time.Since(t.videoWidget.lastVirtualTapAt) < 600*time.Millisecond {
			t.videoWidget.lastVirtualTapAt = time.Time{}
			t.videoWidget.lmbHeld = true
			t.videoWidget.enqueueMouseButtonDown(1)
		}
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

// TouchUp обрабатывает окончание касания (mobile)
func (t *TouchpadWrapper) TouchUp(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	// Release held LMB unconditionally — must not be blocked by shouldIgnoreTouchInput
	// or any other early return, otherwise LMB gets stuck pressed.
	if t.videoWidget.lmbHeld {
		t.videoWidget.lmbHeld = false
		t.videoWidget.enqueueMouseButtonUp(1)
		t.videoWidget.lastVirtualTapAt = time.Time{}
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
	if mode == mouseModeVirtualCursor {
		// Use physical distance from touch start only — don't check isDragging because
		// Dragged sets it even on stationary touches (touch sensor noise).
		if dx < 15 && dy < 15 {
			if duration >= time.Second {
				t.videoWidget.enqueueMouseClick(2) // right click (long press)
			} else if duration < 500*time.Millisecond {
				// Quick tap → left click, arm the drag window (600ms to place second finger).
				t.videoWidget.enqueueMouseClick(1)
				t.videoWidget.lastVirtualTapAt = time.Now()
			}
		}
		// Finger lifted — flush final position regardless of rate limiter.
		t.flushVirtualCursorPosition()
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}

	// Тачскрин: отпускание (tip=false)
	if mode == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.lastTouchX = x
		t.videoWidget.lastTouchY = y
		t.videoWidget.touchActive = false
		t.videoWidget.enqueueTouch(x, y, false)
		t.videoWidget.isDragging = false
		return
	}
	// Режим мыши: как раньше
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

// TouchMove обрабатывает перемещение касания (mobile)
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

	if mode == mouseModeVirtualCursor {
		t.handleVirtualCursorMove(ev.Position.X, ev.Position.Y)
		return
	}

	if t.videoWidget.IsAbsoluteLikeInputMode() {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.SendAbsolutePosition(x, y, false)
		t.videoWidget.lastMouseX = ev.Position.X
		t.videoWidget.lastMouseY = ev.Position.Y
		return
	}

	// Мышь: относительное перемещение
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

// handleVirtualCursorMove moves the virtual cursor by the swipe delta and
// forwards the absolute position to Moonlight at most every 8ms (same rate as
// Mac absolute mode). The local Vulkan cursor is always updated immediately.
func (t *TouchpadWrapper) handleVirtualCursorMove(posX, posY float32) {
	vw := t.videoWidget
	rawDx := posX - vw.lastMouseX
	rawDy := posY - vw.lastMouseY
	vw.lastMouseX = posX
	vw.lastMouseY = posY

	cw := vw.contentRectW
	ch := vw.contentRectH
	if cw > 0 {
		vw.virtualCursorU = clampFloat(vw.virtualCursorU+rawDx/cw, 0, 1)
	}
	if ch > 0 {
		vw.virtualCursorV = clampFloat(vw.virtualCursorV+rawDy/ch, 0, 1)
	}
	// Only mark as dragging after significant movement so that touch noise
	// doesn't prevent tap detection in TouchUp.
	if math.Abs(float64(rawDx)) >= 3 || math.Abs(float64(rawDy)) >= 3 {
		vw.isDragging = true
	}

	// Update the local Vulkan cursor immediately — instant regardless of network.
	vw.centerViewportOnVirtualCursor()
	vw.updateNativeViewportAndCursor()
	if !vw.isNativeVideoActive() {
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
// absolute Moonlight position, accounting for letterbox/pillarbox black bars.
func (vw *VideoWidget) sendVirtualCursorToHost() {
	fX, fY, fW, fH := vw.getFrameContentRect()
	var hostU, hostV float32
	if fW > 0 {
		hostU = clampFloat((vw.virtualCursorU-fX)/fW, 0, 1)
	} else {
		hostU = vw.virtualCursorU
	}
	if fH > 0 {
		hostV = clampFloat((vw.virtualCursorV-fY)/fH, 0, 1)
	} else {
		hostV = vw.virtualCursorV
	}
	absX := int(math.Round(float64(hostU * 32767)))
	absY := int(math.Round(float64(hostV * 32767)))
	mi := vw.moonlightInput()
	if mi != nil && mi.IsInputActive() {
		mi.SendMoonlightMousePosition(int16(absX), int16(absY), 32767, 32767)
		vw.lastVirtualCursorSentTime = time.Now()
	}

}

// flushVirtualCursorPosition sends the current cursor position to the host
// unconditionally — call this when the finger lifts so the final position
// is always delivered regardless of the rate limiter.
func (t *TouchpadWrapper) flushVirtualCursorPosition() {
	t.videoWidget.sendVirtualCursorToHost()
}

// TouchCancel обрабатывает отмену касания (mobile)
func (t *TouchpadWrapper) TouchCancel(ev *mobile.TouchEvent) {
	t.endScrollbarDrag()
	if t.videoWidget.lmbHeld {
		t.videoWidget.lmbHeld = false
		t.videoWidget.enqueueMouseButtonUp(1)
	}
	t.videoWidget.isDragging = false
	t.videoWidget.resetRelativeMoveAccumulator()
}

// Dragged обрабатывает драг (реализация fyne.Draggable)
// На desktop — обновление позиции для polling. На Android — основной способ движения пальца.
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
		} else if mode == mouseModeVirtualCursor {
			t.handleVirtualCursorMove(ev.Position.X, ev.Position.Y)
		} else if t.videoWidget.IsAbsoluteLikeInputMode() {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			t.videoWidget.SendAbsolutePosition(x, y, false)
			t.videoWidget.lastMouseX = ev.Position.X
			t.videoWidget.lastMouseY = ev.Position.Y
		} else {
			// Тачпад: относительное перемещение
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

// DragEnd обрабатывает окончание драга (реализация fyne.Draggable)
// ВАЖНО: На desktop этот метод НЕ используется (используется MouseUp)
// ВАЖНО: На Android вызывается после окончания свайпа
func (t *TouchpadWrapper) DragEnd() {
	isAndroid := fyne.CurrentDevice().IsMobile()

	if isAndroid {
		if t.endScrollbarDrag() {
			return
		}
		logrus.Infof("🖱️ [DRAGGED] Android: DragEnd called, isDragging=%v lmbHeld=%v", t.videoWidget.isDragging, t.videoWidget.lmbHeld)
		// On Android, DragEnd fires when the finger is lifted after a drag — Fyne may
		// not fire TouchUp in this case.  Release held LMB here so it doesn't stay stuck.
		if t.videoWidget.lmbHeld {
			t.videoWidget.lmbHeld = false
			t.videoWidget.enqueueMouseButtonUp(1)
			t.videoWidget.lastVirtualTapAt = time.Time{}
			t.videoWidget.isDragging = false
			t.videoWidget.resetRelativeMoveAccumulator()
			return
		}
		if t.videoWidget.isDragging {
			logrus.Info("🖱️ [DRAGGED] Android: Swipe completed")
			if t.videoWidget.GetMouseInputMode() == mouseModeVirtualCursor {
				t.flushVirtualCursorPosition()
			}
			t.videoWidget.isDragging = false
			t.videoWidget.resetRelativeMoveAccumulator()
		}
	}
	// На desktop ничего не делаем - используется MouseUp
}

// TypedKey обрабатывает нажатие клавиши
// На macOS Canvas.SetOnTypedKey ненадёжен, поэтому пересылаем через виджет с фокусом
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

// TypedRune обрабатывает ввод символа
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

// requestFocus запрашивает фокус для виджета (только на desktop, когда окно задано)
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

// clamp ограничивает значение в пределах min..max
func clamp(value, min, max int) int {
	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

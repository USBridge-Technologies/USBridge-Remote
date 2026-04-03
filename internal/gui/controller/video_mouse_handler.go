package controller

import (
	"image/color"
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// Проверяем что TouchpadWrapper реализует все необходимые интерфейсы
var (
	_ fyne.Tappable          = (*TouchpadWrapper)(nil)
	_ fyne.SecondaryTappable = (*TouchpadWrapper)(nil)
	_ fyne.Scrollable        = (*TouchpadWrapper)(nil)
	_ fyne.Draggable         = (*TouchpadWrapper)(nil)
	_ fyne.Focusable         = (*TouchpadWrapper)(nil)
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
	hScrollBar  *canvas.Rectangle
	vScrollBar  *canvas.Rectangle

	// Обработчики клавиатуры (для macOS, где Canvas.SetOnTypedKey может не работать)
	window      fyne.Window
	onKeyPress  func(*fyne.KeyEvent)
	onRunePress func(rune)
}

// NewTouchpadWrapper создает обертку для тачпада
func NewTouchpadWrapper(videoWidget *VideoWidget) *TouchpadWrapper {
	wrapper := &TouchpadWrapper{
		videoWidget: videoWidget,
		image:       videoWidget.videoCanvas,
		hScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
		vScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
	}
	if wrapper.image != nil {
		wrapper.clip = container.NewClip(wrapper.image)
	}
	wrapper.hScrollBar.Hide()
	wrapper.vScrollBar.Hide()
	wrapper.ExtendBaseWidget(wrapper)
	return wrapper
}

// NewTouchpadWrapperWithImage создает обертку для тачпада с заданным изображением
func NewTouchpadWrapperWithImage(videoWidget *VideoWidget, image *canvas.Image) *TouchpadWrapper {
	wrapper := &TouchpadWrapper{
		videoWidget: videoWidget,
		image:       image,
		hScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
		vScrollBar:  canvas.NewRectangle(color.NRGBA{R: 255, G: 255, B: 255, A: 120}),
	}
	if wrapper.image != nil {
		wrapper.clip = container.NewClip(wrapper.image)
	}
	wrapper.hScrollBar.Hide()
	wrapper.vScrollBar.Hide()
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

// SetKeyHandlers устанавливает обработчики клавиатуры (для macOS, где Canvas.SetOnTypedKey ненадёжен)
func (t *TouchpadWrapper) SetKeyHandlers(onKey func(*fyne.KeyEvent), onRune func(rune)) {
	t.onKeyPress = onKey
	t.onRunePress = onRune
}

// SetWindowForFocus устанавливает окно для запроса фокуса при клике (desktop)
func (t *TouchpadWrapper) SetWindowForFocus(w fyne.Window) {
	t.window = w
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
}

func (r *touchpadRenderer) MinSize() fyne.Size {
	return fyne.NewSize(320, 240)
}

func (r *touchpadRenderer) Refresh() {
	if r.wrapper.clip != nil {
		r.wrapper.wrapperLayoutImage(r.wrapper.Size())
		canvas.Refresh(r.wrapper.clip)
	}
	if r.wrapper.image != nil {
		canvas.Refresh(r.wrapper.image)
	}
}

func (r *touchpadRenderer) Objects() []fyne.CanvasObject {
	if r.wrapper.clip == nil {
		return []fyne.CanvasObject{r.wrapper.hScrollBar, r.wrapper.vScrollBar}
	}
	return []fyne.CanvasObject{r.wrapper.clip, r.wrapper.hScrollBar, r.wrapper.vScrollBar}
}

func (r *touchpadRenderer) Destroy() {
}

// updateTouchpadSize обновляет размер области ввода и прямоугольник видео (для корректного перевода координат в 0..4095).
func (t *TouchpadWrapper) updateTouchpadSize() {
	s := t.Size()
	if s.Width > 0 && s.Height > 0 {
		t.videoWidget.UpdateTouchpadAndContentRect(s.Width, s.Height)
	}
}

func (t *TouchpadWrapper) wrapperLayoutImage(size fyne.Size) {
	if t.image == nil {
		return
	}
	t.videoWidget.UpdateTouchpadAndContentRect(size.Width, size.Height)
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
		t.hScrollBar.Move(fyne.NewPos(margin+(trackW*progress), size.Height-margin-thickness))
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
		if pos.Y >= size.Height-touchBand && pos.X >= thumbX-touchBand/2 && pos.X <= thumbX+thumbW+touchBand/2 {
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
	t.requestFocus()
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
	t.updateTouchpadSize()
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
		if t.videoWidget.dragButton != 0 || t.videoWidget.absButtons != 0 {
			x, y := t.videoWidget.lastAbsX, t.videoWidget.lastAbsY
			t.videoWidget.dragButton = 0
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
		return
	}
	if t.videoWidget.shouldIgnoreTouchInput() {
		return
	}
	t.updateTouchpadSize()
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
	if t.videoWidget.shouldIgnoreTouchInput() {
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	t.updateTouchpadSize()
	if t.endScrollbarDrag() {
		return
	}
	dx := math.Abs(float64(ev.Position.X - t.videoWidget.touchStartX))
	dy := math.Abs(float64(ev.Position.Y - t.videoWidget.touchStartY))
	duration := time.Since(t.videoWidget.touchStartTime)

	// Тачскрин: отпускание (tip=false)
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
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
	if t.updateScrollbarDrag(ev.Position) {
		return
	}

	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
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

// TouchCancel обрабатывает отмену касания (mobile)
func (t *TouchpadWrapper) TouchCancel(ev *mobile.TouchEvent) {
	t.endScrollbarDrag()
	t.videoWidget.isDragging = false
	t.videoWidget.resetRelativeMoveAccumulator()
}

// Dragged обрабатывает драг (реализация fyne.Draggable)
// На desktop — обновление позиции для polling. На Android — основной способ движения пальца.
func (t *TouchpadWrapper) Dragged(ev *fyne.DragEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	if t.videoWidget.shouldIgnoreTouchInput() {
		t.videoWidget.isDragging = false
		t.videoWidget.resetRelativeMoveAccumulator()
		return
	}
	t.updateTouchpadSize()
	if t.updateScrollbarDrag(ev.Position) {
		return
	}

	isAndroid := fyne.CurrentDevice().IsMobile()

	if isAndroid {
		if t.videoWidget.GetMouseInputMode() == "touchscreen" {
			if t.videoWidget.touchActive {
				x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
				if x != t.videoWidget.lastTouchX || y != t.videoWidget.lastTouchY {
					t.videoWidget.lastTouchX = x
					t.videoWidget.lastTouchY = y
					t.videoWidget.enqueueTouch(x, y, true)
				}
			}
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
		logrus.Infof("🖱️ [DRAGGED] Android: DragEnd called, isDragging=%v", t.videoWidget.isDragging)
		// На Android завершаем свайп
		if t.videoWidget.isDragging {
			logrus.Info("🖱️ [DRAGGED] Android: Swipe completed")
			t.videoWidget.isDragging = false
			t.videoWidget.resetRelativeMoveAccumulator()
		}
	}
	// На desktop ничего не делаем - используется MouseUp
}

// TypedKey обрабатывает нажатие клавиши
// На macOS Canvas.SetOnTypedKey ненадёжен, поэтому пересылаем через виджет с фокусом
func (t *TouchpadWrapper) TypedKey(key *fyne.KeyEvent) {
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

// requestFocus запрашивает фокус для виджета (только на desktop, когда окно задано)
func (t *TouchpadWrapper) requestFocus() {
	if t.window == nil || fyne.CurrentDevice().IsMobile() {
		return
	}
	// На macOS fullscreen окно должно быть key window для приёма клавиатуры
	t.window.RequestFocus()
	t.window.Canvas().Focus(t)
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

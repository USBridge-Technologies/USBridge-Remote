package ui

import (
	"math"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
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
	_ desktop.Mouseable      = (*TouchpadWrapper)(nil)
	_ desktop.Hoverable      = (*TouchpadWrapper)(nil)
	_ mobile.Touchable       = (*TouchpadWrapper)(nil)
)

// TouchpadWrapper обертка для videoCanvas с поддержкой мыши и тачскрина
type TouchpadWrapper struct {
	widget.BaseWidget
	videoWidget *VideoWidget
	image       *canvas.Image

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
	}
	wrapper.ExtendBaseWidget(wrapper)
	return wrapper
}

// NewTouchpadWrapperWithImage создает обертку для тачпада с заданным изображением
func NewTouchpadWrapperWithImage(videoWidget *VideoWidget, image *canvas.Image) *TouchpadWrapper {
	wrapper := &TouchpadWrapper{
		videoWidget: videoWidget,
		image:       image,
	}
	wrapper.ExtendBaseWidget(wrapper)
	return wrapper
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

// CreateRenderer создает renderer для виджета
func (t *TouchpadWrapper) CreateRenderer() fyne.WidgetRenderer {
	return &touchpadRenderer{
		wrapper: t,
		image:   t.image,
	}
}

// touchpadRenderer рендерер для TouchpadWrapper
type touchpadRenderer struct {
	wrapper *TouchpadWrapper
	image   *canvas.Image
}

func (r *touchpadRenderer) Layout(size fyne.Size) {
	r.image.Resize(size)
}

func (r *touchpadRenderer) MinSize() fyne.Size {
	return fyne.NewSize(320, 240)
}

func (r *touchpadRenderer) Refresh() {
	canvas.Refresh(r.image)
}

func (r *touchpadRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.image}
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

// Tapped вызывается при полном клике (нажали и отпустили на виджете). В Fyne MouseUp приходит виджету под курсором при отпускании,
// поэтому при отпускании вне виджета MouseUp мы не получим. Tapped же приходит только при завершённом клике по виджету — используем для тапа.
func (t *TouchpadWrapper) Tapped(ev *fyne.PointEvent) {
	t.requestFocus()
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		t.videoWidget.touchActive = false
		t.videoWidget.dragButton = 0
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		go func() {
			_ = t.videoWidget.usbClient.SendTouch(x, y, true)
			_ = t.videoWidget.usbClient.SendTouch(x, y, false)
		}()
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
	t.updateTouchpadSize()
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		t.videoWidget.CancelTouchDownDelay()
		t.videoWidget.touchActive = false
		t.videoWidget.dragButton = 0
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		go func() {
			_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, false)
			_ = t.videoWidget.usbClient.SendMouseClick(2)
		}()
		return
	}
	go func() {
		_ = t.videoWidget.usbClient.SendMouseClick(2)
	}()
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
				go func() {
					_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, false)
					_ = t.videoWidget.usbClient.SendMouseClick(2)
				}()
			} else {
				go func() {
					_ = t.videoWidget.usbClient.SendTouch(x, y, false)
				}()
			}
		} else {
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
		}
		return
	}

	// Режим мыши: как раньше
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		t.videoWidget.dragButton = 0
		return
	}
	if dx < 10 && dy < 10 && duration < 300*time.Millisecond {
		button := t.videoWidget.dragButton
		t.videoWidget.dragButton = 0
		go func() {
			_ = t.videoWidget.usbClient.SendMouseClick(button)
		}()
	} else if duration >= 500*time.Millisecond && dx < 20 && dy < 20 {
		if t.videoWidget.dragButton == 1 {
			t.videoWidget.dragButton = 0
			go func() {
				_ = t.videoWidget.usbClient.SendMouseClick(2)
			}()
		} else {
			button := t.videoWidget.dragButton
			t.videoWidget.dragButton = 0
			go func() {
				_ = t.videoWidget.usbClient.SendMouseClick(button)
			}()
		}
	} else {
		t.videoWidget.dragButton = 0
	}
	t.videoWidget.isDragging = false
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
					go func() {
						_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, true)
					}()
				} else {
					go func() {
						_ = t.videoWidget.usbClient.SendTouch(x, y, true)
					}()
				}
			}
		}
	}

	// Absolute: позиционирование курсора через touch_position (без tip), кнопки остаются мышиными.
	if t.videoWidget.GetMouseInputMode() == "absolute" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		if x != t.videoWidget.lastAbsX || y != t.videoWidget.lastAbsY {
			t.videoWidget.lastAbsX = x
			t.videoWidget.lastAbsY = y
			go func() {
				_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, false)
			}()
		}
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
	if t.videoWidget.GetMouseInputMode() == "absolute" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.lastAbsX = x
		t.videoWidget.lastAbsY = y
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
				go func() {
					_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, false)
				}()
			} else {
				go func() {
					_ = t.videoWidget.usbClient.SendTouch(x, y, false)
				}()
			}
		} else {
			t.videoWidget.dragButton = 0
			t.videoWidget.isDragging = false
		}
		return
	}
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		t.videoWidget.dragButton = 0
		go func() {
			_ = t.videoWidget.usbClient.SendMouseAction(0, 0, 0, 0)
		}()
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
	go func() {
		_ = t.videoWidget.usbClient.SendMouseScroll(scroll)
	}()
}

// TouchDown обрабатывает начало касания (mobile)
func (t *TouchpadWrapper) TouchDown(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()
	t.videoWidget.touchStartX = ev.Position.X
	t.videoWidget.touchStartY = ev.Position.Y
	t.videoWidget.touchStartTime = time.Now()
	t.videoWidget.lastMouseX = ev.Position.X
	t.videoWidget.lastMouseY = ev.Position.Y
	t.videoWidget.isDragging = false
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		if !t.videoWidget.TryRecordTouchDown(x, y) {
			return
		}
		t.videoWidget.touchActive = true
		go func() {
			_ = t.videoWidget.usbClient.SendTouch(x, y, true)
		}()
	}
}

// TouchUp обрабатывает окончание касания (mobile)
func (t *TouchpadWrapper) TouchUp(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()
	dx := math.Abs(float64(ev.Position.X - t.videoWidget.touchStartX))
	dy := math.Abs(float64(ev.Position.Y - t.videoWidget.touchStartY))
	duration := time.Since(t.videoWidget.touchStartTime)

	// Тачскрин: отпускание (tip=false)
	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		t.videoWidget.lastTouchX = x
		t.videoWidget.lastTouchY = y
		t.videoWidget.touchActive = false
		go func() {
			_ = t.videoWidget.usbClient.SendTouch(x, y, false)
		}()
		t.videoWidget.isDragging = false
		return
	}

	// Режим мыши: как раньше
	if t.videoWidget.isDragging {
		t.videoWidget.isDragging = false
		return
	}
	if dx < 10 && dy < 10 && duration < 300*time.Millisecond {
		go func() {
			_ = t.videoWidget.usbClient.SendMouseClick(1)
		}()
	} else if duration >= 1*time.Second && dx < 20 && dy < 20 {
		go func() {
			_ = t.videoWidget.usbClient.SendMouseClick(2)
		}()
	}
	t.videoWidget.isDragging = false
}

// TouchMove обрабатывает перемещение касания (mobile)
func (t *TouchpadWrapper) TouchMove(ev *mobile.TouchEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()

	if t.videoWidget.GetMouseInputMode() == "touchscreen" {
		if t.videoWidget.touchActive {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			if x != t.videoWidget.lastTouchX || y != t.videoWidget.lastTouchY {
				t.videoWidget.lastTouchX = x
				t.videoWidget.lastTouchY = y
				go func() {
					_ = t.videoWidget.usbClient.SendTouch(x, y, true)
				}()
			}
		}
		t.videoWidget.lastMouseX = ev.Position.X
		t.videoWidget.lastMouseY = ev.Position.Y
		return
	}

	if t.videoWidget.GetMouseInputMode() == "absolute" {
		x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
		if x != t.videoWidget.lastAbsX || y != t.videoWidget.lastAbsY {
			t.videoWidget.lastAbsX = x
			t.videoWidget.lastAbsY = y
			go func() {
				_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, false)
			}()
		}
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
	dx := int(float32(rawDx) * touchpadSensitivity)
	dy := int(float32(rawDy) * touchpadSensitivity)
	dx = clamp(dx, -127, 127)
	dy = clamp(dy, -127, 127)
	go func() {
		_ = t.videoWidget.usbClient.SendMouseMove(dx, dy)
	}()
}

// TouchCancel обрабатывает отмену касания (mobile)
func (t *TouchpadWrapper) TouchCancel(ev *mobile.TouchEvent) {
	t.videoWidget.isDragging = false
}

// Dragged обрабатывает драг (реализация fyne.Draggable)
// На desktop — обновление позиции для polling. На Android — основной способ движения пальца.
func (t *TouchpadWrapper) Dragged(ev *fyne.DragEvent) {
	if !t.videoWidget.isMouseConnected {
		return
	}
	t.updateTouchpadSize()

	isAndroid := fyne.CurrentDevice().IsMobile()

	if isAndroid {
		if t.videoWidget.GetMouseInputMode() == "touchscreen" {
			if t.videoWidget.touchActive {
				x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
				if x != t.videoWidget.lastTouchX || y != t.videoWidget.lastTouchY {
					t.videoWidget.lastTouchX = x
					t.videoWidget.lastTouchY = y
					go func() {
						_ = t.videoWidget.usbClient.SendTouch(x, y, true)
					}()
				}
			}
		} else if t.videoWidget.GetMouseInputMode() == "absolute" {
			x, y := t.videoWidget.PositionToAbsolute(ev.Position.X, ev.Position.Y)
			if x != t.videoWidget.lastAbsX || y != t.videoWidget.lastAbsY {
				t.videoWidget.lastAbsX = x
				t.videoWidget.lastAbsY = y
				go func() {
					_ = t.videoWidget.usbClient.SendTouchPositionOnly(x, y, false)
				}()
			}
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
			dx := int(float32(rawDx) * touchpadSensitivity)
			dy := int(float32(rawDy) * touchpadSensitivity)
			dx = clamp(dx, -127, 127)
			dy = clamp(dy, -127, 127)
			go func() {
				_ = t.videoWidget.usbClient.SendMouseMove(dx, dy)
			}()
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
		logrus.Infof("🖱️ [DRAGGED] Android: DragEnd called, isDragging=%v", t.videoWidget.isDragging)
		// На Android завершаем свайп
		if t.videoWidget.isDragging {
			logrus.Info("🖱️ [DRAGGED] Android: Swipe completed")
			t.videoWidget.isDragging = false
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

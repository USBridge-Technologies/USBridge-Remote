package graphics

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// VirtualKeyboard виртуальная клавиатура для полноэкранного режима
type VirtualKeyboard struct {
	container      *fyne.Container
	keyboard       *fyne.Container
	toggleBtn      *widget.Button
	isVisible      bool
	onKeyPress     func(keyCode int, modifiers int)
	onRuneTyped    func(r rune)      // Отправка каждого символа на хост
	onTextTyped    func(text string) // Отправка текста пачкой
	parentWindow   fyne.Window
	keyboardWindow fyne.Window

	// Состояние модификаторов
	ctrlPressed     bool
	altPressed      bool
	shiftPressed    bool
	capsLockPressed bool
	winPressed      bool

	// Кнопки модификаторов для обновления стиля
	ctrlBtn     *widget.Button
	altBtn      *widget.Button
	shiftBtn    *widget.Button
	capsLockBtn *widget.Button
	winBtn      *widget.Button
	
	// Платформозависимые поля: на десктопе используются заглушки типов (определены в _desktop.go),
	// на мобилках — реальные реализации (определены в _mobile.go)
	mobileInput *backspaceEntry

	// Динамический отступ снизу для мобильного IME
	imeSpacer     *imeSpacerLayout
	imeSpacerCont *fyne.Container
	onIMEChanged  func(open bool)
}

// SetOnTextTyped устанавливает обработчик для вставки текста пачкой
func (vk *VirtualKeyboard) SetOnTextTyped(fn func(text string)) {
	vk.onTextTyped = fn
}

// NewVirtualKeyboard создает новую виртуальную клавиатуру.
// onRuneTyped — опциональный колбэк для немедленной отправки каждого символа на хост (Android/iOS).
func NewVirtualKeyboard(parentWindow fyne.Window, onKeyPress func(int, int), onRuneTyped func(r rune)) *VirtualKeyboard {
	vk := &VirtualKeyboard{
		isVisible:    false,
		onKeyPress:   onKeyPress,
		onRuneTyped:  onRuneTyped,
		parentWindow: parentWindow,
	}

	vk.createKeyboard()
	return vk
}

// createKeyboard создает интерфейс клавиатуры
func (vk *VirtualKeyboard) createKeyboard() {
	// Создаем кнопку переключения видимости (всегда видимая в углу)
	vk.toggleBtn = widget.NewButton("⌨", vk.toggleVisibility)
	vk.toggleBtn.Importance = widget.HighImportance // Делаем кнопку заметной

	// Вызываем платформозависимую реализацию раскладки (через build tags)
	vk.keyboard = vk.createKeyboardLayout()
	vk.keyboard.Hide() // Скрываем по умолчанию

	// Создаем контейнер для позиционирования
	vk.container = container.NewWithoutLayout()

	// Добавляем кнопку переключения в правый нижний угол
	vk.container.Add(vk.toggleBtn)

	// Добавляем клавиатуру по центру
	vk.container.Add(vk.keyboard)
}

// createKey создает кнопку клавиши (общее для мобилки и десктопа)
func (vk *VirtualKeyboard) createKey(label string, keyCode int, modifiers int) *widget.Button {
	btn := widget.NewButton(label, func() {
		vk.handleKeyPress(keyCode, modifiers)
	})

	// Устанавливаем размеры клавиш по умолчанию
	btn.Resize(fyne.NewSize(40, 35))

	// Делаем пробел шире
	if label == "Space" {
		btn.Resize(fyne.NewSize(200, 35))
	}

	// Делаем специальные клавиши шире
	if label == "Tab" || label == "Caps" || label == "Shift" || label == "Ctrl" || label == "Alt" {
		btn.Resize(fyne.NewSize(60, 35))
	}

	// Делаем Enter шире
	if label == "Enter" {
		btn.Resize(fyne.NewSize(80, 35))
	}

	// Делаем Backspace шире
	if label == "⌫" {
		btn.Resize(fyne.NewSize(80, 35))
	}

	// Делаем функциональные клавиши меньше
	if label == "F1" || label == "F2" || label == "F3" || label == "F4" ||
		label == "F5" || label == "F6" || label == "F7" || label == "F8" ||
		label == "F9" || label == "F10" || label == "F11" || label == "F12" {
		btn.Resize(fyne.NewSize(35, 30))
	}

	// Делаем Escape меньше
	if label == "Esc" {
		btn.Resize(fyne.NewSize(35, 30))
	}

	// Делаем Windows и Menu клавиши среднего размера
	if label == "Win" || label == "Menu" || label == "⊞" || label == "☰" {
		btn.Resize(fyne.NewSize(50, 35))
	}

	return btn
}

// createModifierKey создает кнопку модификатора с переключением
func (vk *VirtualKeyboard) createModifierKey(label string, keyCode int) *widget.Button {
	btn := widget.NewButton(label, func() {
		vk.toggleModifier(keyCode)
	})

	btn.Resize(fyne.NewSize(60, 35))
	return btn
}

// toggleModifier переключает состояние модификатора
func (vk *VirtualKeyboard) toggleModifier(keyCode int) {
	switch keyCode {
	case 224, 228: // Ctrl (Left/Right)
		vk.ctrlPressed = !vk.ctrlPressed
		vk.updateModifierButton(vk.ctrlBtn, "Ctrl", vk.ctrlPressed)
		logrus.Infof("⌨️ Ctrl toggled: %v", vk.ctrlPressed)
	case 226, 230: // Alt (Left/Right)
		vk.altPressed = !vk.altPressed
		vk.updateModifierButton(vk.altBtn, "Alt", vk.altPressed)
		logrus.Infof("⌨️ Alt toggled: %v", vk.altPressed)
	case 225, 229: // Shift (Left/Right)
		vk.shiftPressed = !vk.shiftPressed
		vk.updateModifierButton(vk.shiftBtn, "Shift", vk.shiftPressed)
		logrus.Infof("⌨️ Shift toggled: %v", vk.shiftPressed)
	case 227, 231: // Win/GUI (Left/Right)
		vk.winPressed = !vk.winPressed
		vk.updateModifierButton(vk.winBtn, "Win", vk.winPressed)
		logrus.Infof("⌨️ Win toggled: %v", vk.winPressed)
	case 57: // Caps Lock
		vk.capsLockPressed = !vk.capsLockPressed
		vk.updateModifierButton(vk.capsLockBtn, "Caps", vk.capsLockPressed)
		if vk.onKeyPress != nil {
			vk.onKeyPress(57, 0)
		}
		logrus.Infof("⌨️ CapsLock toggled: %v", vk.capsLockPressed)
	}
}

// updateModifierButton обновляет внешний вид кнопки модификатора
func (vk *VirtualKeyboard) updateModifierButton(btn *widget.Button, label string, pressed bool) {
	if btn == nil {
		return
	}

	if pressed {
		btn.Importance = widget.HighImportance
	} else {
		btn.Importance = widget.MediumImportance
	}
	btn.Refresh()
}

// handleKeyPress обрабатывает нажатие клавиши
func (vk *VirtualKeyboard) handleKeyPress(keyCode int, modifiers int) {
	currentModifiers := modifiers
	if vk.ctrlPressed {
		currentModifiers |= 1 // Left Control
	}
	if vk.shiftPressed {
		currentModifiers |= 2 // Left Shift
	}
	if vk.altPressed {
		currentModifiers |= 4 // Left Alt
	}
	if vk.winPressed {
		currentModifiers |= 8 // Left GUI (Windows)
	}

	logrus.Infof("⌨️ Virtual keyboard: key %d pressed with modifiers %d (active: Ctrl=%v, Shift=%v, Alt=%v, Win=%v)",
		keyCode, currentModifiers, vk.ctrlPressed, vk.shiftPressed, vk.altPressed, vk.winPressed)

	if vk.onKeyPress != nil {
		vk.onKeyPress(keyCode, currentModifiers)
	}
}

// toggleVisibility переключает видимость клавиатуры
func (vk *VirtualKeyboard) toggleVisibility() {
	if vk.isVisible {
		vk.Hide()
	} else {
		vk.ShowInSeparateWindow()
	}
}

// Show показывает клавиатуру
func (vk *VirtualKeyboard) Show() {
	if vk.isVisible {
		return
	}

	vk.isVisible = true
	vk.keyboard.Show()

	if vk.parentWindow != nil {
		size := vk.parentWindow.Canvas().Size()
		keyboardSize := fyne.NewSize(700, 250)

		x := (size.Width - keyboardSize.Width) / 2
		y := (size.Height - keyboardSize.Height) / 2

		vk.keyboard.Move(fyne.NewPos(x, y))
		vk.keyboard.Resize(keyboardSize)
	}

	if vk.parentWindow != nil {
		size := vk.parentWindow.Canvas().Size()
		btnSize := fyne.NewSize(50, 50)

		x := size.Width - btnSize.Width - 10
		y := size.Height - btnSize.Height - 10

		vk.toggleBtn.Move(fyne.NewPos(x, y))
		vk.toggleBtn.Resize(btnSize)
	}

	logrus.Info("⌨️ Virtual keyboard shown")
}

// ShowInSeparateWindow показывает клавиатуру в отдельном окне
func (vk *VirtualKeyboard) ShowInSeparateWindow() {
	if vk.isVisible {
		return
	}

	logrus.Info("⌨️ Opening virtual keyboard in a separate window")

	vk.keyboardWindow = fyne.CurrentApp().NewWindow(i18n.Current.VirtualKeyboard)
	vk.keyboardWindow.SetContent(vk.keyboard)
	vk.keyboardWindow.Resize(fyne.NewSize(600, 260))
	vk.keyboardWindow.CenterOnScreen()

	vk.keyboardWindow.SetOnClosed(func() {
		logrus.Info("⌨️ Virtual keyboard window closed")
		vk.isVisible = false
		vk.keyboardWindow = nil
	})

	vk.isVisible = true
	vk.keyboard.Show()
	vk.keyboardWindow.Show()

	logrus.Info("⌨️ Virtual keyboard shown in a separate window")
}

// Hide скрывает клавиатуру
func (vk *VirtualKeyboard) Hide() {
	if !vk.isVisible {
		return
	}

	vk.isVisible = false

	if vk.keyboardWindow != nil {
		vk.keyboardWindow.Close()
		vk.keyboardWindow = nil
	}

	// Сбрасываем IME-отступ (метод-заглушка на десктопе, реальный на мобилках)
	vk.setIMEOffset(0)

	if vk.keyboard != nil {
		vk.keyboard.Hide()
	}
	vk.BlurInput()

	logrus.Info("⌨️ Virtual keyboard hidden")
}

// IsVisible возвращает состояние видимости
func (vk *VirtualKeyboard) IsVisible() bool {
	return vk.isVisible
}

// GetContainer возвращает контейнер клавиатуры
func (vk *VirtualKeyboard) GetContainer() *fyne.Container {
	return vk.container
}

// GetKeyboardLayout возвращает только layout клавиатуры
func (vk *VirtualKeyboard) GetKeyboardLayout() *fyne.Container {
	return vk.keyboard
}

// UpdatePosition обновляет позицию элементов клавиатуры
func (vk *VirtualKeyboard) UpdatePosition(windowSize fyne.Size) {
	btnSize := fyne.NewSize(50, 50)
	x := windowSize.Width - btnSize.Width - 10
	y := windowSize.Height - btnSize.Height - 10

	vk.toggleBtn.Move(fyne.NewPos(x, y))
	vk.toggleBtn.Resize(btnSize)
}

// SetVisibleState устанавливает состояние видимости без показа отдельного окна
func (vk *VirtualKeyboard) SetVisibleState(visible bool) {
	vk.isVisible = visible
	if vk.keyboard == nil {
		return
	}
	if visible {
		vk.keyboard.Show()
		return
	}
	vk.setIMEOffset(0)
	vk.keyboard.Hide()
	vk.BlurInput()
}

package graphics

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// VirtualKeyboard virtual keyboard for fullscreen mode
type VirtualKeyboard struct {
	container      *fyne.Container
	keyboard       *fyne.Container
	toggleBtn      *widget.Button
	isVisible      bool
	onKeyPress     func(keyCode int, modifiers int)
	onRuneTyped    func(r rune) // Sending each character to the host
	parentWindow   fyne.Window
	keyboardWindow fyne.Window

	// Modifier state
	ctrlPressed     bool
	altPressed      bool
	shiftPressed    bool
	capsLockPressed bool
	winPressed      bool

	// Modifier buttons for style updates
	ctrlBtn     *widget.Button
	altBtn      *widget.Button
	shiftBtn    *widget.Button
	capsLockBtn *widget.Button
	winBtn      *widget.Button
	
	// Platform-dependent fields: dummy types are used on desktop (defined in _desktop.go),
	// real implementations on mobile (defined in _mobile.go)
	mobileInput *backspaceEntry

	// Dynamic bottom padding for mobile IME
	imeSpacer     *imeSpacerLayout
	imeSpacerCont *fyne.Container
	onIMEChanged  func(imeHeightDp float32) // 0 = IME closed

	// Called after keyboardWindow.Show() — platform code can use this to adjust Z-order.
	onWindowShown func(fyne.Window)
}

// NewVirtualKeyboard creates a new virtual keyboard.
// onRuneTyped is an optional callback for sending each character to the host immediately (Android/iOS).
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

// createKeyboard creates the keyboard interface
func (vk *VirtualKeyboard) createKeyboard() {
	// Create the visibility toggle button (always visible in the corner)
	vk.toggleBtn = widget.NewButton("⌨", vk.toggleVisibility)
	vk.toggleBtn.Importance = widget.HighImportance // Make the button noticeable

	// Call the platform-dependent layout implementation (via build tags)
	vk.keyboard = vk.createKeyboardLayout()
	vk.keyboard.Hide() // Hide by default

	// Create container for positioning
	vk.container = container.NewWithoutLayout()

	// Add toggle button to the bottom right corner
	vk.container.Add(vk.toggleBtn)

	// Add keyboard to the center
	vk.container.Add(vk.keyboard)
}

// createKey creates a key button (common for mobile and desktop)
func (vk *VirtualKeyboard) createKey(label string, keyCode int, modifiers int) *widget.Button {
	btn := widget.NewButton(label, func() {
		vk.handleKeyPress(keyCode, modifiers)
	})

	// Set default key sizes
	btn.Resize(fyne.NewSize(40, 35))

	// Make spacebar wider
	if label == "Space" {
		btn.Resize(fyne.NewSize(200, 35))
	}

	// Make special keys wider
	if label == "Tab" || label == "Caps" || label == "Shift" || label == "Ctrl" || label == "Alt" {
		btn.Resize(fyne.NewSize(60, 35))
	}

	// Make Enter wider
	if label == "Enter" {
		btn.Resize(fyne.NewSize(80, 35))
	}

	// Make Backspace wider
	if label == "⌫" {
		btn.Resize(fyne.NewSize(80, 35))
	}

	// Make function keys smaller
	if label == "F1" || label == "F2" || label == "F3" || label == "F4" ||
		label == "F5" || label == "F6" || label == "F7" || label == "F8" ||
		label == "F9" || label == "F10" || label == "F11" || label == "F12" {
		btn.Resize(fyne.NewSize(35, 30))
	}

	// Make Escape smaller
	if label == "Esc" {
		btn.Resize(fyne.NewSize(35, 30))
	}

	// Make Windows and Menu keys medium size
	if label == "Win" || label == "Menu" || label == "⊞" || label == "☰" {
		btn.Resize(fyne.NewSize(50, 35))
	}

	return btn
}

// createModifierKey creates a modifier button with toggle
func (vk *VirtualKeyboard) createModifierKey(label string, keyCode int) *widget.Button {
	btn := widget.NewButton(label, func() {
		vk.toggleModifier(keyCode)
	})

	btn.Resize(fyne.NewSize(60, 35))
	return btn
}

// toggleModifier toggles modifier state
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

// updateModifierButton updates the modifier button appearance
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

// handleKeyPress handles key press
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

// toggleVisibility toggles keyboard visibility
func (vk *VirtualKeyboard) toggleVisibility() {
	if vk.isVisible {
		vk.Hide()
	} else {
		vk.ShowInSeparateWindow()
	}
}

// Show shows the keyboard
func (vk *VirtualKeyboard) Show() {
	if vk.isVisible {
		return
	}

	vk.isVisible = true
	vk.keyboard.Show()

	// On mobile platforms (Android/iOS), the keyboard is usually embedded in the BorderLayout
	// of the main window via FullscreenUI. Manual positioning here will lead to overlapping with the video.
	// Therefore, we perform manual Move/Resize only if we are NOT on mobile or if the window is separate.
	if vk.parentWindow != nil && fyne.CurrentDevice().IsMobile() == false {
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

// SetOnWindowShown sets a callback invoked after keyboardWindow.Show().
// Platform-specific code (e.g. Windows) can use this to adjust Z-order.
func (vk *VirtualKeyboard) SetOnWindowShown(fn func(fyne.Window)) {
	vk.onWindowShown = fn
}

// ShowInSeparateWindow shows the keyboard in a separate window
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

	if vk.onWindowShown != nil {
		vk.onWindowShown(vk.keyboardWindow)
	}

	logrus.Info("⌨️ Virtual keyboard shown in a separate window")
}

// Hide hides the keyboard
func (vk *VirtualKeyboard) Hide() {
	if !vk.isVisible {
		return
	}

	vk.isVisible = false

	if vk.keyboardWindow != nil {
		vk.keyboardWindow.Close()
		vk.keyboardWindow = nil
	}

	// Reset IME padding (dummy method on desktop, real one on mobile)
	vk.setIMEOffset(0)

	if vk.keyboard != nil {
		vk.keyboard.Hide()
	}
	vk.BlurInput()

	logrus.Info("⌨️ Virtual keyboard hidden")
}

// IsVisible returns visibility state
func (vk *VirtualKeyboard) IsVisible() bool {
	return vk.isVisible
}

// GetContainer returns keyboard container
func (vk *VirtualKeyboard) GetContainer() *fyne.Container {
	return vk.container
}

// GetKeyboardLayout returns only keyboard layout
func (vk *VirtualKeyboard) GetKeyboardLayout() *fyne.Container {
	return vk.keyboard
}

// UpdatePosition updates keyboard elements position
func (vk *VirtualKeyboard) UpdatePosition(windowSize fyne.Size) {
	btnSize := fyne.NewSize(50, 50)
	x := windowSize.Width - btnSize.Width - 10
	y := windowSize.Height - btnSize.Height - 10

	vk.toggleBtn.Move(fyne.NewPos(x, y))
	vk.toggleBtn.Resize(btnSize)
}

// SetVisibleState sets visibility state without showing a separate window
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

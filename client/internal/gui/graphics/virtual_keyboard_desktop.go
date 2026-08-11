//go:build !android && !ios && !(js && wasm)

package graphics

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Keyboard grid constants (like hardware): width/height of one key "unit"
const (
	keyUnitW  = 30
	keyUnitH  = 28
	keyGap    = 2
	keyboardW = 600
	keyboardH = 180
	// Content width of the longest row in units (row 1 and 4: 15)
	keyboardContentUnits = 15
)

// Left margin: center content in the grid so buttons do not press against the left edge
var keyboardLeftMargin = float32((keyboardW - keyboardContentUnits*keyUnitW) / 2)

// centerKeyboardLayout centers the keyboard content when the area size changes
type centerKeyboardLayout struct {
	width  float32
	height float32
}

func (c *centerKeyboardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := (size.Width - c.width) / 2
	if x < 0 {
		x = 0
	}
	y := (size.Height - c.height) / 2
	if y < 0 {
		y = 0
	}
	for _, o := range objects {
		o.Move(fyne.NewPos(x, y))
		o.Resize(fyne.NewSize(c.width, c.height))
	}
}

func (c *centerKeyboardLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(c.width, c.height)
}

// Dummy types for desktop
type backspaceEntry struct {
	widget.Entry
}

type imeSpacerLayout struct {
	height float32
}

// Dummy methods for desktop
func (vk *VirtualKeyboard) RegisterAsIMETarget()                         {}
func (vk *VirtualKeyboard) UnregisterAsIMETarget()                       {}
func (vk *VirtualKeyboard) FocusInput()                                  {}
func (vk *VirtualKeyboard) BlurInput()                                   {}
func (vk *VirtualKeyboard) SetOnIMEChanged(fn func(imeHeightDp float32)) {}
func (vk *VirtualKeyboard) setIMEOffset(imeH float32)                    {}
func (vk *VirtualKeyboard) adjustForIME(open bool)                       {}
func (vk *VirtualKeyboard) ResetIMEState()                               {}

// placeKey places a button in the grid: row/col in key "units", widthUnits is the width in units (1 = normal key).
func (vk *VirtualKeyboard) placeKey(grid *fyne.Container, btn *widget.Button, row int, col float32, widthUnits float32) {
	x := keyboardLeftMargin + col*keyUnitW + keyGap/2
	y := float32(row)*keyUnitH + keyGap/2
	w := widthUnits*keyUnitW - keyGap
	h := keyUnitH - keyGap
	btn.Resize(fyne.NewSize(w, float32(h)))
	btn.Move(fyne.NewPos(x, y))
	grid.Add(btn)
}

// placeInvisiblePlaceholder adds an invisible rectangle (background color) to the grid - for column alignment
func (vk *VirtualKeyboard) placeInvisiblePlaceholder(grid *fyne.Container, row int, col float32) {
	x := keyboardLeftMargin + col*keyUnitW + keyGap/2
	y := float32(row)*keyUnitH + keyGap/2
	w := keyUnitW - keyGap
	h := keyUnitH - keyGap
	rect := canvas.NewRectangle(theme.BackgroundColor())
	rect.Resize(fyne.NewSize(float32(w), float32(h)))
	rect.Move(fyne.NewPos(x, y))
	grid.Add(rect)
}

// createKeyboardLayout creates the desktop keyboard layout
// GetLastIMEH dummy for desktop
func GetLastIMEH() float32 {
	return 0
}

func (vk *VirtualKeyboard) createKeyboardLayout() *fyne.Container {
	grid := container.NewWithoutLayout()
	background := canvas.NewRectangle(theme.BackgroundColor())
	background.FillColor = theme.BackgroundColor()
	grid.Add(background)

	var col float32

	// Row 0: Esc, F1-F12, Del
	col = 0
	vk.placeKey(grid, vk.createKey("Esc", 41, 0), 0, col, 1)
	col++
	fLabels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	fCodes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	for i := 0; i < 12; i++ {
		vk.placeKey(grid, vk.createKey(fLabels[i], fCodes[i], 0), 0, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createKey("Del", 76, 0), 0, col, 1)

	// Row 1: ` 1 2 3 4 5 6 7 8 9 0 - = Backspace(2)
	col = 0
	for _, pair := range []struct {
		l string
		c int
	}{
		{"`", 53}, {"1", 30}, {"2", 31}, {"3", 32}, {"4", 33}, {"5", 34}, {"6", 35}, {"7", 36}, {"8", 37}, {"9", 38}, {"0", 39}, {"-", 45}, {"=", 46},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 1, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createKey("Bksp", 42, 0), 1, col, 2)

	// Row 2: Tab(1.5) Q W E R T Y U I O P [ ] \
	col = 0
	vk.placeKey(grid, vk.createKey("Tab", 43, 0), 2, col, 1.5)
	col += 1.5
	for _, pair := range []struct {
		l string
		c int
	}{
		{"Q", 20}, {"W", 26}, {"E", 8}, {"R", 21}, {"T", 23}, {"Y", 28}, {"U", 24}, {"I", 12}, {"O", 18}, {"P", 19}, {"[", 47}, {"]", 48}, {"\\", 49},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 2, col, 1)
		col++
	}

	// Row 3: Caps(1.75) A S D F G H J K L ; ' Enter(2.25)
	vk.capsLockBtn = vk.createModifierKey("Caps", 57)
	col = 0
	vk.placeKey(grid, vk.capsLockBtn, 3, col, 1.75)
	col += 1.75
	for _, pair := range []struct {
		l string
		c int
	}{
		{"A", 4}, {"S", 22}, {"D", 7}, {"F", 9}, {"G", 10}, {"H", 11}, {"J", 13}, {"K", 14}, {"L", 15}, {";", 51}, {"'", 52},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 3, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createKey("Enter", 40, 0), 3, col, 2.25)

	// Row 4: Shift(1.5) Z X C V B N M , . / Shift(1.5) ↑ [invisible]
	vk.shiftBtn = vk.createModifierKey("Shift", 225)
	col = 0
	vk.placeKey(grid, vk.shiftBtn, 4, col, 1.5)
	col += 1.5
	for _, pair := range []struct {
		l string
		c int
	}{
		{"Z", 29}, {"X", 27}, {"C", 6}, {"V", 25}, {"B", 5}, {"N", 17}, {"M", 16}, {",", 54}, {".", 55}, {"/", 56},
	} {
		vk.placeKey(grid, vk.createKey(pair.l, pair.c, 0), 4, col, 1)
		col++
	}
	vk.placeKey(grid, vk.createModifierKey("Shift", 229), 4, col, 1.5)
	col += 1.5
	vk.placeKey(grid, vk.createKey("↑", 82, 0), 4, col, 1)
	col++
	vk.placeInvisiblePlaceholder(grid, 4, col)

	// Row 5: Ctrl(1.25) Win(1.25) Alt(1.25) Space(3.5) Alt Win Menu Ctrl ← ↓ →
	vk.ctrlBtn = vk.createModifierKey("Ctrl", 224)
	vk.winBtn = vk.createModifierKey("⊞", 227)
	vk.altBtn = vk.createModifierKey("Alt", 226)
	col = 0
	vk.placeKey(grid, vk.ctrlBtn, 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.winBtn, 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.altBtn, 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createKey("Space", 44, 0), 5, col, 3.5)
	col += 3.5
	vk.placeKey(grid, vk.createKey("Alt", 230, 0), 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createModifierKey("⊞", 231), 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createKey("☰", 232, 0), 5, col, 1)
	col++
	vk.placeKey(grid, vk.createModifierKey("Ctrl", 228), 5, col, 1.25)
	col += 1.25
	vk.placeKey(grid, vk.createKey("←", 80, 0), 5, col, 1)
	col++
	vk.placeKey(grid, vk.createKey("↓", 81, 0), 5, col, 1)
	col++
	vk.placeKey(grid, vk.createKey("→", 79, 0), 5, col, 1)

	background.Move(fyne.NewPos(0, 0))
	background.Resize(fyne.NewSize(keyboardW, keyboardH))

	layout := &centerKeyboardLayout{width: keyboardW, height: keyboardH}
	return container.New(layout, grid)
}

//go:build !android && !ios && !(js && wasm)

package graphics

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"
)

const (
	desktopKBUnitsW = float32(15)
	desktopKBRows   = 6
)

type desktopKeySlot struct {
	obj fyne.CanvasObject
	row int
	col float32
	w   float32
}

// scaleKeyboardLayout sizes every key from the window: 15 units wide × 6 rows.
type scaleKeyboardLayout struct {
	slots []desktopKeySlot
	vk    *VirtualKeyboard
}

func (l *scaleKeyboardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	pad := float32(12)
	gap := float32(5)
	innerW := size.Width - pad*2
	innerH := size.Height - pad*2
	if innerW < 80 {
		innerW = 80
	}
	if innerH < 80 {
		innerH = 80
	}
	unitW := innerW / desktopKBUnitsW
	unitH := innerH / float32(desktopKBRows)
	face := unitH * 0.36
	for _, s := range l.slots {
		x := pad + s.col*unitW + gap/2
		y := pad + float32(s.row)*unitH + gap/2
		w := s.w*unitW - gap
		h := unitH - gap
		if w < 8 {
			w = 8
		}
		if h < 8 {
			h = 8
		}
		s.obj.Move(fyne.NewPos(x, y))
		s.obj.Resize(fyne.NewSize(w, h))
		if k, ok := s.obj.(*compactKey); ok {
			k.SetFaceSize(face)
		}
	}
	if l.vk != nil {
		l.vk.rememberKeyboardWindowSize(size)
	}
}

func (l *scaleKeyboardLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(560, 220)
}

type backspaceEntry struct {
	widget.Entry
}

type imeSpacerLayout struct {
	height float32
}

func (vk *VirtualKeyboard) RegisterAsIMETarget()                         {}
func (vk *VirtualKeyboard) UnregisterAsIMETarget()                       {}
func (vk *VirtualKeyboard) FocusInput()                                  {}
func (vk *VirtualKeyboard) BlurInput()                                   {}
func (vk *VirtualKeyboard) SetOnIMEChanged(fn func(imeHeightDp float32)) {}
func (vk *VirtualKeyboard) setIMEOffset(imeH float32)                    {}
func (vk *VirtualKeyboard) adjustForIME(open bool)                       {}
func (vk *VirtualKeyboard) ResetIMEState()                               {}

func GetLastIMEH() float32 { return 0 }

func SetStickySystemIME(_ bool) {}

func SetIMETextHandler(_ func(deleteCount int, text string)) {}

func SetIMEUserDismissedHandler(_ func()) {}

func (vk *VirtualKeyboard) newDesktopKey(label string, keyCode, modifiers int) *compactKey {
	k := newCompactKey(vk, label, compactKeyNormal, 16, func() {
		vk.handleKeyPress(keyCode, modifiers)
	})
	k.radius = design.RadiusMD
	k.minH = 16
	return k
}

func (vk *VirtualKeyboard) newDesktopIconKey(icon fyne.Resource, keyCode, modifiers int) *compactKey {
	k := newCompactIconKey(vk, icon, 16, func() {
		vk.handleKeyPress(keyCode, modifiers)
	})
	k.radius = design.RadiusMD
	k.minH = 16
	return k
}

func (vk *VirtualKeyboard) newDesktopModKey(label string, keyCode int) *compactKey {
	k := newCompactKey(vk, label, compactKeyNormal, 16, func() {
		vk.toggleModifier(keyCode)
	})
	k.radius = design.RadiusMD
	k.minH = 16
	return k
}

func (vk *VirtualKeyboard) placeDesktopKey(lay *scaleKeyboardLayout, obj fyne.CanvasObject, row int, col, widthUnits float32) {
	lay.slots = append(lay.slots, desktopKeySlot{obj: obj, row: row, col: col, w: widthUnits})
}

func (vk *VirtualKeyboard) createKeyboardLayout() *fyne.Container {
	if view.IsMobile() {
		return vk.createCompactSpecialKeysLayout()
	}

	lay := &scaleKeyboardLayout{vk: vk}
	keys := make([]fyne.CanvasObject, 0, 80)

	add := func(obj fyne.CanvasObject, row int, col, w float32) {
		vk.placeDesktopKey(lay, obj, row, col, w)
		keys = append(keys, obj)
	}

	var col float32

	add(vk.newDesktopKey("Esc", 41, 0), 0, 0, 1)
	col = 1
	fLabels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	fCodes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	for i := 0; i < 12; i++ {
		add(vk.newDesktopKey(fLabels[i], fCodes[i], 0), 0, col, 1)
		col++
	}
	add(vk.newDesktopKey("Del", 76, 0), 0, col, 1)

	col = 0
	for _, pair := range []struct {
		l string
		c int
	}{
		{"`", 53}, {"1", 30}, {"2", 31}, {"3", 32}, {"4", 33}, {"5", 34}, {"6", 35}, {"7", 36}, {"8", 37}, {"9", 38}, {"0", 39}, {"-", 45}, {"=", 46},
	} {
		add(vk.newDesktopKey(pair.l, pair.c, 0), 1, col, 1)
		col++
	}
	add(vk.newDesktopKey("Bksp", 42, 0), 1, col, 2)

	col = 0
	add(vk.newDesktopKey("Tab", 43, 0), 2, col, 1.5)
	col += 1.5
	for _, pair := range []struct {
		l string
		c int
	}{
		{"Q", 20}, {"W", 26}, {"E", 8}, {"R", 21}, {"T", 23}, {"Y", 28}, {"U", 24}, {"I", 12}, {"O", 18}, {"P", 19}, {"[", 47}, {"]", 48}, {"\\", 49},
	} {
		add(vk.newDesktopKey(pair.l, pair.c, 0), 2, col, 1)
		col++
	}

	vk.capsLockKey = vk.newDesktopModKey("Caps", 57)
	col = 0
	add(vk.capsLockKey, 3, col, 1.75)
	col += 1.75
	for _, pair := range []struct {
		l string
		c int
	}{
		{"A", 4}, {"S", 22}, {"D", 7}, {"F", 9}, {"G", 10}, {"H", 11}, {"J", 13}, {"K", 14}, {"L", 15}, {";", 51}, {"'", 52},
	} {
		add(vk.newDesktopKey(pair.l, pair.c, 0), 3, col, 1)
		col++
	}
	add(vk.newDesktopKey("Enter", 40, 0), 3, col, 2.25)

	vk.shiftKey = vk.newDesktopModKey("Shift", 225)
	vk.shiftKeyR = vk.newDesktopModKey("Shift", 229)
	col = 0
	add(vk.shiftKey, 4, col, 1.5)
	col += 1.5
	for _, pair := range []struct {
		l string
		c int
	}{
		{"Z", 29}, {"X", 27}, {"C", 6}, {"V", 25}, {"B", 5}, {"N", 17}, {"M", 16}, {",", 54}, {".", 55}, {"/", 56},
	} {
		add(vk.newDesktopKey(pair.l, pair.c, 0), 4, col, 1)
		col++
	}
	add(vk.shiftKeyR, 4, col, 1.5)
	col += 1.5
	add(vk.newDesktopIconKey(theme.MoveUpIcon(), 82, 0), 4, col, 1)

	vk.ctrlKey = vk.newDesktopModKey("Ctrl", 224)
	vk.ctrlKeyR = vk.newDesktopModKey("Ctrl", 228)
	vk.winKey = vk.newDesktopModKey("Win", 227)
	vk.winKeyR = vk.newDesktopModKey("Win", 231)
	vk.altKey = vk.newDesktopModKey("Alt", 226)
	vk.altKeyR = vk.newDesktopModKey("Alt", 230)
	col = 0
	add(vk.ctrlKey, 5, col, 1.25)
	col += 1.25
	add(vk.winKey, 5, col, 1.25)
	col += 1.25
	add(vk.altKey, 5, col, 1.25)
	col += 1.25
	add(vk.newDesktopKey("Space", 44, 0), 5, col, 3.5)
	col += 3.5
	add(vk.altKeyR, 5, col, 1.25)
	col += 1.25
	add(vk.winKeyR, 5, col, 1.25)
	col += 1.25
	add(vk.newDesktopKey("Menu", 232, 0), 5, col, 1)
	col++
	add(vk.ctrlKeyR, 5, col, 1.25)
	col += 1.25
	add(vk.newDesktopIconKey(theme.NavigateBackIcon(), 80, 0), 5, col, 1)
	col++
	add(vk.newDesktopIconKey(theme.MoveDownIcon(), 81, 0), 5, col, 1)
	col++
	add(vk.newDesktopIconKey(theme.NavigateNextIcon(), 79, 0), 5, col, 1)

	bg := canvas.NewRectangle(design.ColorGray950)
	board := container.New(lay, keys...)
	themed := container.NewThemeOverride(board, design.NewBrandTheme())
	return container.NewStack(bg, themed)
}

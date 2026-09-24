package graphics

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const (
	compactKeyHeight   = float32(36)
	compactKeyTextSize = float32(11)
	compactKeyRadius   = float32(6)
	compactKeyStroke   = float32(1)
	compactInputHeight = float32(34)
)

type compactKeyKind int

const compactKeyNormal compactKeyKind = iota

func padCompactKey(key fyne.CanvasObject) fyne.CanvasObject {
	return view.NewInsetExact(key, 1, 1, 1, 0)
}

type compactKey struct {
	widget.BaseWidget
	label   string
	icon    fyne.Resource
	kind    compactKeyKind
	minW    float32
	minH    float32
	radius  float32
	faceSize float32
	active  bool
	hovered bool
	onTap   func()
	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	text    *canvas.Text
	iconImg *canvas.Image
	vk      *VirtualKeyboard
}

func newCompactKey(vk *VirtualKeyboard, label string, kind compactKeyKind, minW float32, onTap func()) *compactKey {
	k := &compactKey{vk: vk, label: label, kind: kind, minW: minW, onTap: onTap}
	k.ExtendBaseWidget(k)
	return k
}

func newCompactIconKey(vk *VirtualKeyboard, icon fyne.Resource, minW float32, onTap func()) *compactKey {
	k := &compactKey{vk: vk, icon: icon, kind: compactKeyNormal, minW: minW, onTap: onTap}
	k.ExtendBaseWidget(k)
	return k
}

func (k *compactKey) SetActive(on bool) {
	k.active = on
	k.syncVisuals()
}

func (k *compactKey) SetFaceSize(sz float32) {
	if sz < 9 {
		sz = 9
	}
	if sz > 22 {
		sz = 22
	}
	k.faceSize = sz
	if k.text != nil {
		k.text.TextSize = sz
		k.text.Refresh()
	}
	if k.iconImg != nil {
		icon := sz + 6
		k.iconImg.SetMinSize(fyne.NewSize(icon, icon))
		k.iconImg.Refresh()
	}
}

func (k *compactKey) keyRadius() float32 {
	if k.radius > 0 {
		return k.radius
	}
	return compactKeyRadius
}

func (k *compactKey) Tapped(*fyne.PointEvent) {
	if k.onTap != nil {
		k.onTap()
	}
}

func (k *compactKey) TappedSecondary(*fyne.PointEvent) {}

func (k *compactKey) MouseIn(*desktop.MouseEvent) {
	k.hovered = true
	k.syncVisuals()
}

func (k *compactKey) MouseMoved(*desktop.MouseEvent) {}

func (k *compactKey) MouseOut() {
	k.hovered = false
	k.syncVisuals()
}

func (k *compactKey) MinSize() fyne.Size {
	w := k.minW
	if w <= 0 {
		w = 40
	}
	h := k.minH
	if h <= 0 {
		h = compactKeyHeight
	}
	return fyne.NewSize(w, h)
}

func (k *compactKey) CreateRenderer() fyne.WidgetRenderer {
	k.bg = canvas.NewRectangle(design.ColorStatusBarFill)
	k.bg.CornerRadius = k.keyRadius()
	k.border = canvas.NewRectangle(color.Transparent)
	k.border.CornerRadius = k.keyRadius()
	k.border.StrokeWidth = compactKeyStroke
	var face fyne.CanvasObject
	textSize := compactKeyTextSize
	if k.faceSize > 0 {
		textSize = k.faceSize
	}
	if k.icon != nil {
		k.iconImg = canvas.NewImageFromResource(k.icon)
		k.iconImg.FillMode = canvas.ImageFillContain
		icon := textSize + 6
		k.iconImg.SetMinSize(fyne.NewSize(icon, icon))
		face = container.NewCenter(k.iconImg)
	} else {
		k.text = canvas.NewText(k.label, design.ColorConnectionsSectionMutedText)
		k.text.TextSize = textSize
		k.text.TextStyle.Bold = true
		k.text.Alignment = fyne.TextAlignCenter
		face = container.NewCenter(k.text)
	}
	k.syncVisuals()
	return widget.NewSimpleRenderer(container.NewStack(k.bg, face, k.border))
}

func (k *compactKey) syncVisuals() {
	if k.bg == nil || k.border == nil {
		return
	}
	fill := color.Color(design.ColorStatusBarFill)
	stroke := color.Color(design.ColorStatusBarBorder)
	fg := color.Color(design.ColorConnectionsSectionMutedText)
	switch {
	case k.active:
		fill = design.ColorConnectionBadgeFill
		stroke = design.ColorConnectionBadgeText
		fg = design.ColorConnectionBadgeText
	case k.hovered:
		fill = design.ColorStatusBarIconChip
		stroke = design.ColorConnectionBadgeBorder
		fg = design.ColorTextLight
	}
	k.bg.FillColor = fill
	k.bg.Refresh()
	k.border.StrokeColor = stroke
	k.border.Refresh()
	if k.text != nil {
		k.text.Text = k.label
		k.text.Color = fg
		k.text.Refresh()
	}
}

type compactKBEntry struct {
	widget.Entry
	border *canvas.Rectangle
}

func newCompactKBEntry() *compactKBEntry {
	e := &compactKBEntry{}
	e.ExtendBaseWidget(e)
	return e
}

func (e *compactKBEntry) FocusGained() {
	e.Entry.FocusGained()
	if e.border != nil {
		e.border.StrokeColor = design.ColorConnectionBadgeText
		e.border.Refresh()
	}
}

func (e *compactKBEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.border != nil {
		e.border.StrokeColor = design.ColorStatusBarBorder
		e.border.Refresh()
	}
}

func (e *compactKBEntry) TappedSecondary(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(e); c != nil {
		c.Focus(e)
	}
	view.ShowEntryContextMenu(&e.Entry, e, e.TypedShortcut)
}

type compactKBEntryTheme struct {
	fyne.Theme
}

func (t *compactKBEntryTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground:
		return design.ColorStatusBarFill
	case theme.ColorNameInputBorder, theme.ColorNameFocus, theme.ColorNameShadow:
		return color.Transparent
	case theme.ColorNamePrimary:
		return design.ColorConnectionBadgeText
	case theme.ColorNameForeground:
		return design.ColorTextLight
	case theme.ColorNamePlaceHolder:
		return design.ColorTextMuted
	case theme.ColorNameSelection:
		return color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0x40}
	}
	return t.Theme.Color(name, variant)
}

func (t *compactKBEntryTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return 11
	case theme.SizeNameInputBorder:
		return 0
	case theme.SizeNameInputRadius:
		return compactKeyRadius
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNamePadding:
		return 2
	}
	return t.Theme.Size(name)
}

type compactKeysTheme struct {
	fyne.Theme
}

func (t *compactKeysTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding, theme.SizeNameInnerPadding:
		return 1
	}
	return t.Theme.Size(name)
}

func wrapCompactKBEntry(entry *compactKBEntry) fyne.CanvasObject {
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = compactKeyRadius
	border.StrokeColor = design.ColorStatusBarBorder
	border.StrokeWidth = 1.2
	entry.border = border
	inner := container.NewThemeOverride(entry, &compactKBEntryTheme{Theme: design.NewBrandTheme()})
	return container.NewStack(border, view.NewInsetExact(inner, 1, 1, 1, 1))
}

// wrapCompactEntryStyles an arbitrary Entry-like widget with the compact input chrome.
func wrapCompactEntry(entry fyne.CanvasObject, setBorder func(*canvas.Rectangle)) fyne.CanvasObject {
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = compactKeyRadius
	border.StrokeColor = design.ColorStatusBarBorder
	border.StrokeWidth = 1.2
	if setBorder != nil {
		setBorder(border)
	}
	inner := container.NewThemeOverride(entry, &compactKBEntryTheme{Theme: design.NewBrandTheme()})
	return container.NewStack(border, view.NewInsetExact(inner, 1, 1, 1, 1))
}

// createCompactSpecialKeysPanel builds Esc/modifiers/Enter/dpad chrome shared by desktop mobile preview and Android/iOS.
// Prefer createCompactKeysChrome — it adds Fn/F-keys and landscape packing.
func (vk *VirtualKeyboard) createCompactSpecialKeysPanel() fyne.CanvasObject {
	return vk.createCompactKeysChrome()
}

// createCompactKeysChrome builds an adaptive special-keys panel: two compact
// rows in portrait and landscape. Arrows are a single ← ↑ ↓ → cluster. Fn
// toggles F1–F12 — in landscape that replaces the modifier rows (still two
// rows total); in portrait F-keys stack above the modifiers.
func (vk *VirtualKeyboard) createCompactKeysChrome() fyne.CanvasObject {
	host := container.NewMax()
	var rebuild func()
	rebuild = func() {
		landscape := view.IsLandscape()
		if landscape {
			if vk.compactFnOn {
				// F1–F12 + dismiss in two squeezed rows — do not stack on
				// top of Esc/modifiers (that made three rows in landscape).
				host.Objects = []fyne.CanvasObject{vk.buildLandscapeCompactFKeys(rebuild)}
			} else {
				host.Objects = []fyne.CanvasObject{vk.buildLandscapeCompactKeys(rebuild)}
			}
		} else {
			body := vk.buildPortraitCompactKeys(rebuild)
			if vk.compactFnOn {
				host.Objects = []fyne.CanvasObject{container.NewVBox(vk.buildCompactFKeysRow(rebuild), body)}
			} else {
				host.Objects = []fyne.CanvasObject{body}
			}
		}
		host.Refresh()
	}
	vk.rebuildCompactKeys = rebuild
	rebuild()
	return container.NewThemeOverride(host, &compactKeysTheme{Theme: design.NewBrandTheme()})
}

// RefreshCompactLayout rebuilds the special-keys chrome after orientation
// changes (portrait ↔ landscape packing / Fn row).
func (vk *VirtualKeyboard) RefreshCompactLayout() {
	if vk == nil || vk.rebuildCompactKeys == nil {
		return
	}
	vk.rebuildCompactKeys()
}

func (vk *VirtualKeyboard) newCompactFnKey(rebuild func()) *compactKey {
	fnKey := newCompactKey(vk, "Fx", compactKeyNormal, 30, nil)
	fnKey.onTap = func() {
		vk.compactFnOn = !vk.compactFnOn
		fnKey.SetActive(vk.compactFnOn)
		if rebuild != nil {
			rebuild()
		}
	}
	fnKey.SetActive(vk.compactFnOn)
	return fnKey
}

func (vk *VirtualKeyboard) buildCompactModifierKeys(rebuild func()) (shiftKey, ctrlKey, winKey, altKey, fnKey *compactKey) {
	shiftKey = newCompactKey(vk, "Shift", compactKeyNormal, 30, func() {
		vk.toggleModifier(225)
		shiftKey.SetActive(vk.shiftPressed)
	})
	ctrlKey = newCompactKey(vk, "Ctrl", compactKeyNormal, 30, func() {
		vk.toggleModifier(224)
		ctrlKey.SetActive(vk.ctrlPressed)
	})
	winKey = newCompactKey(vk, "Win", compactKeyNormal, 30, func() {
		vk.toggleModifier(227)
		winKey.SetActive(vk.winPressed)
	})
	altKey = newCompactKey(vk, "Alt", compactKeyNormal, 30, func() {
		vk.toggleModifier(226)
		altKey.SetActive(vk.altPressed)
	})
	fnKey = vk.newCompactFnKey(rebuild)
	shiftKey.SetActive(vk.shiftPressed)
	ctrlKey.SetActive(vk.ctrlPressed)
	winKey.SetActive(vk.winPressed)
	altKey.SetActive(vk.altPressed)
	return
}

type specialKeysLayout struct {
	gapX float32
	gapY float32
}

func newSpecialKeysLayout() *specialKeysLayout {
	return &specialKeysLayout{
		gapX: 2.5,
		gapY: 2,
	}
}

func (l *specialKeysLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := compactKeyHeight*2 + l.gapY
	w := float32(30)*7 + float32(40) + float32(7)*l.gapX
	return fyne.NewSize(w, h)
}

func (l *specialKeysLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 14 {
		return
	}
	w := size.Width
	h := size.Height
	if w <= 0 || h <= 0 {
		return
	}

	// 2 rows in height
	rowH := (h - l.gapY) / 2
	if rowH < 1 {
		rowH = 1
	}
	row1Y := float32(0)
	row2Y := row1Y + rowH + l.gapY
	row2H := h - row2Y
	if row2H < 1 {
		row2H = rowH
	}

	// 7 gaps across 8 columns
	totalGaps := float32(7) * l.gapX
	availW := w - totalGaps
	if availW < 1 {
		availW = 1
	}

	// Proportions:
	// Left block: 4 columns
	// Enter: 1 column (~1.33x width of a standard key)
	// Right block: 3 columns
	// Total: 7*keyW + enterW = (7 + 1.33)*keyW = 8.33*keyW
	const enterFactor = float32(1.33)
	keyW := availW / (float32(7) + enterFactor)
	enterW := keyW * enterFactor

	x0 := float32(0)
	x1 := x0 + keyW + l.gapX
	x2 := x1 + keyW + l.gapX
	x3 := x2 + keyW + l.gapX

	xEnter := x3 + keyW + l.gapX

	x5 := xEnter + enterW + l.gapX
	x6 := x5 + keyW + l.gapX
	x7 := x6 + keyW + l.gapX
	col7W := w - x7
	if col7W < 1 {
		col7W = keyW
	}

	place := func(obj fyne.CanvasObject, x, y, width, height float32) {
		if obj != nil {
			obj.Move(fyne.NewPos(x, y))
			obj.Resize(fyne.NewSize(width, height))
		}
	}

	// Row 1 left: Esc, Tab, Shift, Fx
	place(objects[0], x0, row1Y, keyW, rowH)
	place(objects[1], x1, row1Y, keyW, rowH)
	place(objects[2], x2, row1Y, keyW, rowH)
	place(objects[3], x3, row1Y, keyW, rowH)

	// Row 2 left: Ctrl, Win, Alt, Del
	place(objects[4], x0, row2Y, keyW, row2H)
	place(objects[5], x1, row2Y, keyW, row2H)
	place(objects[6], x2, row2Y, keyW, row2H)
	place(objects[7], x3, row2Y, keyW, row2H)

	// Middle: Enter (spans 2 rows full height)
	place(objects[8], xEnter, 0, enterW, h)

	// Right block:
	// Row 1: Up, Dismiss (above Left arrow at x5 is empty)
	place(objects[9], x6, row1Y, keyW, rowH)
	place(objects[10], x7, row1Y, col7W, rowH)

	// Row 2: Left, Down, Right
	place(objects[11], x5, row2Y, keyW, row2H)
	place(objects[12], x6, row2Y, keyW, row2H)
	place(objects[13], x7, row2Y, col7W, row2H)
}

func compactKeysSpreadRow(keys ...fyne.CanvasObject) fyne.CanvasObject {
	if len(keys) == 0 {
		return container.NewHBox()
	}
	return container.NewGridWithColumns(len(keys), keys...)
}

func (vk *VirtualKeyboard) buildPortraitCompactKeys(rebuild func()) fyne.CanvasObject {
	shiftKey, ctrlKey, winKey, altKey, fnKey := vk.buildCompactModifierKeys(rebuild)
	escKey := newCompactKey(vk, "Esc", compactKeyNormal, 30, func() { vk.handleKeyPress(41, 0) })
	tabKey := newCompactKey(vk, "Tab", compactKeyNormal, 30, func() { vk.handleKeyPress(43, 0) })
	delKey := newCompactKey(vk, "Del", compactKeyNormal, 30, func() { vk.handleKeyPress(76, 0) })

	enterKey := newCompactKey(vk, "Enter", compactKeyNormal, 40, func() { vk.handleKeyPress(40, 0) })
	enterKey.minH = compactKeyHeight*2 + 2

	upKey := newCompactKey(vk, "↑", compactKeyNormal, 30, func() { vk.handleKeyPress(82, 0) })
	upKey.SetFaceSize(14)
	dismissKey := newCompactIconKey(vk, assets.KeyboardIconDismiss, 30, func() {
		if vk.onDismiss != nil {
			vk.onDismiss()
		}
	})
	leftKey := newCompactKey(vk, "←", compactKeyNormal, 30, func() { vk.handleKeyPress(80, 0) })
	leftKey.SetFaceSize(14)
	downKey := newCompactKey(vk, "↓", compactKeyNormal, 30, func() { vk.handleKeyPress(81, 0) })
	downKey.SetFaceSize(14)
	rightKey := newCompactKey(vk, "→", compactKeyNormal, 30, func() { vk.handleKeyPress(79, 0) })
	rightKey.SetFaceSize(14)

	return container.New(
		newSpecialKeysLayout(),
		escKey, tabKey, shiftKey, fnKey,
		ctrlKey, winKey, altKey, delKey,
		enterKey,
		upKey, dismissKey,
		leftKey, downKey, rightKey,
	)
}

func (vk *VirtualKeyboard) buildLandscapeCompactKeys(rebuild func()) fyne.CanvasObject {
	return vk.buildPortraitCompactKeys(rebuild)
}

func (vk *VirtualKeyboard) compactArrowKeys() []fyne.CanvasObject {
	const aw = compactKeyHeight
	return []fyne.CanvasObject{
		padCompactKey(newCompactKey(vk, "←", compactKeyNormal, aw, func() { vk.handleKeyPress(80, 0) })),
		padCompactKey(newCompactKey(vk, "↑", compactKeyNormal, aw, func() { vk.handleKeyPress(82, 0) })),
		padCompactKey(newCompactKey(vk, "↓", compactKeyNormal, aw, func() { vk.handleKeyPress(81, 0) })),
		padCompactKey(newCompactKey(vk, "→", compactKeyNormal, aw, func() { vk.handleKeyPress(79, 0) })),
		padCompactKey(newCompactIconKey(vk, assets.KeyboardIconDismiss, aw, func() {
			if vk.onDismiss != nil {
				vk.onDismiss()
			}
		})),
	}
}

func (vk *VirtualKeyboard) buildCompactFKeysRow(rebuild func()) fyne.CanvasObject {
	labels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	codes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	kw := float32(32)
	makeKey := func(label string, code int) fyne.CanvasObject {
		return padCompactKey(newCompactKey(vk, label, compactKeyNormal, kw, func() { vk.handleKeyPress(code, 0) }))
	}
	row1 := compactKeysSpreadRow(
		makeKey(labels[0], codes[0]), makeKey(labels[1], codes[1]), makeKey(labels[2], codes[2]),
		makeKey(labels[3], codes[3]), makeKey(labels[4], codes[4]), makeKey(labels[5], codes[5]),
		makeKey(labels[6], codes[6]),
	)
	row2 := compactKeysSpreadRow(
		makeKey(labels[7], codes[7]), makeKey(labels[8], codes[8]), makeKey(labels[9], codes[9]),
		makeKey(labels[10], codes[10]), makeKey(labels[11], codes[11]),
		padCompactKey(newCompactKey(vk, "Back", compactKeyNormal, 48, func() {
			vk.compactFnOn = false
			if rebuild != nil {
				rebuild()
			}
		})),
	)
	return container.NewVBox(row1, row2)
}

// buildLandscapeCompactFKeys packs F1–F12 + dismiss into two squeezed rows
// so landscape special-keys stay at the same height as the modifier layout.
func (vk *VirtualKeyboard) buildLandscapeCompactFKeys(rebuild func()) fyne.CanvasObject {
	labels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	codes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	const kw = float32(26)
	makeKey := func(label string, code int) fyne.CanvasObject {
		k := newCompactKey(vk, label, compactKeyNormal, kw, func() { vk.handleKeyPress(code, 0) })
		k.minW = kw
		return padCompactKey(k)
	}
	row1 := compactKeysSpreadRow(
		makeKey(labels[0], codes[0]), makeKey(labels[1], codes[1]), makeKey(labels[2], codes[2]),
		makeKey(labels[3], codes[3]), makeKey(labels[4], codes[4]), makeKey(labels[5], codes[5]),
		makeKey(labels[6], codes[6]),
	)
	back := newCompactKey(vk, "⌫Fn", compactKeyNormal, 36, func() {
		vk.compactFnOn = false
		if rebuild != nil {
			rebuild()
		}
	})
	back.minW = 36
	row2 := compactKeysSpreadRow(
		makeKey(labels[7], codes[7]), makeKey(labels[8], codes[8]), makeKey(labels[9], codes[9]),
		makeKey(labels[10], codes[10]), makeKey(labels[11], codes[11]),
		padCompactKey(back),
	)
	return container.NewVBox(row1, row2)
}

// createCompactSpecialKeysLayout is the desktop mobile-preview special-keys
// strip (no system IME bridge). Solid header-matching fill; input sits above
// the two key rows when used from createKeyboardLayout.
func (vk *VirtualKeyboard) createCompactSpecialKeysLayout() *fyne.Container {
	keys := view.NewInsetExact(vk.createCompactKeysChrome(), 4, 4, 2, 1)
	background := canvas.NewRectangle(design.ColorGray900)
	return container.NewMax(container.NewThemeOverride(
		container.NewStack(background, keys),
		design.NewBrandTheme(),
	))
}

var (
	_ fyne.Tappable          = (*compactKey)(nil)
	_ desktop.Hoverable      = (*compactKey)(nil)
	_ fyne.Focusable         = (*compactKBEntry)(nil)
	_ fyne.SecondaryTappable = (*compactKBEntry)(nil)
)

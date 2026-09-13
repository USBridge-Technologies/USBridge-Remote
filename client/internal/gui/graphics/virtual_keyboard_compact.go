package graphics

import (
	"image/color"

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
	compactKeyHeight   = float32(30)
	compactKeyTextSize = float32(10)
	compactKeyRadius   = float32(6)
	compactKeyStroke   = float32(1)
)

type compactKeyKind int

const compactKeyNormal compactKeyKind = iota

func padCompactKey(key fyne.CanvasObject) fyne.CanvasObject {
	return view.NewInsetExact(key, 2, 2, 2, 2)
}

type compactKey struct {
	widget.BaseWidget
	label   string
	kind    compactKeyKind
	minW    float32
	active  bool
	hovered bool
	onTap   func()
	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	text    *canvas.Text
}

func newCompactKey(label string, kind compactKeyKind, minW float32, onTap func()) *compactKey {
	k := &compactKey{label: label, kind: kind, minW: minW, onTap: onTap}
	k.ExtendBaseWidget(k)
	return k
}

func (k *compactKey) SetActive(on bool) {
	k.active = on
	k.syncVisuals()
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
	return fyne.NewSize(w, compactKeyHeight)
}

func (k *compactKey) CreateRenderer() fyne.WidgetRenderer {
	k.bg = canvas.NewRectangle(design.ColorStatusBarFill)
	k.bg.CornerRadius = compactKeyRadius
	k.border = canvas.NewRectangle(color.Transparent)
	k.border.CornerRadius = compactKeyRadius
	k.border.StrokeWidth = compactKeyStroke
	k.text = canvas.NewText(k.label, design.ColorConnectionsSectionMutedText)
	k.text.TextSize = compactKeyTextSize
	k.text.TextStyle.Bold = true
	k.text.Alignment = fyne.TextAlignCenter
	k.syncVisuals()
	return widget.NewSimpleRenderer(container.NewStack(k.bg, container.NewCenter(k.text), k.border))
}

func (k *compactKey) syncVisuals() {
	if k.bg == nil || k.border == nil || k.text == nil {
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
	k.text.Text = k.label
	k.text.Color = fg
	k.text.Refresh()
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

// createCompactKeysChrome builds an adaptive special-keys panel: portrait
// keeps the two-row left cluster + Enter + dpad; landscape packs into two
// short rows. Fn toggles an F1–F12 row (third row in landscape, extra row
// above the keys in portrait).
func (vk *VirtualKeyboard) createCompactKeysChrome() fyne.CanvasObject {
	host := container.NewMax()
	var rebuild func()
	rebuild = func() {
		landscape := view.IsLandscape()
		var body fyne.CanvasObject
		if landscape {
			body = vk.buildLandscapeCompactKeys(rebuild)
		} else {
			body = vk.buildPortraitCompactKeys(rebuild)
		}
		if vk.compactFnOn {
			host.Objects = []fyne.CanvasObject{container.NewVBox(vk.buildCompactFKeysRow(rebuild), body)}
		} else {
			host.Objects = []fyne.CanvasObject{body}
		}
		host.Refresh()
	}
	vk.rebuildCompactKeys = rebuild
	rebuild()
	return host
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
	fnKey := newCompactKey("Fn", compactKeyNormal, 36, nil)
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
	shiftKey = newCompactKey("Shift", compactKeyNormal, 48, func() {
		vk.toggleModifier(225)
		shiftKey.SetActive(vk.shiftPressed)
	})
	ctrlKey = newCompactKey("Ctrl", compactKeyNormal, 40, func() {
		vk.toggleModifier(224)
		ctrlKey.SetActive(vk.ctrlPressed)
	})
	winKey = newCompactKey("Win", compactKeyNormal, 40, func() {
		vk.toggleModifier(227)
		winKey.SetActive(vk.winPressed)
	})
	altKey = newCompactKey("Alt", compactKeyNormal, 40, func() {
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

func (vk *VirtualKeyboard) buildPortraitCompactKeys(rebuild func()) fyne.CanvasObject {
	shiftKey, ctrlKey, winKey, altKey, fnKey := vk.buildCompactModifierKeys(rebuild)
	row1 := container.NewHBox(
		padCompactKey(newCompactKey("Esc", compactKeyNormal, 40, func() { vk.handleKeyPress(41, 0) })),
		padCompactKey(newCompactKey("Tab", compactKeyNormal, 40, func() { vk.handleKeyPress(43, 0) })),
		padCompactKey(shiftKey),
		padCompactKey(fnKey),
	)
	row2 := container.NewHBox(
		padCompactKey(ctrlKey),
		padCompactKey(winKey),
		padCompactKey(altKey),
		padCompactKey(newCompactKey("Del", compactKeyNormal, 40, func() { vk.handleKeyPress(76, 0) })),
	)
	leftKeys := container.NewVBox(row1, row2)
	enterBtn := padCompactKey(newCompactKey("Enter", compactKeyNormal, 56, func() { vk.handleKeyPress(40, 0) }))
	return container.NewBorder(nil, nil, leftKeys, vk.buildCompactDPad(), enterBtn)
}

func (vk *VirtualKeyboard) buildLandscapeCompactKeys(rebuild func()) fyne.CanvasObject {
	// Two short rows so the panel stays low; Fn opens F-keys as a third row.
	shiftKey, ctrlKey, winKey, altKey, fnKey := vk.buildCompactModifierKeys(rebuild)
	shiftKey.minW = 42
	ctrlKey.minW = 34
	winKey.minW = 34
	altKey.minW = 34
	fnKey.minW = 34
	const kw = float32(34)
	row1 := container.NewHBox(
		padCompactKey(newCompactKey("Esc", compactKeyNormal, kw, func() { vk.handleKeyPress(41, 0) })),
		padCompactKey(newCompactKey("Tab", compactKeyNormal, kw, func() { vk.handleKeyPress(43, 0) })),
		padCompactKey(shiftKey),
		padCompactKey(ctrlKey),
		padCompactKey(winKey),
		padCompactKey(altKey),
		padCompactKey(newCompactKey("Del", compactKeyNormal, kw, func() { vk.handleKeyPress(76, 0) })),
		padCompactKey(fnKey),
		padCompactKey(newCompactKey("↑", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(82, 0) })),
	)
	row2 := container.NewHBox(
		padCompactKey(newCompactKey("Enter", compactKeyNormal, 72, func() { vk.handleKeyPress(40, 0) })),
		padCompactKey(newCompactKey("←", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(80, 0) })),
		padCompactKey(newCompactKey("↓", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(81, 0) })),
		padCompactKey(newCompactKey("→", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(79, 0) })),
	)
	return container.NewVBox(row1, row2)
}

func (vk *VirtualKeyboard) buildCompactDPad() fyne.CanvasObject {
	ph := func() fyne.CanvasObject {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(compactKeyHeight, compactKeyHeight))
		return r
	}
	return container.NewGridWithColumns(3,
		ph(),
		padCompactKey(newCompactKey("↑", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(82, 0) })),
		ph(),
		padCompactKey(newCompactKey("←", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(80, 0) })),
		padCompactKey(newCompactKey("↓", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(81, 0) })),
		padCompactKey(newCompactKey("→", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(79, 0) })),
	)
}

func (vk *VirtualKeyboard) buildCompactFKeysRow(rebuild func()) fyne.CanvasObject {
	labels := []string{"F1", "F2", "F3", "F4", "F5", "F6", "F7", "F8", "F9", "F10", "F11", "F12"}
	codes := []int{58, 59, 60, 61, 62, 63, 64, 65, 66, 67, 68, 69}
	kw := float32(32)
	if view.IsLandscape() {
		kw = 28
	}
	makeKey := func(label string, code int) fyne.CanvasObject {
		return padCompactKey(newCompactKey(label, compactKeyNormal, kw, func() { vk.handleKeyPress(code, 0) }))
	}
	if view.IsLandscape() {
		objs := make([]fyne.CanvasObject, 0, 13)
		for i := range labels {
			objs = append(objs, makeKey(labels[i], codes[i]))
		}
		objs = append(objs, padCompactKey(newCompactKey("⌫Fn", compactKeyNormal, 40, func() {
			vk.compactFnOn = false
			if rebuild != nil {
				rebuild()
			}
		})))
		return container.NewHBox(objs...)
	}
	// Portrait: two rows so F-keys fit a phone width (same idea as the old Fx panel).
	row1 := container.NewHBox(
		makeKey(labels[0], codes[0]), makeKey(labels[1], codes[1]), makeKey(labels[2], codes[2]),
		makeKey(labels[3], codes[3]), makeKey(labels[4], codes[4]), makeKey(labels[5], codes[5]),
		makeKey(labels[6], codes[6]),
	)
	row2 := container.NewHBox(
		makeKey(labels[7], codes[7]), makeKey(labels[8], codes[8]), makeKey(labels[9], codes[9]),
		makeKey(labels[10], codes[10]), makeKey(labels[11], codes[11]),
		padCompactKey(newCompactKey("Back", compactKeyNormal, 48, func() {
			vk.compactFnOn = false
			if rebuild != nil {
				rebuild()
			}
		})),
	)
	return container.NewVBox(row1, row2)
}

// createCompactSpecialKeysLayout is the desktop mobile-preview special-keys
// strip (no system IME bridge). Transparent so it can float on the video.
func (vk *VirtualKeyboard) createCompactSpecialKeysLayout() *fyne.Container {
	keys := view.NewInsetExact(vk.createCompactKeysChrome(), 6, 6, 4, 6)
	background := canvas.NewRectangle(color.Transparent)
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

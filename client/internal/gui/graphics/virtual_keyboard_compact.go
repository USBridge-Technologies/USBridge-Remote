package graphics

import (
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
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
func (vk *VirtualKeyboard) createCompactSpecialKeysPanel() fyne.CanvasObject {
	var shiftKey, ctrlKey, winKey, altKey *compactKey
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

	row1 := container.NewHBox(
		padCompactKey(newCompactKey("Esc", compactKeyNormal, 40, func() { vk.handleKeyPress(41, 0) })),
		padCompactKey(newCompactKey("Tab", compactKeyNormal, 40, func() { vk.handleKeyPress(43, 0) })),
		padCompactKey(shiftKey),
	)
	row2 := container.NewHBox(
		padCompactKey(ctrlKey),
		padCompactKey(winKey),
		padCompactKey(altKey),
		padCompactKey(newCompactKey("Del", compactKeyNormal, 40, func() { vk.handleKeyPress(76, 0) })),
	)
	leftKeys := container.NewVBox(row1, row2)
	enterBtn := padCompactKey(newCompactKey("Enter", compactKeyNormal, 56, func() { vk.handleKeyPress(40, 0) }))

	ph := func() fyne.CanvasObject {
		r := canvas.NewRectangle(color.Transparent)
		r.SetMinSize(fyne.NewSize(compactKeyHeight, compactKeyHeight))
		return r
	}
	dpad := container.NewGridWithColumns(3,
		ph(),
		padCompactKey(newCompactKey("↑", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(82, 0) })),
		ph(),
		padCompactKey(newCompactKey("←", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(80, 0) })),
		padCompactKey(newCompactKey("↓", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(81, 0) })),
		padCompactKey(newCompactKey("→", compactKeyNormal, compactKeyHeight, func() { vk.handleKeyPress(79, 0) })),
	)
	return container.NewBorder(nil, nil, leftKeys, dpad, enterBtn)
}

// createCompactSpecialKeysLayout is the desktop mobile-preview keyboard (no system IME bridge).
func (vk *VirtualKeyboard) createCompactSpecialKeysLayout() *fyne.Container {
	entry := newCompactKBEntry()
	entry.SetPlaceHolder(i18n.Current.VirtualKeyboardClickToType)
	var prev string
	entry.OnChanged = func(s string) {
		if vk.onRuneTyped == nil {
			prev = s
			return
		}
		old := []rune(prev)
		next := []rune(s)
		if len(next) > len(old) {
			for _, r := range next[len(old):] {
				vk.onRuneTyped(r)
			}
		} else if len(next) < len(old) && vk.onKeyPress != nil {
			for i := 0; i < len(old)-len(next); i++ {
				vk.onKeyPress(42, 0)
			}
		}
		prev = s
	}

	clearBtn := padCompactKey(newCompactKey("×", compactKeyNormal, compactKeyHeight, func() {
		entry.SetText("")
		prev = ""
	}))
	inputRow := container.NewBorder(nil, nil, nil, clearBtn, wrapCompactKBEntry(entry))
	line := canvas.NewRectangle(design.ColorHeaderAccentLine)
	line.SetMinSize(fyne.NewSize(1, 0.5))
	main := view.NewInsetExact(container.NewVBox(vk.createCompactSpecialKeysPanel(), inputRow), 6, 6, 6, 6)
	bg := canvas.NewRectangle(design.ColorGray950)
	return container.NewMax(container.NewThemeOverride(container.NewStack(bg, view.NewTopLine(main, line)), design.NewBrandTheme()))
}

var (
	_ fyne.Tappable          = (*compactKey)(nil)
	_ desktop.Hoverable      = (*compactKey)(nil)
	_ fyne.Focusable         = (*compactKBEntry)(nil)
	_ fyne.SecondaryTappable = (*compactKBEntry)(nil)
)

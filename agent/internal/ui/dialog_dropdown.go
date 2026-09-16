package ui

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const (
	dialogDropdownHeight     float32 = 26
	dialogDropdownTextSize   float32 = 10
	dialogDropdownItemHeight float32 = 24
)

// dialogDropdown is the client's UltraCompact HeaderDropdown (AUTO/TS/LAN
// on a connection card): teal 10px chip, olive chevron, Gray950 menu.
type dialogDropdown struct {
	widget.BaseWidget

	Options    []string
	Selected   string
	OnSelected func(string)

	hovered bool
	opened  bool

	popup  *dialogMenuOverlay
	bg     *canvas.Rectangle
	border *canvas.Rectangle
	label  *canvas.Text
	icon   *canvas.Image
}

func newDialogDropdown(options []string, selected string, onSelected func(string)) *dialogDropdown {
	d := &dialogDropdown{
		Options:    append([]string(nil), options...),
		Selected:   selected,
		OnSelected: onSelected,
	}
	d.ExtendBaseWidget(d)
	return d
}

func (d *dialogDropdown) CreateRenderer() fyne.WidgetRenderer {
	d.bg = canvas.NewRectangle(design.ColorGray900)
	d.bg.CornerRadius = 6
	d.border = canvas.NewRectangle(color.Transparent)
	d.border.CornerRadius = 6
	d.border.StrokeColor = design.ColorTailscaleChipBorder
	d.border.StrokeWidth = 1
	d.label = canvas.NewText(d.Selected, design.ColorTeal)
	d.label.TextSize = dialogDropdownTextSize
	d.label.TextStyle.Monospace = true
	d.icon = canvas.NewImageFromResource(dialogDropdownArrow(false))
	d.icon.FillMode = canvas.ImageFillContain
	d.icon.SetMinSize(fyne.NewSize(16, 16))
	content := container.NewWithoutLayout(d.bg, d.border, d.label, d.icon)
	r := &dialogDropdownRenderer{dropdown: d, objects: []fyne.CanvasObject{content}}
	r.Refresh()
	return r
}

func (d *dialogDropdown) MinSize() fyne.Size {
	label := canvas.NewText(d.Selected, design.ColorTeal)
	label.TextSize = dialogDropdownTextSize
	label.TextStyle.Monospace = true
	w := label.MinSize().Width + 32
	if w < 64 {
		w = 64
	}
	return fyne.NewSize(w, dialogDropdownHeight)
}

func (d *dialogDropdown) Tapped(*fyne.PointEvent) {
	if d.popup != nil && d.popup.Visible() {
		d.closePopup()
		return
	}
	d.openPopup()
}

func (d *dialogDropdown) TappedSecondary(*fyne.PointEvent) {}

func (d *dialogDropdown) MouseIn(*desktop.MouseEvent) {
	d.hovered = true
	d.refreshVisuals()
}

func (d *dialogDropdown) MouseMoved(*desktop.MouseEvent) {}

func (d *dialogDropdown) MouseOut() {
	d.hovered = false
	d.refreshVisuals()
}

func (d *dialogDropdown) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (d *dialogDropdown) SetSelected(value string) {
	d.Selected = value
	d.Refresh()
}

func (d *dialogDropdown) openPopup() {
	rows := make([]fyne.CanvasObject, 0, len(d.Options))
	for _, option := range d.Options {
		value := option
		rows = append(rows, newDialogDropdownItem(value, value == d.Selected, func() {
			d.SetSelected(value)
			d.closePopup()
			if d.OnSelected != nil {
				d.OnSelected(value)
			}
		}))
	}

	menuBG := canvas.NewRectangle(design.ColorGray950)
	menuBG.CornerRadius = 6
	menuBorder := canvas.NewRectangle(color.Transparent)
	menuBorder.CornerRadius = 6
	menuBorder.StrokeColor = design.ColorTailscaleChipBorder
	menuBorder.StrokeWidth = 1
	menuList := container.New(&tightVBoxLayout{gap: 2}, rows...)
	menu := container.NewStack(menuBG, newExactInset(menuList, 4, 4, 4, 4), menuBorder)

	drv := fyne.CurrentApp().Driver()
	c := drv.CanvasForObject(d)
	if c == nil {
		return
	}

	menuWidth := menu.MinSize().Width
	if d.Size().Width > menuWidth {
		menuWidth = d.Size().Width
	}
	for _, option := range d.Options {
		label := canvas.NewText(option, design.ColorTeal)
		label.TextSize = dialogDropdownTextSize
		label.TextStyle.Monospace = true
		if w := label.MinSize().Width + 16; w > menuWidth {
			menuWidth = w
		}
	}

	pos := drv.AbsolutePositionForObject(d)
	canvasSize := c.Size()
	menuHeight := menu.MinSize().Height
	gap := float32(6)
	popupY := pos.Y + d.Size().Height + gap
	if popupY+menuHeight > canvasSize.Height-4 && pos.Y-gap-menuHeight > 4 {
		popupY = pos.Y - menuHeight - gap
	}
	popupX := pos.X
	if popupX+menuWidth > canvasSize.Width-4 {
		popupX = canvasSize.Width - menuWidth - 4
	}
	if popupX < 4 {
		popupX = 4
	}

	d.popup = newDialogMenuOverlay(menu, c, fyne.NewSize(menuWidth, menuHeight), d.popupDismissed)
	d.popup.ShowAt(fyne.NewPos(popupX, popupY))
	d.opened = true
	d.refreshVisuals()
}

func (d *dialogDropdown) closePopup() {
	if d.popup != nil {
		d.popup.onDismiss = nil
		d.popup.Hide()
		d.popup = nil
	}
	d.opened = false
	d.hovered = false
	d.refreshVisuals()
}

func (d *dialogDropdown) popupDismissed() {
	d.popup = nil
	d.opened = false
	d.hovered = false
	d.refreshVisuals()
}

func (d *dialogDropdown) refreshVisuals() {
	if d.bg == nil || d.border == nil || d.label == nil || d.icon == nil {
		return
	}
	fill := design.ColorGray900
	border := design.ColorTailscaleChipBorder
	if d.opened || d.hovered {
		fill = design.ColorGray900
		border = design.ColorTeal
	}
	d.bg.FillColor = fill
	d.border.StrokeColor = border
	d.label.Text = d.Selected
	d.label.Color = design.ColorTeal
	d.icon.Resource = dialogDropdownArrow(d.opened)
	d.bg.Refresh()
	d.border.Refresh()
	d.label.Refresh()
	d.icon.Refresh()
}

type dialogDropdownRenderer struct {
	dropdown *dialogDropdown
	objects  []fyne.CanvasObject
}

func (r *dialogDropdownRenderer) Layout(size fyne.Size) {
	d := r.dropdown
	d.bg.Resize(size)
	d.border.Resize(size)
	labelMin := d.label.MinSize()
	labelW := size.Width - 32
	if labelW < 24 {
		labelW = 24
	}
	d.label.Move(fyne.NewPos(8, (size.Height-labelMin.Height)/2))
	d.label.Resize(fyne.NewSize(labelW, labelMin.Height))
	iconSize := fyne.NewSize(16, 16)
	d.icon.Resize(iconSize)
	d.icon.Move(fyne.NewPos(size.Width-20, (size.Height-iconSize.Height)/2))
}

func (r *dialogDropdownRenderer) MinSize() fyne.Size { return r.dropdown.MinSize() }

func (r *dialogDropdownRenderer) Refresh() {
	r.dropdown.refreshVisuals()
	r.Layout(r.dropdown.Size())
	canvas.Refresh(r.dropdown)
}

func (r *dialogDropdownRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *dialogDropdownRenderer) Destroy()                     {}

type dialogDropdownItem struct {
	widget.BaseWidget
	text     string
	selected bool
	hovered  bool
	onTap    func()
	bg       *canvas.Rectangle
	label    *canvas.Text
}

func newDialogDropdownItem(text string, selected bool, onTap func()) *dialogDropdownItem {
	i := &dialogDropdownItem{text: text, selected: selected, onTap: onTap}
	i.ExtendBaseWidget(i)
	return i
}

func (i *dialogDropdownItem) CreateRenderer() fyne.WidgetRenderer {
	i.bg = canvas.NewRectangle(color.Transparent)
	i.bg.CornerRadius = 4
	i.label = canvas.NewText(i.text, design.ColorTeal)
	i.label.TextSize = dialogDropdownTextSize
	i.label.TextStyle.Monospace = true
	r := &dialogDropdownItemRenderer{item: i, objects: []fyne.CanvasObject{container.NewWithoutLayout(i.bg, i.label)}}
	r.Refresh()
	return r
}

func (i *dialogDropdownItem) MinSize() fyne.Size {
	label := canvas.NewText(i.text, design.ColorTeal)
	label.TextSize = dialogDropdownTextSize
	label.TextStyle.Monospace = true
	return fyne.NewSize(label.MinSize().Width+16, dialogDropdownItemHeight)
}

func (i *dialogDropdownItem) Tapped(*fyne.PointEvent) {
	if i.onTap != nil {
		i.onTap()
	}
}

func (i *dialogDropdownItem) TappedSecondary(*fyne.PointEvent) {}

func (i *dialogDropdownItem) MouseIn(*desktop.MouseEvent) {
	i.hovered = true
	i.Refresh()
}

func (i *dialogDropdownItem) MouseMoved(*desktop.MouseEvent) {}

func (i *dialogDropdownItem) MouseOut() {
	i.hovered = false
	i.Refresh()
}

func (i *dialogDropdownItem) Cursor() desktop.Cursor { return desktop.PointerCursor }

type dialogDropdownItemRenderer struct {
	item    *dialogDropdownItem
	objects []fyne.CanvasObject
}

func (r *dialogDropdownItemRenderer) Layout(size fyne.Size) {
	r.item.bg.Resize(size)
	min := r.item.label.MinSize()
	r.item.label.Move(fyne.NewPos(8, (size.Height-min.Height)/2))
	r.item.label.Resize(min)
}

func (r *dialogDropdownItemRenderer) MinSize() fyne.Size { return r.item.MinSize() }

func (r *dialogDropdownItemRenderer) Refresh() {
	fill := color.Color(color.Transparent)
	if r.item.selected {
		fill = design.ColorGray900
	}
	if r.item.hovered {
		fill = design.ColorSurfaceLight
	}
	r.item.bg.FillColor = fill
	r.item.bg.Refresh()
	r.item.label.Refresh()
	r.Layout(r.item.Size())
	canvas.Refresh(r.item)
}

func (r *dialogDropdownItemRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *dialogDropdownItemRenderer) Destroy()                     {}

type dialogMenuOverlay struct {
	widget.BaseWidget
	content   fyne.CanvasObject
	c         fyne.Canvas
	pos       fyne.Position
	size      fyne.Size
	shown     bool
	onDismiss func()
}

func newDialogMenuOverlay(content fyne.CanvasObject, c fyne.Canvas, size fyne.Size, onDismiss func()) *dialogMenuOverlay {
	p := &dialogMenuOverlay{content: content, c: c, size: size, onDismiss: onDismiss}
	p.ExtendBaseWidget(p)
	return p
}

func (p *dialogMenuOverlay) CreateRenderer() fyne.WidgetRenderer {
	return &dialogMenuOverlayRenderer{popup: p, objects: []fyne.CanvasObject{p.content}}
}

func (p *dialogMenuOverlay) MinSize() fyne.Size { return p.size }

func (p *dialogMenuOverlay) ShowAt(pos fyne.Position) {
	p.pos = pos
	if !p.shown && p.c != nil {
		p.c.Overlays().Add(p)
		p.shown = true
	}
	p.BaseWidget.Resize(p.c.Size())
	p.Show()
	p.Refresh()
}

func (p *dialogMenuOverlay) Hide() {
	if p.shown && p.c != nil {
		p.c.Overlays().Remove(p)
		p.shown = false
	}
	p.BaseWidget.Hide()
	if p.onDismiss != nil {
		p.onDismiss()
	}
}

func (p *dialogMenuOverlay) Tapped(ev *fyne.PointEvent) {
	if ev != nil &&
		ev.Position.X >= p.pos.X && ev.Position.Y >= p.pos.Y &&
		ev.Position.X <= p.pos.X+p.size.Width && ev.Position.Y <= p.pos.Y+p.size.Height {
		return
	}
	p.Hide()
}

func (p *dialogMenuOverlay) TappedSecondary(ev *fyne.PointEvent) { p.Tapped(ev) }

type dialogMenuOverlayRenderer struct {
	popup   *dialogMenuOverlay
	objects []fyne.CanvasObject
}

func (r *dialogMenuOverlayRenderer) Layout(fyne.Size) {
	r.popup.content.Move(r.popup.pos)
	r.popup.content.Resize(r.popup.size)
}

func (r *dialogMenuOverlayRenderer) MinSize() fyne.Size { return r.popup.size }

func (r *dialogMenuOverlayRenderer) Refresh() {
	if r.popup.c != nil && r.popup.c.Size() != r.popup.Size() {
		r.popup.BaseWidget.Resize(r.popup.c.Size())
	}
	r.Layout(r.popup.Size())
	r.popup.content.Refresh()
	canvas.Refresh(r.popup)
}

func (r *dialogMenuOverlayRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *dialogMenuOverlayRenderer) Destroy()                     {}

func dialogDropdownArrow(up bool) fyne.Resource {
	c := design.ColorMutedOlive
	r, g, b, _ := c.RGBA()
	hex := fmt.Sprintf("#%02x%02x%02x", r>>8, g>>8, b>>8)
	path := "M7 10l5 5 5-5z"
	name := "dialog_arrow_down.svg"
	if up {
		path = "M7 14l5-5 5 5z"
		name = "dialog_arrow_up.svg"
	}
	svg := fmt.Sprintf(`<svg viewBox="0 0 24 24" fill="%s"><path d="%s"/></svg>`, hex, path)
	return fyne.NewStaticResource(name, []byte(svg))
}

var (
	_ fyne.Tappable     = (*dialogDropdown)(nil)
	_ desktop.Hoverable = (*dialogDropdown)(nil)
	_ fyne.Tappable     = (*dialogDropdownItem)(nil)
	_ desktop.Hoverable = (*dialogDropdownItem)(nil)
	_ fyne.Tappable     = (*dialogMenuOverlay)(nil)
)

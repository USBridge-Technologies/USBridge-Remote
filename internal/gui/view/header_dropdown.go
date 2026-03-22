package view

import (
	"image/color"
	"time"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type HeaderDropdown struct {
	widget.BaseWidget

	Options    []string
	Selected   string
	OnSelected func(string)

	disabled bool
	hovered  bool
	opened   bool
	hidden   bool

	popup *dropdownPopup

	bg          *canvas.Rectangle
	border      *canvas.Rectangle
	label       *canvas.Text
	icon        *canvas.Image
	iconOpening bool
}

func NewHeaderDropdown(options []string, selected string, onSelected func(string)) *HeaderDropdown {
	d := &HeaderDropdown{
		Options:    append([]string(nil), options...),
		Selected:   selected,
		OnSelected: onSelected,
	}
	d.ExtendBaseWidget(d)
	return d
}

func (d *HeaderDropdown) CreateRenderer() fyne.WidgetRenderer {
	d.bg = canvas.NewRectangle(design.ColorSurface)
	d.bg.CornerRadius = design.RadiusMD

	d.border = canvas.NewRectangle(color.Transparent)
	d.border.CornerRadius = design.RadiusMD
	d.border.StrokeColor = design.ColorBorder
	d.border.StrokeWidth = 1

	d.label = canvas.NewText(d.Selected, design.ColorTextLight)
	d.label.TextSize = 14

	d.icon = canvas.NewImageFromResource(theme.Icon(theme.IconNameArrowDropDown))
	d.icon.FillMode = canvas.ImageFillContain
	d.icon.SetMinSize(fyne.NewSize(16, 16))

	content := container.NewWithoutLayout(d.bg, d.border, d.label, d.icon)
	r := &headerDropdownRenderer{
		dropdown: d,
		objects:  []fyne.CanvasObject{content},
	}
	r.Refresh()
	return r
}

func (d *HeaderDropdown) MinSize() fyne.Size {
	label := canvas.NewText(d.Selected, design.ColorTextLight)
	label.TextSize = 14
	width := label.MinSize().Width + 52
	if width < 88 {
		width = 88
	}
	if width > 132 {
		width = 132
	}
	return fyne.NewSize(width, 40)
}

func (d *HeaderDropdown) Tapped(*fyne.PointEvent) {
	if d.disabled {
		return
	}
	if d.popup != nil && d.popup.Visible() {
		d.closePopup()
		return
	}
	d.openPopup()
}

func (d *HeaderDropdown) TappedSecondary(*fyne.PointEvent) {}

func (d *HeaderDropdown) MouseIn(*desktop.MouseEvent) {
	d.hovered = true
	d.refreshVisuals()
}

func (d *HeaderDropdown) MouseMoved(*desktop.MouseEvent) {}

func (d *HeaderDropdown) MouseOut() {
	d.hovered = false
	d.refreshVisuals()
}

func (d *HeaderDropdown) SetOptions(options []string) {
	d.Options = append([]string(nil), options...)
	d.Refresh()
}

func (d *HeaderDropdown) SetSelected(value string) {
	d.Selected = value
	d.Refresh()
}

func (d *HeaderDropdown) SetDisabled(disabled bool) {
	d.disabled = disabled
	if disabled {
		d.closePopup()
	}
	d.Refresh()
}

func (d *HeaderDropdown) Disabled() bool {
	return d.disabled
}

func (d *HeaderDropdown) Hide() {
	d.hidden = true
	d.closePopup()
	d.BaseWidget.Hide()
}

func (d *HeaderDropdown) Show() {
	d.hidden = false
	d.BaseWidget.Show()
}

func (d *HeaderDropdown) openPopup() {
	if d.hidden {
		return
	}

	rows := make([]fyne.CanvasObject, 0, len(d.Options))
	for _, option := range d.Options {
		value := option
		rows = append(rows, newDropdownItem(value, value == d.Selected, func() {
			d.SetSelected(value)
			d.closePopup()
			if d.OnSelected != nil {
				d.OnSelected(value)
			}
		}))
	}

	menuBG := canvas.NewRectangle(design.ColorGray950)
	menuBG.CornerRadius = design.RadiusMD

	menuBorder := canvas.NewRectangle(color.Transparent)
	menuBorder.CornerRadius = design.RadiusMD
	menuBorder.StrokeColor = design.ColorBorder
	menuBorder.StrokeWidth = 1

	menu := container.NewStack(
		menuBG,
		NewInset(container.NewVBox(rows...), 6, 6, 6, 6),
		menuBorder,
	)

	canvasForObj := fyne.CurrentApp().Driver().CanvasForObject(d)
	if canvasForObj == nil {
		return
	}

	menuWidth := menu.MinSize().Width
	for _, option := range d.Options {
		label := canvas.NewText(option, design.ColorTextLight)
		label.TextSize = 14
		optionWidth := label.MinSize().Width + 40
		if optionWidth > menuWidth {
			menuWidth = optionWidth
		}
	}
	if d.Size().Width > menuWidth {
		menuWidth = d.Size().Width
	}

	d.popup = newDropdownPopup(
		menu,
		canvasForObj,
		fyne.NewSize(menuWidth, menu.MinSize().Height),
		d.popupDismissed,
	)

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(d)
	d.popup.ShowAtPosition(pos.Add(fyne.NewPos(0, d.Size().Height+6)))
	d.opened = true
	d.animateArrow(theme.Icon(theme.IconNameArrowDropDown), theme.Icon(theme.IconNameArrowDropUp))
	d.Refresh()
}

func (d *HeaderDropdown) closePopup() {
	if d.popup != nil {
		d.popup.onDismiss = nil
		d.popup.Hide()
		d.popup = nil
	}
	d.opened = false
	d.hovered = false
	d.animateArrow(theme.Icon(theme.IconNameArrowDropUp), theme.Icon(theme.IconNameArrowDropDown))
	d.Refresh()
}

func (d *HeaderDropdown) popupDismissed() {
	d.popup = nil
	d.opened = false
	d.hovered = false
	d.animateArrow(theme.Icon(theme.IconNameArrowDropUp), theme.Icon(theme.IconNameArrowDropDown))
	d.Refresh()
}

func (d *HeaderDropdown) refreshVisuals() {
	if d.bg == nil || d.border == nil || d.label == nil || d.icon == nil {
		return
	}

	fill := design.ColorGray900
	textColor := design.ColorTextLight
	iconResource := theme.Icon(theme.IconNameArrowDropDown)
	if d.opened {
		iconResource = theme.Icon(theme.IconNameArrowDropUp)
	}

	switch {
	case d.disabled:
		fill = design.ColorSurface
		textColor = design.ColorBorder
		iconResource = theme.NewDisabledResource(iconResource)
	case d.opened:
		fill = design.ColorSurfaceLight
	}

	d.bg.FillColor = fill
	d.label.Text = d.Selected
	d.label.Color = textColor
	d.icon.Resource = iconResource
	d.bg.Refresh()
	d.border.Refresh()
	d.label.Refresh()
	d.icon.Refresh()
}

func (d *HeaderDropdown) animateArrow(from fyne.Resource, to fyne.Resource) {
	if d.icon == nil || d.iconOpening {
		return
	}
	app := fyne.CurrentApp()
	if app == nil || !app.Settings().ShowAnimations() {
		d.icon.Resource = to
		d.icon.Translucency = 0
		d.icon.Refresh()
		return
	}

	d.iconOpening = true
	swapped := false
	anim := fyne.NewAnimation(120*time.Millisecond, func(done float32) {
		switch {
		case done < 0.5:
			d.icon.Translucency = float64(done * 2)
		default:
			if !swapped {
				d.icon.Resource = to
				swapped = true
			}
			d.icon.Translucency = float64((1 - done) * 2)
		}

		if done >= 1 {
			d.icon.Resource = to
			d.icon.Translucency = 0
			d.iconOpening = false
		}
		d.icon.Refresh()
	})
	anim.Curve = fyne.AnimationEaseInOut
	d.icon.Resource = from
	d.icon.Translucency = 0
	d.icon.Refresh()
	anim.Start()
}

type dropdownItem struct {
	widget.BaseWidget

	text     string
	selected bool
	hovered  bool
	onTap    func()

	bg    *canvas.Rectangle
	label *canvas.Text
}

func newDropdownItem(text string, selected bool, onTap func()) *dropdownItem {
	i := &dropdownItem{text: text, selected: selected, onTap: onTap}
	i.ExtendBaseWidget(i)
	return i
}

func (i *dropdownItem) CreateRenderer() fyne.WidgetRenderer {
	i.bg = canvas.NewRectangle(color.Transparent)
	i.bg.CornerRadius = design.RadiusMD
	i.label = canvas.NewText(i.text, design.ColorTextLight)
	i.label.TextSize = 14
	r := &dropdownItemRenderer{
		item:    i,
		objects: []fyne.CanvasObject{container.NewWithoutLayout(i.bg, i.label)},
	}
	r.Refresh()
	return r
}

func (i *dropdownItem) MinSize() fyne.Size {
	return fyne.NewSize(120, 40)
}

func (i *dropdownItem) Tapped(*fyne.PointEvent) {
	if i.onTap != nil {
		i.onTap()
	}
}

func (i *dropdownItem) TappedSecondary(*fyne.PointEvent) {}

func (i *dropdownItem) MouseIn(*desktop.MouseEvent) {
	i.hovered = true
	i.Refresh()
}

func (i *dropdownItem) MouseMoved(*desktop.MouseEvent) {}

func (i *dropdownItem) MouseOut() {
	i.hovered = false
	i.Refresh()
}

func (i *dropdownItem) refreshVisuals() {
	if i.bg == nil || i.label == nil {
		return
	}

	var fill color.Color = color.Transparent
	if i.selected {
		fill = design.ColorGray900
	}
	if i.hovered {
		fill = design.ColorSurfaceLight
	}

	i.bg.FillColor = fill
	i.bg.Refresh()
	i.label.Refresh()
}

type headerDropdownRenderer struct {
	dropdown *HeaderDropdown
	objects  []fyne.CanvasObject
}

func (r *headerDropdownRenderer) Layout(size fyne.Size) {
	d := r.dropdown
	d.bg.Resize(size)
	d.border.Resize(size)

	labelMin := d.label.MinSize()
	labelWidth := size.Width - 56
	if labelWidth < 24 {
		labelWidth = 24
	}
	d.label.Move(fyne.NewPos(16, (size.Height-labelMin.Height)/2))
	d.label.Resize(fyne.NewSize(labelWidth, labelMin.Height))

	iconSize := fyne.NewSize(16, 16)
	d.icon.Resize(iconSize)
	d.icon.Move(fyne.NewPos(size.Width-28, (size.Height-iconSize.Height)/2))
}

func (r *headerDropdownRenderer) MinSize() fyne.Size {
	return r.dropdown.MinSize()
}

func (r *headerDropdownRenderer) Refresh() {
	r.dropdown.refreshVisuals()
	r.Layout(r.dropdown.Size())
	canvas.Refresh(r.dropdown)
}

func (r *headerDropdownRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *headerDropdownRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *headerDropdownRenderer) Destroy() {}

type dropdownPopup struct {
	widget.BaseWidget

	content   fyne.CanvasObject
	canvas    fyne.Canvas
	pos       fyne.Position
	size      fyne.Size
	shown     bool
	onDismiss func()
}

type StyledMenuItem struct {
	Label    string
	Selected bool
	OnTap    func()
}

func newDropdownPopup(content fyne.CanvasObject, canvas fyne.Canvas, size fyne.Size, onDismiss func()) *dropdownPopup {
	p := &dropdownPopup{
		content:   content,
		canvas:    canvas,
		size:      size,
		onDismiss: onDismiss,
	}
	p.ExtendBaseWidget(p)
	return p
}

func ShowStyledMenu(anchor fyne.CanvasObject, items []StyledMenuItem) {
	showStyledMenu(anchor, items, false)
}

func ShowStyledMenuAbove(anchor fyne.CanvasObject, items []StyledMenuItem) {
	showStyledMenu(anchor, items, true)
}

func showStyledMenu(anchor fyne.CanvasObject, items []StyledMenuItem, openAbove bool) {
	if anchor == nil || len(items) == 0 {
		return
	}

	rows := make([]fyne.CanvasObject, 0, len(items))
	var popup *dropdownPopup
	for _, item := range items {
		menuItem := item
		rows = append(rows, newDropdownItem(menuItem.Label, menuItem.Selected, func() {
			if popup != nil {
				popup.Hide()
			}
			if menuItem.OnTap != nil {
				menuItem.OnTap()
			}
		}))
	}

	menuBG := canvas.NewRectangle(design.ColorGray950)
	menuBG.CornerRadius = design.RadiusMD

	menuBorder := canvas.NewRectangle(color.Transparent)
	menuBorder.CornerRadius = design.RadiusMD
	menuBorder.StrokeColor = design.ColorBorder
	menuBorder.StrokeWidth = 1

	menu := container.NewStack(
		menuBG,
		NewInset(container.NewVBox(rows...), 6, 6, 6, 6),
		menuBorder,
	)

	canvasForObj := fyne.CurrentApp().Driver().CanvasForObject(anchor)
	if canvasForObj == nil {
		return
	}

	menuMin := menu.MinSize()
	width := menuMin.Width
	for _, option := range items {
		label := canvas.NewText(option.Label, design.ColorTextLight)
		label.TextSize = 14
		optionWidth := label.MinSize().Width + 40
		if optionWidth > width {
			width = optionWidth
		}
	}
	if anchor.Size().Width > width {
		width = anchor.Size().Width
	}

	popup = newDropdownPopup(
		menu,
		canvasForObj,
		fyne.NewSize(width, menuMin.Height),
		nil,
	)

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	popupPos := pos.Add(fyne.NewPos(0, anchor.Size().Height+6))
	if openAbove {
		popupPos = pos.Add(fyne.NewPos(0, -menuMin.Height-6))
		if popupPos.Y < 0 {
			popupPos.Y = 0
		}
	}
	popup.ShowAtPosition(popupPos)
}

func (p *dropdownPopup) CreateRenderer() fyne.WidgetRenderer {
	return &dropdownPopupRenderer{
		popup:   p,
		objects: []fyne.CanvasObject{p.content},
	}
}

func (p *dropdownPopup) MinSize() fyne.Size {
	return p.size
}

func (p *dropdownPopup) Tapped(ev *fyne.PointEvent) {
	if p.isInside(ev.Position) {
		return
	}
	p.Hide()
	if p.onDismiss != nil {
		p.onDismiss()
	}
}

func (p *dropdownPopup) TappedSecondary(ev *fyne.PointEvent) {
	p.Tapped(ev)
}

func (p *dropdownPopup) ShowAtPosition(pos fyne.Position) {
	p.pos = pos
	if !p.shown {
		p.canvas.Overlays().Add(p)
		p.shown = true
	}
	p.BaseWidget.Resize(p.canvas.Size())
	p.Show()
	p.Refresh()
}

func (p *dropdownPopup) Hide() {
	if p.shown {
		p.canvas.Overlays().Remove(p)
		p.shown = false
	}
	p.BaseWidget.Hide()
}

func (p *dropdownPopup) isInside(pos fyne.Position) bool {
	return pos.X >= p.pos.X &&
		pos.Y >= p.pos.Y &&
		pos.X <= p.pos.X+p.size.Width &&
		pos.Y <= p.pos.Y+p.size.Height
}

type dropdownPopupRenderer struct {
	popup   *dropdownPopup
	objects []fyne.CanvasObject
}

func (r *dropdownPopupRenderer) Layout(size fyne.Size) {
	r.popup.content.Move(r.popup.pos)
	r.popup.content.Resize(r.popup.size)
}

func (r *dropdownPopupRenderer) MinSize() fyne.Size {
	return r.popup.size
}

func (r *dropdownPopupRenderer) Refresh() {
	if r.popup.canvas.Size() != r.popup.Size() {
		r.popup.BaseWidget.Resize(r.popup.canvas.Size())
	}
	r.Layout(r.popup.Size())
	r.popup.content.Refresh()
	canvas.Refresh(r.popup)
}

func (r *dropdownPopupRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *dropdownPopupRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *dropdownPopupRenderer) Destroy() {}

type dropdownItemRenderer struct {
	item    *dropdownItem
	objects []fyne.CanvasObject
}

func (r *dropdownItemRenderer) Layout(size fyne.Size) {
	r.item.bg.Resize(size)
	labelMin := r.item.label.MinSize()
	r.item.label.Move(fyne.NewPos(14, (size.Height-labelMin.Height)/2))
	r.item.label.Resize(labelMin)
}

func (r *dropdownItemRenderer) MinSize() fyne.Size {
	return r.item.MinSize()
}

func (r *dropdownItemRenderer) Refresh() {
	r.item.refreshVisuals()
	r.Layout(r.item.Size())
	canvas.Refresh(r.item)
}

func (r *dropdownItemRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *dropdownItemRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *dropdownItemRenderer) Destroy() {}

var (
	_ fyne.Tappable     = (*HeaderDropdown)(nil)
	_ desktop.Hoverable = (*HeaderDropdown)(nil)
	_ fyne.Widget       = (*HeaderDropdown)(nil)
	_ fyne.Tappable     = (*dropdownPopup)(nil)
	_ fyne.Widget       = (*dropdownPopup)(nil)
	_ fyne.Tappable     = (*dropdownItem)(nil)
	_ desktop.Hoverable = (*dropdownItem)(nil)
)

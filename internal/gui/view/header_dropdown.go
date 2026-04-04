package view

import (
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type dropdownMenuTheme struct {
	base fyne.Theme
}

func (t *dropdownMenuTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameShadow:
		return color.Transparent
	default:
		return t.base.Color(name, variant)
	}
}

func (t *dropdownMenuTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *dropdownMenuTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *dropdownMenuTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}

type HeaderDropdown struct {
	widget.BaseWidget

	Options    []string
	Selected   string
	OnSelected func(string)
	MinWidth   float32

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
	d.updateMinWidth()
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
	if d.MinWidth > 0 {
		return fyne.NewSize(d.MinWidth, 36)
	}
	label := canvas.NewText(d.Selected, design.ColorTextLight)
	label.TextSize = 14
	width := label.MinSize().Width + 52
	minWidth := float32(88)
	if width < minWidth {
		width = minWidth
	}
	return fyne.NewSize(width, 36)
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
	d.updateMinWidth()
	d.Refresh()
}

func (d *HeaderDropdown) SetSelected(value string) {
	d.Selected = value
	d.updateMinWidth()
	d.Refresh()
}

func (d *HeaderDropdown) updateMinWidth() {
	longest := strings.TrimSpace(d.Selected)
	for _, option := range d.Options {
		option = strings.TrimSpace(option)
		if len(option) > len(longest) {
			longest = option
		}
	}

	label := canvas.NewText(longest, design.ColorTextLight)
	label.TextSize = 14
	width := label.MinSize().Width + 52
	if width < 88 {
		width = 88
	}
	d.MinWidth = width
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
		rows = append(rows, newDropdownItem(value, "", value == d.Selected, func() {
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

	menuList := container.NewVBox(rows...)
	menuContent := fyne.CanvasObject(NewInset(menuList, 6, 6, 6, 6))
	menu := container.NewStack(menuBG, menuContent, menuBorder)

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

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(d)
	gap := float32(6)
	canvasSize := canvasForObj.Size()
	menuHeight := menu.MinSize().Height
	belowSpace := canvasSize.Height - (pos.Y + d.Size().Height + gap)
	aboveSpace := pos.Y - gap
	openAbove := menuHeight > belowSpace && aboveSpace > belowSpace

	popupContent := fyne.CanvasObject(menu)
	popupHeight := menuHeight
	chosenSpace := belowSpace
	if openAbove {
		chosenSpace = aboveSpace
	}

	maxPopupHeight := chosenSpace
	if maxPopupHeight > canvasSize.Height-12 {
		maxPopupHeight = canvasSize.Height - 12
	}
	if maxPopupHeight < 80 {
		maxPopupHeight = 80
	}
	if popupHeight > maxPopupHeight {
		scroll := container.NewVScroll(menuList)
		scroll.SetMinSize(fyne.NewSize(menuWidth-18, maxPopupHeight-12))
		menuContent = NewInset(scroll, 6, 10, 6, 6)
		popupContent = container.NewThemeOverride(container.NewStack(menuBG, menuContent, menuBorder), &dropdownMenuTheme{base: design.NewBrandTheme()})
		popupHeight = maxPopupHeight
	}

	popupX := pos.X
	if popupX+menuWidth > canvasSize.Width {
		popupX = canvasSize.Width - menuWidth - 4
	}
	if popupX < 4 {
		popupX = 4
	}

	popupY := pos.Y + d.Size().Height + gap
	if openAbove {
		popupY = pos.Y - popupHeight - gap
	}
	if popupY < 4 {
		popupY = 4
	}
	if popupY+popupHeight > canvasSize.Height-4 {
		popupY = canvasSize.Height - popupHeight - 4
	}

	d.popup = newDropdownPopup(
		popupContent,
		canvasForObj,
		fyne.NewSize(menuWidth, popupHeight),
		d.popupDismissed,
	)

	d.popup.ShowAtPosition(fyne.NewPos(popupX, popupY))
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
	iconTranslucency := float64(0)
	if d.opened {
		iconResource = theme.Icon(theme.IconNameArrowDropUp)
	}

	switch {
	case d.disabled:
		fill = design.ColorGray900
		textColor = design.ColorBorder
		iconTranslucency = 0.35
	case d.opened:
		fill = design.ColorSurfaceLight
	case d.hovered:
		fill = design.ColorSurfaceLight
	}

	d.bg.FillColor = fill
	d.label.Text = d.Selected
	d.label.Color = textColor
	d.icon.Resource = iconResource
	d.icon.Translucency = iconTranslucency
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

	text      string
	secondary string
	selected  bool
	hovered   bool
	onTap     func()

	bg             *canvas.Rectangle
	label          *canvas.Text
	secondaryLabel *canvas.Text
}

func newDropdownItem(text, secondary string, selected bool, onTap func()) *dropdownItem {
	i := &dropdownItem{text: text, secondary: secondary, selected: selected, onTap: onTap}
	i.ExtendBaseWidget(i)
	return i
}

func (i *dropdownItem) CreateRenderer() fyne.WidgetRenderer {
	i.bg = canvas.NewRectangle(color.Transparent)
	i.bg.CornerRadius = design.RadiusMD
	i.label = canvas.NewText(i.text, design.ColorTextLight)
	i.label.TextSize = 14
	i.secondaryLabel = canvas.NewText(i.secondary, design.ColorTextMuted)
	i.secondaryLabel.TextSize = 14
	r := &dropdownItemRenderer{
		item:    i,
		objects: []fyne.CanvasObject{container.NewWithoutLayout(i.bg, i.label, i.secondaryLabel)},
	}
	r.Refresh()
	return r
}

func (i *dropdownItem) MinSize() fyne.Size {
	label := canvas.NewText(i.text, design.ColorTextLight)
	label.TextSize = 14
	width := label.MinSize().Width + 28
	if i.secondary != "" {
		secondary := canvas.NewText(i.secondary, design.ColorTextMuted)
		secondary.TextSize = 14
		width += secondary.MinSize().Width + 18
	}
	if width < 72 {
		width = 72
	}
	return fyne.NewSize(width, 36)
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
	if i.bg == nil || i.label == nil || i.secondaryLabel == nil {
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
	i.secondaryLabel.Refresh()
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
	Label          string
	SecondaryLabel string
	Selected       bool
	OnTap          func()
}

type StyledMenuOptions struct {
	OpenAbove bool
	Centered  bool
	Width     float32
	MaxHeight float32
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
	showStyledMenu(anchor, items, StyledMenuOptions{})
}

func ShowStyledMenuAbove(anchor fyne.CanvasObject, items []StyledMenuItem) {
	showStyledMenu(anchor, items, StyledMenuOptions{OpenAbove: true})
}

func ShowStyledMenuCentered(anchor fyne.CanvasObject, items []StyledMenuItem, width, maxHeight float32) {
	showStyledMenu(anchor, items, StyledMenuOptions{
		Centered:  true,
		Width:     width,
		MaxHeight: maxHeight,
	})
}

func showStyledMenu(anchor fyne.CanvasObject, items []StyledMenuItem, options StyledMenuOptions) {
	if anchor == nil || len(items) == 0 {
		return
	}

	rows := make([]fyne.CanvasObject, 0, len(items))
	var popup *dropdownPopup
	for _, item := range items {
		menuItem := item
		rows = append(rows, newDropdownItem(menuItem.Label, menuItem.SecondaryLabel, menuItem.Selected, func() {
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

	menuList := container.NewVBox(rows...)
	menuContent := fyne.CanvasObject(NewInset(menuList, 6, 6, 6, 6))
	menu := container.NewStack(menuBG, menuContent, menuBorder)

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
	if options.Width > width {
		width = options.Width
	}

	height := menuMin.Height
	maxHeight := options.MaxHeight
	if maxHeight <= 0 {
		maxHeight = canvasForObj.Size().Height - 16
	}
	if maxHeight < 80 {
		maxHeight = 80
	}

	content := fyne.CanvasObject(menu)
	if height > maxHeight {
		scroll := container.NewVScroll(menuList)
		scroll.SetMinSize(fyne.NewSize(width-18, maxHeight-12))
		menuContent = NewInset(scroll, 6, 10, 6, 6)
		content = container.NewThemeOverride(container.NewStack(menuBG, menuContent, menuBorder), &dropdownMenuTheme{base: design.NewBrandTheme()})
		height = maxHeight
	}

	popup = newDropdownPopup(
		content,
		canvasForObj,
		fyne.NewSize(width, height),
		nil,
	)

	pos := fyne.CurrentApp().Driver().AbsolutePositionForObject(anchor)
	popupPos := fyne.NewPos(
		pos.X+(anchor.Size().Width-width)/2,
		pos.Y+anchor.Size().Height+6,
	)
	if options.Centered {
		canvasSize := canvasForObj.Size()
		popupPos = fyne.NewPos(
			maxFloat32(8, (canvasSize.Width-width)/2),
			maxFloat32(8, pos.Y),
		)
		if popupPos.Y+height > canvasSize.Height-8 {
			popupPos.Y = canvasSize.Height - height - 8
		}
	} else if options.OpenAbove {
		popupPos = fyne.NewPos(
			pos.X+(anchor.Size().Width-width)/2,
			pos.Y-height-6,
		)
		if popupPos.Y < 0 {
			popupPos.Y = 0
		}
	}
	canvasSize := canvasForObj.Size()
	if popupPos.X < 8 {
		popupPos.X = 8
	}
	if popupPos.X+width > canvasSize.Width-8 {
		popupPos.X = canvasSize.Width - width - 8
	}
	if popupPos.Y+height > canvasSize.Height-8 && !options.OpenAbove && !options.Centered {
		popupPos.Y = canvasSize.Height - height - 8
	}
	if popupPos.Y < 8 {
		popupPos.Y = 8
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

	if r.item.secondary != "" {
		rightMin := r.item.secondaryLabel.MinSize()
		rightX := size.Width - rightMin.Width - 14
		if rightX < 14+labelMin.Width+12 {
			rightX = 14 + labelMin.Width + 12
		}
		r.item.secondaryLabel.Show()
		r.item.secondaryLabel.Move(fyne.NewPos(rightX, (size.Height-rightMin.Height)/2))
		r.item.secondaryLabel.Resize(rightMin)
		return
	}

	r.item.secondaryLabel.Hide()
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

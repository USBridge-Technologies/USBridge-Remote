package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// styledMenuItem is one row in showStyledTealMenu — the agent's copy of the
// client's teal header dropdown (ShowStyledMenuTeal): 10px teal labels on a
// Gray950 rounded panel.
type styledMenuItem struct {
	Label    string
	Icon     fyne.Resource
	Selected bool
	Disabled bool
	OnTap    func()
}

func showStyledTealMenu(anchor fyne.CanvasObject, items []styledMenuItem) {
	placeStyledTealMenu(anchor, items, false)
}

func showStyledTealMenuAbove(anchor fyne.CanvasObject, items []styledMenuItem) {
	placeStyledTealMenu(anchor, items, true)
}

func placeStyledTealMenu(anchor fyne.CanvasObject, items []styledMenuItem, above bool) {
	if anchor == nil || len(items) == 0 {
		return
	}
	drv := fyne.CurrentApp().Driver()
	c := drv.CanvasForObject(anchor)
	if c == nil {
		return
	}

	const (
		textSize  float32 = 10
		rowHeight float32 = 26
		inset     float32 = 6
	)

	var popup *tealMenuPopup
	rows := make([]fyne.CanvasObject, 0, len(items))
	width := float32(96)
	for _, item := range items {
		menuItem := item
		onTap := func() {
			if menuItem.Disabled {
				return
			}
			if popup != nil {
				popup.Hide()
			}
			if menuItem.OnTap != nil {
				menuItem.OnTap()
			}
		}
		rows = append(rows, newTealMenuRow(menuItem.Label, menuItem.Icon, menuItem.Selected, menuItem.Disabled, textSize, rowHeight, onTap))
		label := canvas.NewText(menuItem.Label, design.ColorTeal)
		label.TextSize = textSize
		extra := float32(40)
		if menuItem.Icon != nil {
			extra += 18
		}
		if w := label.MinSize().Width + extra; w > width {
			width = w
		}
	}

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	inner := newExactInset(container.NewVBox(rows...), inset, inset, inset, inset)
	content := container.NewStack(bg, inner, border)

	height := content.MinSize().Height
	if content.MinSize().Width > width {
		width = content.MinSize().Width
	}

	popup = newTealMenuPopup(content, c, fyne.NewSize(width, height))
	pos := drv.AbsolutePositionForObject(anchor)
	popupY := pos.Y + anchor.Size().Height + 6
	if above {
		popupY = pos.Y - height - 6
	}
	popupPos := fyne.NewPos(
		pos.X+(anchor.Size().Width-width)/2,
		popupY,
	)
	canvasSize := c.Size()
	if popupPos.X < 8 {
		popupPos.X = 8
	}
	if popupPos.X+width > canvasSize.Width-8 {
		popupPos.X = canvasSize.Width - width - 8
	}
	if popupPos.Y+height > canvasSize.Height-8 {
		popupPos.Y = canvasSize.Height - height - 8
	}
	if popupPos.Y < 8 {
		popupPos.Y = 8
	}
	popup.ShowAtPosition(popupPos)
}

type tealMenuPopup struct {
	widget.BaseWidget
	content fyne.CanvasObject
	canvas  fyne.Canvas
	pos     fyne.Position
	size    fyne.Size
	shown   bool
}

func newTealMenuPopup(content fyne.CanvasObject, c fyne.Canvas, size fyne.Size) *tealMenuPopup {
	p := &tealMenuPopup{content: content, canvas: c, size: size}
	p.ExtendBaseWidget(p)
	return p
}

func (p *tealMenuPopup) CreateRenderer() fyne.WidgetRenderer {
	return &tealMenuPopupRenderer{popup: p, objects: []fyne.CanvasObject{p.content}}
}

func (p *tealMenuPopup) MinSize() fyne.Size { return p.size }

func (p *tealMenuPopup) Tapped(ev *fyne.PointEvent) {
	if ev != nil && p.isInside(ev.Position) {
		return
	}
	p.Hide()
}

func (p *tealMenuPopup) TappedSecondary(ev *fyne.PointEvent) { p.Tapped(ev) }

func (p *tealMenuPopup) ShowAtPosition(pos fyne.Position) {
	p.pos = pos
	if !p.shown {
		p.canvas.Overlays().Add(p)
		p.shown = true
	}
	p.BaseWidget.Resize(p.canvas.Size())
	p.Show()
	p.Refresh()
}

func (p *tealMenuPopup) Hide() {
	if p.shown {
		p.canvas.Overlays().Remove(p)
		p.shown = false
	}
	p.BaseWidget.Hide()
}

func (p *tealMenuPopup) isInside(pos fyne.Position) bool {
	return pos.X >= p.pos.X &&
		pos.Y >= p.pos.Y &&
		pos.X <= p.pos.X+p.size.Width &&
		pos.Y <= p.pos.Y+p.size.Height
}

type tealMenuPopupRenderer struct {
	popup   *tealMenuPopup
	objects []fyne.CanvasObject
}

func (r *tealMenuPopupRenderer) Layout(size fyne.Size) {
	r.popup.content.Move(r.popup.pos)
	r.popup.content.Resize(r.popup.size)
}

func (r *tealMenuPopupRenderer) MinSize() fyne.Size { return r.popup.size }

func (r *tealMenuPopupRenderer) Refresh() {
	if r.popup.canvas.Size() != r.popup.Size() {
		r.popup.BaseWidget.Resize(r.popup.canvas.Size())
	}
	r.Layout(r.popup.Size())
	r.popup.content.Refresh()
	canvas.Refresh(r.popup)
}

func (r *tealMenuPopupRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *tealMenuPopupRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *tealMenuPopupRenderer) Destroy()                     {}

type tealMenuRow struct {
	widget.BaseWidget
	label     string
	icon      fyne.Resource
	selected  bool
	disabled  bool
	hovered   bool
	textSize  float32
	minHeight float32
	onTap     func()
}

func newTealMenuRow(label string, icon fyne.Resource, selected, disabled bool, textSize, minHeight float32, onTap func()) *tealMenuRow {
	r := &tealMenuRow{label: label, icon: icon, selected: selected, disabled: disabled, textSize: textSize, minHeight: minHeight, onTap: onTap}
	r.ExtendBaseWidget(r)
	return r
}

func (r *tealMenuRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = 4
	text := canvas.NewText(r.label, design.ColorTeal)
	text.TextSize = r.textSize
	objects := []fyne.CanvasObject{bg, text}
	var img *canvas.Image
	if r.icon != nil {
		img = canvas.NewImageFromResource(r.icon)
		img.FillMode = canvas.ImageFillContain
		img.SetMinSize(fyne.NewSize(12, 12))
		objects = []fyne.CanvasObject{bg, img, text}
	}
	return &tealMenuRowRenderer{row: r, bg: bg, icon: img, text: text, objects: objects}
}

func (r *tealMenuRow) MinSize() fyne.Size {
	t := canvas.NewText(r.label, design.ColorTeal)
	t.TextSize = r.textSize
	w := t.MinSize().Width + 28
	if r.icon != nil {
		w += 18
	}
	return fyne.NewSize(w, r.minHeight)
}

func (r *tealMenuRow) Tapped(*fyne.PointEvent) {
	if r.disabled {
		return
	}
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *tealMenuRow) TappedSecondary(*fyne.PointEvent) {}

func (r *tealMenuRow) MouseIn(*desktop.MouseEvent) {
	r.hovered = true
	r.Refresh()
}

func (r *tealMenuRow) MouseOut() {
	r.hovered = false
	r.Refresh()
}

func (r *tealMenuRow) MouseMoved(*desktop.MouseEvent) {}

func (r *tealMenuRow) Cursor() desktop.Cursor {
	if r.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

type tealMenuRowRenderer struct {
	row     *tealMenuRow
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *tealMenuRowRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	x := float32(14)
	if r.icon != nil {
		const side float32 = 12
		placeSquareIcon(r.icon, fyne.NewPos(x, (size.Height-side)/2), side)
		x += side + 6
	}
	ts := r.text.MinSize()
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos(x, (size.Height-ts.Height)/2-0.5))
}

func (r *tealMenuRowRenderer) MinSize() fyne.Size { return r.row.MinSize() }

func (r *tealMenuRowRenderer) Refresh() {
	fill := color.Color(color.Transparent)
	if r.row.selected {
		fill = design.ColorGray900
	}
	if r.row.hovered && !r.row.disabled {
		fill = design.ColorSurfaceLight
	}
	r.bg.FillColor = fill
	r.text.Text = r.row.label
	r.text.Color = design.ColorTeal
	if r.row.disabled {
		r.text.Color = design.ColorEmptyHint
	}
	r.text.TextSize = r.row.textSize
	if r.icon != nil {
		r.icon.Refresh()
	}
	r.bg.Refresh()
	r.text.Refresh()
	r.Layout(r.row.Size())
}

func (r *tealMenuRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *tealMenuRowRenderer) Destroy()                     {}

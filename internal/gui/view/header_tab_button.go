package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type HeaderTabButton struct {
	widget.BaseWidget

	labelText string
	iconRes   fyne.Resource
	activeRes fyne.Resource
	onTapped  func()
	active    bool
	hovered   bool

	icon      *canvas.Image
	label     *canvas.Text
	underline *canvas.Rectangle
}

func NewHeaderTabButton(label string, icon, activeIcon fyne.Resource, onTapped func()) *HeaderTabButton {
	b := &HeaderTabButton{
		labelText: label,
		iconRes:   icon,
		activeRes: activeIcon,
		onTapped:  onTapped,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *HeaderTabButton) CreateRenderer() fyne.WidgetRenderer {
	b.icon = canvas.NewImageFromResource(b.iconRes)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(18, 18))

	b.label = canvas.NewText(b.labelText, design.ColorTextMuted)
	b.label.TextSize = 18
	b.label.TextStyle.Bold = true

	b.underline = canvas.NewRectangle(color.Transparent)
	b.underline.SetMinSize(fyne.NewSize(1, 4))

	content := container.NewHBox(
		b.icon,
		NewInset(b.label, 8, 0, 0, 0),
	)

	root := container.NewBorder(
		nil,
		b.underline,
		nil,
		nil,
		NewInset(container.NewCenter(content), 10, 10, 6, 2),
	)

	b.refreshVisuals()
	return widget.NewSimpleRenderer(root)
}

func (b *HeaderTabButton) MinSize() fyne.Size {
	textWidth := fyne.MeasureText(b.labelText, 18, fyne.TextStyle{Bold: true}).Width
	width := float32(10 + 18 + 8 + textWidth + 10)
	return fyne.NewSize(width, 44)
}

func (b *HeaderTabButton) SetActive(active bool) {
	if b.active == active {
		return
	}
	b.active = active
	b.Refresh()
}

func (b *HeaderTabButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *HeaderTabButton) TappedSecondary(*fyne.PointEvent) {}

func (b *HeaderTabButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *HeaderTabButton) MouseMoved(*desktop.MouseEvent) {}

func (b *HeaderTabButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *HeaderTabButton) refreshVisuals() {
	if b.label == nil || b.icon == nil || b.underline == nil {
		return
	}

	switch {
	case b.active:
		b.label.Color = design.ColorAccent
		if b.activeRes != nil {
			b.icon.Resource = b.activeRes
		} else {
			b.icon.Resource = b.iconRes
		}
		b.icon.Translucency = 0
		b.underline.FillColor = design.ColorAccent
	case b.hovered:
		b.label.Color = design.ColorTextLight
		b.icon.Resource = b.iconRes
		b.icon.Translucency = 0
		b.underline.FillColor = color.Transparent
	default:
		b.label.Color = design.ColorTextMuted
		b.icon.Resource = b.iconRes
		b.icon.Translucency = 0.2
		b.underline.FillColor = color.Transparent
	}

	b.label.Refresh()
	b.icon.Refresh()
	b.underline.Refresh()
}

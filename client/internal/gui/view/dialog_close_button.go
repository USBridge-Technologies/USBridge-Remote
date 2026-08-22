package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type DialogCloseButton struct {
	widget.BaseWidget

	icon    fyne.Resource
	onTap   func()
	hovered bool
	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	img     *canvas.Image
}

func NewDialogCloseButton(onTap func()) *DialogCloseButton {
	btn := &DialogCloseButton{
		icon:  theme.CancelIcon(),
		onTap: onTap,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *DialogCloseButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD

	b.img = canvas.NewImageFromResource(b.icon)
	b.img.FillMode = canvas.ImageFillContain

	return &dialogCloseButtonRenderer{
		btn:     b,
		objects: []fyne.CanvasObject{b.bg, b.border, b.img},
	}
}

func (b *DialogCloseButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *DialogCloseButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *DialogCloseButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *DialogCloseButton) MouseMoved(*desktop.MouseEvent) {}

type dialogCloseButtonRenderer struct {
	btn     *DialogCloseButton
	objects []fyne.CanvasObject
}

func (r *dialogCloseButtonRenderer) Destroy() {}

func (r *dialogCloseButtonRenderer) Layout(size fyne.Size) {
	r.btn.bg.Resize(size)
	r.btn.border.Resize(size)
	pad := float32(4)
	r.btn.img.Move(fyne.NewPos(pad, pad))
	r.btn.img.Resize(fyne.NewSize(size.Width-pad*2, size.Height-pad*2))
}

func (r *dialogCloseButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(24, 24)
}

func (r *dialogCloseButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *dialogCloseButtonRenderer) Refresh() {
	if r.btn.hovered {
		r.btn.bg.FillColor = design.ColorSurfaceLight
	} else {
		r.btn.bg.FillColor = color.Transparent
	}
	r.btn.bg.Refresh()
}

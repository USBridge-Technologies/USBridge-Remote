package gui

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type headerStatusBadgeButton struct {
	widget.BaseWidget

	iconRes   fyne.Resource
	badgeText string
	onTapped  func()
	hovered   bool
	iconSize  fyne.Size

	bg       *canvas.Rectangle
	icon     *canvas.Image
	badgeBg  *canvas.Rectangle
	badgeTxt *canvas.Text
}

func newHeaderStatusBadgeButton(icon fyne.Resource, onTapped func()) *headerStatusBadgeButton {
	b := &headerStatusBadgeButton{
		iconRes:   icon,
		badgeText: "0",
		onTapped:  onTapped,
		iconSize:  fyne.NewSize(22, 22),
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *headerStatusBadgeButton) SetIcon(icon fyne.Resource) {
	b.iconRes = icon
	b.Refresh()
}

func (b *headerStatusBadgeButton) SetBadgeText(text string) {
	b.badgeText = text
	b.Refresh()
}

func (b *headerStatusBadgeButton) SetIconSize(size fyne.Size) {
	if size.Width <= 0 || size.Height <= 0 {
		size = fyne.NewSize(22, 22)
	}
	b.iconSize = size
	b.Refresh()
}

func (b *headerStatusBadgeButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *headerStatusBadgeButton) TappedSecondary(*fyne.PointEvent) {}

func (b *headerStatusBadgeButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *headerStatusBadgeButton) MouseMoved(*desktop.MouseEvent) {}

func (b *headerStatusBadgeButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *headerStatusBadgeButton) MinSize() fyne.Size {
	return fyne.NewSize(36, 36)
}

func (b *headerStatusBadgeButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.icon = canvas.NewImageFromResource(b.iconRes)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(b.iconSize)

	b.badgeBg = canvas.NewRectangle(design.ColorAccent)
	b.badgeBg.CornerRadius = 7

	b.badgeTxt = canvas.NewText(b.badgeText, design.ColorBackground)
	b.badgeTxt.TextSize = 8
	b.badgeTxt.TextStyle.Bold = true
	b.badgeTxt.Alignment = fyne.TextAlignCenter

	renderer := &headerStatusBadgeButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.bg, b.icon, b.badgeBg, b.badgeTxt},
	}
	renderer.Refresh()
	return renderer
}

type headerStatusBadgeButtonRenderer struct {
	button  *headerStatusBadgeButton
	objects []fyne.CanvasObject
}

func (r *headerStatusBadgeButtonRenderer) Layout(size fyne.Size) {
	r.button.bg.Move(fyne.NewPos(0, 0))
	r.button.bg.Resize(size)

	iconSize := r.button.iconSize
	if iconSize.Width <= 0 || iconSize.Height <= 0 {
		iconSize = fyne.NewSize(22, 22)
	}
	r.button.icon.Resize(iconSize)
	r.button.icon.Move(fyne.NewPos((size.Width-iconSize.Width)/2, (size.Height-iconSize.Height)/2))

	if r.button.badgeText == "" {
		return
	}

	badgeMin := fyne.MeasureText(r.button.badgeText, 8, fyne.TextStyle{Bold: true})
	badgeW := badgeMin.Width + 8
	if badgeW < 14 {
		badgeW = 14
	}
	badgeSize := fyne.NewSize(badgeW, 14)
	r.button.badgeBg.Resize(badgeSize)
	r.button.badgeBg.Move(fyne.NewPos(0, 0))
	textSize := fyne.MeasureText(r.button.badgeText, 8, fyne.TextStyle{Bold: true})
	r.button.badgeTxt.Resize(textSize)
	r.button.badgeTxt.Move(fyne.NewPos(
		(badgeSize.Width-textSize.Width)/2,
		(badgeSize.Height-textSize.Height)/2-1,
	))
}

func (r *headerStatusBadgeButtonRenderer) MinSize() fyne.Size {
	return r.button.MinSize()
}

func (r *headerStatusBadgeButtonRenderer) Refresh() {
	r.button.bg.FillColor = color.Transparent
	if r.button.hovered {
		r.button.bg.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10}
	}
	r.button.bg.Refresh()

	r.button.icon.Resource = r.button.iconRes
	r.button.icon.Refresh()

	r.button.badgeTxt.Text = r.button.badgeText
	if r.button.badgeText == "" {
		r.button.badgeBg.Hide()
		r.button.badgeTxt.Hide()
	} else {
		r.button.badgeBg.Show()
		r.button.badgeTxt.Show()
		r.button.badgeBg.FillColor = design.ColorAccent
		r.button.badgeBg.Refresh()
		r.button.badgeTxt.Color = design.ColorBackground
		r.button.badgeTxt.Refresh()
	}
	r.Layout(r.button.Size())
}

func (r *headerStatusBadgeButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *headerStatusBadgeButtonRenderer) Destroy() {}

func (r *headerStatusBadgeButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

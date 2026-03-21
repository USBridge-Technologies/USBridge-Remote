package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

func NewInset(content fyne.CanvasObject, left, right, top, bottom float32) *fyne.Container {
	var leftObj, rightObj, topObj, bottomObj fyne.CanvasObject

	if left > 0 {
		rect := canvas.NewRectangle(color.Transparent)
		rect.SetMinSize(fyne.NewSize(left, 1))
		leftObj = rect
	}
	if right > 0 {
		rect := canvas.NewRectangle(color.Transparent)
		rect.SetMinSize(fyne.NewSize(right, 1))
		rightObj = rect
	}
	if top > 0 {
		rect := canvas.NewRectangle(color.Transparent)
		rect.SetMinSize(fyne.NewSize(1, top))
		topObj = rect
	}
	if bottom > 0 {
		rect := canvas.NewRectangle(color.Transparent)
		rect.SetMinSize(fyne.NewSize(1, bottom))
		bottomObj = rect
	}

	return container.NewBorder(topObj, bottomObj, leftObj, rightObj, content)
}

func NewSurfacePanel(content fyne.CanvasObject, fill color.Color, radius float32) *fyne.Container {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	return container.NewStack(
		bg,
		container.NewPadded(container.NewPadded(content)),
	)
}

func NewCompactSurfacePanel(content fyne.CanvasObject, fill color.Color, radius float32) *fyne.Container {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	return container.NewStack(
		bg,
		container.NewPadded(content),
	)
}

func NewBrandBadge(text string, size fyne.Size) *fyne.Container {
	bg := canvas.NewRectangle(design.ColorAccent)
	bg.CornerRadius = design.RadiusMD

	label := canvas.NewText(text, design.ColorBackground)
	label.TextSize = 16
	label.TextStyle.Bold = true

	return container.NewGridWrap(
		size,
		container.NewStack(bg, container.NewCenter(label)),
	)
}

func NewBrandText(text string, size float32, col color.Color, bold bool) *canvas.Text {
	txt := canvas.NewText(text, col)
	txt.TextSize = size
	txt.TextStyle.Bold = bold
	return txt
}

func NewOutlinedControl(content fyne.CanvasObject, width, height float32) *fyne.Container {
	panel := NewCompactSurfacePanel(content, design.ColorSurface, design.RadiusMD)
	if width > 0 && height > 0 {
		return container.NewGridWrap(fyne.NewSize(width, height), panel)
	}
	return panel
}

func NewHeaderBand(title string, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(design.ColorGray900)

	body := NewInset(content, 16, 16, 12, 12)
	if strings.TrimSpace(title) != "" {
		titleText := NewBrandText(strings.ToUpper(strings.TrimSpace(title)), 12, design.ColorTextMuted, true)
		titleWrap := NewInset(titleText, 16, 16, 10, 4)
		body = container.NewVBox(titleWrap, NewInset(content, 16, 16, 8, 12))
	}

	return container.NewStack(bg, body)
}

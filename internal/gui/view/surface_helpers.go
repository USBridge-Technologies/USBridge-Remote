package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

const (
	headerBandHorizontalInset float32 = 10
	headerBandTitleTopInset   float32 = 10
	headerBandBodyTopInset    float32 = 6
	headerBandBodyBottomInset float32 = 5
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
	shadowSoft := canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x12})
	shadowSoft.CornerRadius = radius + 2

	shadowTight := canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x18})
	shadowTight.CornerRadius = radius + 1

	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius

	panelContent := container.NewPadded(content)

	return container.New(
		&compactSurfacePanelLayout{},
		shadowSoft,
		shadowTight,
		bg,
		panelContent,
	)
}

type compactSurfacePanelLayout struct{}

func (l *compactSurfacePanelLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 4 {
		return
	}

	shadowSoft := objects[0]
	shadowTight := objects[1]
	bg := objects[2]
	content := objects[3]

	panelHeight := size.Height - 8
	if panelHeight < 0 {
		panelHeight = 0
	}

	shadowSoft.Move(fyne.NewPos(6, 6))
	shadowSoft.Resize(fyne.NewSize(maxFloat32(0, size.Width-12), panelHeight))

	shadowTight.Move(fyne.NewPos(3, 3))
	shadowTight.Resize(fyne.NewSize(maxFloat32(0, size.Width-6), panelHeight))

	bg.Move(fyne.NewPos(0, 0))
	bg.Resize(fyne.NewSize(size.Width, panelHeight))

	content.Move(fyne.NewPos(0, 0))
	content.Resize(fyne.NewSize(size.Width, panelHeight))
}

func (l *compactSurfacePanelLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 4 {
		return fyne.NewSize(0, 0)
	}

	contentMin := objects[3].MinSize()
	return fyne.NewSize(contentMin.Width, contentMin.Height+8)
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
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(bg, content, border)
	if width > 0 && height > 0 {
		return container.NewGridWrap(fyne.NewSize(width, height), panel)
	}
	if height > 0 {
		return NewFixedHeight(panel, height)
	}
	return panel
}

func NewFixedHeight(content fyne.CanvasObject, height float32) *fyne.Container {
	if height <= 0 {
		return container.NewMax(content)
	}
	return container.New(&fixedHeightLayout{height: height}, content)
}

type fixedHeightLayout struct {
	height float32
}

func (l *fixedHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	child := objects[0]
	targetHeight := l.height
	if targetHeight <= 0 {
		targetHeight = size.Height
	}
	if targetHeight > size.Height {
		targetHeight = size.Height
	}

	y := (size.Height - targetHeight) / 2
	if y < 0 {
		y = 0
	}

	child.Move(fyne.NewPos(0, y))
	child.Resize(fyne.NewSize(size.Width, targetHeight))
}

func (l *fixedHeightLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	childMin := objects[0].MinSize()
	return fyne.NewSize(childMin.Width, l.height)
}

func NewHeaderBand(title string, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(design.ColorGray900)

	body := NewInset(content, headerBandHorizontalInset, headerBandHorizontalInset, headerBandBodyTopInset, headerBandBodyBottomInset)
	if strings.TrimSpace(title) != "" {
		titleText := NewBrandText(strings.ToUpper(strings.TrimSpace(title)), 12, design.ColorTextMuted, true)
		titleWrap := NewInset(titleText, headerBandHorizontalInset, headerBandHorizontalInset, headerBandTitleTopInset, 4)
		body = container.NewVBox(titleWrap, NewInset(content, headerBandHorizontalInset, headerBandHorizontalInset, 8, headerBandBodyBottomInset))
	}

	return container.NewStack(bg, body)
}

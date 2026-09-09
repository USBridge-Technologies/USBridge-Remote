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

// insetLayout is NewInset's own layout: a plain fyne.Layout that insets its
// one child by exact left/right/top/bottom pixels.
//
// This used to be built on container.NewBorder (a transparent spacer
// rectangle per non-zero side, passed as Border's top/bottom/left/right).
// That silently adds theme.Padding() (4px by default) *again* for every one
// of those sides on top of the spacer's own size (see Fyne's
// layout/borderlayout.go: borderLayout.Layout/MinSize both do
// `topHeight+padding`/`bottomHeight+padding`/etc. unconditionally whenever
// that slot is non-nil) -- so e.g. NewInset(x, 10, 10, 3, 3) was actually
// adding 3+4=7px top and bottom, 10+4=14px left and right, not the 3/10
// asked for, and every nested NewInset compounded the same hidden extra
// again. That compounding is what was making the Control header's status
// strip (and the header band wrapping it) run visibly taller than its own
// padding constants implied, no matter how those constants were tuned.
type insetLayout struct {
	left, right, top, bottom float32
}

func (l *insetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	content := objects[0]
	w := size.Width - l.left - l.right
	h := size.Height - l.top - l.bottom
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	content.Move(fyne.NewPos(l.left, l.top))
	content.Resize(fyne.NewSize(w, h))
}

func (l *insetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	min := objects[0].MinSize()
	return fyne.NewSize(min.Width+l.left+l.right, min.Height+l.top+l.bottom)
}

func NewInset(content fyne.CanvasObject, left, right, top, bottom float32) *fyne.Container {
	return container.New(&insetLayout{left: left, right: right, top: top, bottom: bottom}, content)
}

// bottomLineLayout is NewBottomLine's own layout -- content on top, a thin
// line (its own MinSize().Height, typically <1px) directly under it, with
// zero added padding. Same reasoning as insetLayout: the previous
// `container.NewBorder(nil, line, nil, nil, content)` pattern silently adds
// theme.Padding() (4px) under the line whenever the bottom slot is non-nil,
// with nothing matching it on top (top is nil) -- a real, if small,
// top/bottom asymmetry (see NewInset's own doc comment for the general
// mechanism).
type bottomLineLayout struct{}

func (l *bottomLineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	content, line := objects[0], objects[1]
	lineHeight := line.MinSize().Height
	contentHeight := size.Height - lineHeight
	if contentHeight < 0 {
		contentHeight = 0
	}
	content.Move(fyne.NewPos(0, 0))
	content.Resize(fyne.NewSize(size.Width, contentHeight))
	line.Move(fyne.NewPos(0, contentHeight))
	line.Resize(fyne.NewSize(size.Width, lineHeight))
}

func (l *bottomLineLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	contentMin := objects[0].MinSize()
	lineMin := objects[1].MinSize()
	width := contentMin.Width
	if lineMin.Width > width {
		width = lineMin.Width
	}
	return fyne.NewSize(width, contentMin.Height+lineMin.Height)
}

// NewBottomLine stacks content above a thin line (e.g. a header's accent
// line) with no padding between them, pixel-exact -- see bottomLineLayout's
// own doc comment for why this isn't just container.NewBorder(nil, line,
// nil, nil, content).
func NewBottomLine(content, line fyne.CanvasObject) *fyne.Container {
	return container.New(&bottomLineLayout{}, content, line)
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

	panelContent := NewInset(content, 4, 4, 2, 2)

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
		return container.NewStack(content)
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

	// Same hairline accent under the band as the connections screen's own
	// header (connection_header.go's newConnectionHeader) -- this used to be
	// missing here, the one visible difference once Control's header
	// (createMainAddressBar, this function's only caller) started reusing
	// that header's own accessory menu.
	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))

	return container.NewStack(bg, NewBottomLine(body, accentLine))
}

// MobileFooterBottomInset adds padding to the bottom of the container on mobile devices
// to keep it above the system navigation bar (Android).
func MobileFooterBottomInset(base float32) float32 {
	if !fyne.CurrentDevice().IsMobile() {
		return base
	}

	// In Edge-to-Edge mode we need very little padding.
	return base + 4
}

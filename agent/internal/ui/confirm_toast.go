package ui

import (
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const confirmToastRadius float32 = 4
const confirmToastButtonRadius float32 = 4

type confirmToastButtonsLayout struct {
	gap float32
}

func (l *confirmToastButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	left := objects[0]
	right := objects[1]
	leftMin := left.MinSize()
	rightMin := right.MinSize()

	x := size.Width - rightMin.Width
	right.Move(fyne.NewPos(x, (size.Height-rightMin.Height)/2))
	right.Resize(rightMin)

	x -= l.gap + leftMin.Width
	left.Move(fyne.NewPos(x, (size.Height-leftMin.Height)/2))
	left.Resize(leftMin)
}

func (l *confirmToastButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	leftMin := objects[0].MinSize()
	rightMin := objects[1].MinSize()
	h := leftMin.Height
	if rightMin.Height > h {
		h = rightMin.Height
	}
	return fyne.NewSize(leftMin.Width+rightMin.Width+l.gap, h)
}

type confirmToastButton struct {
	widget.BaseWidget

	labelText      string
	onTapped       func()
	hovered        bool
	fillColor      color.Color
	borderColor    color.Color
	textColor      color.Color
	hoverFillColor color.Color
	hoverTextColor color.Color

	bg     *canvas.Rectangle
	border *canvas.Rectangle
	label  *canvas.Text
}

func newConfirmToastButton(label string, fillColor, borderColor, textColor, hoverFillColor, hoverTextColor color.Color, onTapped func()) *confirmToastButton {
	btn := &confirmToastButton{
		labelText:      label,
		onTapped:       onTapped,
		fillColor:      fillColor,
		borderColor:    borderColor,
		textColor:      textColor,
		hoverFillColor: hoverFillColor,
		hoverTextColor: hoverTextColor,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *confirmToastButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.fillColor)
	b.bg.CornerRadius = confirmToastButtonRadius

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = confirmToastButtonRadius
	if b.borderColor != nil && b.borderColor != color.Transparent {
		b.border.StrokeColor = b.borderColor
		b.border.StrokeWidth = 1
	}

	b.label = canvas.NewText(b.labelText, b.textColor)
	b.label.TextSize = 11
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	content := container.NewStack(b.bg, b.border, newExactInset(container.NewCenter(b.label), 12, 12, 5, 5))
	return widget.NewSimpleRenderer(content)
}

func (b *confirmToastButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *confirmToastButton) TappedSecondary(*fyne.PointEvent) {}

func (b *confirmToastButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *confirmToastButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *confirmToastButton) MouseMoved(*desktop.MouseEvent) {}

func (b *confirmToastButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *confirmToastButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}
	fill := b.fillColor
	text := b.textColor
	if b.hovered {
		if b.hoverFillColor != nil {
			fill = b.hoverFillColor
		}
		if b.hoverTextColor != nil {
			text = b.hoverTextColor
		}
	}
	b.bg.FillColor = fill
	b.label.Color = text
	b.bg.Refresh()
	b.label.Refresh()
}

// showConfirmToast is the client's bottom-center Yes/No chip: no title,
// no dim. Yes uses the agent CTA lime (#c4e77a / #4c6803).
func showConfirmToast(message string, callback func(bool), parent fyne.Window) {
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	var once sync.Once
	closePopup := func(ok bool) {
		once.Do(func() {
			if callback != nil {
				callback(ok)
			}
			if popup != nil {
				popup.Hide()
			}
		})
	}

	noBtn := newConfirmToastButton("No",
		color.Transparent, design.ColorTailscaleChipBorder, design.ColorMutedOlive,
		design.ColorSurfaceLight, design.ColorTextLight,
		func() { closePopup(false) })
	yesBtn := newConfirmToastButton("Yes",
		design.ColorCTA, color.Transparent, design.ColorCTALabel,
		design.ColorCTAHover, design.ColorCTALabel,
		func() { closePopup(true) })

	text := canvas.NewText(message, design.ColorMutedOlive)
	text.TextSize = 11
	buttons := container.New(&confirmToastButtonsLayout{gap: 6}, noBtn, yesBtn)
	body := container.NewBorder(nil, nil, nil, buttons, newExactInset(text, 0, 10, 0, 0))

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = confirmToastRadius
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = confirmToastRadius
	border.StrokeColor = design.ColorTailscaleChipBorder
	border.StrokeWidth = 1
	panel := container.NewStack(
		bg,
		newExactInset(body, 14, 14, 10, 10),
		border,
	)

	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel:        panel,
		DimColor:     color.Transparent,
		OnOutsideTap: func() { closePopup(false) },
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := canvasSize.Width * 0.04
			if margin < 20 {
				margin = 20
			}
			if margin > 28 {
				margin = 28
			}
			maxWidth := canvasSize.Width - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			panelMin := panel.MinSize()
			w := panelMin.Width
			if w > maxWidth {
				w = maxWidth
			}
			return fyne.NewSize(w, panelMin.Height)
		},
		PanelPos: func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position {
			bottom := canvasSize.Height * 0.05
			if bottom < 24 {
				bottom = 24
			}
			if bottom > 40 {
				bottom = 40
			}
			return fyne.NewPos((canvasSize.Width-panelSize.Width)/2, canvasSize.Height-panelSize.Height-bottom)
		},
	})
}

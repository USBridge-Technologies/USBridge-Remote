package view

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// StorageProgressBar — компактный chip: тонкая шкала и строка занятости.
type StorageProgressBar struct {
	widget.BaseWidget
	usedPercent float64 // 0-100
	sizeText    string  // "66/119 GB"
}

// NewStorageProgressBar создаёт новый компактный индикатор места на диске
func NewStorageProgressBar() *StorageProgressBar {
	s := &StorageProgressBar{}
	s.ExtendBaseWidget(s)
	return s
}

// SetValue устанавливает процент занятости (0-1).
func (s *StorageProgressBar) SetValue(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.usedPercent = v * 100
	s.Refresh()
}

// SetText устарел, используйте SetSizeText для строки занятости
func (s *StorageProgressBar) SetText(text string) {
	s.sizeText = text
	s.Refresh()
}

// SetSizeText устанавливает текст занятости: "66/119 GB"
func (s *StorageProgressBar) SetSizeText(text string) {
	s.sizeText = text
	s.Refresh()
}

// colorByUsedPercent возвращает цвет в зависимости от заполненности
func colorByUsedPercent(pct float64) color.Color {
	if pct < 60 {
		return color.NRGBA{R: 76, G: 175, B: 80, A: 255}
	}
	if pct < 85 {
		return color.NRGBA{R: 255, G: 152, B: 0, A: 255}
	}
	return color.NRGBA{R: 244, G: 67, B: 54, A: 255}
}

// CreateRenderer создаёт рендерер
func (s *StorageProgressBar) CreateRenderer() fyne.WidgetRenderer {
	t := s.Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	bgColor := t.Color(theme.ColorNameButton, variant)
	fgColor := t.Color(theme.ColorNameForeground, variant)

	bg := canvas.NewRectangle(bgColor)
	bg.CornerRadius = design.RadiusMD
	icon := canvas.NewImageFromResource(assets.MemoryChipIcon)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(iconSize, iconSize))
	track := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c})
	track.CornerRadius = design.RadiusMD
	fill := canvas.NewRectangle(colorByUsedPercent(s.usedPercent))
	fill.CornerRadius = design.RadiusMD

	sizeText := canvas.NewText(s.sizeText, fgColor)
	sizeText.TextSize = theme.TextSize() * 11 / 14
	sizeText.TextStyle.Bold = false

	return &storageProgressBarRenderer{
		s:        s,
		bg:       bg,
		icon:     icon,
		track:    track,
		fill:     fill,
		sizeText: sizeText,
		objs:     []fyne.CanvasObject{bg, icon, track, fill, sizeText},
	}
}

type storageProgressBarRenderer struct {
	s        *StorageProgressBar
	bg       *canvas.Rectangle
	icon     *canvas.Image
	track    *canvas.Rectangle
	fill     *canvas.Rectangle
	sizeText *canvas.Text
	objs     []fyne.CanvasObject
}

const (
	padH         = float32(8)
	padV         = float32(5)
	iconSize     = float32(20)
	iconGap      = float32(6)
	barHeight    = float32(4)
	barTopOffset = float32(2)
	textTopGap   = float32(6)
)

func (r *storageProgressBarRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	iconX := padH
	iconY := maxFloat32(0, (size.Height-iconSize)/2)
	r.icon.Move(fyne.NewPos(iconX, iconY))
	r.icon.Resize(fyne.NewSize(iconSize, iconSize))

	contentX := padH + iconSize + iconGap
	contentWidth := size.Width - contentX - padH
	if contentWidth < 0 {
		contentWidth = 0
	}
	textWidth := r.sizeText.MinSize().Width
	groupWidth := textWidth
	if groupWidth < 1 {
		groupWidth = 1
	}
	if groupWidth > contentWidth && contentWidth > 0 {
		groupWidth = contentWidth
	}
	barWidth := groupWidth

	barY := padV + barTopOffset
	barX := contentX
	r.track.Move(fyne.NewPos(barX, barY))
	r.track.Resize(fyne.NewSize(barWidth, barHeight))

	fillWidth := barWidth * float32(r.s.usedPercent/100)
	if fillWidth < 2 {
		fillWidth = 0
	}
	r.fill.Resize(fyne.NewSize(fillWidth, barHeight))
	r.fill.Move(fyne.NewPos(barX, barY))

	lineY := barY + barHeight + textTopGap
	lineH := size.Height - lineY - padV
	if lineH < 4 {
		lineH = 4
	}
	r.sizeText.Resize(fyne.NewSize(groupWidth, lineH))
	r.sizeText.Move(fyne.NewPos(contentX, lineY))
}

func (r *storageProgressBarRenderer) MinSize() fyne.Size {
	measure := canvas.NewText(r.s.sizeText, r.sizeText.Color)
	measure.TextSize = r.sizeText.TextSize
	measure.TextStyle = r.sizeText.TextStyle

	textWidth := measure.MinSize().Width
	if textWidth < 1 {
		textWidth = 1
	}

	width := iconSize + iconGap + textWidth + padH*2
	height := padV + barHeight + textTopGap + measure.MinSize().Height + padV
	return fyne.NewSize(width, height)
}

func (r *storageProgressBarRenderer) Refresh() {
	t := r.s.Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	r.bg.FillColor = t.Color(theme.ColorNameButton, variant)
	r.track.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c}
	r.fill.FillColor = colorByUsedPercent(r.s.usedPercent)
	r.icon.Resource = assets.MemoryChipIcon

	fg := t.Color(theme.ColorNameForeground, variant)
	r.sizeText.Color = fg
	r.sizeText.Text = r.s.sizeText

	if sz := r.s.Size(); sz.Width > 0 {
		barWidth := r.sizeText.MinSize().Width
		if barWidth < 1 {
			barWidth = 1
		}
		fillWidth := barWidth * float32(r.s.usedPercent/100)
		if fillWidth < 2 {
			fillWidth = 0
		}
		r.fill.Resize(fyne.NewSize(fillWidth, barHeight))
	}

	canvas.Refresh(r.bg)
	canvas.Refresh(r.icon)
	canvas.Refresh(r.track)
	canvas.Refresh(r.fill)
	canvas.Refresh(r.sizeText)
}

func (r *storageProgressBarRenderer) Objects() []fyne.CanvasObject {
	return r.objs
}

func (r *storageProgressBarRenderer) Destroy() {}

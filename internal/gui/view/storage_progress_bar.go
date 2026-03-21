package view

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// StorageProgressBar — компактный chip: иконка + процент, под ним занятость мелким шрифтом
type StorageProgressBar struct {
	widget.BaseWidget
	usedPercent float64 // 0-100
	percentText string  // "43%"
	sizeText    string  // "66/119 GB"
}

// NewStorageProgressBar создаёт новый компактный индикатор места на диске
func NewStorageProgressBar() *StorageProgressBar {
	s := &StorageProgressBar{}
	s.ExtendBaseWidget(s)
	return s
}

// SetValue устанавливает процент занятости (0-1) и обновляет текст процента
func (s *StorageProgressBar) SetValue(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.usedPercent = v * 100
	s.percentText = fmt.Sprintf("%.0f%%", s.usedPercent)
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

// SetPercentText устанавливает текст процента: "43%"
func (s *StorageProgressBar) SetPercentText(text string) {
	s.percentText = text
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
	fill := canvas.NewRectangle(colorByUsedPercent(s.usedPercent))
	fill.CornerRadius = design.RadiusMD

	icon := widget.NewIcon(theme.StorageIcon())
	percentText := canvas.NewText(s.percentText, fgColor)
	percentText.TextSize = theme.TextSize()
	percentText.TextStyle.Bold = true

	sizeText := canvas.NewText(s.sizeText, fgColor)
	sizeText.TextSize = theme.TextSize() * 11 / 14 // ~79% от обычного
	sizeText.TextStyle.Bold = false

	return &storageProgressBarRenderer{
		s:           s,
		bg:          bg,
		fill:        fill,
		icon:        icon,
		percentText: percentText,
		sizeText:    sizeText,
		objs:        []fyne.CanvasObject{bg, fill, icon, percentText, sizeText},
	}
}

type storageProgressBarRenderer struct {
	s           *StorageProgressBar
	bg          *canvas.Rectangle
	fill        *canvas.Rectangle
	icon        *widget.Icon
	percentText *canvas.Text
	sizeText    *canvas.Text
	objs        []fyne.CanvasObject
}

const (
	iconSize = float32(14)
	padH     = float32(4)
	padV     = float32(2)
	rowGap   = float32(1)
)

func (r *storageProgressBarRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	fillWidth := size.Width * float32(r.s.usedPercent/100)
	if fillWidth < 2 {
		fillWidth = 0
	}
	r.fill.Resize(fyne.NewSize(fillWidth, size.Height))
	r.fill.Move(fyne.NewPos(0, 0))

	// Верхняя строка: иконка + процент
	r.icon.Resize(fyne.NewSize(iconSize, iconSize))
	r.icon.Move(fyne.NewPos(padH, padV))

	percentW := float32(32)
	r.percentText.Resize(fyne.NewSize(percentW, iconSize))
	r.percentText.Move(fyne.NewPos(padH+iconSize+2, padV))

	// Нижняя строка: занятость мелким шрифтом
	topRowBottom := padV + iconSize + rowGap
	lineH := size.Height - topRowBottom - padV
	if lineH < 4 {
		lineH = 4
	}
	r.sizeText.Resize(fyne.NewSize(size.Width-padH*2, lineH))
	r.sizeText.Move(fyne.NewPos(padH, topRowBottom))
}

func (r *storageProgressBarRenderer) MinSize() fyne.Size {
	return fyne.NewSize(70, 36)
}

func (r *storageProgressBarRenderer) Refresh() {
	t := r.s.Theme()
	variant := fyne.CurrentApp().Settings().ThemeVariant()
	r.bg.FillColor = t.Color(theme.ColorNameButton, variant)
	r.fill.FillColor = colorByUsedPercent(r.s.usedPercent)

	fg := t.Color(theme.ColorNameForeground, variant)
	r.percentText.Color = fg
	r.percentText.Text = r.s.percentText
	r.sizeText.Color = fg
	r.sizeText.Text = r.s.sizeText

	if sz := r.s.Size(); sz.Width > 0 {
		fillWidth := sz.Width * float32(r.s.usedPercent/100)
		if fillWidth < 2 {
			fillWidth = 0
		}
		r.fill.Resize(fyne.NewSize(fillWidth, sz.Height))
	}

	canvas.Refresh(r.bg)
	canvas.Refresh(r.fill)
	canvas.Refresh(r.percentText)
	canvas.Refresh(r.sizeText)
	r.icon.Refresh()
}

func (r *storageProgressBarRenderer) Objects() []fyne.CanvasObject {
	return r.objs
}

func (r *storageProgressBarRenderer) Destroy() {}

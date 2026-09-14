package view

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// StorageProgressBar — a compact chip: a thin bar and a usage line.
type StorageProgressBar struct {
	widget.BaseWidget
	usedPercent float64 // 0-100
	sizeText    string  // "66/119 GB"
	iconRes     fyne.Resource
	onTapped    func()
	hovered     bool
}

// NewStorageProgressBar creates a new compact disk space indicator
func NewStorageProgressBar() *StorageProgressBar {
	s := &StorageProgressBar{iconRes: assets.MemoryChipIcon}
	s.ExtendBaseWidget(s)
	return s
}

// SetValue sets the usage percentage (0-1).
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

// SetText is deprecated, use SetSizeText for the usage line
func (s *StorageProgressBar) SetText(text string) {
	s.sizeText = text
	s.Refresh()
}

// SetSizeText sets the usage text: "66/119 GB"
func (s *StorageProgressBar) SetSizeText(text string) {
	s.sizeText = text
	s.Refresh()
}

func (s *StorageProgressBar) SetIcon(icon fyne.Resource) {
	if icon == nil {
		icon = assets.MemoryChipIcon
	}
	s.iconRes = icon
	s.Refresh()
}

func (s *StorageProgressBar) SetOnTapped(onTapped func()) {
	s.onTapped = onTapped
}

func (s *StorageProgressBar) Tapped(*fyne.PointEvent) {
	if s.onTapped != nil {
		s.onTapped()
	}
}

func (s *StorageProgressBar) TappedSecondary(*fyne.PointEvent) {}

func (s *StorageProgressBar) MouseIn(*desktop.MouseEvent) {
	s.hovered = true
	s.Refresh()
}

func (s *StorageProgressBar) MouseMoved(*desktop.MouseEvent) {}

func (s *StorageProgressBar) MouseOut() {
	s.hovered = false
	s.Refresh()
}

// storageBarFillColor is the used-space fill's own color -- a fixed
// turquoise (matches design.ColorConnectionBadgeText) rather than the
// red/orange/green usage-tier scheme this used to have, per the Control
// header's own status-indicator-strip design.
var storageBarFillColor color.Color = design.ColorConnectionBadgeText

// CreateRenderer creates the renderer
func (s *StorageProgressBar) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = design.RadiusMD
	icon := canvas.NewImageFromResource(s.iconRes)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(iconSize, iconSize))
	track := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c})
	track.CornerRadius = design.RadiusMD
	fill := canvas.NewRectangle(storageBarFillColor)
	fill.CornerRadius = design.RadiusMD

	sizeText := canvas.NewText(s.sizeText, design.ColorStatusBarIndicatorText)
	sizeText.TextSize = 7
	sizeText.TextStyle.Bold = false

	topRow := container.NewWithoutLayout(icon, track, fill)
	return &storageProgressBarRenderer{
		s:        s,
		bg:       bg,
		icon:     icon,
		topRow:   topRow,
		track:    track,
		fill:     fill,
		sizeText: sizeText,
		objs:     []fyne.CanvasObject{bg, topRow, sizeText},
	}
}

type storageProgressBarRenderer struct {
	s        *StorageProgressBar
	bg       *canvas.Rectangle
	icon     *canvas.Image
	topRow   *fyne.Container
	track    *canvas.Rectangle
	fill     *canvas.Rectangle
	sizeText *canvas.Text
	objs     []fyne.CanvasObject
}

const (
	padH = float32(8)
	// padV/iconSize/rowGap started at 4/12/1 and were trimmed so this
	// chip's MinSize stays inside the status-indicator strip's 22px
	// content slot (statusBarIconBoxSize) and does not stretch the
	// Control header when the storage plaque appears.
	padV      = float32(1)
	rowGap    = float32(0)
	iconSize  = float32(9)
	iconGap   = float32(6)
	// storageChipMaxH matches statusBarIconBoxSize.Height so this chip
	// cannot push the Control header taller than the icon row when it
	// appears (icon + "12/32 GB" used to overflow by a couple of pixels).
	storageChipMaxH = float32(22)
	barHeight = float32(4)
	barMaxW   = float32(58)
)

func (r *storageProgressBarRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	contentWidth := size.Width - padH*2
	if contentWidth < 0 {
		contentWidth = 0
	}
	textWidth := r.sizeText.MinSize().Width

	topY := padV
	trackX := iconSize + iconGap
	trackWidth := textWidth - trackX
	if trackWidth > contentWidth-trackX {
		trackWidth = contentWidth - trackX
	}
	if trackWidth < 0 {
		trackWidth = 0
	}
	if trackWidth > barMaxW {
		trackWidth = barMaxW
	}

	r.icon.Move(fyne.NewPos(0, 0))
	r.icon.Resize(fyne.NewSize(iconSize, iconSize))

	trackY := (iconSize - barHeight) / 2
	if trackY < 0 {
		trackY = 0
	}
	r.track.Move(fyne.NewPos(trackX, trackY))
	r.track.Resize(fyne.NewSize(trackWidth, barHeight))

	fillWidth := trackWidth * float32(r.s.usedPercent/100)
	if fillWidth < 2 {
		fillWidth = 0
	}
	r.fill.Move(fyne.NewPos(trackX, trackY))
	r.fill.Resize(fyne.NewSize(fillWidth, barHeight))

	r.topRow.Move(fyne.NewPos(padH, topY))
	r.topRow.Resize(fyne.NewSize(contentWidth, iconSize))

	lineY := topY + iconSize + rowGap
	lineH := size.Height - lineY - padV
	if lineH < 4 {
		lineH = 4
	}
	r.sizeText.Resize(fyne.NewSize(contentWidth, lineH))
	r.sizeText.Move(fyne.NewPos(padH, lineY))
}

func (r *storageProgressBarRenderer) MinSize() fyne.Size {
	measure := canvas.NewText(r.s.sizeText, r.sizeText.Color)
	measure.TextSize = r.sizeText.TextSize
	measure.TextStyle = r.sizeText.TextStyle

	textWidth := measure.MinSize().Width
	if textWidth < 1 {
		textWidth = 1
	}

	width := textWidth + padH*2
	height := padV + iconSize + rowGap + measure.MinSize().Height + padV
	if height > storageChipMaxH {
		height = storageChipMaxH
	}
	return fyne.NewSize(width, height)
}

func (r *storageProgressBarRenderer) Refresh() {
	r.bg.FillColor = color.Transparent
	if r.s.hovered && r.s.onTapped != nil {
		r.bg.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10}
	}
	r.track.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c}
	r.fill.FillColor = storageBarFillColor
	if r.s.iconRes == nil {
		r.s.iconRes = assets.MemoryChipIcon
	}
	r.icon.Resource = r.s.iconRes

	r.sizeText.Color = design.ColorStatusBarIndicatorText
	r.sizeText.Text = r.s.sizeText

	if sz := r.s.Size(); sz.Width > 0 {
		barWidth := r.sizeText.MinSize().Width - iconSize - iconGap
		maxAvailable := sz.Width - padH*2 - iconSize - iconGap
		if barWidth > maxAvailable {
			barWidth = maxAvailable
		}
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
	r.topRow.Refresh()
}

func (r *storageProgressBarRenderer) Objects() []fyne.CanvasObject {
	return r.objs
}

func (r *storageProgressBarRenderer) Destroy() {}

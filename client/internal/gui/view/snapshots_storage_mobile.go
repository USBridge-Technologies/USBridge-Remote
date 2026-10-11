package view

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// MobileSnapshotStorage is the memory plaque above the phone snapshot list.
// Same card chrome as that list, with the usage line and a bar that can
// use the card's width. Tap opens the storage breakdown.
type MobileSnapshotStorage struct {
	widget.BaseWidget
	usedPercent float64
	sizeText    string
	iconRes     fyne.Resource
	onTapped    func()
	hovered     bool
}

func NewMobileSnapshotStorage() *MobileSnapshotStorage {
	s := &MobileSnapshotStorage{iconRes: assets.MemoryChipIcon}
	s.ExtendBaseWidget(s)
	return s
}

// SetValue sets usage as a fraction from 0 to 1.
func (s *MobileSnapshotStorage) SetValue(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	s.usedPercent = v * 100
	s.Refresh()
}

func (s *MobileSnapshotStorage) SetSizeText(text string) {
	s.sizeText = text
	s.Refresh()
}

func (s *MobileSnapshotStorage) SetIcon(icon fyne.Resource) {
	if icon == nil {
		icon = assets.MemoryChipIcon
	}
	s.iconRes = icon
	s.Refresh()
}

func (s *MobileSnapshotStorage) SetOnTapped(onTapped func()) {
	s.onTapped = onTapped
}

func (s *MobileSnapshotStorage) Tapped(*fyne.PointEvent) {
	if s.onTapped != nil {
		s.onTapped()
	}
}

func (s *MobileSnapshotStorage) TappedSecondary(*fyne.PointEvent) {}

func (s *MobileSnapshotStorage) MouseIn(*desktop.MouseEvent) {
	s.hovered = true
	s.Refresh()
}

func (s *MobileSnapshotStorage) MouseOut() {
	s.hovered = false
	s.Refresh()
}

func (s *MobileSnapshotStorage) MouseMoved(*desktop.MouseEvent) {}

func (s *MobileSnapshotStorage) Cursor() desktop.Cursor {
	if s.onTapped == nil {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (s *MobileSnapshotStorage) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusLG
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1

	tile := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10})
	tile.CornerRadius = design.RadiusMD

	icon := canvas.NewImageFromResource(s.iconRes)
	icon.FillMode = canvas.ImageFillContain

	title := canvas.NewText("", design.ColorConnectionsSectionSubtitle)
	title.TextSize = 9

	sizeText := canvas.NewText("", design.ColorTextLight)
	sizeText.TextSize = 13
	sizeText.TextStyle.Bold = true

	percent := canvas.NewText("", design.ColorConnectionBadgeText)
	percent.TextSize = 12
	percent.TextStyle.Bold = true

	track := canvas.NewRectangle(color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c})
	track.CornerRadius = design.RadiusMD
	fill := canvas.NewRectangle(storageBarFillColor)
	fill.CornerRadius = design.RadiusMD

	return &mobileSnapshotStorageRenderer{
		s:        s,
		bg:       bg,
		tile:     tile,
		icon:     icon,
		title:    title,
		sizeText: sizeText,
		percent:  percent,
		track:    track,
		fill:     fill,
		objs:     []fyne.CanvasObject{bg, tile, icon, title, sizeText, percent, track, fill},
	}
}

const (
	mobileStoragePadX    = float32(12)
	mobileStoragePadY    = float32(10)
	mobileStorageIconBox = float32(28)
	mobileStorageIcon    = float32(16)
	mobileStorageGap     = float32(10)
	mobileStorageBarH    = float32(5)
	mobileStorageTextGap = float32(6)
)

type mobileSnapshotStorageRenderer struct {
	s        *MobileSnapshotStorage
	bg       *canvas.Rectangle
	tile     *canvas.Rectangle
	icon     *canvas.Image
	title    *canvas.Text
	sizeText *canvas.Text
	percent  *canvas.Text
	track    *canvas.Rectangle
	fill     *canvas.Rectangle
	objs     []fyne.CanvasObject
}

func (r *mobileSnapshotStorageRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	r.bg.Move(fyne.NewPos(0, 0))

	textX := mobileStoragePadX + mobileStorageIconBox + mobileStorageGap
	textW := size.Width - textX - mobileStoragePadX
	if textW < 1 {
		textW = 1
	}
	titleH := r.title.MinSize().Height
	sizeH := r.sizeText.MinSize().Height
	textBlock := titleH + mobileStorageTextGap + sizeH
	rowH := mobileStorageIconBox
	if textBlock > rowH {
		rowH = textBlock
	}

	tileY := mobileStoragePadY + (rowH-mobileStorageIconBox)/2
	r.tile.Move(fyne.NewPos(mobileStoragePadX, tileY))
	r.tile.Resize(fyne.NewSize(mobileStorageIconBox, mobileStorageIconBox))
	iconInset := (mobileStorageIconBox - mobileStorageIcon) / 2
	r.icon.Move(fyne.NewPos(mobileStoragePadX+iconInset, tileY+iconInset))
	r.icon.Resize(fyne.NewSize(mobileStorageIcon, mobileStorageIcon))

	r.title.Move(fyne.NewPos(textX, mobileStoragePadY))
	r.title.Resize(fyne.NewSize(textW, titleH))

	percentW := r.percent.MinSize().Width
	if percentW > textW {
		percentW = textW
	}
	sizeW := textW - percentW - 8
	if sizeW < 0 {
		sizeW = 0
	}
	lineY := mobileStoragePadY + titleH + mobileStorageTextGap
	r.sizeText.Move(fyne.NewPos(textX, lineY))
	r.sizeText.Resize(fyne.NewSize(sizeW, sizeH))
	r.percent.Move(fyne.NewPos(textX+textW-percentW, lineY))
	r.percent.Resize(fyne.NewSize(percentW, sizeH))

	barY := mobileStoragePadY + rowH + 8
	barW := size.Width - mobileStoragePadX*2
	if barW < 0 {
		barW = 0
	}
	r.track.Move(fyne.NewPos(mobileStoragePadX, barY))
	r.track.Resize(fyne.NewSize(barW, mobileStorageBarH))
	fillW := barW * float32(r.s.usedPercent/100)
	if fillW < 2 {
		fillW = 0
	}
	r.fill.Move(fyne.NewPos(mobileStoragePadX, barY))
	r.fill.Resize(fyne.NewSize(fillW, mobileStorageBarH))
}

func (r *mobileSnapshotStorageRenderer) MinSize() fyne.Size {
	titleH := r.title.MinSize().Height
	sizeH := r.sizeText.MinSize().Height
	rowH := mobileStorageIconBox
	if textBlock := titleH + mobileStorageTextGap + sizeH; textBlock > rowH {
		rowH = textBlock
	}
	return fyne.NewSize(1, mobileStoragePadY+rowH+8+mobileStorageBarH+mobileStoragePadY)
}

func (r *mobileSnapshotStorageRenderer) Refresh() {
	r.bg.FillColor = design.ColorGray900
	if r.s.hovered && r.s.onTapped != nil {
		r.bg.FillColor = color.NRGBA{R: 0x24, G: 0x28, B: 0x22, A: 0xff}
	}
	r.bg.StrokeColor = design.ColorTailscaleChipBorder

	label := "Storage"
	if i18n.Current != nil && i18n.Current.DevicesSectionStorage != "" {
		label = i18n.Current.DevicesSectionStorage
	}
	r.title.Text = label
	r.title.Color = design.ColorConnectionsSectionSubtitle

	r.sizeText.Text = r.s.sizeText
	r.sizeText.Color = design.ColorTextLight

	r.percent.Text = fmt.Sprintf("%.0f%%", r.s.usedPercent)
	r.percent.Color = design.ColorConnectionBadgeText

	if r.s.iconRes == nil {
		r.s.iconRes = assets.MemoryChipIcon
	}
	r.icon.Resource = r.s.iconRes
	r.track.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x1c}
	r.fill.FillColor = storageBarFillColor
	if sz := r.s.Size(); sz.Width > 0 && sz.Height > 0 {
		r.Layout(sz)
	}

	canvas.Refresh(r.bg)
	canvas.Refresh(r.tile)
	canvas.Refresh(r.icon)
	canvas.Refresh(r.title)
	canvas.Refresh(r.sizeText)
	canvas.Refresh(r.percent)
	canvas.Refresh(r.track)
	canvas.Refresh(r.fill)
}

func (r *mobileSnapshotStorageRenderer) Objects() []fyne.CanvasObject { return r.objs }

func (r *mobileSnapshotStorageRenderer) Destroy() {}

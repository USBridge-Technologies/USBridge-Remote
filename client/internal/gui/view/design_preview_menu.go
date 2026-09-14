package view

import (
	"image/color"
	"math"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

const (
	sizeMenuThumbR     = float32(3)
	sizeMenuTrackH     = float32(2)
	sizeMenuSliderH    = float32(12)
	sizeMenuSliderMinW = float32(64)
)

type sizeMenuSlider struct {
	widget.BaseWidget

	Min, Max, Step float64
	Value          float64
	OnChanged      func(float64)
	OnChangeEnded  func(float64)
	enabled        bool

	track *canvas.Rectangle
	thumb *canvas.Circle
}

var (
	_ fyne.Tappable     = (*sizeMenuSlider)(nil)
	_ fyne.Draggable    = (*sizeMenuSlider)(nil)
	_ desktop.Cursorable = (*sizeMenuSlider)(nil)
)

func newSizeMenuSlider(min, max, step, value float64) *sizeMenuSlider {
	s := &sizeMenuSlider{Min: min, Max: max, Step: step, Value: value, enabled: true}
	s.ExtendBaseWidget(s)
	return s
}

func (s *sizeMenuSlider) SetEnabled(on bool) {
	s.enabled = on
	s.Refresh()
}

func (s *sizeMenuSlider) SetValue(value float64) {
	if value < s.Min {
		value = s.Min
	}
	if value > s.Max {
		value = s.Max
	}
	if s.Step > 0 {
		value = math.Round(value/s.Step) * s.Step
	}
	if value == s.Value {
		return
	}
	s.Value = value
	s.Refresh()
	if s.OnChanged != nil {
		s.OnChanged(s.Value)
	}
}

func (s *sizeMenuSlider) fraction() float32 {
	if s.Max <= s.Min {
		return 0
	}
	f := (s.Value - s.Min) / (s.Max - s.Min)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return float32(f)
}

func (s *sizeMenuSlider) setFromX(x float32) {
	if !s.enabled {
		return
	}
	usable := s.Size().Width - sizeMenuThumbR*2
	if usable <= 0 {
		return
	}
	rel := (x - sizeMenuThumbR) / usable
	if rel < 0 {
		rel = 0
	}
	if rel > 1 {
		rel = 1
	}
	s.SetValue(s.Min + float64(rel)*(s.Max-s.Min))
}

func (s *sizeMenuSlider) Tapped(e *fyne.PointEvent) { s.setFromX(e.Position.X) }
func (s *sizeMenuSlider) TappedSecondary(*fyne.PointEvent) {}
func (s *sizeMenuSlider) Dragged(e *fyne.DragEvent)        { s.setFromX(e.Position.X) }
func (s *sizeMenuSlider) DragEnd() {
	if s.enabled && s.OnChangeEnded != nil {
		s.OnChangeEnded(s.Value)
	}
}
func (s *sizeMenuSlider) Cursor() desktop.Cursor { return desktop.PointerCursor }
func (s *sizeMenuSlider) MinSize() fyne.Size     { return fyne.NewSize(sizeMenuSliderMinW, sizeMenuSliderH) }

func (s *sizeMenuSlider) CreateRenderer() fyne.WidgetRenderer {
	s.track = canvas.NewRectangle(design.ColorBorder)
	s.track.CornerRadius = sizeMenuTrackH / 2
	s.thumb = canvas.NewCircle(design.ColorConnectionBadgeText)
	return &sizeMenuSliderRenderer{slider: s}
}

type sizeMenuSliderRenderer struct {
	slider *sizeMenuSlider
}

func (r *sizeMenuSliderRenderer) Layout(size fyne.Size) {
	s := r.slider
	trackY := (size.Height - sizeMenuTrackH) / 2
	s.track.Move(fyne.NewPos(0, trackY))
	s.track.Resize(fyne.NewSize(size.Width, sizeMenuTrackH))
	span := size.Width - sizeMenuThumbR*2
	if span < 0 {
		span = 0
	}
	cx := sizeMenuThumbR + s.fraction()*span
	cy := size.Height / 2
	s.thumb.Move(fyne.NewPos(cx-sizeMenuThumbR, cy-sizeMenuThumbR))
	s.thumb.Resize(fyne.NewSize(sizeMenuThumbR*2, sizeMenuThumbR*2))
}

func (r *sizeMenuSliderRenderer) MinSize() fyne.Size { return r.slider.MinSize() }

func (r *sizeMenuSliderRenderer) Refresh() {
	s := r.slider
	if s.enabled {
		s.track.FillColor = design.ColorBorder
		s.thumb.FillColor = design.ColorConnectionBadgeText
	} else {
		s.track.FillColor = color.NRGBA{R: 0x3a, G: 0x3c, B: 0x38, A: 0xff}
		s.thumb.FillColor = design.ColorTextMuted
	}
	s.track.Refresh()
	s.thumb.Refresh()
	r.Layout(s.Size())
	canvas.Refresh(s)
}

func (r *sizeMenuSliderRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *sizeMenuSliderRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.slider.track, r.slider.thumb}
}
func (r *sizeMenuSliderRenderer) Destroy() {}

func sizeMenuString(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

// ShowDesignPreviewMenu is the Size footer panel: Desktop, Compact, and a
// compact scale slider nested under Compact.
func ShowDesignPreviewMenu(anchor fyne.CanvasObject, onDesktop, onCompact func(), onScale func(float32)) {
	if anchor == nil {
		return
	}

	var popup *dropdownPopup
	hideThen := func(fn func()) {
		if popup != nil {
			popup.Hide()
		}
		if fn != nil {
			fn()
		}
	}

	opts := tealStyledMenuOptions(true)
	row := func(label string, selected bool, onTap func()) *dropdownItem {
		item := newDropdownItem(label, "", selected, func() {
			hideThen(onTap)
		})
		if opts.TextColor != nil {
			item.textColor = opts.TextColor
		}
		if opts.TextSize > 0 {
			item.textSize = opts.TextSize
		}
		if opts.RowHeight > 0 {
			item.minHeight = opts.RowHeight
		}
		return item
	}

	desktopLbl := "Desktop"
	compactLbl := "Compact"
	scaleTitle := "Scale"
	if i18n.Current != nil {
		desktopLbl = sizeMenuString(i18n.Current.WindowSizeDesktop, desktopLbl)
		compactLbl = sizeMenuString(i18n.Current.WindowSizeCompact, compactLbl)
		scaleTitle = sizeMenuString(i18n.Current.WindowSizeScale, scaleTitle)
	}

	scaleOn := ForceMobileDesign
	titleColor := design.ColorTextMuted
	if scaleOn {
		titleColor = design.ColorConnectionBadgeText
	}

	title := canvas.NewText(scaleTitle, titleColor)
	title.TextSize = 8
	percent := canvas.NewText(FormatPhonePreviewScale(ForceMobileScale), design.ColorTextMuted)
	percent.TextSize = 8
	percent.Alignment = fyne.TextAlignTrailing

	slider := newSizeMenuSlider(float64(PhonePreviewScaleMin), float64(PhonePreviewScaleMax), 0.1, float64(ClampPhonePreviewScale(ForceMobileScale)))
	slider.SetEnabled(scaleOn)
	slider.OnChanged = func(v float64) {
		percent.Text = FormatPhonePreviewScale(float32(v))
		percent.Refresh()
	}
	slider.OnChangeEnded = func(v float64) {
		if !ForceMobileDesign {
			return
		}
		next := ClampPhonePreviewScale(float32(v))
		percent.Text = FormatPhonePreviewScale(next)
		percent.Refresh()
		if popup != nil {
			popup.Hide()
		}
		if onScale != nil {
			onScale(next)
		}
	}

	scaleRow := container.NewBorder(nil, nil, title, percent, slider)
	compactBlock := container.NewVBox(
		row(compactLbl, ForceMobileDesign, onCompact),
		NewInset(scaleRow, 1, 6, 2, 0),
	)

	content := container.NewVBox(
		row(desktopLbl, !ForceMobileDesign, onDesktop),
		compactBlock,
	)
	popup = showStyledPanelAbove(anchor, content, 168)
}

package view

import (
	"image/color"
	"sync"
	"time"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

type HeaderActionButtonSpec struct {
	Disabled      bool
	Fill          color.Color
	Foreground    color.Color
	Stroke        color.Color
	StrokeWidth   float32
	Icon          fyne.Resource
	IconSize      fyne.Size
	SpinnerFrames []fyne.Resource
	Text          string

	// HoverFill/HoverStroke/HoverForeground override Fill/Stroke/Foreground
	// while the button is hovered -- nil keeps that same resting color, so
	// a caller that never sets these (every one besides the Exit button)
	// gets the exact old no-hover-change look. HoverIcon likewise swaps
	// Icon's resource while hovered (an SVG's color is baked in at asset
	// build time, so Foreground alone can't retint it).
	HoverFill       color.Color
	HoverStroke     color.Color
	HoverForeground color.Color
	HoverIcon       fyne.Resource
}

type HeaderActionButton struct {
	widget.BaseWidget

	onTapped func()
	spec     HeaderActionButtonSpec
	hovered  bool

	bg          *canvas.Rectangle
	border      *canvas.Rectangle
	icon        *canvas.Image
	label       *canvas.Text
	spinnerMu   sync.Mutex
	spinnerStop chan struct{}
	spinnerStep int
}

func NewHeaderActionButton(onTapped func()) *HeaderActionButton {
	btn := &HeaderActionButton{
		onTapped: onTapped,
		spec: HeaderActionButtonSpec{
			Fill:       design.ColorAccent,
			Foreground: design.ColorBackground,
			Stroke:     color.Transparent,
		},
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *HeaderActionButton) ApplySpec(spec HeaderActionButtonSpec) {
	b.spec = spec
	b.syncVisuals()
}

func (b *HeaderActionButton) Enable() {
	b.spec.Disabled = false
	b.syncVisuals()
}

func (b *HeaderActionButton) Disable() {
	b.spec.Disabled = true
	b.syncVisuals()
}

func (b *HeaderActionButton) Disabled() bool {
	return b.spec.Disabled
}

// SetText updates just the label (e.g. the Exit button's own LAN/Tailscale
// text -- see main_window_layout.go's updateStatusBarUI, called whenever
// mw.connectedProtocol changes) without touching Fill/Stroke/Icon/etc., so
// a caller doesn't have to reconstruct and reapply the whole spec just to
// change this one field.
func (b *HeaderActionButton) SetText(text string) {
	b.spec.Text = text
	b.syncVisuals()
	b.Refresh()
}

func (b *HeaderActionButton) Tapped(*fyne.PointEvent) {
	if b.spec.Disabled || b.onTapped == nil {
		return
	}
	b.onTapped()
}

func (b *HeaderActionButton) TappedSecondary(*fyne.PointEvent) {}

func (b *HeaderActionButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.syncVisuals()
}

func (b *HeaderActionButton) MouseMoved(*desktop.MouseEvent) {}

func (b *HeaderActionButton) MouseOut() {
	b.hovered = false
	b.syncVisuals()
}

// headerActionButtonTextSize/Gap/PadX/PadY size this button's content when
// it's showing icon+text together (e.g. the Exit button's own LAN/Tailscale
// label -- see main_window_layout.go's createMainAddressBar) -- small
// enough that the whole button stays compact rather than the old fixed
// 36x36 square every caller got regardless of content.
const (
	headerActionButtonTextSize = float32(9)
	headerActionButtonGap      = float32(6)
	headerActionButtonPadX     = float32(10)
	// headerActionButtonPadY was 2 -- 0 shaves ~4px off the button's total
	// height (this term counts twice, top and bottom).
	headerActionButtonPadY = float32(0)
)

// MinSize sizes the button to its actual content (icon and/or text, side by
// side, plus padding) instead of a fixed square -- a button with both grows
// as wide as its label needs (see spec.Text), not just its icon's own
// width, and a shorter/taller icon changes the button's height too instead
// of always reporting the same fixed value regardless of spec.IconSize.
func (b *HeaderActionButton) MinSize() fyne.Size {
	iconSize := b.spec.IconSize
	if iconSize.Width <= 0 || iconSize.Height <= 0 {
		iconSize = fyne.NewSize(22, 22)
	}
	hasIcon := b.spec.Icon != nil || len(b.spec.SpinnerFrames) > 0
	hasText := b.spec.Text != ""

	var width, height float32
	if hasIcon {
		width += iconSize.Width
		height = iconSize.Height
	}
	if hasText {
		textSize := fyne.MeasureText(b.spec.Text, headerActionButtonTextSize, fyne.TextStyle{Bold: true})
		if hasIcon {
			width += headerActionButtonGap
		}
		width += textSize.Width
		if textSize.Height > height {
			height = textSize.Height
		}
	}
	if !hasIcon && !hasText {
		width, height = 22, 22
	}
	return fyne.NewSize(width+headerActionButtonPadX*2, height+headerActionButtonPadY*2)
}

func (b *HeaderActionButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.spec.Fill)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeColor = b.spec.Stroke
	b.border.StrokeWidth = b.spec.StrokeWidth

	b.icon = canvas.NewImageFromResource(nil)
	b.icon.FillMode = canvas.ImageFillContain
	iconSize := b.spec.IconSize
	if iconSize.Width <= 0 || iconSize.Height <= 0 {
		iconSize = fyne.NewSize(22, 22)
	}
	b.icon.SetMinSize(iconSize)

	b.label = canvas.NewText("", b.spec.Foreground)
	b.label.TextSize = headerActionButtonTextSize
	b.label.TextStyle.Bold = true

	// DeviceRowControlsLayout (label then icon, left to right, skipping
	// whichever one is hidden) instead of the old Stack -- icon and label
	// used to be mutually exclusive (see syncVisuals' old forced
	// b.icon.Hide() whenever spec.Text was set) and so could just sit on
	// top of each other; now both can show at once. Label first, icon last
	// -- the Exit button's own "LAN"/"Tailscale" text reads before its
	// icon, not after.
	content := container.NewCenter(container.New(&DeviceRowControlsLayout{Gap: headerActionButtonGap}, b.label, b.icon))
	renderer := widget.NewSimpleRenderer(container.NewStack(b.bg, content, b.border))
	b.syncVisuals()
	return renderer
}

func (b *HeaderActionButton) syncVisuals() {
	if b.bg == nil || b.border == nil || b.icon == nil || b.label == nil {
		return
	}

	fill, stroke, foreground := b.spec.Fill, b.spec.Stroke, b.spec.Foreground
	if b.hovered {
		if b.spec.HoverFill != nil {
			fill = b.spec.HoverFill
		}
		if b.spec.HoverStroke != nil {
			stroke = b.spec.HoverStroke
		}
		if b.spec.HoverForeground != nil {
			foreground = b.spec.HoverForeground
		}
	}

	b.bg.FillColor = fill
	b.bg.Refresh()

	b.border.StrokeColor = stroke
	b.border.StrokeWidth = b.spec.StrokeWidth
	b.border.Refresh()

	b.label.Text = b.spec.Text
	b.label.Color = foreground
	if b.spec.Text != "" {
		b.label.Show()
	} else {
		b.label.Hide()
	}
	b.label.Refresh()

	if len(b.spec.SpinnerFrames) > 0 {
		b.icon.Resource = b.spec.SpinnerFrames[0]
		b.icon.Show()
		b.startSpinner()
	} else {
		b.stopSpinner()
		icon := b.spec.Icon
		if b.hovered && b.spec.HoverIcon != nil {
			icon = b.spec.HoverIcon
		}
		b.icon.Resource = icon
		if icon != nil {
			b.icon.Show()
		} else {
			b.icon.Hide()
		}
	}
	b.icon.Refresh()
	if b.spec.IconSize.Width > 0 && b.spec.IconSize.Height > 0 {
		b.icon.SetMinSize(b.spec.IconSize)
	} else {
		b.icon.SetMinSize(fyne.NewSize(22, 22))
	}
}

func (b *HeaderActionButton) startSpinner() {
	if len(b.spec.SpinnerFrames) == 0 {
		return
	}

	b.stopSpinner()

	stop := make(chan struct{})

	b.spinnerMu.Lock()
	b.spinnerStop = stop
	b.spinnerStep = 0
	b.spinnerMu.Unlock()
	b.icon.Resource = b.spec.SpinnerFrames[0]
	b.icon.Refresh()

	go func() {
		ticker := time.NewTicker(140 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fyne.Do(func() {
					b.spinnerMu.Lock()
					active := b.spinnerStop == stop
					if active {
						b.spinnerStep = (b.spinnerStep + 1) % len(b.spec.SpinnerFrames)
					}
					step := b.spinnerStep
					b.spinnerMu.Unlock()
					if !active || b.icon == nil {
						return
					}
					b.icon.Resource = b.spec.SpinnerFrames[step]
					b.icon.Refresh()
				})
			case <-stop:
				return
			}
		}
	}()
}

func (b *HeaderActionButton) stopSpinner() {
	b.spinnerMu.Lock()
	stop := b.spinnerStop
	b.spinnerStop = nil
	b.spinnerMu.Unlock()

	if stop != nil {
		close(stop)
	}
}

var (
	_ fyne.Tappable     = (*HeaderActionButton)(nil)
	_ desktop.Hoverable = (*HeaderActionButton)(nil)
)

package view

import (
	"image/color"
	"sync"
	"time"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type HeaderActionButtonSpec struct {
	Disabled      bool
	Fill          color.Color
	Foreground    color.Color
	Stroke        color.Color
	StrokeWidth   float32
	Icon          fyne.Resource
	SpinnerFrames []fyne.Resource
	Text          string
}

type HeaderActionButton struct {
	widget.BaseWidget

	onTapped func()
	spec     HeaderActionButtonSpec

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

func (b *HeaderActionButton) Tapped(*fyne.PointEvent) {
	if b.spec.Disabled || b.onTapped == nil {
		return
	}
	b.onTapped()
}

func (b *HeaderActionButton) TappedSecondary(*fyne.PointEvent) {}

func (b *HeaderActionButton) MinSize() fyne.Size {
	return fyne.NewSize(40, 40)
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
	b.icon.SetMinSize(fyne.NewSize(24, 24))

	b.label = canvas.NewText("", b.spec.Foreground)
	b.label.TextSize = 20
	b.label.TextStyle.Bold = true

	content := container.NewCenter(container.NewStack(b.icon, b.label))
	renderer := widget.NewSimpleRenderer(container.NewMax(b.bg, content, b.border))
	b.syncVisuals()
	return renderer
}

func (b *HeaderActionButton) syncVisuals() {
	if b.bg == nil || b.border == nil || b.icon == nil || b.label == nil {
		return
	}

	b.bg.FillColor = b.spec.Fill
	b.bg.Refresh()

	b.border.StrokeColor = b.spec.Stroke
	b.border.StrokeWidth = b.spec.StrokeWidth
	b.border.Refresh()

	b.label.Text = b.spec.Text
	b.label.Color = b.spec.Foreground
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
		b.icon.Resource = b.spec.Icon
		if b.spec.Icon != nil {
			b.icon.Show()
		} else {
			b.icon.Hide()
		}
	}
	b.icon.Refresh()

	if b.spec.Text != "" {
		b.icon.Hide()
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

var _ fyne.Tappable = (*HeaderActionButton)(nil)

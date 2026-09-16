package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// tailscaleHeaderToggle is the client's header Tailscale pill (label +
// switch), copied so Agent and Client share the same chip. Tap runs the
// same sign-in / sign-out path as the Tailscale card's login button.
type tailscaleHeaderToggle struct {
	widget.BaseWidget

	onTapped func()
	on       bool
	loading  bool
	disabled bool
	hovered  bool

	bg     *canvas.Rectangle
	border *canvas.Rectangle
	label  *canvas.Text
	track  *canvas.Rectangle
	thumb  *canvas.Circle
}

func newTailscaleHeaderToggle(onTapped func()) *tailscaleHeaderToggle {
	t := &tailscaleHeaderToggle{onTapped: onTapped}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tailscaleHeaderToggle) SetOn(on bool) {
	t.on = on
	t.refreshVisuals()
	t.Refresh()
}

func (t *tailscaleHeaderToggle) SetLoading(loading bool) {
	t.loading = loading
	if loading {
		t.hovered = false
	}
	t.refreshVisuals()
	t.Refresh()
}

func (t *tailscaleHeaderToggle) Tapped(*fyne.PointEvent) {
	if t.disabled || t.loading || t.onTapped == nil {
		return
	}
	t.onTapped()
}

func (t *tailscaleHeaderToggle) TappedSecondary(*fyne.PointEvent) {}

func (t *tailscaleHeaderToggle) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	if t.disabled || t.loading {
		return
	}
	t.hovered = true
	t.refreshVisuals()
}

func (t *tailscaleHeaderToggle) MouseMoved(*desktop.MouseEvent) {}

func (t *tailscaleHeaderToggle) MouseOut() {
	noteChromeHoverOut()
	if !t.hovered {
		return
	}
	t.hovered = false
	t.refreshVisuals()
}

func (t *tailscaleHeaderToggle) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (t *tailscaleHeaderToggle) MinSize() fyne.Size {
	return fyne.NewSize(92, 24)
}

func (t *tailscaleHeaderToggle) CreateRenderer() fyne.WidgetRenderer {
	t.bg = canvas.NewRectangle(design.ColorGray950)
	t.bg.CornerRadius = 12

	t.border = canvas.NewRectangle(color.Transparent)
	t.border.CornerRadius = 12
	t.border.StrokeColor = design.ColorTailscaleChipBorder
	t.border.StrokeWidth = 1

	t.label = canvas.NewText("Tailscale", design.ColorTailscaleChipLabel)
	t.label.TextSize = 10
	t.label.TextStyle = fyne.TextStyle{Bold: true}
	t.label.Alignment = fyne.TextAlignLeading

	t.track = canvas.NewRectangle(design.ColorGray900)
	t.track.CornerRadius = 7

	t.thumb = canvas.NewCircle(design.ColorGray400)

	t.refreshVisuals()
	return &tailscaleHeaderToggleRenderer{toggle: t}
}

func (t *tailscaleHeaderToggle) refreshVisuals() {
	if t.bg == nil || t.border == nil || t.label == nil || t.track == nil || t.thumb == nil {
		return
	}

	bgColor := design.ColorGray950
	borderColor := design.ColorTailscaleChipBorder
	labelColor := design.ColorTailscaleChipLabel
	trackColor := design.ColorGray900
	thumbColor := design.ColorGray400

	if t.on {
		trackColor = design.ColorAccent
		thumbColor = design.ColorWhite
	}
	if t.disabled || t.loading {
		labelColor = design.ColorGray400
		trackColor = design.ColorGray950
		borderColor = design.ColorGray900
		thumbColor = design.ColorGray900
	}

	t.bg.FillColor = bgColor
	t.border.StrokeColor = borderColor
	t.border.StrokeWidth = 1
	t.label.Color = labelColor
	t.track.FillColor = trackColor
	t.thumb.FillColor = thumbColor

	t.bg.Refresh()
	t.border.Refresh()
	t.label.Refresh()
	t.track.Refresh()
	t.thumb.Refresh()
}

type tailscaleHeaderToggleRenderer struct {
	toggle *tailscaleHeaderToggle
}

func (r *tailscaleHeaderToggleRenderer) Layout(size fyne.Size) {
	if r.toggle.bg == nil || r.toggle.border == nil || r.toggle.label == nil || r.toggle.track == nil || r.toggle.thumb == nil {
		return
	}

	r.toggle.bg.Move(fyne.NewPos(0, 0))
	r.toggle.bg.Resize(size)
	r.toggle.border.Move(fyne.NewPos(0, 0))
	r.toggle.border.Resize(size)

	labelH := float32(14)
	r.toggle.label.Move(fyne.NewPos(10, (size.Height-labelH)/2))
	r.toggle.label.Resize(fyne.NewSize(55, labelH))

	trackSize := fyne.NewSize(24, 14)
	trackX := size.Width - trackSize.Width - 6
	trackY := (size.Height - trackSize.Height) / 2
	r.toggle.track.Move(fyne.NewPos(trackX, trackY))
	r.toggle.track.Resize(trackSize)

	thumbSize := float32(10)
	thumbPad := float32(2)
	thumbY := trackY + thumbPad
	thumbX := trackX + thumbPad
	if r.toggle.on {
		thumbX = trackX + trackSize.Width - thumbSize - thumbPad
	}
	r.toggle.thumb.Move(fyne.NewPos(thumbX, thumbY))
	r.toggle.thumb.Resize(fyne.NewSize(thumbSize, thumbSize))
}

func (r *tailscaleHeaderToggleRenderer) MinSize() fyne.Size { return r.toggle.MinSize() }

func (r *tailscaleHeaderToggleRenderer) Refresh() {
	r.toggle.refreshVisuals()
	r.Layout(r.toggle.Size())
}

func (r *tailscaleHeaderToggleRenderer) Destroy() {}

func (r *tailscaleHeaderToggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.toggle.bg, r.toggle.label, r.toggle.track, r.toggle.thumb, r.toggle.border}
}

func (r *tailscaleHeaderToggleRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

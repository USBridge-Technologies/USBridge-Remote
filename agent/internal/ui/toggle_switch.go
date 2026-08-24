package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// toggleSwitch is a compact iOS-style on/off switch. Used where a two-state
// choice reads more clearly as a physical switch than as a button whose
// label flips between "Switch to X"/"Switch to Y" (see the RustShine/
// Sunshine control in showPatreonDialog) -- Fyne has no built-in switch
// widget, only widget.Check (a checkbox) and plain buttons.
type toggleSwitch struct {
	widget.BaseWidget

	On        bool
	OnChanged func(bool)
}

const (
	toggleSwitchWidth  float32 = 44
	toggleSwitchHeight float32 = 24
	toggleThumbPad     float32 = 3
)

func newToggleSwitch(on bool, onChanged func(bool)) *toggleSwitch {
	t := &toggleSwitch{On: on, OnChanged: onChanged}
	t.ExtendBaseWidget(t)
	return t
}

// SetOn updates state and re-renders without firing OnChanged -- for
// programmatic sync (e.g. a poll tick catching a change made elsewhere),
// as opposed to Tapped, which is the user flipping it themselves.
func (t *toggleSwitch) SetOn(on bool) {
	if t.On == on {
		return
	}
	t.On = on
	t.Refresh()
}

// Tapped implements fyne.Tappable -- flips state and notifies OnChanged.
func (t *toggleSwitch) Tapped(_ *fyne.PointEvent) {
	t.On = !t.On
	t.Refresh()
	if t.OnChanged != nil {
		t.OnChanged(t.On)
	}
}

func (t *toggleSwitch) MinSize() fyne.Size {
	return fyne.NewSize(toggleSwitchWidth, toggleSwitchHeight)
}

func (t *toggleSwitch) CreateRenderer() fyne.WidgetRenderer {
	track := canvas.NewRectangle(design.ColorSurfaceLight)
	track.CornerRadius = toggleSwitchHeight / 2
	thumb := canvas.NewCircle(design.ColorTextLight)
	r := &toggleSwitchRenderer{sw: t, track: track, thumb: thumb}
	r.Refresh()
	return r
}

type toggleSwitchRenderer struct {
	sw    *toggleSwitch
	track *canvas.Rectangle
	thumb *canvas.Circle
}

func (r *toggleSwitchRenderer) Layout(size fyne.Size) {
	r.track.Resize(size)
	r.track.Move(fyne.NewPos(0, 0))

	d := size.Height - toggleThumbPad*2
	x := toggleThumbPad
	if r.sw.On {
		x = size.Width - d - toggleThumbPad
	}
	r.thumb.Resize(fyne.NewSize(d, d))
	r.thumb.Move(fyne.NewPos(x, toggleThumbPad))
}

func (r *toggleSwitchRenderer) MinSize() fyne.Size {
	return fyne.NewSize(toggleSwitchWidth, toggleSwitchHeight)
}

func (r *toggleSwitchRenderer) Refresh() {
	if r.sw.On {
		r.track.FillColor = design.ColorAccent
	} else {
		r.track.FillColor = design.ColorSurfaceLight
	}
	r.Layout(r.sw.Size())
	r.track.Refresh()
	r.thumb.Refresh()
}

func (r *toggleSwitchRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.track, r.thumb}
}

func (r *toggleSwitchRenderer) Destroy() {}

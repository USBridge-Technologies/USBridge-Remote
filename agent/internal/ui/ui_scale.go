package ui

import (
	"os"
	"strconv"

	"fyne.io/fyne/v2"
)

const (
	uiScalePrefKey = "ui_scale"
	fyneScaleEnv   = "FYNE_SCALE"
)

// uiScaleSteps are the General Settings choices. 1 is 100%, the same size
// Fyne already picks from the monitor. The others multiply that, so an
// ultrawide at 100% Windows scaling can grow the whole interface.
var uiScaleSteps = []float32{1, 1.25, 1.5, 1.75}

func uiScaleLabel(v float32) string {
	return strconv.Itoa(int(v*100+0.5)) + "%"
}

func uiScaleFromLabel(label string) (float32, bool) {
	for _, v := range uiScaleSteps {
		if uiScaleLabel(v) == label {
			return v, true
		}
	}
	return 1, false
}

func savedUIScale(a fyne.App) float32 {
	if a == nil {
		return 1
	}
	raw := a.Preferences().String(uiScalePrefKey)
	if raw == "" {
		return 1
	}
	v, err := strconv.ParseFloat(raw, 32)
	if err != nil || v <= 0 {
		return 1
	}
	for _, step := range uiScaleSteps {
		if abs32(float32(v)-step) < 0.01 {
			return step
		}
	}
	return 1
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// ApplySavedUIScale writes the saved choice into FYNE_SCALE before the
// window is built. An empty preference leaves an externally set FYNE_SCALE
// alone. Fyne multiplies this by the monitor scale.
func ApplySavedUIScale(a fyne.App) {
	if a == nil || a.Preferences().String(uiScalePrefKey) == "" {
		return
	}
	setFyneScaleEnv(savedUIScale(a))
}

func setFyneScaleEnv(v float32) {
	_ = os.Setenv(fyneScaleEnv, strconv.FormatFloat(float64(v), 'f', 2, 32))
}

// saveUIScale persists the choice and asks Fyne to re-read FYNE_SCALE.
// Settings.SetTheme is the public hook that runs the driver's reloadScale.
func (w *Window) saveUIScale(v float32) {
	if w == nil || w.app == nil {
		return
	}
	w.app.Preferences().SetString(uiScalePrefKey, strconv.FormatFloat(float64(v), 'f', 2, 32))
	setFyneScaleEnv(v)
	if s := w.app.Settings(); s != nil {
		if t := s.Theme(); t != nil {
			s.SetTheme(t)
		}
	}
}

func (w *Window) uiScaleOptions() (labels []string, selected string) {
	current := savedUIScale(w.app)
	selected = uiScaleLabel(1)
	for _, v := range uiScaleSteps {
		labels = append(labels, uiScaleLabel(v))
		if abs32(v-current) < 0.01 {
			selected = uiScaleLabel(v)
		}
	}
	return labels, selected
}

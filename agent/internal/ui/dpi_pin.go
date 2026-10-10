package ui

import (
	"os"
	"strconv"
	"sync"

	"fyne.io/fyne/v2"
)

const disableDPIDetectionEnv = "FYNE_DISABLE_DPI_DETECTION"

var (
	dpiPinMu   sync.Mutex
	dpiPinBase float32 // 0 = not pinned
	// externalScale is FYNE_SCALE as the process started with it, used as
	// the user scale while no UI scale preference is saved.
	externalScale float32 = 1
)

// pinDetectedDPIScale freezes Fyne's monitor DPI factor at base.
//
// Fyne's X11 driver (the agent always runs through XWayland) re-detects the
// scale on every window move, picking the monitor whose rect -- shrunk by that
// monitor's own scale -- contains the window centre, and falling back to the
// primary monitor otherwise. With HiDPI or mirrored outputs (KDE + XWayland
// reports every output as 3840x2160+0+0) the scale flips while the window
// moves inside one monitor, so the UI jumps in size until the next resize.
//
// Detection is disabled and base is folded into FYNE_SCALE instead, so the
// saved UI scale keeps working on top of it. Skipped when the user already
// set FYNE_DISABLE_DPI_DETECTION themselves.
func pinDetectedDPIScale(a fyne.App, base float32) bool {
	dpiPinMu.Lock()
	defer dpiPinMu.Unlock()
	if dpiPinBase > 0 || base <= 0 {
		return false
	}
	if _, set := os.LookupEnv(disableDPIDetectionEnv); set {
		return false
	}
	if raw, ok := os.LookupEnv(fyneScaleEnv); ok && raw != "" && raw != "auto" {
		if v, err := strconv.ParseFloat(raw, 32); err == nil && v > 0 {
			externalScale = float32(v)
		}
	}
	dpiPinBase = base
	_ = os.Setenv(disableDPIDetectionEnv, "1")
	user := externalScale
	if a != nil && a.Preferences().String(uiScalePrefKey) != "" {
		user = savedUIScale(a)
	}
	setFyneScaleEnvLocked(user)
	return true
}

// pinnedScaleValue is what FYNE_SCALE must hold for the user scale v: the
// pinned DPI factor is multiplied in once detection is off.
func pinnedScaleValue(v float32) float32 {
	dpiPinMu.Lock()
	defer dpiPinMu.Unlock()
	if dpiPinBase > 0 {
		return v * dpiPinBase
	}
	return v
}

func setFyneScaleEnvLocked(user float32) {
	if dpiPinBase > 0 {
		user *= dpiPinBase
	}
	_ = os.Setenv(fyneScaleEnv, strconv.FormatFloat(float64(user), 'f', 3, 32))
}

// reloadFyneCanvasScale asks the driver to re-read FYNE_SCALE.
// Settings.SetTheme is the public hook that runs the driver's reloadScale.
func reloadFyneCanvasScale(a fyne.App) {
	if a == nil {
		return
	}
	if s := a.Settings(); s != nil {
		if t := s.Theme(); t != nil {
			s.SetTheme(t)
		}
	}
}

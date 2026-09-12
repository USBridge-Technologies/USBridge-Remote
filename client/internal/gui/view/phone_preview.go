package view

import (
	"fmt"
	"math"
	"os"
	"sync"

	"fyne.io/fyne/v2"
)

// ForceMobilePresetPrefKey persists which phone frame the desktop preview
// is using (see PhonePreviewPresets).
const ForceMobilePresetPrefKey = "force_mobile_preset"

// ForceMobileScalePrefKey persists the desktop phone-preview canvas scale.
const ForceMobileScalePrefKey = "force_mobile_scale"

// DefaultPhonePreviewID is the first-run phone frame (iPhone 15 logical dp).
const DefaultPhonePreviewID = "iphone-15"

// Phone preview on a typical monitor paints ~1.5× a real handset. 0.7
// brings the physical window closer to the phone you hold next to it.
const DefaultPhonePreviewScale float32 = 0.7

const (
	PhonePreviewScaleMin float32 = 0.5
	PhonePreviewScaleMax float32 = 1.5
)

const phonePreviewScaleEnv = "FYNE_SCALE"

// ForceMobilePresetID is the active phone frame while ForceMobileDesign is
// on. Loaded from ForceMobilePresetPrefKey at startup.
var ForceMobilePresetID = DefaultPhonePreviewID

// ForceMobileScale is the Fyne user scale used only while the desktop
// phone preview is on. Logical layout stays 390×844; the window paints
// smaller. Loaded from ForceMobileScalePrefKey at startup.
var ForceMobileScale = DefaultPhonePreviewScale

// PhonePreviewPreset is a popular phone viewport in Fyne logical units
// (CSS/iOS points), portrait.
type PhonePreviewPreset struct {
	ID     string
	Name   string
	Width  float32
	Height float32
}

// PhonePreviewPresets are the frames offered in the Connections footer
// Desktop/Mobile menu. Sizes are logical dp, not physical pixels.
var PhonePreviewPresets = []PhonePreviewPreset{
	{ID: "iphone-se", Name: "iPhone SE", Width: 375, Height: 667},
	{ID: "iphone-15", Name: "iPhone 15", Width: 390, Height: 844},
	{ID: "iphone-15-pro-max", Name: "iPhone 15 Pro Max", Width: 430, Height: 932},
	{ID: "pixel-8", Name: "Pixel 8", Width: 412, Height: 915},
	{ID: "pixel-8-pro", Name: "Pixel 8 Pro", Width: 448, Height: 998},
	{ID: "galaxy-s24", Name: "Galaxy S24", Width: 360, Height: 780},
	{ID: "galaxy-s24-ultra", Name: "Galaxy S24 Ultra", Width: 384, Height: 824},
}

// PhonePreviewByID returns the preset for id, or the default iPhone 15
// frame if id is empty/unknown.
func PhonePreviewByID(id string) PhonePreviewPreset {
	for _, p := range PhonePreviewPresets {
		if p.ID == id {
			return p
		}
	}
	return PhonePreviewByID(DefaultPhonePreviewID)
}

// CurrentPhonePreview is the frame ForceMobileDesign should open at.
func CurrentPhonePreview() PhonePreviewPreset {
	return PhonePreviewByID(ForceMobilePresetID)
}

func (p PhonePreviewPreset) SizeLabel() string {
	return fmt.Sprintf("%.0f×%.0f", p.Width, p.Height)
}

// ClampPhonePreviewScale keeps the slider on Fyne's 0.1 scale steps.
func ClampPhonePreviewScale(v float32) float32 {
	if v < PhonePreviewScaleMin {
		v = PhonePreviewScaleMin
	}
	if v > PhonePreviewScaleMax {
		v = PhonePreviewScaleMax
	}
	return float32(math.Round(float64(v)*10)) / 10
}

// FormatPhonePreviewScale is the percent next to the preview scale slider.
func FormatPhonePreviewScale(v float32) string {
	return fmt.Sprintf("%.0f%%", ClampPhonePreviewScale(v)*100)
}

// ShouldApplyPreviewUserScale is true only for the desktop phone frame.
// A real handset already has the OS scale; do not override it.
func ShouldApplyPreviewUserScale() bool {
	return ForceMobileDesign && !fyne.CurrentDevice().IsMobile()
}

var previewScaleEnv struct {
	once    sync.Once
	value   string
	present bool
}

func capturePreviewScaleEnv() {
	previewScaleEnv.once.Do(func() {
		previewScaleEnv.value, previewScaleEnv.present = os.LookupEnv(phonePreviewScaleEnv)
	})
}

// ApplyPreviewUserScale sets FYNE_SCALE so Fyne paints the same logical
// phone layout at a smaller physical size. Call before the window is
// shown, or with ReloadFyneCanvasScale after it is visible.
func ApplyPreviewUserScale() {
	capturePreviewScaleEnv()
	if !ShouldApplyPreviewUserScale() {
		restorePreviewScaleEnv()
		return
	}
	_ = os.Setenv(phonePreviewScaleEnv, fmt.Sprintf("%.1f", ClampPhonePreviewScale(ForceMobileScale)))
}

// RestorePreviewUserScale puts FYNE_SCALE back to whatever the process
// had before the phone preview overrode it.
func RestorePreviewUserScale() {
	capturePreviewScaleEnv()
	restorePreviewScaleEnv()
}

func restorePreviewScaleEnv() {
	if previewScaleEnv.present {
		_ = os.Setenv(phonePreviewScaleEnv, previewScaleEnv.value)
		return
	}
	_ = os.Unsetenv(phonePreviewScaleEnv)
}

// ReloadFyneCanvasScale asks the glfw driver to re-read userScale()
// (FYNE_SCALE). Settings.SetTheme is the public hook that runs reloadScale.
func ReloadFyneCanvasScale() {
	a := fyne.CurrentApp()
	if a == nil {
		return
	}
	s := a.Settings()
	if s == nil {
		return
	}
	if t := s.Theme(); t != nil {
		s.SetTheme(t)
	}
}

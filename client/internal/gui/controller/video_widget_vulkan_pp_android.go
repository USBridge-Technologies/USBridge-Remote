//go:build android

package controller

import (
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
)

// VulkanPostprocessSettings mirrors view.VulkanPostprocessValues field for
// field so the two are convertible with a plain Go struct conversion — kept
// in sync deliberately, see internal/gui/view/vulkan_postprocess_dialog.go.
type VulkanPostprocessSettings struct {
	Enabled    bool
	Sharpen    float64
	Denoise    float64
	Temporal   float64
	Gamma      float64
	Contrast   float64
	Saturation float64
}

const (
	vkPPPrefEnabled    = "vulkan_postprocess.enabled"
	vkPPPrefSharpen    = "vulkan_postprocess.sharpen"
	vkPPPrefDenoise    = "vulkan_postprocess.denoise"
	vkPPPrefTemporal   = "vulkan_postprocess.temporal"
	vkPPPrefGamma      = "vulkan_postprocess.gamma"
	vkPPPrefContrast   = "vulkan_postprocess.contrast"
	vkPPPrefSaturation = "vulkan_postprocess.saturation"
)

// DefaultVulkanPostprocessSettings returns the pipeline's neutral tuning —
// post-processing itself defaults to off so existing sessions are completely
// unaffected until the user opts in from the "Vulkan" popup.
func DefaultVulkanPostprocessSettings() VulkanPostprocessSettings {
	return VulkanPostprocessSettings{
		Enabled:    false,
		Sharpen:    0.35,
		Denoise:    0.35,
		Temporal:   0.5,
		Gamma:      1.0,
		Contrast:   1.0,
		Saturation: 1.0,
	}
}

// LoadVulkanPostprocessSettings reads the persisted settings, falling back to
// DefaultVulkanPostprocessSettings for anything never saved before.
func LoadVulkanPostprocessSettings(app fyne.App) VulkanPostprocessSettings {
	d := DefaultVulkanPostprocessSettings()
	if app == nil {
		return d
	}
	p := app.Preferences()
	return VulkanPostprocessSettings{
		Enabled:    p.BoolWithFallback(vkPPPrefEnabled, d.Enabled),
		Sharpen:    p.FloatWithFallback(vkPPPrefSharpen, d.Sharpen),
		Denoise:    p.FloatWithFallback(vkPPPrefDenoise, d.Denoise),
		Temporal:   p.FloatWithFallback(vkPPPrefTemporal, d.Temporal),
		Gamma:      p.FloatWithFallback(vkPPPrefGamma, d.Gamma),
		Contrast:   p.FloatWithFallback(vkPPPrefContrast, d.Contrast),
		Saturation: p.FloatWithFallback(vkPPPrefSaturation, d.Saturation),
	}
}

func saveVulkanPostprocessSettings(app fyne.App, s VulkanPostprocessSettings) {
	if app == nil {
		return
	}
	p := app.Preferences()
	p.SetBool(vkPPPrefEnabled, s.Enabled)
	p.SetFloat(vkPPPrefSharpen, s.Sharpen)
	p.SetFloat(vkPPPrefDenoise, s.Denoise)
	p.SetFloat(vkPPPrefTemporal, s.Temporal)
	p.SetFloat(vkPPPrefGamma, s.Gamma)
	p.SetFloat(vkPPPrefContrast, s.Contrast)
	p.SetFloat(vkPPPrefSaturation, s.Saturation)
}

func pushVulkanPostprocessSettings(s VulkanPostprocessSettings) {
	service.VKVideoAndroidSetPostprocess(s.Enabled, s.Sharpen, s.Denoise, s.Temporal, s.Gamma, s.Contrast, s.Saturation)
}

// ApplyVulkanPostprocessSettings persists the settings and immediately pushes
// them into the native Vulkan renderer, so dragging a slider in the popup
// updates the live video with no extra "Apply" step.
func ApplyVulkanPostprocessSettings(app fyne.App, s VulkanPostprocessSettings) {
	saveVulkanPostprocessSettings(app, s)
	pushVulkanPostprocessSettings(s)
}

// reapplyVulkanPostprocessSettings re-pushes the persisted settings into the
// native renderer after every successful VKVideoAndroidCreate. The C side
// keeps its own copy of these parameters (so they survive an
// android_vk_destroy/create reconnect within the same process), but that
// copy starts at hardcoded defaults on a fresh process — without this call,
// a user's saved sharpen/denoise/etc. values from a previous app run would
// silently stop applying the next time the app is launched.
func reapplyVulkanPostprocessSettings() {
	pushVulkanPostprocessSettings(LoadVulkanPostprocessSettings(fyne.CurrentApp()))
}

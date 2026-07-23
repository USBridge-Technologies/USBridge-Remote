package view

import (
	"fmt"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// VulkanPostprocessValues holds the live-tunable parameters of the Vulkan
// compute post-processing pass (denoise → sharpen → temporal accumulation →
// gamma/contrast/saturation). Field-for-field identical to
// controller.VulkanPostprocessSettings so the two convert with a plain Go
// struct conversion at the call site.
type VulkanPostprocessValues struct {
	Enabled    bool
	Sharpen    float64
	Denoise    float64
	Temporal   float64
	Gamma      float64
	Contrast   float64
	Saturation float64
}

// ShowVulkanPostprocessDialog opens the "Vulkan" popup: a master toggle plus
// sliders for sharpen/denoise/temporal accumulation/gamma/contrast/
// saturation. Every change calls onChange immediately (live preview) — there
// is no separate Apply step. onReset, if non-nil, restores factory defaults.
func ShowVulkanPostprocessDialog(parent fyne.Window, current VulkanPostprocessValues, onChange func(VulkanPostprocessValues), onReset func() VulkanPostprocessValues) {
	values := current
	var rows []*percentRow // percent-style rows (0..100%), disabled while master toggle is off
	var multRows []*percentRow

	push := func() {
		if onChange != nil {
			onChange(values)
		}
	}

	setControlsEnabled := func(enabled bool) {
		for _, r := range rows {
			r.setEnabled(enabled)
		}
		for _, r := range multRows {
			r.setEnabled(enabled)
		}
	}

	enableCheck := widget.NewCheck(i18n.Current.VulkanEnable, func(checked bool) {
		values.Enabled = checked
		setControlsEnabled(checked)
		push()
	})
	enableCheck.SetChecked(values.Enabled)

	sharpenRow := newPercentRow(i18n.Current.VulkanSharpen, values.Sharpen, func(v float64) {
		values.Sharpen = v
		push()
	})
	denoiseRow := newPercentRow(i18n.Current.VulkanDenoise, values.Denoise, func(v float64) {
		values.Denoise = v
		push()
	})
	temporalRow := newPercentRow(i18n.Current.VulkanTemporal, values.Temporal, func(v float64) {
		values.Temporal = v
		push()
	})
	rows = []*percentRow{sharpenRow, denoiseRow, temporalRow}

	gammaRow := newMultiplierRow(i18n.Current.VulkanGamma, values.Gamma, 0.5, 2.0, func(v float64) {
		values.Gamma = v
		push()
	})
	contrastRow := newMultiplierRow(i18n.Current.VulkanContrast, values.Contrast, 0.5, 1.5, func(v float64) {
		values.Contrast = v
		push()
	})
	saturationRow := newMultiplierRow(i18n.Current.VulkanSaturation, values.Saturation, 0.0, 2.0, func(v float64) {
		values.Saturation = v
		push()
	})
	multRows = []*percentRow{gammaRow, contrastRow, saturationRow}

	setControlsEnabled(values.Enabled)

	resetBtn := widget.NewButton(i18n.Current.VulkanResetDefaults, func() {
		if onReset == nil {
			return
		}
		values = onReset()
		enableCheck.SetChecked(values.Enabled)
		sharpenRow.setValue(values.Sharpen)
		denoiseRow.setValue(values.Denoise)
		temporalRow.setValue(values.Temporal)
		gammaRow.setValue(values.Gamma)
		contrastRow.setValue(values.Contrast)
		saturationRow.setValue(values.Saturation)
		setControlsEnabled(values.Enabled)
		push()
	})

	content := container.NewVBox(
		widget.NewLabel(i18n.Current.VulkanPostprocessHint),
		widget.NewSeparator(),
		enableCheck,
		widget.NewSeparator(),
		sharpenRow.container,
		denoiseRow.container,
		temporalRow.container,
		widget.NewSeparator(),
		gammaRow.container,
		contrastRow.container,
		saturationRow.container,
		widget.NewSeparator(),
		resetBtn,
	)

	d := dialog.NewCustom(i18n.Current.VulkanPostprocessTitle, i18n.Current.Close, content, parent)
	// The Android Vulkan video overlay is a separate SurfaceView drawn above
	// Fyne's canvas, so it would otherwise cover this dialog. overlayShow/
	// overlayHide (see overlay_hooks.go) drive OnOverlayShow/OnOverlayHide,
	// which video_widget_android.go wires to hide/restore that overlay —
	// same mechanism every other popup (dropdownPopup, VideoStartDialog) uses.
	overlayShow()
	d.SetOnClosed(overlayHide)
	d.Show()
}

// percentRow is a labelled slider row shared by both the 0..100% controls
// (sharpen/denoise/temporal) and the multiplier controls (gamma/contrast/
// saturation) — only the slider's range and the value formatter differ.
type percentRow struct {
	container *fyne.Container
	slider    *widget.Slider
	valueLbl  *widget.Label
	format    func(float64) string
}

func newPercentRow(label string, value float64, onSet func(float64)) *percentRow {
	format := func(v float64) string { return fmt.Sprintf("%d%%", int(v*100+0.5)) }
	slider := widget.NewSlider(0, 1)
	slider.Step = 0.01
	slider.Value = value
	valueLbl := widget.NewLabel(format(value))
	slider.OnChanged = func(v float64) {
		valueLbl.SetText(format(v))
		onSet(v)
	}
	row := &percentRow{slider: slider, valueLbl: valueLbl, format: format}
	row.container = container.NewBorder(nil, nil, widget.NewLabel(label), valueLbl, slider)
	return row
}

func newMultiplierRow(label string, value, min, max float64, onSet func(float64)) *percentRow {
	format := func(v float64) string { return fmt.Sprintf("x%.2f", v) }
	slider := widget.NewSlider(min, max)
	slider.Step = 0.01
	slider.Value = value
	valueLbl := widget.NewLabel(format(value))
	slider.OnChanged = func(v float64) {
		valueLbl.SetText(format(v))
		onSet(v)
	}
	row := &percentRow{slider: slider, valueLbl: valueLbl, format: format}
	row.container = container.NewBorder(nil, nil, widget.NewLabel(label), valueLbl, slider)
	return row
}

// setValue updates the slider (and its label, via the slider's own
// OnChanged) without the caller needing to duplicate that logic. OnChanged
// firing again with the same value the caller already applied is harmless.
func (r *percentRow) setValue(v float64) {
	r.slider.SetValue(v)
}

func (r *percentRow) setEnabled(enabled bool) {
	if enabled {
		r.slider.Enable()
	} else {
		r.slider.Disable()
	}
}

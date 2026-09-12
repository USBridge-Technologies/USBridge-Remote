package view

import (
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// ShowDesignPreviewMenu is the Connections footer Desktop/phone panel:
// device frames plus a preview-scale slider so a monitor mock can match
// a real handset's physical size.
func ShowDesignPreviewMenu(anchor fyne.CanvasObject, onDesktop func(), onPhone func(PhonePreviewPreset), onScale func(float32)) {
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
	row := func(label, secondary string, selected bool, onTap func()) *dropdownItem {
		item := newDropdownItem(label, secondary, selected, func() {
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

	rows := []fyne.CanvasObject{
		row("Desktop", "", !ForceMobileDesign, onDesktop),
	}
	current := CurrentPhonePreview()
	for _, preset := range PhonePreviewPresets {
		p := preset
		rows = append(rows, row(p.Name, p.SizeLabel(), ForceMobileDesign && p.ID == current.ID, func() {
			if onPhone != nil {
				onPhone(p)
			}
		}))
	}

	rule := canvas.NewRectangle(design.ColorBorder)
	rule.SetMinSize(fyne.NewSize(1, 1))

	title := canvas.NewText("Preview scale", design.ColorConnectionBadgeText)
	title.TextSize = 10
	title.TextStyle.Bold = true
	percent := canvas.NewText(FormatPhonePreviewScale(ForceMobileScale), design.ColorTextMuted)
	percent.TextSize = 10
	percent.Alignment = fyne.TextAlignTrailing

	slider := widget.NewSlider(float64(PhonePreviewScaleMin), float64(PhonePreviewScaleMax))
	slider.Step = 0.1
	slider.Value = float64(ClampPhonePreviewScale(ForceMobileScale))
	slider.OnChanged = func(v float64) {
		percent.Text = FormatPhonePreviewScale(float32(v))
		percent.Refresh()
	}
	slider.OnChangeEnded = func(v float64) {
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

	hint := canvas.NewText("Match a phone held to the monitor", design.ColorTextMuted)
	hint.TextSize = 9

	scaleBlock := container.NewVBox(
		container.NewBorder(nil, nil, title, percent),
		slider,
		hint,
	)

	content := container.NewVBox(append(rows, rule, NewInset(scaleBlock, 4, 4, 6, 2))...)
	popup = showStyledPanelAbove(anchor, content, 220)
}

package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
)

// NewOptionToggleRow is the video dialog's checkbox row -- checkbox and bold
// title on the first line, a wrapped muted description under it, the whole
// row tappable -- for other dialogs. badge may be "" for none. Returns the
// row and a getter for the checkbox state.
func NewOptionToggleRow(title, badge, description string, descWidth float32, checked bool, onChanged func(bool)) (fyne.CanvasObject, func() bool) {
	check := newVideoDialogCheckbox(checked, onChanged)
	var badgeObj fyne.CanvasObject = canvas.NewRectangle(color.Transparent)
	if badge != "" {
		badgeObj = newVideoDialogBadge(badge, design.ColorConnectionBadgeText)
	}
	row := newVideoDialogToggleRow(check, newVideoDialogRowTitle(title), badgeObj, newVideoDialogDescription(description, descWidth))
	return row, func() bool { return check.Checked }
}

// NewDeviceDashboardTextToggle is a dashboard row button with a short text
// label that shows an on/off state: accent border and text while on, the
// muted delete-button chrome while off. Rebuilt on every dashboard refresh,
// so the state is fixed per instance.
func NewDeviceDashboardTextToggle(label string, on bool, onTap func(), onHover func(bool)) *iconChromeButton {
	var stroke, text color.Color = design.ColorTailscaleChipBorder, design.ColorTextMuted
	if on {
		stroke, text = design.ColorAccent, design.ColorAccent
	}
	b := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		DisabledFill: connectionActionBlockedFill,
		Stroke:       stroke,
		StrokeWidth:  1,
		CornerRadius: 6,
		LabelColor:   text,
		LabelBold:    true,
		LabelSize:    9,
		LabelPadX:    3,
		ButtonSize:   fyne.NewSize(0, 23),
		OnTapped:     onTap,
		OnHover:      onHover,
	})
	b.SetText(label)
	return b
}

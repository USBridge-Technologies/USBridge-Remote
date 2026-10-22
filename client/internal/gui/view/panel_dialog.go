package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// PanelDialog is a dialog in the video parameters dialog's design -- opaque
// panel, accent bar, title with a close button, scrolling body, footer pill
// buttons -- for content that used to go through fyne's stock dialogs
// (dialog.NewCustom...), whose panel is a translucent sheet in the theme's
// overlay color. It is a dialog.Dialog, so it drops in where those were.
type PanelDialog struct {
	parent   fyne.Window
	popup    *widget.PopUp
	dismiss  *videoDialogPillButton
	size     fyne.Size
	onClosed func()
	shown    bool
}

var _ dialog.Dialog = (*PanelDialog)(nil)

// NewPanelDialog builds (but doesn't show) a dialog with title and content;
// dismiss, when not empty, is a footer button that closes it.
func NewPanelDialog(title, dismiss string, content fyne.CanvasObject, parent fyne.Window) *PanelDialog {
	d := &PanelDialog{parent: parent}

	titleText := NewBrandText(title, 13, design.ColorTextLight, true)
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   d.Hide,
	})
	sepColor := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(sepColor)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	header := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(titleText, 21, 44, 9, 4), headerSep)

	body := NewInset(content, videoDialogBodyInsetLR, videoDialogBodyInsetLR, 12, 8)
	bodyScroll := container.NewVScroll(body)

	var footer fyne.CanvasObject
	if dismiss != "" {
		d.dismiss = newVideoDialogApplyButton(dismiss, d.Hide)
		footerSep := canvas.NewRectangle(sepColor)
		footerSep.SetMinSize(fyne.NewSize(0, 1))
		footer = container.NewVBox(footerSep, NewInsetExact(container.NewBorder(nil, nil, nil, d.dismiss), 12, 18, 6, 0))
	}
	form := container.NewBorder(header, footer, nil, nil, bodyScroll)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 8), cornerBtn, border)

	d.popup = NewOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := maxFloat32(canvasSize.Width-margin*2, 1)
			maxHeight := maxFloat32(canvasSize.Height-margin*2, 1)
			// As tall as the whole body when it fits, not the scroll's own
			// small minimum; at least the size asked for with Resize.
			min := panel.MinSize()
			min.Height += body.MinSize().Height - bodyScroll.MinSize().Height
			width := maxFloat32(maxFloat32(min.Width, videoDialogCanvasPanelWidth(canvasSize)), d.size.Width)
			height := maxFloat32(min.Height, d.size.Height)
			return fyne.NewSize(minFloat32(width, maxWidth), minFloat32(height, maxHeight))
		},
		OnOutsideTap: d.Hide,
	})
	return d
}

// ShowPanelDialog shows a PanelDialog with a dismiss button.
func ShowPanelDialog(title, dismiss string, content fyne.CanvasObject, parent fyne.Window) *PanelDialog {
	d := NewPanelDialog(title, dismiss, content, parent)
	d.Show()
	return d
}

func (d *PanelDialog) Show() {
	if d.popup == nil {
		return
	}
	d.shown = true
	if d.parent != nil {
		d.popup.Move(fyne.NewPos(0, 0))
		d.popup.Resize(d.parent.Canvas().Size())
	}
	d.popup.Show()
}

func (d *PanelDialog) Hide() {
	if d.popup == nil || !d.shown {
		return
	}
	d.shown = false
	d.popup.Hide()
	if d.onClosed != nil {
		d.onClosed()
	}
}

func (d *PanelDialog) Dismiss() { d.Hide() }

func (d *PanelDialog) SetDismissText(label string) {
	if d.dismiss != nil {
		d.dismiss.SetText(label)
	}
}

func (d *PanelDialog) SetOnClosed(closed func()) { d.onClosed = closed }

func (d *PanelDialog) Refresh() {
	if d.popup != nil {
		d.popup.Refresh()
	}
}

// Resize asks for at least this panel size (clamped to the screen).
func (d *PanelDialog) Resize(size fyne.Size) {
	d.size = size
	d.Refresh()
}

func (d *PanelDialog) MinSize() fyne.Size {
	if d.popup == nil {
		return fyne.Size{}
	}
	return d.popup.MinSize()
}

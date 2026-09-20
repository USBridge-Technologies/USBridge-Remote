package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const chromeNoticeDialogWidth = float32(400)

// ShowChromeActionDialog is the shared Account / Video Parameters chrome
// for a short notice plus one compact lime action: accent hairline, title,
// close X, body copy, footer pill. No copy button.
func ShowChromeActionDialog(parent fyne.Window, title, message, actionLabel string, onAction func()) *widget.PopUp {
	if parent == nil {
		return nil
	}

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := NewBrandText(title, 13, design.ColorTextLight, true)
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   closePopup,
	})

	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(headerSepLine)
	footerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(titleText, 21, 44, 9, 4), headerSep)

	msg := newVideoDialogWrapText(
		chromeNoticeDialogWidth-52,
		12,
		false,
		videoDialogWrapSpan{Text: message, Color: color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}},
	)

	actionBtn := newVideoDialogLimeButton(actionLabel, func() {
		closePopup()
		if onAction != nil {
			onAction()
		}
	})
	footerButtons := container.NewCenter(actionBtn)
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerButtons, 12, 18, 10, 4))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInset(msg, 18, 18, 14, 8))
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 8), cornerBtn, border)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:        panel,
		DimColor:     color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		OnOutsideTap: closePopup,
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}
			panelMin := panel.MinSize()
			w := minFloat32(maxFloat32(panelMin.Width, chromeNoticeDialogWidth), maxWidth)
			h := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(w, h)
		},
	})
	return popup
}

// newVideoDialogLimeButton is the compact lime pill Account's Google login
// uses -- content-sized, not stretched across the panel.
func newVideoDialogLimeButton(text string, onTap func()) *videoDialogPillButton {
	b := &videoDialogPillButton{
		text:           text,
		OnTapped:       onTap,
		fillColor:      design.ColorConnectionAddFill,
		borderColor:    color.Transparent,
		textColor:      DeviceDashboardHeaderButtonTextColor,
		hoverFillColor: design.ColorConnectionAddFillHover,
		hoverTextColor: DeviceDashboardHeaderButtonTextColor,
		textSize:       12,
		minHeight:      36,
		padX:           18,
		radius:         8,
	}
	b.ExtendBaseWidget(b)
	return b
}

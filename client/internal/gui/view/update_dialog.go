package view

import (
	"fmt"
	"image/color"
	"sync"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const updateDialogPanelWidth = float32(360)

// ShowUpdateAvailableDialog is the "new version ready" card — same overlay
// chrome as Video Parameters (accent bar, close X, teal Apply pill), not
// Fyne's stock Confirm dialog.
func ShowUpdateAvailableDialog(parent fyne.Window, newVersion, currentVersion string, onResult func(bool)) {
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	var once sync.Once
	finish := func(ok bool) {
		once.Do(func() {
			if popup != nil {
				popup.Hide()
			}
			if onResult != nil {
				onResult(ok)
			}
		})
	}

	title := NewBrandText(i18n.Current.UpdateAvailableTitle, 13, design.ColorTextLight, true)
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   func() { finish(false) },
	})

	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(headerSepLine)
	footerSep.SetMinSize(fyne.NewSize(0, 1))

	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 44, 9, 4), headerSep)

	bodyContent := newVideoDialogWrapText(
		updateDialogPanelWidth-52,
		11,
		false,
		videoDialogWrapSpan{
			Text:  fmt.Sprintf(i18n.Current.UpdateAvailableMessage, newVersion, currentVersion),
			Color: design.ColorTextLight,
		},
	)

	later := newVideoDialogCancelButton(i18n.Current.UpdateLaterButton, func() { finish(false) })
	now := newVideoDialogApplyButton(i18n.Current.UpdateNowButton, func() { finish(true) })
	footerButtons := container.NewBorder(nil, nil, container.NewCenter(later), now, nil)
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerButtons, 12, 18, 6, 0))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInset(bodyContent, 18, 18, 14, 8))
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
		OnOutsideTap: func() { finish(false) },
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
			w := minFloat32(maxFloat32(panelMin.Width, updateDialogPanelWidth), maxWidth)
			h := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(w, h)
		},
	})
}

// UpdateProgress is a live handle to the styled download overlay.
type UpdateProgress struct {
	popup *widget.PopUp
	bar   *updateProgressBar
}

// ShowUpdateProgressDialog shows a determinate download card matching
// ShowUpdateAvailableDialog's chrome.
func ShowUpdateProgressDialog(parent fyne.Window, version string) *UpdateProgress {
	if parent == nil {
		return nil
	}
	up := &UpdateProgress{}
	title := NewBrandText(i18n.Current.UpdateDownloadingTitle, 13, design.ColorTextLight, true)
	headerSep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 21, 9, 4), headerSep)

	hint := canvas.NewText(fmt.Sprintf(i18n.Current.UpdateDownloadingMessage, version), videoDialogHintColor)
	hint.TextSize = 10

	bar := newUpdateProgressBar()
	up.bar = bar

	body := container.NewVBox(hint, videoDialogVSpace(10), bar)
	form := container.NewBorder(headerBlock, nil, nil, nil, NewInset(body, 18, 18, 14, 16))
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 8), border)

	up.popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
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
			w := minFloat32(maxFloat32(panelMin.Width, updateDialogPanelWidth), maxWidth)
			h := minFloat32(maxFloat32(panelMin.Height, 140), maxHeight)
			return fyne.NewSize(w, h)
		},
	})
	return up
}

// SetFraction paints the download bar; fraction is 0..1.
func (up *UpdateProgress) SetFraction(fraction float64) {
	if up == nil || up.bar == nil {
		return
	}
	up.bar.SetFraction(fraction)
}

// Close dismisses the progress overlay.
func (up *UpdateProgress) Close() {
	if up == nil || up.popup == nil {
		return
	}
	up.popup.Hide()
}

type updateProgressBar struct {
	widget.BaseWidget
	fraction float64
	track    *canvas.Rectangle
	fill     *canvas.Rectangle
}

func newUpdateProgressBar() *updateProgressBar {
	b := &updateProgressBar{}
	b.ExtendBaseWidget(b)
	return b
}

func (b *updateProgressBar) SetFraction(fraction float64) {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	b.fraction = fraction
	b.Refresh()
}

func (b *updateProgressBar) MinSize() fyne.Size {
	return fyne.NewSize(0, 6)
}

func (b *updateProgressBar) CreateRenderer() fyne.WidgetRenderer {
	b.track = canvas.NewRectangle(color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff})
	b.track.CornerRadius = 3
	b.fill = canvas.NewRectangle(design.ColorConnectionBadgeText)
	b.fill.CornerRadius = 3
	return &updateProgressBarRenderer{bar: b}
}

type updateProgressBarRenderer struct {
	bar *updateProgressBar
}

func (r *updateProgressBarRenderer) Layout(size fyne.Size) {
	r.bar.track.Resize(size)
	r.bar.track.Move(fyne.NewPos(0, 0))
	r.bar.fill.Move(fyne.NewPos(0, 0))
	r.bar.fill.Resize(fyne.NewSize(size.Width*float32(r.bar.fraction), size.Height))
}

func (r *updateProgressBarRenderer) MinSize() fyne.Size { return r.bar.MinSize() }

func (r *updateProgressBarRenderer) Refresh() {
	r.Layout(r.bar.Size())
	r.bar.track.Refresh()
	r.bar.fill.Refresh()
}

func (r *updateProgressBarRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *updateProgressBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bar.track, r.bar.fill}
}
func (r *updateProgressBarRenderer) Destroy() {}

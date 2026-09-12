package gui

import (
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

func accountDialogMobile() bool {
	return view.UseMobileConnections()
}

func accountDialogTitleInset() (left, right, top, bottom float32) {
	if accountDialogMobile() {
		return 14, 40, 8, 4
	}
	return 21, 44, 9, 4
}

func accountDialogFooterInset() (left, right, top, bottom float32) {
	if accountDialogMobile() {
		return 14, 14, 10, 12
	}
	return 21, 21, 14, 18
}

func accountDialogCardInset() (left, right, top, bottom float32) {
	if accountDialogMobile() {
		return 12, 12, 10, 10
	}
	return 14, 14, 12, 12
}

func accountDialogScrollMetrics(loggedIn, hasSyncKey, loginProgress bool) (left, right, top, bottom, minH float32) {
	switch {
	case loggedIn:
		left, right, top, bottom = 21, 21, 14, 18
		minH = 200
		if !hasSyncKey {
			minH = 250
		}
		if accountDialogMobile() {
			left, right, top, bottom = 14, 14, 10, 10
		}
	case loginProgress:
		left, right, top, bottom, minH = 21, 21, 2, 6, 110
		if accountDialogMobile() {
			left, right = 14, 14
			minH = 96
		}
	default:
		left, right, top, bottom, minH = 21, 21, 2, 12, 110
		if accountDialogMobile() {
			left, right = 14, 14
			minH = 96
		}
	}
	return
}

func applyAccountDialogScroll(scroll *container.Scroll, body fyne.CanvasObject, loggedIn, hasSyncKey, loginProgress bool) {
	if scroll == nil {
		return
	}
	l, r, t, b, minH := accountDialogScrollMetrics(loggedIn, hasSyncKey, loginProgress)
	inset := view.NewInset(body, l, r, t, b)
	scroll.Content = inset
	// On mobile size the scroll to the body so the panel grows with the
	// card instead of staying at a short min-height and scrolling inside.
	if accountDialogMobile() {
		if h := inset.MinSize().Height; h > minH {
			minH = h
		}
	}
	scroll.SetMinSize(fyne.NewSize(0, minH))
	scroll.Refresh()
}

func accountDialogPanelSize(panel fyne.CanvasObject, canvasSize fyne.Size) fyne.Size {
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 32)
	if accountDialogMobile() {
		margin = 14
	}
	maxWidth := canvasSize.Width - margin*2
	maxHeight := canvasSize.Height - margin*2
	if maxWidth <= 0 {
		maxWidth = canvasSize.Width
	}
	if maxHeight <= 0 {
		maxHeight = canvasSize.Height
	}

	panelMin := panel.MinSize()
	panelWidth := minFloat32(maxFloat32(panelMin.Width, 420), maxWidth)
	if accountDialogMobile() {
		// Fill the phone column. Ignore content MinSize.Width so a long
		// license id / email cannot force the panel wider than the canvas.
		panelWidth = maxWidth
	}
	panelHeight := minFloat32(panelMin.Height, maxHeight)
	return fyne.NewSize(panelWidth, panelHeight)
}

func accountDialogPanelPos(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position {
	return fyne.NewPos((canvasSize.Width-panelSize.Width)/2, (canvasSize.Height-panelSize.Height)/2)
}

func newAccountEmailText(email string) fyne.CanvasObject {
	t := canvas.NewText(email, design.ColorTextLight)
	t.TextSize = 13
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
}

func newAccountSyncOnDescription() fyne.CanvasObject {
	text := "End-to-end encrypted sync of your saved connections across devices."
	muted := color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}
	if !accountDialogMobile() {
		desc := canvas.NewText(text, muted)
		desc.TextSize = 8
		return desc
	}
	lbl := widget.NewLabel(text)
	lbl.Wrapping = fyne.TextWrapWord
	// Label wrap reports one-line MinSize; reserve two lines so the
	// panel grows instead of clipping into a scroll.
	lineH := fyne.MeasureText("Ag", 8, fyne.TextStyle{}).Height
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(0, lineH*2+6))
	return container.NewStack(lock, wrapAccountField(lbl, 8, muted))
}

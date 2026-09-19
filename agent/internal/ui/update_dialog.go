package ui

import (
	"fmt"
	"image/color"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const agentUpdateDialogWidth float32 = 360

// showUpdateAvailableDialog is the "new agent version ready" card — same
// branded overlay as General Settings (accent bar, close X, teal Update
// pill), not Fyne's stock Confirm dialog.
func showUpdateAvailableDialog(parent fyne.Window, newVersion, currentVersion string, onResult func(bool)) {
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

	bodyText := widget.NewLabel(fmt.Sprintf(loc().UpdateAvailableBody, newVersion, currentVersion))
	bodyText.Wrapping = fyne.TextWrapWord
	bodyText.Alignment = fyne.TextAlignLeading
	body := wrapDialogLabel(bodyText, 11, design.ColorTextLight)

	later := newIconActionButton(loc().NotNow, nil, func() { finish(false) })
	later.Compact = true
	now := newDialogCTA(loc().Update, func() { finish(true) })
	footer := container.New(&flushEndsLayout{}, later, now)

	panel := newBrandedDialogPanelInsets(loc().UpdateAvailable, agentUpdateDialogWidth, 20, 10, body, footer, func() { finish(false) })
	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel:        panel,
		OnOutsideTap: func() { finish(false) },
	})
}

// updateProgress is a live handle to an in-flight update's progress
// overlay. Every method is safe to call from any goroutine (they hop to the
// UI thread via fyne.Do) and safe to call on a nil receiver.
type updateProgress struct {
	popup *widget.PopUp
	bar   *agentUpdateProgressBar
}

func showUpdateProgressDialog(parent fyne.Window, version string) *updateProgress {
	up := &updateProgress{bar: newAgentUpdateProgressBar()}
	if parent == nil {
		return up
	}

	hint := canvas.NewText(fmt.Sprintf(loc().DownloadingVersion, version), design.ColorMutedOlive)
	hint.TextSize = 10
	body := container.New(&tightVBoxLayout{gap: 10}, hint, up.bar)

	var popup *widget.PopUp
	panel := newBrandedDialogPanelInsets(loc().Updating, agentUpdateDialogWidth, 20, 10, body, nil, func() {
		if popup != nil {
			popup.Hide()
		}
	})
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
	up.popup = popup
	return up
}

// Update sets the progress bar's fraction from downloaded/total bytes —
// intended to be passed directly as internal/update's ProgressFunc.
func (up *updateProgress) Update(downloaded, total int64) {
	if up == nil || up.bar == nil || total <= 0 {
		return
	}
	fraction := float64(downloaded) / float64(total)
	fyne.Do(func() { up.bar.SetFraction(fraction) })
}

// Close dismisses the progress overlay. A successful apply relaunches the
// whole agent before this would ever run, but the error path needs it to
// avoid leaving a stuck-looking dialog on screen.
func (up *updateProgress) Close() {
	if up == nil || up.popup == nil {
		return
	}
	fyne.Do(func() { up.popup.Hide() })
}

type agentUpdateProgressBar struct {
	widget.BaseWidget
	fraction float64
	track    *canvas.Rectangle
	fill     *canvas.Rectangle
}

func newAgentUpdateProgressBar() *agentUpdateProgressBar {
	b := &agentUpdateProgressBar{}
	b.ExtendBaseWidget(b)
	return b
}

func (b *agentUpdateProgressBar) SetFraction(fraction float64) {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	b.fraction = fraction
	b.Refresh()
}

func (b *agentUpdateProgressBar) MinSize() fyne.Size {
	return fyne.NewSize(0, 6)
}

func (b *agentUpdateProgressBar) CreateRenderer() fyne.WidgetRenderer {
	b.track = canvas.NewRectangle(design.ColorGray950)
	b.track.CornerRadius = 3
	b.fill = canvas.NewRectangle(design.ColorTeal)
	b.fill.CornerRadius = 3
	return &agentUpdateProgressBarRenderer{bar: b}
}

type agentUpdateProgressBarRenderer struct {
	bar *agentUpdateProgressBar
}

func (r *agentUpdateProgressBarRenderer) Layout(size fyne.Size) {
	r.bar.track.Resize(size)
	r.bar.track.Move(fyne.NewPos(0, 0))
	r.bar.fill.Move(fyne.NewPos(0, 0))
	r.bar.fill.Resize(fyne.NewSize(size.Width*float32(r.bar.fraction), size.Height))
}

func (r *agentUpdateProgressBarRenderer) MinSize() fyne.Size { return r.bar.MinSize() }

func (r *agentUpdateProgressBarRenderer) Refresh() {
	r.Layout(r.bar.Size())
	r.bar.track.Refresh()
	r.bar.fill.Refresh()
}

func (r *agentUpdateProgressBarRenderer) BackgroundColor() color.Color { return color.Transparent }
func (r *agentUpdateProgressBarRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.bar.track, r.bar.fill}
}
func (r *agentUpdateProgressBarRenderer) Destroy() {}

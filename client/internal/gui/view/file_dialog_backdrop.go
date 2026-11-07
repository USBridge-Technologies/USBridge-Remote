package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
)

// ShowFileDialog shows Fyne's own file dialog -- the fallback when no
// native save/open panel could be opened -- over the same dimmed backdrop
// and panel (ColorGray900, rounded, bordered) as the app's other dialogs.
//
// The file dialog paints its panel in ColorNameOverlayBackground, which
// BrandTheme keeps fully transparent for the overlay popups (see
// design.BrandTheme); unlike dialog.NewCustom and friends it draws no
// alpha-230 panel of its own, so on its own the file list floated over the
// app with no background at all. Here the panel is a separate popup under
// it, sized to the dialog's exact geometry.
func ShowFileDialog(fd *dialog.FileDialog, parent fyne.Window) {
	if fd == nil {
		return
	}
	if parent == nil || parent.Canvas() == nil {
		fd.Show()
		return
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	var minSize fyne.Size // the file dialog's own minimum, known once shown
	sizeFor := func(canvasSize fyne.Size) fyne.Size {
		want := fyne.NewSize(
			minFloat32(canvasSize.Width*0.86, 1100),
			minFloat32(canvasSize.Height*0.86, 760),
		)
		return want.Max(minSize).Min(canvasSize)
	}

	backdrop := ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    container.NewStack(bg, border),
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
			return sizeFor(canvasSize)
		},
	})
	fd.SetOnClosed(func() {
		if backdrop != nil {
			backdrop.Hide()
		}
	})
	fd.Show()
	minSize = fd.MinSize()
	// Pin the file dialog to the size the panel uses -- the modal popup
	// centres it in the canvas, as the overlay layout centres the panel.
	fd.Resize(sizeFor(parent.Canvas().Size()))
	if backdrop != nil {
		// Container.Refresh re-runs the overlay layout with minSize known;
		// PopUp.Refresh only relayouts when its own size changed.
		backdrop.Content.Refresh()
	}
}

package view

import (
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type OverlayPopupSpec struct {
	Panel     fyne.CanvasObject
	DimColor  color.Color
	PanelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
}

type overlayPopupLayout struct {
	panelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
}

func (l *overlayPopupLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	dim := objects[0]
	panel := objects[1]

	dim.Move(fyne.NewPos(0, 0))
	dim.Resize(size)

	panelSize := defaultOverlayPanelSize(size, panel)
	if l.panelSize != nil {
		panelSize = l.panelSize(size, panel)
	}

	if panelSize.Width > size.Width {
		panelSize.Width = size.Width
	}
	if panelSize.Height > size.Height {
		panelSize.Height = size.Height
	}
	if panelSize.Width < 0 {
		panelSize.Width = 0
	}
	if panelSize.Height < 0 {
		panelSize.Height = 0
	}

	panel.Move(fyne.NewPos((size.Width-panelSize.Width)/2, (size.Height-panelSize.Height)/2))
	panel.Resize(panelSize)
}

func (l *overlayPopupLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

func NewOverlayPopup(parent fyne.Window, spec OverlayPopupSpec) *widget.PopUp {
	if parent == nil || spec.Panel == nil {
		return nil
	}

	dimColor := spec.DimColor
	if dimColor == nil {
		dimColor = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72}
	}

	dim := canvas.NewRectangle(dimColor)
	content := container.New(&overlayPopupLayout{panelSize: spec.PanelSize}, dim, spec.Panel)
	popup := widget.NewPopUp(content, parent.Canvas())
	popup.Move(fyne.NewPos(0, 0))
	popup.Resize(parent.Canvas().Size())
	watchOverlayPopup(parent, popup)
	return popup
}

func ShowOverlayPopup(parent fyne.Window, spec OverlayPopupSpec) *widget.PopUp {
	popup := NewOverlayPopup(parent, spec)
	if popup != nil {
		popup.Show()
	}
	return popup
}

func ShowDimOverlay(parent fyne.Window, dimColor color.Color) *widget.PopUp {
	if parent == nil {
		return nil
	}
	if dimColor == nil {
		dimColor = color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72}
	}

	dim := canvas.NewRectangle(dimColor)
	popup := widget.NewPopUp(dim, parent.Canvas())
	popup.Move(fyne.NewPos(0, 0))
	popup.Resize(parent.Canvas().Size())
	watchOverlayPopup(parent, popup)
	popup.Show()
	return popup
}

func watchOverlayPopup(parent fyne.Window, popup *widget.PopUp) {
	if parent == nil || popup == nil {
		return
	}

	go func() {
		lastSize := parent.Canvas().Size()
		wasShown := false

		for {
			currentVisible := popup.Visible()
			if currentVisible {
				wasShown = true
			} else if wasShown {
				return
			}

			currentSize := parent.Canvas().Size()
			if currentSize != lastSize {
				lastSize = currentSize
				fyne.Do(func() {
					if popup == nil || !popup.Visible() {
						return
					}
					popup.Move(fyne.NewPos(0, 0))
					popup.Resize(currentSize)
				})
			}

			time.Sleep(120 * time.Millisecond)
		}
	}()
}

func defaultOverlayPanelSize(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
	if panel == nil {
		return fyne.NewSize(0, 0)
	}

	panelMin := panel.MinSize()
	if panelMin.Width > canvasSize.Width {
		panelMin.Width = canvasSize.Width
	}
	if panelMin.Height > canvasSize.Height {
		panelMin.Height = canvasSize.Height
	}
	return panelMin
}

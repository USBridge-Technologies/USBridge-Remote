package view

import (
	"image/color"
	"time"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type OverlayPopupSpec struct {
	Panel     fyne.CanvasObject
	DimColor  color.Color
	PanelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	PanelPos  func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position
}

type overlayPopupLayout struct {
	panelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	panelPos  func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position
}

func (l *overlayPopupLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	dim := objects[0]
	panel := objects[1]

	// widget.PopUp insets its content by theme.InnerPadding() on each side.
	// Extend the dim rectangle beyond the layout area to cover that border so
	// the popup widget's own background color is never visible on Android.
	innerPad := theme.InnerPadding()
	dim.Move(fyne.NewPos(-innerPad/2, -innerPad/2))
	dim.Resize(fyne.NewSize(size.Width+innerPad, size.Height+innerPad))

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

	panelPos := fyne.NewPos((size.Width-panelSize.Width)/2, (size.Height-panelSize.Height)/2)
	if l.panelPos != nil {
		panelPos = l.panelPos(size, panelSize)
	}
	if panelPos.X < 0 {
		panelPos.X = 0
	}
	if panelPos.Y < 0 {
		panelPos.Y = 0
	}
	maxX := size.Width - panelSize.Width
	maxY := size.Height - panelSize.Height
	if panelPos.X > maxX {
		panelPos.X = maxX
	}
	if panelPos.Y > maxY {
		panelPos.Y = maxY
	}

	panel.Move(panelPos)
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
	// Wrap the panel in a ThemeOverride so all child widgets (entries, buttons,
	// labels) reliably use BrandTheme inside the overlay on Android where the
	// popup's rendering context may not propagate the app theme correctly.
	themedPanel := container.NewThemeOverride(spec.Panel, design.NewBrandTheme())
	content := container.New(&overlayPopupLayout{panelSize: spec.PanelSize, panelPos: spec.PanelPos}, dim, themedPanel)
	popup := widget.NewPopUp(content, parent.Canvas())
	popup.Move(fyne.NewPos(0, 0))
	popup.Resize(parent.Canvas().Size())
	watchOverlayPopupHooks(parent, popup, true)
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
	watchOverlayPopupHooks(parent, popup, false)
}

func watchOverlayPopupHooks(parent fyne.Window, popup *widget.PopUp, fireHooks bool) {
	if parent == nil || popup == nil {
		return
	}

	go func() {
		var lastSize fyne.Size
		syncDone := make(chan struct{})
		fyne.Do(func() {
			if parent.Canvas() != nil {
				lastSize = parent.Canvas().Size()
			}
			close(syncDone)
		})
		<-syncDone

		wasShown := false

		for {
			var currentVisible bool
			var currentSize fyne.Size
			var hasCanvas bool

			syncDone = make(chan struct{})
			fyne.Do(func() {
				if popup != nil {
					currentVisible = popup.Visible()
				}
				if parent != nil && parent.Canvas() != nil {
					currentSize = parent.Canvas().Size()
					hasCanvas = true
				}
				close(syncDone)
			})
			<-syncDone

			if currentVisible {
				if !wasShown {
					wasShown = true
					if fireHooks {
						overlayShow()
					}
				}
			} else if wasShown {
				if fireHooks {
					overlayHide()
				}
				return
			}

			if hasCanvas && currentSize != lastSize {
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

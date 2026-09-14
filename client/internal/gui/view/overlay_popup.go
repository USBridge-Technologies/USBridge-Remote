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

// KeyboardHeight returns the current on-screen IME keyboard height in Fyne units.
// Set this from platform-specific code (Android) to enable keyboard-aware popups.
// Returns 0 if unset (desktop or no keyboard visible).
var KeyboardHeight func() float32

type OverlayPopupSpec struct {
	Panel     fyne.CanvasObject
	Footer    fyne.CanvasObject // optional; placed below the panel, uses full canvas coords
	DimColor  color.Color
	PanelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	PanelPos  func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position
	// KeyboardOverlap keeps the panel's natural height when the IME opens
	// and lets its bottom (typically the footer buttons) slide under the
	// keyboard. Without this flag the overlay shrinks the panel to fit
	// above the keyboard.
	KeyboardOverlap bool
	// KeyboardShift, with KeyboardOverlap, nudges the panel up a little
	// when the IME opens. Leave it false to keep the rest position (the
	// mobile edit card: plenty of gap above the keyboard, so a lift just
	// jitters the panel).
	KeyboardShift bool
}

type overlayPopupLayout struct {
	panelSize       func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	panelPos        func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position
	keyboardOverlap bool
	keyboardShift   bool
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

	// Effective area excludes the on-screen keyboard (IME) if any.
	// On Android the canvas size does not shrink when the IME opens (edge-to-edge),
	// so we subtract the keyboard height explicitly.
	keyboardH := float32(0)
	if KeyboardHeight != nil {
		keyboardH = KeyboardHeight()
		if keyboardH < 0 {
			keyboardH = 0
		}
	}
	effective := size
	if keyboardH > 0 {
		effective.Height -= keyboardH
		if effective.Height < 0 {
			effective.Height = 0
		}
	}

	// KeyboardOverlap sizes the panel against the full canvas so it does
	// not compress when the IME opens; the visible-area clamp below is
	// skipped for height/Y so the footer can sit under the keyboard.
	sizeForPanel := effective
	if l.keyboardOverlap {
		sizeForPanel = size
	}

	panelSize := defaultOverlayPanelSize(sizeForPanel, panel)
	if l.panelSize != nil {
		panelSize = l.panelSize(sizeForPanel, panel)
	}

	if panelSize.Width > size.Width {
		panelSize.Width = size.Width
	}
	if l.keyboardOverlap {
		if panelSize.Height > size.Height {
			panelSize.Height = size.Height
		}
	} else if panelSize.Height > effective.Height {
		panelSize.Height = effective.Height
	}
	if panelSize.Width < 0 {
		panelSize.Width = 0
	}
	if panelSize.Height < 0 {
		panelSize.Height = 0
	}

	panelPos := fyne.NewPos((sizeForPanel.Width-panelSize.Width)/2, (sizeForPanel.Height-panelSize.Height)/2)
	if l.panelPos != nil {
		panelPos = l.panelPos(sizeForPanel, panelSize)
	}
	if l.keyboardOverlap && l.keyboardShift && keyboardH > 0 {
		// A short lift — not the old jump to Y=12. The Tailscale row at
		// the bottom of Add Connection can sit under the keyboard; the
		// Name/LAN/Token fields stay in view without pinning the panel
		// against the header.
		minTop := float32(48)
		maxLift := float32(36)
		if panelPos.Y > minTop {
			lift := keyboardH * 0.22
			if lift > maxLift {
				lift = maxLift
			}
			if room := panelPos.Y - minTop; lift > room {
				lift = room
			}
			panelPos.Y -= lift
		}
	}
	if panelPos.X < 0 {
		panelPos.X = 0
	}
	if panelPos.Y < 0 {
		panelPos.Y = 0
	}
	maxX := size.Width - panelSize.Width
	if panelPos.X > maxX {
		panelPos.X = maxX
	}
	if !l.keyboardOverlap {
		maxY := effective.Height - panelSize.Height
		if panelPos.Y > maxY {
			panelPos.Y = maxY
		}
	}

	panel.Move(panelPos)
	panel.Resize(panelSize)

	// Footer: centered in the free space below the panel using full canvas
	// coordinates, so it stays fixed regardless of IME keyboard state.
	if len(objects) >= 3 && objects[2] != nil {
		footer := objects[2]
		footerMin := footer.MinSize()
		panelBottom := panelPos.Y + panelSize.Height
		spaceBelow := size.Height - panelBottom
		footerY := panelBottom + (spaceBelow-footerMin.Height)/2
		if footerY < panelBottom+14 {
			footerY = panelBottom + 14
		}
		footer.Move(fyne.NewPos(panelPos.X, footerY))
		footer.Resize(fyne.NewSize(panelSize.Width, footerMin.Height))
	}
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
	contentObjs := []fyne.CanvasObject{dim, themedPanel}
	if spec.Footer != nil {
		contentObjs = append(contentObjs, spec.Footer)
	}
	content := container.New(&overlayPopupLayout{
		panelSize:       spec.PanelSize,
		panelPos:        spec.PanelPos,
		keyboardOverlap: spec.KeyboardOverlap,
		keyboardShift:   spec.KeyboardShift,
	}, contentObjs...)
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
		var lastKeyboardH float32
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

			currentKeyboardH := float32(0)
			if KeyboardHeight != nil {
				currentKeyboardH = KeyboardHeight()
			}

			if hasCanvas && (currentSize != lastSize || currentKeyboardH != lastKeyboardH) {
				lastSize = currentSize
				lastKeyboardH = currentKeyboardH
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

// CompactOverlayTopMargin is the phone/compact overlay inset below the
// app header -- same 10%/80-110 band Add Connection already used, so
// stacked dialogs sit just under the chrome instead of on top of it.
func CompactOverlayTopMargin(canvasSize fyne.Size) float32 {
	return clampFloat32(canvasSize.Height*0.10, 80, 110)
}

// MobileEditOverlayTopMargin sits the phone connection-edit card further
// below the header than Add Connection, so it doesn't read as glued to
// the chrome.
func MobileEditOverlayTopMargin(canvasSize fyne.Size) float32 {
	return clampFloat32(canvasSize.Height*0.16, 128, 168)
}

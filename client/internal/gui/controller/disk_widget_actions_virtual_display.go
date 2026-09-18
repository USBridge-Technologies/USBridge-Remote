package controller

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"github.com/sirupsen/logrus"
)

const (
	virtualDisplayPreset1080p  = "1080p (1920x1080) @ 60Hz"
	virtualDisplayPreset1440p  = "1440p (2560x1440) @ 60Hz"
	virtualDisplayPreset4K     = "4K (3840x2160) @ 60Hz"
	virtualDisplayPresetCustom = "Custom..."
)

func (dw *DiskWidget) handleAddVirtualDisplay() {
	if dw.usbClient == nil {
		return
	}

	widthEntry := view.NewStyledEntry()
	widthEntry.SetText("1920")
	heightEntry := view.NewStyledEntry()
	heightEntry.SetText("1080")
	fpsEntry := view.NewStyledEntry()
	fpsEntry.SetText("60")

	applyPreset := func(s string) {
		switch s {
		case virtualDisplayPreset1080p:
			widthEntry.SetText("1920")
			heightEntry.SetText("1080")
			fpsEntry.SetText("60")
		case virtualDisplayPreset1440p:
			widthEntry.SetText("2560")
			heightEntry.SetText("1440")
			fpsEntry.SetText("60")
		case virtualDisplayPreset4K:
			widthEntry.SetText("3840")
			heightEntry.SetText("2160")
			fpsEntry.SetText("60")
		}
	}

	customBox := container.NewVBox(
		newVirtualDisplayFieldRow("Width", view.WrapBrandEntry(widthEntry, 11, design.ColorTextLight)),
		newVirtualDisplayFieldRow("Height", view.WrapBrandEntry(heightEntry, 11, design.ColorTextLight)),
		newVirtualDisplayFieldRow("FPS", view.WrapBrandEntry(fpsEntry, 11, design.ColorTextLight)),
	)
	customBox.Hide()

	var popup fyne.CanvasObject
	dropdown := view.NewDeviceDashboardModePicker(
		[]string{virtualDisplayPreset1080p, virtualDisplayPreset1440p, virtualDisplayPreset4K, virtualDisplayPresetCustom},
		virtualDisplayPreset1080p,
		func(s string) {
			applyPreset(s)
			if s == virtualDisplayPresetCustom {
				customBox.Show()
			} else {
				customBox.Hide()
			}
			if popup != nil {
				popup.Refresh()
			}
		},
	)
	presetRow := container.NewBorder(nil, nil, newVirtualDisplayFieldLabel("Preset"), nil, dropdown)
	body := container.NewVBox(presetRow, customBox)

	var closePopup func()
	addBtn := newVirtualDisplayAddButton(func() {
		var w, h, f int
		fmt.Sscanf(widthEntry.Text, "%d", &w)
		fmt.Sscanf(heightEntry.Text, "%d", &h)
		fmt.Sscanf(fpsEntry.Text, "%d", &f)
		if w <= 0 || h <= 0 || f <= 0 {
			view.ShowErrorDialog(fmt.Errorf("Invalid resolution or FPS"), dw.window)
			return
		}
		if closePopup != nil {
			closePopup()
		}

		dw.userOperationInFlight.Store(true)
		defer dw.userOperationInFlight.Store(false)

		_, err := dw.usbClient.AddVirtualDisplay(w, h, f)
		if err != nil {
			logrus.Errorf("Failed to add virtual display: %v", err)
			view.ShowErrorDialog(fmt.Errorf("Failed to add virtual display: %v", err), dw.window)
			return
		}
		dw.loadVideoDevices()
	})

	shown, close := showBrandedOverlayDialog(brandedOverlayDialogSpec{
		parent:          dw.window,
		title:           i18n.Current.AddVirtualDisplayTitle,
		subtitle:        i18n.Current.AddVirtualDisplaySubtitle,
		body:            body,
		rightButtons:    []fyne.CanvasObject{addBtn},
		compactFooter:   true,
		hideCancel:      true,
		headerPadBottom: 6,
		footerPadTop:    6,
		footerPadBottom: 8,
		panelSize:       virtualDisplayDialogPanelSize,
	})
	popup = shown
	closePopup = close
}

func virtualDisplayLabelColWidth() float32 {
	style := fyne.TextStyle{Monospace: true}
	w := float32(0)
	for _, s := range []string{"Preset", "Width", "Height", "FPS"} {
		if tw := fyne.MeasureText(s, 9, style).Width; tw > w {
			w = tw
		}
	}
	return w
}

type virtualDisplayLabelColLayout struct{}

func (l *virtualDisplayLabelColLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objects {
		if m := o.MinSize(); m.Height > h {
			h = m.Height
		}
	}
	return fyne.NewSize(virtualDisplayLabelColWidth(), h)
}

func (l *virtualDisplayLabelColLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	m := objects[0].MinSize()
	objects[0].Resize(m)
	objects[0].Move(fyne.NewPos(0, (size.Height-m.Height)/2))
}

func newVirtualDisplayFieldLabel(text string) fyne.CanvasObject {
	label := canvas.NewText(text, color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
	label.TextSize = 9
	label.TextStyle.Monospace = true
	return view.NewInset(container.New(&virtualDisplayLabelColLayout{}, label), 0, 12, 0, 0)
}

func newVirtualDisplayFieldRow(label string, field fyne.CanvasObject) fyne.CanvasObject {
	return view.NewInset(container.NewBorder(nil, nil, newVirtualDisplayFieldLabel(label), nil, field), 0, 0, 8, 0)
}

func newVirtualDisplayAddButton(onTapped func()) *connectionDialogSecondaryButton {
	btn := &connectionDialogSecondaryButton{
		labelText:        "Add",
		onTapped:         onTapped,
		compact:          true,
		short:            true,
		fillColor:        design.ColorConnectionAddFill,
		borderColor:      color.Transparent,
		textColor:        color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		hoverFillColor:   color.NRGBA{R: 0xd4, G: 0xf7, B: 0x8a, A: 0xff},
		hoverTextColor:   color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff},
		hoverBorderColor: color.Transparent,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func virtualDisplayDialogPanelSize(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
	maxW := canvasSize.Width - margin*2
	maxH := canvasSize.Height - margin*2
	if maxW < 0 {
		maxW = canvasSize.Width
	}
	if maxH < 0 {
		maxH = canvasSize.Height
	}
	min := panel.MinSize()
	w := minFloat32(360, maxW)
	if w < 0 {
		w = 0
	}
	h := min.Height
	if h > maxH {
		h = maxH
	}
	return fyne.NewSize(w, h)
}

func (dw *DiskWidget) handleDeleteVirtualDisplay(id string) {
	if dw.usbClient == nil {
		return
	}

	view.ShowConfirmYesLeft("Delete Virtual Display", "Are you sure you want to remove this virtual display?", func(b bool) {
		if !b {
			return
		}

		dw.userOperationInFlight.Store(true)
		defer dw.userOperationInFlight.Store(false)

		_, err := dw.usbClient.RemoveVirtualDisplay(id)
		if err != nil {
			logrus.Errorf("Failed to remove virtual display: %v", err)
			view.ShowErrorDialog(fmt.Errorf("Failed to remove virtual display: %v", err), dw.window)
			return
		}

		dw.loadVideoDevices()
	}, dw.window)
}

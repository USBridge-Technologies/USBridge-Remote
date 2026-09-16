package controller

import (
	"fmt"

	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
	"usbridge-client/internal/gui/view"
)

func (dw *DiskWidget) handleAddVirtualDisplay() {
	if dw.usbClient == nil {
		return
	}

	// Create a simple form for adding a virtual display
	widthEntry := widget.NewEntry()
	widthEntry.SetText("1920")
	heightEntry := widget.NewEntry()
	heightEntry.SetText("1080")
	fpsEntry := widget.NewEntry()
	fpsEntry.SetText("60")

	var form *widget.Form
	widthItem := widget.NewFormItem("Width", widthEntry)
	heightItem := widget.NewFormItem("Height", heightEntry)
	fpsItem := widget.NewFormItem("Refresh Rate (FPS)", fpsEntry)

	presets := widget.NewSelect([]string{"1080p (1920x1080) @ 60Hz", "1440p (2560x1440) @ 60Hz", "4K (3840x2160) @ 60Hz", "Custom..."}, func(s string) {})
	presets.OnChanged = func(s string) {
		switch s {
		case "1080p (1920x1080) @ 60Hz":
			widthEntry.SetText("1920")
			heightEntry.SetText("1080")
			fpsEntry.SetText("60")
		case "1440p (2560x1440) @ 60Hz":
			widthEntry.SetText("2560")
			heightEntry.SetText("1440")
			fpsEntry.SetText("60")
		case "4K (3840x2160) @ 60Hz":
			widthEntry.SetText("3840")
			heightEntry.SetText("2160")
			fpsEntry.SetText("60")
		}

		isCustom := s == "Custom..."
		if isCustom {
			if len(form.Items) == 1 {
				form.AppendItem(widthItem)
				form.AppendItem(heightItem)
				form.AppendItem(fpsItem)
			}
		} else {
			if len(form.Items) > 1 {
				form.Items = []*widget.FormItem{form.Items[0]}
				form.Refresh()
			}
		}
	}

	form = widget.NewForm(widget.NewFormItem("Preset", presets))
	presets.SetSelected("1080p (1920x1080) @ 60Hz")

	view.ShowCustomConfirmDialog("Add Virtual Display", "Add", "Cancel", form, func(b bool) {
		if !b {
			return
		}

		var w, h, f int
		fmt.Sscanf(widthEntry.Text, "%d", &w)
		fmt.Sscanf(heightEntry.Text, "%d", &h)
		fmt.Sscanf(fpsEntry.Text, "%d", &f)

		if w <= 0 || h <= 0 || f <= 0 {
			view.ShowErrorDialog(fmt.Errorf("Invalid resolution or FPS"), dw.window)
			return
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
	}, dw.window)
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

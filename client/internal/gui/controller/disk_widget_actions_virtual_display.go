package controller

import (
	"fmt"

	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
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

	presets := widget.NewSelect([]string{"1080p (1920x1080)", "1440p (2560x1440)", "4K (3840x2160)"}, func(s string) {
		switch s {
		case "1080p (1920x1080)":
			widthEntry.SetText("1920")
			heightEntry.SetText("1080")
		case "1440p (2560x1440)":
			widthEntry.SetText("2560")
			heightEntry.SetText("1440")
		case "4K (3840x2160)":
			widthEntry.SetText("3840")
			heightEntry.SetText("2160")
		}
	})
	presets.SetSelected("1080p (1920x1080)")

	items := []*widget.FormItem{
		widget.NewFormItem("Preset", presets),
		widget.NewFormItem("Width", widthEntry),
		widget.NewFormItem("Height", heightEntry),
		widget.NewFormItem("Refresh Rate (FPS)", fpsEntry),
	}

	dialog.ShowForm("Add Virtual Display", "Add", "Cancel", items, func(b bool) {
		if !b {
			return
		}

		var w, h, f int
		fmt.Sscanf(widthEntry.Text, "%d", &w)
		fmt.Sscanf(heightEntry.Text, "%d", &h)
		fmt.Sscanf(fpsEntry.Text, "%d", &f)

		if w <= 0 || h <= 0 || f <= 0 {
			dialog.ShowError(fmt.Errorf("Invalid resolution or FPS"), dw.window)
			return
		}

		dw.userOperationInFlight.Store(true)
		defer dw.userOperationInFlight.Store(false)

		_, err := dw.usbClient.AddVirtualDisplay(w, h, f)
		if err != nil {
			logrus.Errorf("Failed to add virtual display: %v", err)
			dialog.ShowError(fmt.Errorf("Failed to add virtual display: %v", err), dw.window)
			return
		}
		
		dw.requestDevicesRefresh()
	}, dw.window)
}

func (dw *DiskWidget) handleDeleteVirtualDisplay(id string) {
	if dw.usbClient == nil {
		return
	}
	
	dialog.ShowConfirm("Delete Virtual Display", "Are you sure you want to remove this virtual display?", func(b bool) {
		if !b {
			return
		}
		
		dw.userOperationInFlight.Store(true)
		defer dw.userOperationInFlight.Store(false)

		_, err := dw.usbClient.RemoveVirtualDisplay(id)
		if err != nil {
			logrus.Errorf("Failed to remove virtual display: %v", err)
			dialog.ShowError(fmt.Errorf("Failed to remove virtual display: %v", err), dw.window)
			return
		}
		
		dw.requestDevicesRefresh()
	}, dw.window)
}

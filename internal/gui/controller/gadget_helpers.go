package controller

import (
	"fmt"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

const gadgetRebuildDelay = 1200 * time.Millisecond

func newKeyboardStartRequest() models.DeviceStartRequest {
	return models.DeviceStartRequest{
		Device:       "keyboard",
		VendorID:     "0x1d6b",
		ProductID:    "0x0104",
		ProductName:  "USBridge Keyboard",
		Manufacturer: "USBridge",
		KeyboardMode: true,
	}
}

func newMouseStartRequest(mode string) models.DeviceStartRequest {
	mouseType := normalizeMouseMode(mode)
	productID := "0x0106"
	productName := "USBridge Relative Mouse"

	switch mouseType {
	case MouseModeAbsolute:
		productID = "0x0107"
		productName = "USBridge Absolute Pointer"
	case MouseModeTouchScreen:
		productID = "0x0108"
		productName = "USBridge Touch Pointer"
	}

	return models.DeviceStartRequest{
		Device:       "mouse",
		Type:         mouseTransportType(mouseType),
		VendorID:     "0x1d6b",
		ProductID:    productID,
		ProductName:  productName,
		Manufacturer: "USBridge",
	}
}

func newRNDISStartRequest(mode string) models.DeviceStartRequest {
	return models.DeviceStartRequest{
		Device:       "rndis",
		RNDISMode:    normalizeRNDISMode(mode),
		VendorID:     "0x1d6b",
		ProductID:    "0x0104",
		ProductName:  "USBridge RNDIS",
		Manufacturer: "USBridge",
	}
}

func rebuildUSBGadgetDevices(
	usbClient *api.USBClient,
	startBatch func(models.DeviceStartBatchRequest) (*models.APIResponse, error),
	requests models.DeviceStartBatchRequest,
) (*models.APIResponse, error) {
	if usbClient == nil {
		return nil, fmt.Errorf("usb client is not initialized")
	}

	logrus.Infof("♻️ [GADGET] Rebuilding USB gadget with %d device(s) (Full Replace)", len(requests))

	if len(requests) == 0 {
		// Если список пуст, мы должны просто остановить все устройства
		logrus.Infof("🛑 [GADGET] Stopping all devices since request list is empty")
		err := usbClient.StopAllDevices()
		if err != nil {
			return nil, err
		}
		return &models.APIResponse{Success: true, Message: "All devices stopped"}, nil
	}

	return startBatch(requests)
}

func driveSelectionKey(drive DriveItem) string {
	switch {
	case drive.IsKeyboard:
		return "keyboard"
	case drive.IsMouse:
		return "mouse"
	case drive.IsRNDIS:
		return "rndis"
	case drive.IsVideo && drive.VideoDevice != nil:
		return "video:" + drive.VideoDevice.Path
	case drive.DiskInfo != nil:
		return drive.Source + ":" + drive.DiskInfo.Path
	case drive.LocalDrive != nil:
		return drive.Source + ":" + drive.LocalDrive.SourceType + ":" + drive.LocalDrive.Name
	default:
		return drive.Source + ":" + drive.Name
	}
}

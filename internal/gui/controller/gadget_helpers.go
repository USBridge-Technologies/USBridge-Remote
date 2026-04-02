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
	return models.DeviceStartRequest{
		Device:       "mouse",
		Type:         mouseTransportType(mouseType),
		VendorID:     "0x1d6b",
		ProductID:    "0x0104",
		ProductName:  "USBridge Mouse",
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

	logrus.Infof("♻️ [GADGET] Rebuilding USB gadget with %d device(s)", len(requests))
	if err := usbClient.StopAllDevices(); err != nil {
		logrus.Warnf("⚠️ [GADGET] StopAllDevices during rebuild returned error: %v", err)
	} else {
		logrus.Info("🛑 [GADGET] Existing gadget devices stopped before rebuild")
	}

	if len(requests) == 0 {
		return nil, nil
	}

	time.Sleep(gadgetRebuildDelay)
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

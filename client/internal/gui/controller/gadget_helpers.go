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
		KeyboardMode: true,
	}
}

func newMouseStartRequest(mode string) models.DeviceStartRequest {
	return newMouseStartRequestWithDisplay(mode, 0, 1)
}

func newMouseStartRequestWithDisplay(mode string, displayIndex, displayCount int) models.DeviceStartRequest {
	if displayCount < 1 {
		displayCount = 1
	}
	return models.DeviceStartRequest{
		Device:       "mouse",
		Type:         mouseTransportType(normalizeMouseMode(mode)),
		DisplayIndex: displayIndex,
		DisplayCount: displayCount,
	}
}

func newRNDISStartRequest(mode string) models.DeviceStartRequest {
	return models.DeviceStartRequest{
		Device:    "rndis",
		RNDISMode: normalizeRNDISMode(mode),
	}
}

func newGamepadStartRequest(mode, vendorID, productID string) models.DeviceStartRequest {
	return models.DeviceStartRequest{
		Device:    "gamepad",
		Type:      normalizeGamepadMode(mode),
		VendorID:  vendorID,
		ProductID: productID,
	}
}

func executeDeviceBatch(
	usbClient *api.USBClient,
	startBatch func(models.DeviceStartBatchRequest, bool) (*models.APIResponse, error),
	requests models.DeviceStartBatchRequest,
	merge bool,
) (*models.APIResponse, error) {
	if usbClient == nil {
		return nil, fmt.Errorf("usb client is not initialized")
	}

	modeStr := "Full Replace"
	if merge {
		modeStr = "Merge"
	}

	logrus.Infof("♻️ [GADGET] Executing USB gadget batch with %d device(s) (%s)", len(requests), modeStr)

	if len(requests) == 0 && !merge {
		// Если список пуст и это Full Replace, мы должны просто остановить все устройства
		logrus.Infof("🛑 [GADGET] Stopping all devices since request list is empty and mode is Full Replace")
		err := usbClient.StopAllDevices()
		if err != nil {
			return nil, err
		}
		return &models.APIResponse{Success: true, Message: "All devices stopped"}, nil
	}

	return startBatch(requests, merge)
}

func driveSelectionKey(drive DriveItem) string {
	switch {
	case drive.IsKeyboard:
		return "keyboard"
	case drive.IsMouse:
		return "mouse"
	case drive.IsRNDIS:
		return "rndis"
	case drive.IsGamepad:
		if drive.GamepadID != "" {
			return "gamepad:" + drive.GamepadID
		}
		return "gamepad"
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

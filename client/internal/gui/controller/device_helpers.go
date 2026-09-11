package controller

import (
	"strings"
)

// IsMouseDeviceType checks if the device type string represents a mouse/pointer device.
func IsMouseDeviceType(deviceType string) bool {
	_, ok := parseMouseMode(deviceType)
	return ok
}

// IsKeyboardDeviceType checks if the device type represents a keyboard.
func IsKeyboardDeviceType(deviceType string) bool {
	return deviceType == "keyboard" || strings.HasPrefix(deviceType, "keyboard:")
}

// IsRNDISDeviceType checks if the device type represents a network device.
func IsRNDISDeviceType(deviceType string) bool {
	return deviceType == "rndis" || strings.HasPrefix(deviceType, "rndis:")
}

// IsGamepadDeviceType checks if the device type represents a gamepad.
func IsGamepadDeviceType(deviceType string) bool {
	return deviceType == "gamepad" || strings.HasPrefix(deviceType, "gamepad:")
}

// IsGamepadXInputDeviceType checks if the device type represents a gamepad in XInput mode.
func IsGamepadXInputDeviceType(deviceType string) bool {
	return deviceType == "gamepad:xinput"
}

// IsStorageDeviceType checks if the device type represents a storage device.
func IsStorageDeviceType(deviceType string, deviceName string) bool {
	switch {
	case deviceType == "local" && !strings.Contains(deviceName, "data"):
		return true
	case deviceType == "nbd":
		return true
	case strings.HasPrefix(deviceType, "disk:"):
		return true
	default:
		return false
	}
}

// backupFlashMTPName is the agent's live backup-flash MTP export.
// Snapshot exports use the same MTP gadget with a different name
// (typically data_<timestamp>), so name must be matched exactly.
const backupFlashMTPName = "data"

// IsBackupDeviceType checks if the device represents the backup/data flash.
func IsBackupDeviceType(deviceType string, deviceName string, productName string) bool {
	return deviceType == "mtp" && deviceName == backupFlashMTPName
}

// IsSnapshotMTPDevice checks if the device is a snapshot MTP source.
// The agent allows only one mtp:// source at a time. Snapshot names look
// like data_<timestamp> and often omit the word "snapshot"; empty names
// still count as long as this is not the live backup flash ("data").
func IsSnapshotMTPDevice(deviceType string, deviceName string) bool {
	return IsMTPGadget(deviceType, "") && deviceName != backupFlashMTPName
}

// IsMTPGadget reports a USB gadget that occupies the agent's single mtp:// slot.
func IsMTPGadget(deviceType, deviceKey string) bool {
	if deviceType == "mtp" || strings.HasPrefix(deviceType, "mtp:") {
		return true
	}
	return strings.HasPrefix(deviceKey, "mtp:")
}

// IsSnapshotDeviceType checks if the device represents a storage snapshot.
func IsSnapshotDeviceType(deviceType string, deviceName string, productName string) bool {
	if deviceType == "nbd" {
		return true
	}
	return IsSnapshotMTPDevice(deviceType, deviceName)
}

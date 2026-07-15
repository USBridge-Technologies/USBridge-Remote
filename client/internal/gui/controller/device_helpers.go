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

// IsBackupDeviceType checks if the device represents the backup/data flash.
func IsBackupDeviceType(deviceType string, deviceName string, productName string) bool {
	return deviceType == "mtp" && strings.Contains(deviceName, "data") && !strings.Contains(productName, "snapshot")
}

// IsSnapshotDeviceType checks if the device represents a storage snapshot.
func IsSnapshotDeviceType(deviceType string, deviceName string, productName string) bool {
	if deviceType == "nbd" {
		return true
	}
	return deviceType == "mtp" && (strings.Contains(productName, "snapshot") || strings.Contains(deviceName, "snapshot"))
}

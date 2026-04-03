package controller

import (
	"strings"

	"fyne.io/fyne/v2"
)

const (
	mouseModeTouchPad    = "mouse"
	mouseModeTouchScreen = "touchscreen"
	mouseModeAbsolute    = "absolute"

	MouseModeTouchPad    = mouseModeTouchPad
	MouseModeTouchScreen = mouseModeTouchScreen
	MouseModeAbsolute    = mouseModeAbsolute
)

func defaultMouseMode() string {
	if fyne.CurrentDevice().IsMobile() {
		return mouseModeTouchPad
	}
	return mouseModeAbsolute
}

func parseMouseMode(mode string) (string, bool) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch {
	case strings.HasPrefix(mode, "mouse:"):
		return mouseModeTouchPad, true
	case strings.HasPrefix(mode, "touchscreen:"):
		return mouseModeTouchScreen, true
	case strings.HasPrefix(mode, "absolute:"):
		return mouseModeAbsolute, true
	}

	switch mode {
	case mouseModeTouchPad, mouseModeTouchScreen, mouseModeAbsolute:
		return mode, true
	case "double":
		return mouseModeAbsolute, true
	default:
		return "", false
	}
}

func normalizeMouseMode(mode string) string {
	if normalized, ok := parseMouseMode(mode); ok {
		if normalized == mouseModeTouchScreen {
			return mouseModeTouchPad
		}
		return normalized
	}
	return defaultMouseMode()
}

func mouseTransportType(mode string) string {
	return normalizeMouseMode(mode)
}

func isMouseDeviceType(deviceType string) bool {
	_, ok := parseMouseMode(deviceType)
	return ok
}

func mouseModeFromDeviceType(deviceType string) string {
	if mode, ok := parseMouseMode(deviceType); ok {
		return mode
	}
	return defaultMouseMode()
}

func IsMouseDeviceType(deviceType string) bool {
	return isMouseDeviceType(deviceType)
}

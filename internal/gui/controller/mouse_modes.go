package controller

import "strings"

const (
	mouseModeTouchPad    = "mouse"
	mouseModeTouchScreen = "touchscreen"
	mouseModeAbsolute    = "absolute"

	MouseModeTouchPad    = mouseModeTouchPad
	MouseModeTouchScreen = mouseModeTouchScreen
	MouseModeAbsolute    = mouseModeAbsolute
)

func defaultMouseMode() string {
	return mouseModeTouchPad
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

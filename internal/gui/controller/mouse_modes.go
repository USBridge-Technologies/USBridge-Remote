package controller

import (
	"strings"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/gui/i18n"
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

func mouseConfigOptions() []string {
	return []string{
		i18n.Current.DeviceTouchPad,
		i18n.Current.DeviceAbsolute,
		// i18n.Current.DeviceAbsoluteLeft2,
		// i18n.Current.DeviceAbsoluteRight2,
	}
}

func mouseConfigToLabel(mode string, dispIdx, dispCnt int) string {
	switch normalizeMouseMode(mode) {
	case mouseModeAbsolute:
		/* Disabled for now
		if dispCnt >= 2 {
			if dispIdx == 0 {
				return i18n.Current.DeviceAbsoluteLeft2
			}
			return i18n.Current.DeviceAbsoluteRight2
		}
		*/
		return i18n.Current.DeviceAbsolute
	default:
		return i18n.Current.DeviceTouchPad
	}
}

func mouseLabelToConfig(s string) (mode string, dispIdx, dispCnt int) {
	switch s {
	/* Disabled for now
	case i18n.Current.DeviceAbsoluteLeft2:
		return mouseModeAbsolute, 0, 2
	case i18n.Current.DeviceAbsoluteRight2:
		return mouseModeAbsolute, 1, 2
	*/
	case i18n.Current.DeviceAbsolute:
		return mouseModeAbsolute, 0, 1
	default:
		return mouseModeTouchPad, 0, 1
	}
}

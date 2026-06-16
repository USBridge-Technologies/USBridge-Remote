package controller

import (
	"strings"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/gui/i18n"
)

const (
	mouseModeTouchPad      = "mouse"
	mouseModeTouchScreen   = "touchscreen"
	mouseModeAbsolute      = "absolute"
	mouseModeVirtualCursor = "cursor" // Android-only: local cursor rendered in Vulkan

	MouseModeTouchPad      = mouseModeTouchPad
	MouseModeTouchScreen   = mouseModeTouchScreen
	MouseModeAbsolute      = mouseModeAbsolute
	MouseModeVirtualCursor = mouseModeVirtualCursor
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
	case strings.HasPrefix(mode, "cursor:"):
		return mouseModeVirtualCursor, true
	}

	switch mode {
	case mouseModeTouchPad, mouseModeTouchScreen, mouseModeAbsolute, mouseModeVirtualCursor:
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

func isVirtualCursorMode(mode string) bool {
	return normalizeMouseMode(mode) == mouseModeVirtualCursor
}

func mouseTransportType(mode string) string {
	normalized := normalizeMouseMode(mode)
	// Virtual cursor is a client-only concept: the USB gadget and server bridge
	// must be configured in absolute mode so LiSendMousePositionEvent is routed
	// through bridgeAbsMouse → HID tablet on the host.
	if normalized == mouseModeVirtualCursor {
		return mouseModeAbsolute
	}
	return normalized
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
	opts := []string{
		i18n.Current.DeviceTouchPad,
		i18n.Current.DeviceAbsolute,
	}
	if fyne.CurrentDevice().IsMobile() {
		opts = append(opts, i18n.Current.DeviceVirtualCursor)
	}
	return opts
}

func mouseConfigToLabel(mode string, dispIdx, dispCnt int) string {
	switch normalizeMouseMode(mode) {
	case mouseModeAbsolute:
		return i18n.Current.DeviceAbsolute
	case mouseModeVirtualCursor:
		return i18n.Current.DeviceVirtualCursor
	default:
		return i18n.Current.DeviceTouchPad
	}
}

func mouseLabelToConfig(s string) (mode string, dispIdx, dispCnt int) {
	switch s {
	case i18n.Current.DeviceAbsolute:
		return mouseModeAbsolute, 0, 1
	case i18n.Current.DeviceVirtualCursor:
		return mouseModeVirtualCursor, 0, 1
	default:
		return mouseModeTouchPad, 0, 1
	}
}

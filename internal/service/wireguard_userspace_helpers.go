package service

import (
	"runtime"
	"strings"
)

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func userspaceDefaultInterfaceName(value string) string {
	value = strings.TrimSpace(value)
	switch runtime.GOOS {
	case "darwin":
		if value == "" || !strings.HasPrefix(value, "utun") {
			return "utun"
		}
		return value
	default:
		if value == "" {
			return "usbwg0"
		}
		return value
	}
}

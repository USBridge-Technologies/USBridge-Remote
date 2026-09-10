//go:build !linux

package usbpass

import (
	"fmt"

	"usbridge-client/internal/models"
)

func listSysfs() ([]models.USBPassthroughDevice, error) {
	return nil, fmt.Errorf("sysfs USB enumeration is Linux-only")
}

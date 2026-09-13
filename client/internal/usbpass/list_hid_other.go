//go:build !darwin

package usbpass

import (
	"fmt"
	"usbridge-client/internal/models"
)

func listHIDDarwin() ([]models.USBPassthroughDevice, error) {
	return nil, fmt.Errorf("HID enumeration is Darwin-only")
}
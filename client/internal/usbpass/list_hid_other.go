//go:build !darwin || ios

package usbpass

import (
	"fmt"

	"usbridge-client/internal/models"
)

// listHIDDarwin is real only on darwin (list_hid_darwin.go); this stub just
// keeps ListLocal's runtime.GOOS dispatch linking on every other platform,
// the same way list_sysfs_other.go stubs listSysfs for non-Linux.
func listHIDDarwin() ([]models.USBPassthroughDevice, error) {
	return nil, fmt.Errorf("HID USB enumeration is macOS-only")
}

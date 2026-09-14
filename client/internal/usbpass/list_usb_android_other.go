//go:build !android

package usbpass

import (
	"fmt"

	"usbridge-client/internal/models"
)

// listUSBAndroid is real only on android (list_usb_android.go); this stub
// keeps ListLocal's runtime.GOOS dispatch linking on every other platform,
// the same way list_sysfs_other.go stubs listSysfs for non-Linux.
func listUSBAndroid() ([]models.USBPassthroughDevice, error) {
	return nil, fmt.Errorf("Android USB host enumeration is android-only")
}

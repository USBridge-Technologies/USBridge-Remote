//go:build !windows

package usbpass

import (
	"fmt"

	"usbridge-client/internal/models"
)

// listSetupAPI is real only on windows (list_setupapi_windows.go); this stub
// keeps ListLocal's runtime.GOOS dispatch linking on every other platform,
// the same way list_sysfs_other.go stubs listSysfs for non-Linux.
func listSetupAPI() ([]models.USBPassthroughDevice, error) {
	return nil, fmt.Errorf("SetupAPI USB enumeration is Windows-only")
}

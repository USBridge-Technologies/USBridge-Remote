//go:build windows

package usbpass

import (
	"golang.org/x/sys/windows/registry"
)

// usbipWin2Services are the kernel services usbip-win2's installer
// registers: the UDE virtual host controller USB devices attach through,
// and its upper filter. Both must exist for passthrough to work.
var usbipWin2Services = []string{"usbip2_ude", "usbip2_filter"}

// serviceKeyExists is a package var so tests can fake the registry.
var serviceKeyExists = func(name string) bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Services\`+name, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	k.Close()
	return true
}

// USBIPDriverInstalled reports whether usbip-win2's drivers are installed,
// read straight from the service registry -- unlike Status().VhciDriver,
// which comes from the broker and so is unknown until the user has
// consented to running it.
func USBIPDriverInstalled() bool {
	for _, s := range usbipWin2Services {
		if !serviceKeyExists(s) {
			return false
		}
	}
	return true
}

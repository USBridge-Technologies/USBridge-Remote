//go:build !linux && !windows

package usbpass

import "fmt"

// USB passthrough is Linux/Windows only. Other client targets (hardware KVM,
// macOS — HID passthrough only, no USB/IP) build against this stub instead
// of usbaes_attach.go.
func Attach(opts AttachOptions) error {
	return fmt.Errorf("USB passthrough is not supported on this platform")
}

func StopAttach() {}

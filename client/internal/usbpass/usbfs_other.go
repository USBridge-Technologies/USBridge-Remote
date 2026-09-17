//go:build !linux && usbpass_gousb

package usbpass

import "fmt"

// usbfsClearHalt is Linux-only (see usbfs_linux.go's own doc comment: it
// exists specifically because usbfs there doesn't propagate a forwarded
// CLEAR_FEATURE control transfer to the host controller's own xHCI endpoint
// state). On Windows and other platforms, gousb's Device.ClearHalt call in
// clearEndpointHalt (libusb_clear_halt, backed by WinUsb_ResetPipe on
// Windows) already resets the pipe at the host-controller level by itself,
// so this fallback is never expected to actually fire there -- stubbed here
// only so the package builds with -tags usbpass_gousb on those platforms.
func usbfsClearHalt(busnum, devnum uint32, ep uint8) error {
	return fmt.Errorf("usbfs clear-halt is Linux-only")
}

//go:build linux && usbpass_gousb

package usbpass

import (
	"fmt"
	"os"
	"unsafe"

	"golang.org/x/sys/unix"
)

// usbdevfsClearHalt clears the halt/stall condition on one endpoint, both on
// the wire (device-side STALL, via a CLEAR_FEATURE(ENDPOINT_HALT) the kernel
// sends itself) AND in the host controller's own endpoint state (toggle bit,
// and on xHCI the endpoint context's Halted->Running transition).
//
// gousb/libusb on Linux is backed by usbfs, and forwarding a raw
// CLEAR_FEATURE control transfer through it (as HandleControl's "live"
// fallback does) only reaches the device — it does not touch the host
// controller's internal endpoint state. On xHCI that state is tracked
// separately from the device: after a STALL, the endpoint context stays
// "Halted" until something issues a Reset Endpoint command, so every
// subsequent bulk transfer keeps failing even though the device itself
// already cleared its STALL. USBDEVFS_CLEAR_HALT is the ioctl libusb's own
// libusb_clear_halt() uses to do both at once; gousb does not expose it, so
// we call it directly against the usbfs node.
const usbdevfsClearHalt = 0x80045515 // _IOR('U', 21, unsigned int)

func usbfsClearHalt(busnum, devnum uint32, ep uint8) error {
	if busnum == 0 || devnum == 0 {
		return fmt.Errorf("usbfs clear-halt: no busnum/devnum")
	}
	path := fmt.Sprintf("/dev/bus/usb/%03d/%03d", busnum, devnum)
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	arg := uint32(ep)
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), usbdevfsClearHalt, uintptr(unsafe.Pointer(&arg)))
	if errno != 0 {
		return fmt.Errorf("USBDEVFS_CLEAR_HALT ep=%#02x: %w", ep, errno)
	}
	return nil
}

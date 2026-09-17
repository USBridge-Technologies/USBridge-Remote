//go:build darwin

// hidlist prints every HID device usbpass.ListAllHIDDevices() (the darwin
// USB-passthrough device list's enumeration step) currently sees -- i.e.
// every candidate that would show up in the client's passthrough device
// list, after the built-in/no-VID exclusions in enumerateAllHIDDevices.
// Permanent dev tool, matching cmd/hidprobe/cmd/pentest/cmd/hidbridgetest's
// precedent.
package main

import (
	"fmt"

	"usbridge-client/internal/usbpass"
)

func main() {
	devices := usbpass.ListAllHIDDevices()
	if len(devices) == 0 {
		fmt.Println("(no HID devices found)")
		return
	}
	for _, d := range devices {
		fmt.Printf("%04x:%04x  %-40s  (registry id %d)\n", d.VID, d.PID, d.Name, d.ID)
	}
}

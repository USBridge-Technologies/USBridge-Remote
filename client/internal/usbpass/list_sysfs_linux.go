//go:build linux && !android

package usbpass

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"usbridge-client/internal/models"
)

const sysfsUSB = "/sys/bus/usb/devices"

// listSysfs enumerates local USB devices from sysfs so a Linux client can
// show the Passthrough section without the closed usbridge-usb-broker --list
// helper (Windows SetupAPI path). Attach still needs the broker binary.
func listSysfs() ([]models.USBPassthroughDevice, error) {
	entries, err := os.ReadDir(sysfsUSB)
	if err != nil {
		return nil, fmt.Errorf("sysfs usb: %w", err)
	}
	var devices []models.USBPassthroughDevice
	for _, ent := range entries {
		name := ent.Name()
		// Device nodes look like "1-5" or "2-1.3"; skip interfaces ("1-5:1.0")
		// and root hubs ("usb1").
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") {
			continue
		}
		dir := filepath.Join(sysfsUSB, name)
		vidHex, err := readSysfsTrim(filepath.Join(dir, "idVendor"))
		if err != nil {
			continue
		}
		pidHex, err := readSysfsTrim(filepath.Join(dir, "idProduct"))
		if err != nil {
			continue
		}
		classHex, _ := readSysfsTrim(filepath.Join(dir, "bDeviceClass"))
		if classHex == "09" { // hub
			continue
		}
		product, _ := readSysfsTrim(filepath.Join(dir, "product"))
		manufacturer, _ := readSysfsTrim(filepath.Join(dir, "manufacturer"))
		desc := strings.TrimSpace(strings.Join([]string{manufacturer, product}, " "))
		if desc == "" {
			desc = vidHex + ":" + pidHex
		}
		interfaces := readInterfaceClasses(dir)
		devices = append(devices, models.USBPassthroughDevice{
			BusID:       name,
			InstanceID:  name,
			VID:         strings.ToLower(vidHex),
			PID:         strings.ToLower(pidHex),
			Description: desc,
			Protected:   isProtectedInterfaces(interfaces),
			Interfaces:  interfaces,
		})
	}
	return devices, nil
}

func readSysfsTrim(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

// readInterfaceClasses reads every interface's real (bInterfaceClass,
// bInterfaceSubClass, bInterfaceProtocol) triple for the device at dir --
// the same sysfs walk isProtectedInterfaces below classifies, and also
// surfaced on models.USBPassthroughDevice.Interfaces for the dashboard's
// display-only "Pro" badge (see that field's own doc comment for why it's
// display-only, never an enforcement decision).
func readInterfaceClasses(dir string) [][3]uint8 {
	matches, _ := filepath.Glob(filepath.Join(dir + ":*"))
	var out [][3]uint8
	for _, iface := range matches {
		class, _ := readSysfsTrim(filepath.Join(iface, "bInterfaceClass"))
		sub, _ := readSysfsTrim(filepath.Join(iface, "bInterfaceSubClass"))
		proto, _ := readSysfsTrim(filepath.Join(iface, "bInterfaceProtocol"))
		if triple, ok := parseHexTriple(class, sub, proto); ok {
			out = append(out, triple)
		}
	}
	return out
}

// isProtectedInterfaces: boot HID keyboards/mice stay local so the user
// cannot detach the only input device driving the client UI (matches
// Windows broker "protected").
func isProtectedInterfaces(interfaces [][3]uint8) bool {
	for _, iface := range interfaces {
		if iface[0] != 0x03 || iface[1] != 0x01 {
			continue
		}
		// 01 = keyboard, 02 = mouse (HID boot protocol)
		if iface[2] == 0x01 || iface[2] == 0x02 {
			return true
		}
	}
	return false
}

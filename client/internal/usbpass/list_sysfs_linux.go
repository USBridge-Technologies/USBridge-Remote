//go:build linux

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
		devices = append(devices, models.USBPassthroughDevice{
			BusID:       name,
			InstanceID:  name,
			VID:         strings.ToLower(vidHex),
			PID:         strings.ToLower(pidHex),
			Description: desc,
			Protected:   isProtectedSysfsDevice(dir),
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

// Boot HID keyboards/mice stay local so the user cannot detach the only
// input device driving the client UI (matches Windows broker "protected").
func isProtectedSysfsDevice(dir string) bool {
	matches, _ := filepath.Glob(filepath.Join(dir + ":*"))
	for _, iface := range matches {
		class, _ := readSysfsTrim(filepath.Join(iface, "bInterfaceClass"))
		sub, _ := readSysfsTrim(filepath.Join(iface, "bInterfaceSubClass"))
		proto, _ := readSysfsTrim(filepath.Join(iface, "bInterfaceProtocol"))
		if class != "03" || sub != "01" {
			continue
		}
		// 01 = keyboard, 02 = mouse (HID boot protocol)
		if proto == "01" || proto == "02" {
			return true
		}
	}
	return false
}

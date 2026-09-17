//go:build windows

package usbpass

import (
	"fmt"
	"strings"

	"golang.org/x/sys/windows"

	"usbridge-client/internal/models"
)

// listSetupAPI enumerates local USB devices directly via SetupAPI -- the
// Windows counterpart to listSysfs (Linux) / listHIDDarwin (macOS) /
// listUSBAndroid (Android), so a Windows client can show the Devices tab's
// USB passthrough candidates without the closed usbridge-usb-broker --list
// helper. See listSysfs's own doc comment: "Attach still needs the broker
// binary" holds here too (usbaes_attach.go's AES control-plane handshake
// launches it with --role client on mount, on every platform) -- only
// *listing* was Windows-exclusively broker-gated before this file existed,
// which is what left the Devices tab unable to show anything at all on a
// machine that had no broker staged next to the client .exe, regardless of
// how a target device's driver was bound (see ResolveBroker in list.go).
//
// Walks every present device node under the "USB" enumerator (the same
// scope Zadig's own Options > List All Devices covers), not just
// GUID_DEVINTERFACE_USB_DEVICE, which would miss anything Windows hasn't
// already bound to a class driver that registers that interface.
func listSetupAPI() ([]models.USBPassthroughDevice, error) {
	set, err := windows.SetupDiGetClassDevsEx(nil, "USB", 0, windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES, 0, "")
	if err != nil {
		return nil, fmt.Errorf("SetupDiGetClassDevsEx: %w", err)
	}
	defer set.Close()

	var devices []models.USBPassthroughDevice
	for i := 0; ; i++ {
		data, err := set.EnumDeviceInfo(i)
		if err != nil {
			break // ERROR_NO_MORE_ITEMS
		}
		instanceID, err := set.DeviceInstanceID(data)
		if err != nil || instanceID == "" {
			continue
		}
		vid, pid, ok := parseUSBInstanceVIDPID(instanceID)
		if !ok {
			continue // e.g. a composite device's own umbrella node, no VID/PID of its own
		}
		compatIDs := setupDiMultiSzProperty(set, data, windows.SPDRP_COMPATIBLEIDS)
		hardwareIDs := setupDiMultiSzProperty(set, data, windows.SPDRP_HARDWAREID)
		if isUSBHub(hardwareIDs, compatIDs) {
			continue // hub -- never a passthrough target, matches listSysfs's own bDeviceClass==09 skip
		}

		desc := setupDiStringProperty(set, data, windows.SPDRP_FRIENDLYNAME)
		if desc == "" {
			desc = setupDiStringProperty(set, data, windows.SPDRP_DEVICEDESC)
		}
		if desc == "" {
			desc = vid + ":" + pid
		}

		devices = append(devices, models.USBPassthroughDevice{
			BusID:       StableUSBIPBusID(instanceID),
			InstanceID:  instanceID,
			VID:         strings.ToLower(vid),
			PID:         strings.ToLower(pid),
			Description: desc,
			// Boot HID keyboards/mice stay local so the user cannot detach
			// the only input device driving the client UI -- matches
			// listSysfs's isProtectedSysfsDevice and the Windows broker's
			// own "protected" flag.
			Protected: hasUSBInterfaceClass(compatIDs, "03&SubClass_01&Prot_01") || hasUSBInterfaceClass(compatIDs, "03&SubClass_01&Prot_02"),
		})
	}
	return devices, nil
}

// parseUSBInstanceVIDPID extracts VID/PID hex from a SetupAPI device
// instance id such as "USB\VID_1532&PID_0A29\0000790365644C2A" or
// "USB\VID_1532&PID_00C1&MI_01\...". ok is false for a node with no VID_/PID_
// segment at all (a few USB-enumerator nodes, like a root hub's own child
// summary entries, don't carry one).
func parseUSBInstanceVIDPID(instanceID string) (vid, pid string, ok bool) {
	upper := strings.ToUpper(instanceID)
	vid, vidOK := extractHexField(upper, "VID_")
	pid, pidOK := extractHexField(upper, "PID_")
	return vid, pid, vidOK && pidOK
}

func extractHexField(s, prefix string) (string, bool) {
	idx := strings.Index(s, prefix)
	if idx < 0 {
		return "", false
	}
	start := idx + len(prefix)
	end := start
	for end < len(s) && isHexDigit(s[end]) {
		end++
	}
	if end-start != 4 {
		return "", false
	}
	return s[start:end], true
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f')
}

// hasUSBInterfaceClass reports whether compatIDs (a device's
// SPDRP_COMPATIBLEIDS list) contains a "USB\Class_xx..." entry matching
// suffix -- the SetupAPI equivalent of reading bInterfaceClass/SubClass/
// Protocol from sysfs (see listSysfs's isProtectedSysfsDevice), since
// Windows exposes the same USB class/subclass/protocol triple as a
// compatible hardware ID string instead of separate files.
func hasUSBInterfaceClass(compatIDs []string, suffix string) bool {
	target := "CLASS_" + strings.ToUpper(suffix)
	for _, id := range compatIDs {
		if strings.Contains(strings.ToUpper(id), target) {
			return true
		}
	}
	return false
}

// isUSBHub reports whether a device node is a USB hub -- confirmed live
// (dumping real hardware/compatible IDs on a machine with several hubs)
// that Windows does NOT expose hub-ness via a "Class_09" compatible ID the
// way sysfs's bDeviceClass file does on Linux: a root hub's own HardwareID
// contains "ROOT_HUB" (no CompatibleIDs at all), and an external/generic
// hub's CompatibleIDs end in "_HUB" (e.g. "USB\USB20_HUB", "USB\USB30_HUB")
// instead.
func isUSBHub(hardwareIDs, compatIDs []string) bool {
	for _, id := range hardwareIDs {
		if strings.Contains(strings.ToUpper(id), "ROOT_HUB") {
			return true
		}
	}
	for _, id := range compatIDs {
		if strings.HasSuffix(strings.ToUpper(id), "_HUB") {
			return true
		}
	}
	return false
}

func setupDiStringProperty(set windows.DevInfo, data *windows.DevInfoData, prop windows.SPDRP) string {
	v, err := set.DeviceRegistryProperty(data, prop)
	if err != nil {
		return ""
	}
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func setupDiMultiSzProperty(set windows.DevInfo, data *windows.DevInfoData, prop windows.SPDRP) []string {
	v, err := set.DeviceRegistryProperty(data, prop)
	if err != nil {
		return nil
	}
	multi, _ := v.([]string)
	return multi
}

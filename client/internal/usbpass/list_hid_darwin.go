//go:build darwin

package usbpass

import (
	"fmt"

	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
)

// listHIDDarwin enumerates Wacom-family HID tablets as passthrough
// candidates -- the only device family hidbridge_darwin.go currently knows
// how to build a faithful HID descriptor for. Reuses platform.ListPenTablets
// (same Wacom-vendor-ID IOHIDManager match already used for the semantic
// CGEvent pen path) rather than duplicating the enumeration. BusID is
// synthesized from vid:pid via StableUSBIPBusID since macOS has no
// Linux-style busid.
func listHIDDarwin() ([]models.USBPassthroughDevice, error) {
	var out []models.USBPassthroughDevice
	for _, d := range platform.ListPenTablets() {
		instanceID := fmt.Sprintf("HID\\VID_%04X&PID_%04X", d.VID, d.PID)
		out = append(out, models.USBPassthroughDevice{
			BusID:       StableUSBIPBusID(instanceID),
			InstanceID:  instanceID,
			VID:         fmt.Sprintf("%04x", d.VID),
			PID:         fmt.Sprintf("%04x", d.PID),
			Description: d.Name,
		})
	}
	return out, nil
}

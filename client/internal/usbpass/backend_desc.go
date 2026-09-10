package usbpass

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

// NewExportedFromVIDPID builds an exportable MSC-shaped device with a
// descriptor/control backend. Claim/bulk via gousb is layered on later
// (build tag usbpass_gousb).
func NewExportedFromVIDPID(busID string, vid, pid uint16) *ExportedDevice {
	busnum, devnum := parseBusDev(busID)
	devDesc := syntheticDeviceDesc(vid, pid)
	cfgDesc := syntheticMSCConfig()
	return &ExportedDevice{
		BusID:      busID,
		Path:       "/sys/devices/usbridge/" + busID,
		Busnum:     busnum,
		Devnum:     devnum,
		Speed:      3,
		VID:        vid,
		PID:        pid,
		BCDDevice:  0x0100,
		Class:      0,
		SubClass:   0,
		Protocol:   0,
		ConfigVal:  1,
		NumConfigs: 1,
		Interfaces: [][3]uint8{{0x08, 0x06, 0x50}},
		DeviceDesc: devDesc,
		ConfigDesc: cfgDesc,
		Backend: &descBackend{
			deviceDesc: devDesc,
			configDesc: cfgDesc,
		},
	}
}

func parseBusDev(busID string) (uint32, uint32) {
	parts := strings.SplitN(busID, "-", 2)
	var bus, dev uint64 = 1, 1
	if len(parts) >= 1 {
		if v, err := strconv.ParseUint(parts[0], 10, 32); err == nil {
			bus = v
		}
	}
	if len(parts) >= 2 {
		if v, err := strconv.ParseUint(parts[1], 10, 32); err == nil {
			dev = v
		}
	}
	return uint32(bus), uint32(dev)
}

func ParseVIDPID(vidS, pidS string) (uint16, uint16, error) {
	vid, err := strconv.ParseUint(strings.TrimSpace(vidS), 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("vid: %w", err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(pidS), 16, 16)
	if err != nil {
		return 0, 0, fmt.Errorf("pid: %w", err)
	}
	return uint16(vid), uint16(pid), nil
}

type descBackend struct {
	deviceDesc []byte
	configDesc []byte
}

func (b *descBackend) HandleControl(setup [8]byte, wLength int) (int32, []byte) {
	bm := setup[0]
	req := setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])
	descType := uint8(wValue >> 8)
	descIndex := uint8(wValue & 0xff)

	if bm == 0x80 && req == 0x06 { // GET_DESCRIPTOR
		var src []byte
		switch descType {
		case 0x01:
			src = b.deviceDesc
		case 0x02:
			src = b.configDesc
		case 0x03:
			if descIndex == 0 {
				src = []byte{4, 0x03, 0x09, 0x04}
			} else {
				src = []byte{2, 0x03}
			}
		}
		if wLength < len(src) {
			src = src[:wLength]
		}
		return 0, append([]byte(nil), src...)
	}
	// SET_ADDRESS / CLEAR_FEATURE / SET_CONFIGURATION / SET_INTERFACE
	if req == 0x05 || req == 0x01 || req == 0x09 || req == 0x0b {
		return 0, nil
	}
	if req == 0x00 { // GET_STATUS
		n := 2
		if wLength < n {
			n = wLength
		}
		return 0, make([]byte, n)
	}
	return errnoEPIPE, nil
}

func (b *descBackend) HandleBulk(ep uint8, dirIn bool, length int, outData []byte) (int32, []byte) {
	_ = ep
	_ = dirIn
	_ = length
	_ = outData
	// Claim path (gousb) not active in default build.
	return errnoEPIPE, nil
}

func (b *descBackend) Close() error { return nil }

func syntheticDeviceDesc(vid, pid uint16) []byte {
	d := []byte{
		18, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 64,
		0, 0, 0, 0, 0x00, 0x01, 1, 2, 3, 1,
	}
	binary.LittleEndian.PutUint16(d[8:10], vid)
	binary.LittleEndian.PutUint16(d[10:12], pid)
	return d
}

func syntheticMSCConfig() []byte {
	return []byte{
		9, 2, 32, 0, 1, 1, 0, 0x80, 50,
		9, 4, 0, 0, 2, 0x08, 0x06, 0x50, 0,
		7, 5, 0x01, 0x02, 0x00, 0x02, 0,
		7, 5, 0x81, 0x02, 0x00, 0x02, 0,
	}
}

package usbpass

import (
	"embed"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"sync"
)

// A static model of a Wacom tablet: the descriptors and feature reports of the
// real device, captured from hardware (hidraw on Linux, see wacom_models/).
//
// Wacom's own drivers initialise the tablet from its complete report
// descriptor and a handful of feature reports (serial, firmware, mode...). An OS
// HID API does not hand a bridge either faithfully (Windows hid.dll returns a
// reconstructed, shorter descriptor and cannot read reports it dropped), so for
// the tablets in wacom_models the exported USB device is built from the
// captured data instead, and only the live input reports come from the real
// tablet. To the importer's driver it is then indistinguishable from the tablet
// on a cable.

//go:embed wacom_models/*.json
var wacomModelFS embed.FS

type wacomModelFile struct {
	Name         string            `json:"name"`
	VID          uint16            `json:"vid"`
	PID          uint16            `json:"pid"`
	DeviceDesc   string            `json:"device_desc"`
	ConfigDesc   string            `json:"config_desc"`
	Manufacturer string            `json:"manufacturer"`
	Product      string            `json:"product"`
	Serial       string            `json:"serial"`
	ReportDesc   string            `json:"report_desc"`
	Features     map[string]string `json:"features"` // report id -> hex (report id first), or "ERRnn" when the tablet refuses it
}

// wacomModel is a decoded wacomModelFile.
type wacomModel struct {
	Name                          string
	VID, PID                      uint16
	DeviceDesc, ConfigDesc        []byte
	Manufacturer, Product, Serial string
	ReportDesc                    []byte
	// Features holds the tablet's feature reports (report id first); a nil value is a
	// report the real tablet answers with a STALL.
	Features map[uint8][]byte
}

var (
	wacomModelsOnce sync.Once
	wacomModels     map[[2]uint16]*wacomModel
)

func loadWacomModels() {
	wacomModels = map[[2]uint16]*wacomModel{}
	ents, err := wacomModelFS.ReadDir("wacom_models")
	if err != nil {
		return
	}
	for _, e := range ents {
		raw, err := wacomModelFS.ReadFile("wacom_models/" + e.Name())
		if err != nil {
			continue
		}
		var f wacomModelFile
		if json.Unmarshal(raw, &f) != nil {
			continue
		}
		m := &wacomModel{
			Name: f.Name, VID: f.VID, PID: f.PID,
			Manufacturer: f.Manufacturer, Product: f.Product, Serial: f.Serial,
			Features: map[uint8][]byte{},
		}
		var e1, e2, e3 error
		m.DeviceDesc, e1 = hex.DecodeString(f.DeviceDesc)
		m.ConfigDesc, e2 = hex.DecodeString(f.ConfigDesc)
		m.ReportDesc, e3 = hex.DecodeString(f.ReportDesc)
		if e1 != nil || e2 != nil || e3 != nil || len(m.DeviceDesc) != 18 {
			continue
		}
		for k, v := range f.Features {
			id, err := strconv.ParseUint(k, 10, 8)
			if err != nil {
				continue
			}
			if strings.HasPrefix(v, "ERR") {
				m.Features[uint8(id)] = nil
				continue
			}
			if b, err := hex.DecodeString(v); err == nil && len(b) > 0 {
				m.Features[uint8(id)] = b
			}
		}
		wacomModels[[2]uint16{f.VID, f.PID}] = m
	}
}

// wacomModelFor returns the captured model of a tablet, or nil if there is none.
func wacomModelFor(vid, pid uint16) *wacomModel {
	wacomModelsOnce.Do(loadWacomModels)
	return wacomModels[[2]uint16{vid, pid}]
}

// wacomFeaturePort serves a model's feature reports. A SET_REPORT is remembered
// (the driver switches modes that way and reads them back).
type wacomFeaturePort struct {
	mu   sync.Mutex
	vals map[uint8][]byte
}

func newWacomFeaturePort(m *wacomModel) *wacomFeaturePort {
	p := &wacomFeaturePort{vals: map[uint8][]byte{}}
	for id, v := range m.Features {
		p.vals[id] = append([]byte(nil), v...)
	}
	return p
}

func (p *wacomFeaturePort) idSets() (s [ppReportTypes]map[uint8]bool) {
	for i := range s {
		s[i] = map[uint8]bool{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.vals {
		s[2][id] = true
	}
	return s
}

func (p *wacomFeaturePort) setReport(typ, id uint8, data []byte, hasReportIDs bool) error {
	if typ != 3 {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.vals[id]) > 0 && len(data) > 0 {
		v := append([]byte(nil), data...)
		if hasReportIDs && v[0] != id {
			v = append([]byte{id}, v...)
		}
		p.vals[id] = v
	}
	return nil
}

func (p *wacomFeaturePort) getReport(typ, id uint8, length int, _ bool) ([]byte, error) {
	if typ != 3 {
		return nil, errHIDNoInterfaces
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	v := p.vals[id]
	if len(v) == 0 {
		return nil, errHIDNoInterfaces // the real tablet STALLs this one
	}
	if length < len(v) {
		v = v[:length]
	}
	return append([]byte(nil), v...), nil
}

// newWacomModelBackend builds the synthetic USB device for a model. Input
// reports of the real tablet are queued with push(0, report).
func newWacomModelBackend(m *wacomModel) *hidGenBackend {
	f := &hidGenIface{
		Number:     0,
		ReportDesc: m.ReportDesc,
		HasIDs:     true,
		MaxInput:   hidGenMaxPacket,
		Ports:      []hidGenPort{newWacomFeaturePort(m)},
	}
	b := newHIDGenBackend(hidGenInfo{
		VID: m.VID, PID: m.PID,
		Manufacturer: m.Manufacturer, Product: m.Product, Serial: m.Serial,
		Interfaces: []*hidGenIface{f},
	})
	b.deviceDesc = append([]byte(nil), m.DeviceDesc...)
	b.configDesc = append([]byte(nil), m.ConfigDesc...)
	return b
}

// applyWacomModel fills an ExportedDevice from a model and returns its backend.
func applyWacomModel(dev *ExportedDevice, m *wacomModel) *hidGenBackend {
	b := newWacomModelBackend(m)
	dev.Backend = b
	dev.DeviceDesc = b.deviceDesc
	dev.ConfigDesc = b.configDesc
	dev.Class, dev.SubClass, dev.Protocol = 0, 0, 0
	dev.ConfigVal, dev.NumConfigs = 1, 1
	dev.VID, dev.PID = m.VID, m.PID
	dev.BCDDevice = uint16(m.DeviceDesc[12]) | uint16(m.DeviceDesc[13])<<8
	dev.Interfaces = [][3]uint8{{0x03, 0x00, 0x00}}
	dev.Speed = 2
	return b
}

// wacomWireLen is the size (report id included) of the tablet's input reports on
// the USB wire, from the report descriptor.
var wacomWireLen = map[uint8]int{0x10: 27, 0x11: 9, 0x13: 9, 0xAC: 192}

// wacomWireReport turns an input report from any source into the report the tablet
// puts on the wire, or nil for one to drop. Reports read straight from the device
// (hidraw, IOHID) already are wire reports. Under the Wacom Windows driver the
// tablet's reports arrive wrapped: vendor report 0xDC, then the report padded to
// 192 bytes.
func wacomWireReport(rep []byte) []byte {
	if len(rep) > 1 && rep[0] == 0xDC {
		rep = rep[1:]
	}
	if len(rep) == 0 {
		return nil
	}
	n, ok := wacomWireLen[rep[0]]
	if !ok || len(rep) < n {
		return nil
	}
	return append([]byte(nil), rep[:n]...)
}

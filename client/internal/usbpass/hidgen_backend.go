package usbpass

// Platform independent half of the HID bridge: turns a HID device that an OS
// HID API can read (Windows hid.dll today) into a synthetic USB HID device for
// the USB/IP export server. The importer's OS then binds its own class or
// vendor driver (e.g. Wacom's) to it, exactly like the macOS bridge in
// hidbridge_darwin.go, so neither side needs a WinUSB/Zadig driver swap.
//
// The OS-specific part only has to provide, per HID interface: the
// reconstructed report descriptor, a stream of input reports (pushed into the
// backend) and feature/output report round trips (hidGenPort).

import (
	"context"
	"encoding/binary"
	"errors"
	"sync"
	"unicode/utf16"

	"github.com/sirupsen/logrus"
)

const (
	hidGenMaxPacket = 64
	hidGenInterval  = 1
	hidGenQueue     = 16
)

// hidGenPort is one OS-level handle onto (part of) a HID interface.
type hidGenPort interface {
	// idSets returns the report IDs the port serves, per report type
	// (index 0 input, 1 output, 2 feature).
	idSets() [ppReportTypes]map[uint8]bool
	// setReport writes an output (wire type 2) or feature (3) report given in
	// wire form.
	setReport(typ, id uint8, data []byte, hasReportIDs bool) error
	// getReport reads an input (1) or feature (3) report in wire form.
	getReport(typ, id uint8, length int, hasReportIDs bool) ([]byte, error)
}

// hidGenIface is one USB HID interface of the exported device.
type hidGenIface struct {
	Number     uint8 // bInterfaceNumber presented to the importer
	ReportDesc []byte
	HasIDs     bool // the device uses report IDs (report bytes start with the ID)
	MaxInput   int  // largest input report on the wire, in bytes
	Ports      []hidGenPort

	ep      uint8
	turn    sync.Mutex // one waiting IN URB at a time keeps report order
	pending chan []byte
}

type hidGenInfo struct {
	VID, PID, BCD                 uint16
	Manufacturer, Product, Serial string
	Interfaces                    []*hidGenIface
}

type hidGenBackend struct {
	deviceDesc []byte
	configDesc []byte
	strs       map[uint8][]byte
	ifaces     []*hidGenIface
	onClose    func()
}

func hidGenString(s string) []byte {
	u := utf16.Encode([]rune(s))
	if len(u) > 126 {
		u = u[:126]
	}
	b := make([]byte, 2+2*len(u))
	b[0], b[1] = byte(len(b)), 0x03
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2+2*i:], c)
	}
	return b
}

func hidGenDeviceDesc(info hidGenInfo) []byte {
	d := []byte{
		18, 0x01, 0x00, 0x02, 0x00, 0x00, 0x00, 64,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 1,
	}
	binary.LittleEndian.PutUint16(d[8:], info.VID)
	binary.LittleEndian.PutUint16(d[10:], info.PID)
	binary.LittleEndian.PutUint16(d[12:], info.BCD)
	if info.Manufacturer != "" {
		d[14] = 1
	}
	if info.Product != "" {
		d[15] = 2
	}
	if info.Serial != "" {
		d[16] = 3
	}
	return d
}

// hidGenConfigDesc builds one configuration with a HID interface (plus its HID
// descriptor and one interrupt-IN endpoint) per entry.
func hidGenConfigDesc(ifaces []*hidGenIface) []byte {
	total := 9 + len(ifaces)*(9+9+7)
	d := make([]byte, 0, total)
	d = append(d, 9, 0x02, byte(total), byte(total>>8), byte(len(ifaces)), 1, 0, 0x80, 50)
	for _, f := range ifaces {
		mps := f.MaxInput
		if mps < 1 || mps > hidGenMaxPacket {
			mps = hidGenMaxPacket
		}
		d = append(d,
			9, 0x04, f.Number, 0, 1, 0x03, 0x00, 0x00, 0, // interface: HID
			9, 0x21, 0x11, 0x01, 0, 1, 0x22, byte(len(f.ReportDesc)), byte(len(f.ReportDesc)>>8), // HID descriptor
			7, 0x05, f.ep, 0x03, byte(mps), byte(mps>>8), hidGenInterval, // interrupt IN
		)
	}
	return d
}

func newHIDGenBackend(info hidGenInfo) *hidGenBackend {
	b := &hidGenBackend{ifaces: info.Interfaces, strs: map[uint8][]byte{}}
	for i, f := range b.ifaces {
		f.ep = 0x81 + uint8(i)
		f.pending = make(chan []byte, hidGenQueue)
	}
	b.deviceDesc = hidGenDeviceDesc(info)
	b.configDesc = hidGenConfigDesc(b.ifaces)
	if info.Manufacturer != "" {
		b.strs[1] = hidGenString(info.Manufacturer)
	}
	if info.Product != "" {
		b.strs[2] = hidGenString(info.Product)
	}
	if info.Serial != "" {
		b.strs[3] = hidGenString(info.Serial)
	}
	return b
}

// push queues an input report for the interface at index i. When the importer
// polls slower than the device reports, the oldest report is dropped so pointer
// and pressure data stay fresh rather than growing a stale backlog.
func (b *hidGenBackend) push(i int, report []byte) {
	if i < 0 || i >= len(b.ifaces) {
		return
	}
	q := b.ifaces[i].pending
	for {
		select {
		case q <- report:
			return
		default:
			select {
			case <-q:
			default:
			}
		}
	}
}

func (b *hidGenBackend) ifaceByNumber(n uint8) *hidGenIface {
	for _, f := range b.ifaces {
		if f.Number == n {
			return f
		}
	}
	if len(b.ifaces) == 1 {
		return b.ifaces[0]
	}
	return nil
}

// portFor picks the port that owns report (wire type, id).
func (f *hidGenIface) portFor(wireType, id uint8) hidGenPort {
	rt := int(wireType) - 1
	for _, p := range f.Ports {
		if rt >= 0 && rt < ppReportTypes && p.idSets()[rt][id] {
			return p
		}
	}
	// Nobody declares this report. A vendor driver (Wacom's router) sends such
	// "hidden" feature reports straight at the USB level; hid.dll refuses report
	// IDs the descriptor does not declare, so they cannot be forwarded.
	return nil
}

var hidGenUndeclaredLogged sync.Map

// noteUndeclared logs each undeclared (direction, type, id) once, so a driver
// that retries does not flood the log.
func noteUndeclared(get bool, typ, id uint8) {
	key := [3]uint8{0, typ, id}
	if get {
		key[0] = 1
	}
	if _, seen := hidGenUndeclaredLogged.LoadOrStore(key, true); !seen {
		dir := "SET_REPORT"
		if get {
			dir = "GET_REPORT"
		}
		logrus.Infof("usbpass: hidbridge: importer sent %s type %d id %#02x that the device does not declare; not forwarded (hid.dll cannot reach undeclared reports)", dir, typ, id)
	}
}

func (b *hidGenBackend) HandleControl(_ context.Context, setup [8]byte, wLength int, outData []byte) (int32, []byte) {
	bm, req := setup[0], setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])
	wIndex := binary.LittleEndian.Uint16(setup[4:6])

	clip := func(p []byte) (int32, []byte) {
		if wLength < len(p) {
			p = p[:wLength]
		}
		return 0, append([]byte(nil), p...)
	}

	if (bm == 0x80 || bm == 0x81) && req == 0x06 { // GET_DESCRIPTOR
		idx := uint8(wValue)
		switch uint8(wValue >> 8) {
		case 0x01:
			return clip(b.deviceDesc)
		case 0x02:
			return clip(b.configDesc)
		case 0x03:
			if idx == 0 {
				return clip([]byte{4, 0x03, 0x09, 0x04})
			}
			if s, ok := b.strs[idx]; ok {
				return clip(s)
			}
			return errnoEPIPE, nil
		case 0x21, 0x22:
			f := b.ifaceByNumber(uint8(wIndex))
			if f == nil {
				return errnoEPIPE, nil
			}
			if uint8(wValue>>8) == 0x22 {
				return clip(f.ReportDesc)
			}
			return clip([]byte{9, 0x21, 0x11, 0x01, 0, 1, 0x22, byte(len(f.ReportDesc)), byte(len(f.ReportDesc) >> 8)})
		}
		return errnoEPIPE, nil
	}

	switch bm {
	case 0x00, 0x01, 0x02: // standard host-to-device: SET_ADDRESS/CLEAR_FEATURE/SET_CONFIGURATION/SET_INTERFACE...
		return 0, nil
	case 0x80, 0x81, 0x82:
		switch req {
		case 0x00: // GET_STATUS
			return clip([]byte{0, 0})
		case 0x08: // GET_CONFIGURATION
			return clip([]byte{1})
		case 0x0A: // GET_INTERFACE
			return clip([]byte{0})
		}
		return errnoEPIPE, nil
	case 0x21: // HID class, host to device
		switch req {
		case 0x09: // SET_REPORT
			f := b.ifaceByNumber(uint8(wIndex))
			if f == nil {
				return errnoEPIPE, nil
			}
			typ, id := uint8(wValue>>8), uint8(wValue)
			if typ < 2 || typ > 3 {
				return errnoEPIPE, nil
			}
			p := f.portFor(typ, id)
			if p == nil {
				// Acknowledged, like the macOS bridge does: vendor init writes are
				// idempotent mode/config settings, and the device was already set
				// up by its own host driver. Failing them would make the importer's
				// driver treat the whole device as broken.
				noteUndeclared(false, typ, id)
				return 0, nil
			}
			if err := p.setReport(typ, id, outData, f.HasIDs); err != nil {
				return errnoEPIPE, nil
			}
			return 0, nil
		case 0x0A, 0x0B: // SET_IDLE, SET_PROTOCOL
			return 0, nil
		}
	case 0xA1: // HID class, device to host
		switch req {
		case 0x01: // GET_REPORT
			f := b.ifaceByNumber(uint8(wIndex))
			if f == nil {
				return errnoEPIPE, nil
			}
			typ, id := uint8(wValue>>8), uint8(wValue)
			p := f.portFor(typ, id)
			if p == nil || (typ != 1 && typ != 3) {
				if p == nil {
					noteUndeclared(true, typ, id)
				}
				return errnoEPIPE, nil
			}
			data, err := p.getReport(typ, id, wLength, f.HasIDs)
			if err != nil {
				return errnoEPIPE, nil
			}
			return 0, data
		case 0x02: // GET_IDLE
			return clip([]byte{0})
		case 0x03: // GET_PROTOCOL: report protocol
			return clip([]byte{1})
		}
	}
	return errnoEPIPE, nil
}

func (b *hidGenBackend) HandleBulk(ctx context.Context, ep uint8, dirIn bool, length int, _ []byte) (int32, []byte) {
	if !dirIn {
		return errnoEPIPE, nil
	}
	var f *hidGenIface
	for _, c := range b.ifaces {
		if c.ep&0x7f == ep&0x7f {
			f = c
			break
		}
	}
	if f == nil {
		return errnoEPIPE, nil
	}
	f.turn.Lock()
	defer f.turn.Unlock()
	select {
	case rep := <-f.pending:
		if len(rep) > length {
			rep = rep[:length]
		}
		return 0, rep
	case <-ctx.Done():
		return errnoEPIPE, nil
	}
}

func (b *hidGenBackend) Close() error {
	if b.onClose != nil {
		b.onClose()
		b.onClose = nil
	}
	return nil
}

var errHIDNoInterfaces = errors.New("no HID interface could be bridged")

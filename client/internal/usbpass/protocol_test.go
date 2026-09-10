package usbpass

import (
	"encoding/binary"
	"testing"
)

func TestStableUSBIPBusIDShape(t *testing.T) {
	id := StableUSBIPBusID(`USB\VID_24A9&PID_205A\6&abcd`)
	var bus, port int
	if _, err := fmtSscanf(id, &bus, &port); err != nil {
		t.Fatal(err)
	}
	if bus < 1 || bus > 9 || port < 1 || port > 200 {
		t.Fatalf("out of range: %s", id)
	}
}

func fmtSscanf(id string, bus, port *int) (int, error) {
	var b, p int
	n, err := parseBusPort(id, &b, &p)
	*bus, *port = b, p
	return n, err
}

func parseBusPort(id string, bus, port *int) (int, error) {
	bn, dn := parseBusDev(id)
	*bus, *port = int(bn), int(dn)
	return 2, nil
}

func TestPackDevlistBusID(t *testing.T) {
	d := NewExportedFromVIDPID("1-92", 0x24a9, 0x205a)
	p := packRepDevlist([]*ExportedDevice{d})
	if binary.BigEndian.Uint16(p[0:2]) != usbipVersion {
		t.Fatal("version")
	}
	if binary.BigEndian.Uint16(p[2:4]) != opRepDevlist {
		t.Fatal("op")
	}
	if binary.BigEndian.Uint32(p[8:12]) != 1 {
		t.Fatal("count")
	}
	bus := cstr(p[12+256 : 12+256+32])
	if bus != "1-92" {
		t.Fatalf("busid %q", bus)
	}
}

func TestPackImportErr(t *testing.T) {
	p := packRepImport(nil)
	if len(p) != 8 {
		t.Fatalf("len %d", len(p))
	}
	if binary.BigEndian.Uint32(p[4:8]) != 1 {
		t.Fatal("status")
	}
}

func TestDescGetDevice(t *testing.T) {
	d := NewExportedFromVIDPID("1-92", 0x24a9, 0x205a)
	setup := [8]byte{0x80, 0x06, 0x00, 0x01, 0x00, 0x00, 0x12, 0x00}
	st, data := d.Backend.HandleControl(setup, 18)
	if st != 0 || len(data) != 18 {
		t.Fatalf("st=%d len=%d", st, len(data))
	}
	if binary.LittleEndian.Uint16(data[8:10]) != 0x24a9 {
		t.Fatal("vid")
	}
}

func TestParseCmdSubmitIn(t *testing.T) {
	// Minimal CMD_SUBMIT header, IN, no payload.
	buf := make([]byte, 48)
	binary.BigEndian.PutUint32(buf[0:4], cmdSubmit)
	binary.BigEndian.PutUint32(buf[4:8], 7)
	binary.BigEndian.PutUint32(buf[12:16], dirIn)
	binary.BigEndian.PutUint32(buf[24:28], 18)
	binary.BigEndian.PutUint32(buf[32:36], 0xffffffff)
	copy(buf[40:48], []byte{0x80, 0x06, 0x00, 0x01, 0x00, 0x00, 0x12, 0x00})
	f, n, err := parseCmd(buf)
	if err != nil || n != 48 {
		t.Fatalf("err=%v n=%d", err, n)
	}
	if f.seq != 7 || f.transferLen != 18 {
		t.Fatalf("%+v", f)
	}
}

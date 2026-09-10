package usbpass

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestExportDevlistAndImport(t *testing.T) {
	d := NewExportedFromVIDPID("1-92", 0x24a9, 0x205a)
	srv, err := StartExport("127.0.0.1:0", []*ExportedDevice{d})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()
	addr := srv.Addr()

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	req := make([]byte, 8)
	binary.BigEndian.PutUint16(req[0:2], usbipVersion)
	binary.BigEndian.PutUint16(req[2:4], opReqDevlist)
	if _, err := conn.Write(req); err != nil {
		t.Fatal(err)
	}
	hdr := make([]byte, 12)
	if _, err := io.ReadFull(conn, hdr); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(hdr[2:4]) != opRepDevlist {
		t.Fatalf("op %#x", binary.BigEndian.Uint16(hdr[2:4]))
	}
	if binary.BigEndian.Uint32(hdr[8:12]) != 1 {
		t.Fatal("count")
	}
	_ = conn.Close()

	conn2, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn2.Close()
	imp := make([]byte, 8+32)
	binary.BigEndian.PutUint16(imp[0:2], usbipVersion)
	binary.BigEndian.PutUint16(imp[2:4], opReqImport)
	copy(imp[8:], []byte("1-92"))
	if _, err := conn2.Write(imp); err != nil {
		t.Fatal(err)
	}
	ih := make([]byte, 8)
	if _, err := io.ReadFull(conn2, ih); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(ih[4:8]) != 0 {
		t.Fatalf("import status %d", binary.BigEndian.Uint32(ih[4:8]))
	}
	body := make([]byte, 256+32+4+4+4+2+2+2+6)
	if _, err := io.ReadFull(conn2, body); err != nil {
		t.Fatal(err)
	}
	if cstr(body[256:256+32]) != "1-92" {
		t.Fatalf("bus %q", cstr(body[256:256+32]))
	}

	// GET_DESCRIPTOR device via CMD_SUBMIT
	cmd := make([]byte, 48)
	binary.BigEndian.PutUint32(cmd[0:4], cmdSubmit)
	binary.BigEndian.PutUint32(cmd[4:8], 1)
	binary.BigEndian.PutUint32(cmd[12:16], dirIn)
	binary.BigEndian.PutUint32(cmd[24:28], 18)
	binary.BigEndian.PutUint32(cmd[32:36], 0xffffffff)
	copy(cmd[40:48], []byte{0x80, 0x06, 0x00, 0x01, 0x00, 0x00, 0x12, 0x00})
	if _, err := conn2.Write(cmd); err != nil {
		t.Fatal(err)
	}
	ret := make([]byte, 48+18)
	if _, err := io.ReadFull(conn2, ret); err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint32(ret[0:4]) != retSubmit {
		t.Fatalf("ret %#x", binary.BigEndian.Uint32(ret[0:4]))
	}
	if int32(binary.BigEndian.Uint32(ret[20:24])) != 0 {
		t.Fatalf("status %d", int32(binary.BigEndian.Uint32(ret[20:24])))
	}
	if binary.LittleEndian.Uint16(ret[48+8:48+10]) != 0x24a9 {
		t.Fatal("vid in payload")
	}
}

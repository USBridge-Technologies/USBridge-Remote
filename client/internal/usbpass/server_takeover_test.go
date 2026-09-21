package usbpass

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

type nopBackend struct{}

func (nopBackend) HandleControl(context.Context, [8]byte, int, []byte) (int32, []byte) { return 0, nil }
func (nopBackend) HandleBulk(context.Context, uint8, bool, int, []byte) (int32, []byte) {
	return 0, nil
}
func (nopBackend) Close() error { return nil }

// importOnce sends OP_REQ_IMPORT for busID and returns the still-open
// connection after checking the reply's status.
func importOnce(t *testing.T, addr, busID string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	req := make([]byte, 8+32)
	binary.BigEndian.PutUint16(req[0:], usbipVersion)
	binary.BigEndian.PutUint16(req[2:], opReqImport)
	copy(req[8:], busID)
	if _, err := c.Write(req); err != nil {
		t.Fatal(err)
	}
	rep := make([]byte, 8)
	_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := io.ReadFull(c, rep); err != nil {
		t.Fatal(err)
	}
	if st := binary.BigEndian.Uint32(rep[4:]); st != 0 {
		t.Fatalf("import status %d", st)
	}
	if _, err := io.ReadFull(c, make([]byte, 312)); err != nil { // usbip_usb_device
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Time{})
	return c
}

// A second import of the same bus id must replace the first session, not run
// beside it: the importer would otherwise create two virtual devices for one
// physical pad.
func TestSecondImportReplacesFirst(t *testing.T) {
	d := NewExportedFromVIDPID("9-8", 0x045e, 0x028e)
	d.Backend = nopBackend{}
	srv, err := StartExport("127.0.0.1:0", []*ExportedDevice{d})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Stop()

	first := importOnce(t, srv.Addr(), "9-8")
	defer first.Close()
	second := importOnce(t, srv.Addr(), "9-8")
	defer second.Close()

	// The first session is dropped by the server...
	_ = first.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := first.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("first session still open (err=%v)", err)
	}
	// ...and the second one is alive: it can be superseded in turn.
	third := importOnce(t, srv.Addr(), "9-8")
	defer third.Close()
	_ = second.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil || isTimeout(err) {
		t.Fatalf("second session still open after third import (err=%v)", err)
	}
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}

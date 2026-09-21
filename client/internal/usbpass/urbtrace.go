package usbpass

import (
	"encoding/hex"
	"os"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// The per-URB detail in backend_gousb.go is Debugf, and the client runs at
// Info, so a passthrough session that connects fine but never completes the
// device's own handshake (e.g. an Xbox One pad that never lights up) leaves
// nothing in app.log to look at. urbTrace records every URB served for one
// exported device at Info, bounded so a bulk-heavy device (a USB drive)
// can't flood the log: the first urbTraceLines URBs in full, after that only
// failed ones (and only a sample of those).
//
// USBRIDGE_USB_TRACE=0 turns it off, =all lifts the cap.
const (
	urbTraceLines  = 800
	urbTraceErrCap = 200
	urbTraceHexMax = 48
)

var urbTraceMode = os.Getenv("USBRIDGE_USB_TRACE")

type urbTrace struct {
	lines atomic.Int64
	errs  atomic.Int64
}

func hexHead(b []byte) string {
	if len(b) == 0 {
		return "-"
	}
	n := len(b)
	if n > urbTraceHexMax {
		n = urbTraceHexMax
	}
	s := hex.EncodeToString(b[:n])
	if len(b) > n {
		s += "..."
	}
	return s
}

// budget returns whether this URB's line should be emitted at Info, and
// logs the one-time notice when the cap is first exceeded.
func (t *urbTrace) budget() bool {
	if urbTraceMode == "0" {
		return false
	}
	n := t.lines.Add(1)
	if urbTraceMode == "all" || n <= urbTraceLines {
		return true
	}
	if n == urbTraceLines+1 {
		logrus.Infof("usbpass: URB trace: first %d URBs logged, only failures from here (USBRIDGE_USB_TRACE=all lifts the cap)", urbTraceLines)
	}
	return false
}

func (t *urbTrace) record(f urbFrame, status int32, data []byte, elapsed time.Duration) {
	if urbTraceMode == "0" {
		return
	}
	full := t.budget()
	failed := status != 0
	if failed {
		e := t.errs.Add(1)
		if e > urbTraceErrCap && e%100 != 0 {
			return
		}
	} else if !full {
		return
	}

	dir := "OUT"
	payload := f.data
	if f.direction == dirIn {
		dir = "IN"
		payload = data
	}
	ms := float64(elapsed.Microseconds()) / 1000
	logf := logrus.Infof
	if failed {
		logf = logrus.Warnf
	}

	if uint8(f.ep)&0x7f == 0 {
		logf("usbpass: URB ctl %s seq=%d bm=0x%02x req=0x%02x val=0x%04x idx=0x%04x wLen=%d data=%s -> st=%d ret=%d (%.1fms)",
			dir, f.seq, f.setup[0], f.setup[1],
			uint16(f.setup[2])|uint16(f.setup[3])<<8, uint16(f.setup[4])|uint16(f.setup[5])<<8,
			int(f.setup[6])|int(f.setup[7])<<8, hexHead(payload), status, len(data), ms)
		return
	}
	ep := uint8(f.ep) & 0x7f
	if f.direction == dirIn {
		ep |= 0x80
	}
	logf("usbpass: URB ep=0x%02x %s seq=%d len=%d data=%s -> st=%d ret=%d (%.1fms)",
		ep, dir, f.seq, f.transferLen, hexHead(payload), status, len(data), ms)
}

func (t *urbTrace) unlink(seq, target uint32) {
	if t.budget() {
		logrus.Infof("usbpass: URB unlink seq=%d target=%d", seq, target)
	}
}

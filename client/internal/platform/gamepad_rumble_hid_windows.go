//go:build windows

package platform

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Rumble for PlayStation-layout DirectInput pads. They are read through WinMM,
// which cannot write, and have no XInput slot, so the pad's own HID output
// report is written directly.
//
// The protocol is DualShock 4's (USB output report 0x05, 32 bytes), which the
// Razer Raiju family and other DS4 clones speak. The Raiju Tournament Edition's
// game collection reports exactly that output length. Only pads listed in
// hidRumbleProtocols are driven: writing an output report the pad does not
// expect could reconfigure it.

const (
	ds4OutputReportID  = 0x05
	ds4OutputReportLen = 32
	// Byte 1 of the report says which parts of it the pad should apply.
	ds4FlagRumble = 0x01

	// A DS4 keeps vibrating until told otherwise, but a pad that loses a report
	// or a firmware watchdog would stop early; refreshing while active is cheap.
	hidRumbleRefresh = 250 * time.Millisecond
)

// ds4RumbleReport builds the output report for Moonlight's 0..65535 levels:
// low is the large (left) motor, high the small (right) one; the pad takes 8 bits.
func ds4RumbleReport(low, high uint16) []byte {
	r := make([]byte, ds4OutputReportLen)
	r[0] = ds4OutputReportID
	r[1] = ds4FlagRumble
	r[4] = byte(high >> 8) // right / fast motor
	r[5] = byte(low >> 8)  // left / heavy motor
	return r
}

type hidRumbleProtocol struct {
	name    string
	matches func(vid, pid uint16) bool
	build   func(low, high uint16) []byte
	// outLen is the output report length the pad's game collection must report
	// before the protocol is trusted for it.
	outLen uint16
}

var hidRumbleProtocols = []hidRumbleProtocol{
	{
		name: "DualShock 4",
		matches: func(vid, pid uint16) bool {
			switch vid {
			case 0x054C: // Sony: DualShock 4 v1/v2 and the USB adapter
				return pid == 0x05C4 || pid == 0x09CC || pid == 0x0BA0
			case 0x1532: // Razer Raiju Tournament Edition (wired), verified on hardware
				return pid == 0x1007
			}
			return false
		},
		build:  ds4RumbleReport,
		outLen: ds4OutputReportLen,
	},
}

func hidRumbleProtocolFor(vid, pid uint16) *hidRumbleProtocol {
	for i := range hidRumbleProtocols {
		if hidRumbleProtocols[i].matches(vid, pid) {
			return &hidRumbleProtocols[i]
		}
	}
	return nil
}

// hidRumbler drives one pad's motors and keeps them going while non-zero.
type hidRumbler struct {
	proto *hidRumbleProtocol
	w     *hidWriter

	mu        sync.Mutex
	low, high uint16
	stop      chan struct{}
	done      chan struct{}
}

// newHIDRumbler opens the pad's game collection for writing.
func newHIDRumbler(vid, pid uint16) *hidRumbler {
	proto := hidRumbleProtocolFor(vid, pid)
	if proto == nil {
		return nil
	}
	cols, err := hidCollections(vid, pid)
	if err != nil {
		logrus.Warnf("🎮 [Rumble] cannot list the HID collections of %04x:%04x: %v", vid, pid, err)
		return nil
	}
	for _, c := range cols {
		// The gamepad collection (generic desktop / game pad) that takes the report.
		if c.UsagePage != 0x01 || c.Usage != 0x05 || c.OutputLen != proto.outLen {
			continue
		}
		w, err := openHIDWriter(c.Path)
		if err != nil {
			logrus.Warnf("🎮 [Rumble] cannot open %s for writing: %v", c.Path, err)
			return nil
		}
		r := &hidRumbler{proto: proto, w: w, stop: make(chan struct{}), done: make(chan struct{})}
		go r.refresh()
		logrus.Infof("🎮 [Rumble] %04x:%04x vibrates through its %s HID output report", vid, pid, proto.name)
		return r
	}
	logrus.Warnf("🎮 [Rumble] %04x:%04x has no HID collection that takes a %s rumble report (bound to WinUSB?)", vid, pid, proto.name)
	return nil
}

func (r *hidRumbler) set(low, high uint16) {
	r.mu.Lock()
	r.low, r.high = low, high
	err := r.w.write(r.proto.build(low, high))
	r.mu.Unlock()
	if err != nil {
		logrus.Debugf("🎮 [Rumble] write failed: %v", err)
	}
}

// refresh re-sends the current levels while any motor runs.
func (r *hidRumbler) refresh() {
	defer close(r.done)
	t := time.NewTicker(hidRumbleRefresh)
	defer t.Stop()
	for {
		select {
		case <-r.stop:
			return
		case <-t.C:
			r.mu.Lock()
			if r.low != 0 || r.high != 0 {
				_ = r.w.write(r.proto.build(r.low, r.high))
			}
			r.mu.Unlock()
		}
	}
}

// close silences the motors and releases the device.
func (r *hidRumbler) close() {
	r.set(0, 0)
	close(r.stop)
	<-r.done
	r.w.close()
}

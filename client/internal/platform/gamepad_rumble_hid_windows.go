//go:build windows

package platform

import (
	"slices"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// Rumble for PlayStation-layout DirectInput pads. They are read through WinMM,
// which cannot write, and have no XInput slot, so the pad's own HID output
// report is written directly (gamepad_ps.go): a DualShock 4's report 0x05,
// which the Razer Raiju family and other DS4 clones speak, or a DualSense's
// 0x02 (USB) / 0x31 (Bluetooth), which also carries its adaptive triggers.
// Only pads listed in hidRumbleProtocols are driven: writing an output report
// the pad does not expect could reconfigure it.

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
	return ds4Output(&psOutState{low: low, high: high})
}

type hidRumbleProtocol struct {
	name    string
	family  PadFamily
	matches func(vid, pid uint16) bool
	// outLens are the output report lengths the pad's game collection may
	// report before the protocol is trusted for it (a DualSense: USB, Bluetooth).
	outLens []uint16
}

var hidRumbleProtocols = []hidRumbleProtocol{
	{
		name:   "DualShock 4",
		family: PadDS4,
		matches: func(vid, pid uint16) bool {
			switch vid {
			case 0x054C: // Sony: DualShock 4 v1/v2 and the USB adapter
				return pid == 0x05C4 || pid == 0x09CC || pid == 0x0BA0
			case 0x1532: // Razer Raiju Tournament Edition (wired), verified on hardware
				return pid == 0x1007
			}
			return false
		},
		outLens: []uint16{ds4OutputReportLen},
	},
	{
		name:    "DualSense",
		family:  PadDS5,
		matches: func(vid, pid uint16) bool { return PadFamilyOf(vid, pid).IsDualSense() },
		outLens: []uint16{ds5OutputUSBLen, ds5OutputBTLen},
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
	proto     *hidRumbleProtocol
	w         *hidWriter
	bluetooth bool // the collection takes the Bluetooth report

	mu   sync.Mutex
	st   psOutState // motors, light, trigger effects
	stop chan struct{}
	done chan struct{}
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
		if c.UsagePage != 0x01 || c.Usage != 0x05 || !slices.Contains(proto.outLens, c.OutputLen) {
			continue
		}
		w, err := openHIDWriter(c.Path)
		if err != nil {
			logrus.Warnf("🎮 [Rumble] cannot open %s for writing: %v", c.Path, err)
			return nil
		}
		r := &hidRumbler{proto: proto, w: w, bluetooth: c.OutputLen == ds5OutputBTLen, stop: make(chan struct{}), done: make(chan struct{})}
		go r.refresh()
		logrus.Infof("🎮 [Rumble] %04x:%04x vibrates through its %s HID output report", vid, pid, proto.name)
		return r
	}
	logrus.Warnf("🎮 [Rumble] %04x:%04x has no HID collection that takes a %s rumble report (bound to WinUSB?)", vid, pid, proto.name)
	return nil
}

// report is the output report for the current state; r.mu held.
func (r *hidRumbler) report() []byte {
	return psOutputReport(r.proto.family, &r.st, r.bluetooth)
}

// update changes the state with f and writes the report.
func (r *hidRumbler) update(f func(*psOutState)) {
	r.mu.Lock()
	f(&r.st)
	err := r.w.write(r.report())
	r.mu.Unlock()
	if err != nil {
		logrus.Debugf("🎮 [Rumble] write failed: %v", err)
	}
}

func (r *hidRumbler) set(low, high uint16) {
	r.update(func(st *psOutState) { st.low, st.high = low, high })
}

// setLight sets the pad's light colour.
func (r *hidRumbler) setLight(red, green, blue uint8) {
	r.update(func(st *psOutState) { st.rgb = &[3]uint8{red, green, blue} })
}

// setTriggers applies adaptive-trigger effects (a DualSense only).
func (r *hidRumbler) setTriggers(flags, typeLeft, typeRight uint8, left, right [10]byte) {
	if r.proto.family.IsDualSense() {
		r.update(func(st *psOutState) { st.setTriggers(flags, typeLeft, typeRight, left, right) })
	}
}

// noteHaptics records that the voice coils play the host's haptics.
func (r *hidRumbler) noteHaptics() {
	r.mu.Lock()
	wasOff := time.Since(r.st.hapticsAt) > 200*time.Millisecond
	r.st.hapticsAt = time.Now()
	r.mu.Unlock()
	if wasOff && r.proto.family.IsDualSense() {
		r.update(func(*psOutState) {})
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
			if r.st.low != 0 || r.st.high != 0 {
				_ = r.w.write(r.report())
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

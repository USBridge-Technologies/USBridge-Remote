//go:build linux && !android

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// A PlayStation pad's HID input report on Linux comes from the hidraw node of
// the HID device behind the pad's evdev node: hid-generic (a Razer Raiju) and
// hid-playstation (a real DualShock 4 or DualSense) both create one, and the
// logged-in user may read it (uaccess).

// psHID is the hidraw side of a PlayStation pad.
type psHID struct {
	node      string
	vid, pid  uint16
	family    PadFamily
	bluetooth bool
	// usbDir is the pad's USB device in sysfs ("" over Bluetooth): its
	// sound card is there too.
	usbDir string
}

// psHIDOf finds the PlayStation pad behind an evdev node
// ("/dev/input/eventN"), or ErrNoTouchpad.
func psHIDOf(deviceID string) (psHID, error) {
	node, vid, pid, err := ds4HIDRaw(deviceID)
	h := psHID{node: node, vid: vid, pid: pid, family: PadFamilyOf(vid, pid)}
	if err != nil {
		return h, err
	}
	base := filepath.Join("/sys/class/input", filepath.Base(deviceID), "device", "device")
	if uevent, err := os.ReadFile(filepath.Join(base, "uevent")); err == nil {
		h.bluetooth = strings.Contains(string(uevent), "HID_ID=0005:")
	}
	if !h.bluetooth {
		// .../usb1/1-1/1-1.1/1-1.1:1.3/0003:1532:1007.0055: the HID device's
		// interface, then the USB device.
		if real, err := filepath.EvalSymlinks(base); err == nil {
			h.usbDir = filepath.Dir(filepath.Dir(real))
		}
	}
	return h, nil
}

// ds4HIDRaw returns the hidraw node of the PlayStation pad behind an evdev
// node ("/dev/input/eventN"), or ErrNoTouchpad.
func ds4HIDRaw(deviceID string) (string, uint16, uint16, error) {
	ev := filepath.Base(deviceID)
	if !strings.HasPrefix(ev, "event") {
		return "", 0, 0, ErrNoTouchpad
	}
	base := filepath.Join("/sys/class/input", ev, "device")
	readHex := func(name string) uint16 {
		b, err := os.ReadFile(filepath.Join(base, "id", name))
		if err != nil {
			return 0
		}
		v, _ := strconv.ParseUint(strings.TrimSpace(string(b)), 16, 16)
		return uint16(v)
	}
	vid, pid := readHex("vendor"), readHex("product")
	if PadFamilyOf(vid, pid) == PadOther {
		return "", vid, pid, ErrNoTouchpad
	}
	nodes, _ := filepath.Glob(filepath.Join(base, "device", "hidraw", "hidraw*"))
	if len(nodes) == 0 {
		return "", vid, pid, fmt.Errorf("%w: %04x:%04x has no hidraw node", ErrNoTouchpad, vid, pid)
	}
	return "/dev/" + filepath.Base(nodes[0]), vid, pid, nil
}

// StartGamepadHID reads the HID input report of a PlayStation pad (its evdev
// node) and hands each one to onReport, from its own goroutine. It returns
// ErrNoTouchpad for any other pad.
func StartGamepadHID(deviceID string, onReport func(DS4Report)) (*TouchpadCapture, error) {
	h, err := psHIDOf(deviceID)
	if err != nil {
		return nil, err
	}
	node, vid, pid := h.node, h.vid, h.pid
	f, err := os.Open(node)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", node, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 128)
		for {
			n, err := f.Read(buf)
			if err != nil {
				if !errors.Is(err, os.ErrClosed) {
					logrus.Debugf("🎮 [Touchpad] %s: %v", node, err)
				}
				return // closed by Stop, or the pad went away
			}
			if r, ok := ParsePadReport(buf[:n], h.family); ok {
				onReport(r)
			}
		}
	}()
	logrus.Infof("🎮 [Touchpad] %s (%04x:%04x): reading its HID report from %s (touchpad, motion, battery)", deviceID, vid, pid, node)
	return &TouchpadCapture{stop: func() {
		f.Close() // a hidraw node is pollable: the blocked Read returns
		<-done
		logrus.Infof("🎮 [Touchpad] stopped for %s", deviceID)
	}}, nil
}

// A PlayStation pad's motors, lightbar and adaptive triggers are driven
// through its hidraw node with its own output report (gamepad_ps.go), as on
// Windows: hid-generic (a Razer Raiju) gives such a pad no evdev force
// feedback at all, and no driver has the triggers. All of it shares one
// report, so the host's last wishes are kept per pad and every write carries
// them.
var ds4Out = struct {
	sync.Mutex
	pads map[string]*psOutState
}{pads: map[string]*psOutState{}}

// writeDS4Output changes a PlayStation pad's output state with f and writes
// it; false if id is not such a pad or the write failed.
func writeDS4Output(id string, f func(*psOutState)) bool {
	h, err := psHIDOf(id)
	if err != nil {
		return false
	}
	node := h.node
	ds4Out.Lock()
	defer ds4Out.Unlock()
	st := ds4Out.pads[id]
	if st == nil {
		st = &psOutState{}
		ds4Out.pads[id] = st
	}
	f(st)
	rep := psOutputReport(h.family, st, h.bluetooth)
	w, err := os.OpenFile(node, os.O_WRONLY, 0)
	if err != nil {
		logrus.Debugf("🎮 [Rumble] open %s: %v", node, err)
		return false
	}
	defer w.Close()
	if _, err := w.Write(rep); err != nil {
		logrus.Debugf("🎮 [Rumble] write %s: %v", node, err)
		return false
	}
	return true
}

// forgetDS4Output drops a pad's kept output state (it stopped being captured).
func forgetDS4Output(id string) {
	ds4Out.Lock()
	delete(ds4Out.pads, id)
	ds4Out.Unlock()
}

// SetGamepadLight sets the lightbar of a PlayStation pad, keeping its motors
// as they are. Other pads have no light.
func SetGamepadLight(id string, r, g, b uint8) {
	writeDS4Output(id, func(st *psOutState) { st.rgb = &[3]uint8{r, g, b} })
}

// SetGamepadTriggers applies the host's adaptive-trigger effects to a
// DualSense (IDX_DS_ADAPTIVE_TRIGGERS); other pads have none.
func SetGamepadTriggers(id string, flags, typeLeft, typeRight uint8, left, right [10]byte) {
	if h, err := psHIDOf(id); err != nil || !h.family.IsDualSense() {
		return
	}
	writeDS4Output(id, func(st *psOutState) { st.setTriggers(flags, typeLeft, typeRight, left, right) })
}

// NoteGamepadHaptics records that the host's haptics play on a DualSense's
// voice coils, so its motors' rumble emulation stays off.
func NoteGamepadHaptics(id string) {
	ds4Out.Lock()
	st := ds4Out.pads[id]
	if st == nil {
		st = &psOutState{}
		ds4Out.pads[id] = st
	}
	wasOff := time.Since(st.hapticsAt) > 200*time.Millisecond
	st.hapticsAt = time.Now()
	ds4Out.Unlock()
	if wasOff {
		writeDS4Output(id, func(*psOutState) {})
	}
}

// GamepadFamily is the PlayStation family of the pad behind an evdev node,
// PadOther for any other pad.
func GamepadFamily(deviceID string) PadFamily {
	h, err := psHIDOf(deviceID)
	if err != nil {
		return PadOther
	}
	return h.family
}

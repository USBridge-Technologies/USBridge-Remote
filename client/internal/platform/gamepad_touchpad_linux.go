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

	"github.com/sirupsen/logrus"
)

// A DualShock 4-layout pad's HID input report on Linux comes from the hidraw
// node of the HID device behind the pad's evdev node: hid-generic (a Razer
// Raiju) and hid-playstation (a real DualShock 4) both create one, and the
// logged-in user may read it (uaccess).

// ds4HIDRaw returns the hidraw node of the DS4-layout pad behind an evdev
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
	if !isDS4Family(vid, pid) {
		return "", vid, pid, ErrNoTouchpad
	}
	nodes, _ := filepath.Glob(filepath.Join(base, "device", "hidraw", "hidraw*"))
	if len(nodes) == 0 {
		return "", vid, pid, fmt.Errorf("%w: %04x:%04x has no hidraw node", ErrNoTouchpad, vid, pid)
	}
	return "/dev/" + filepath.Base(nodes[0]), vid, pid, nil
}

// StartGamepadHID reads the HID input report of a DualShock 4-layout pad
// (its evdev node) and hands each one to onReport, from its own goroutine. It
// returns ErrNoTouchpad for any other pad.
func StartGamepadHID(deviceID string, onReport func(DS4Report)) (*TouchpadCapture, error) {
	node, vid, pid, err := ds4HIDRaw(deviceID)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(node)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", node, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 64)
		for {
			n, err := f.Read(buf)
			if err != nil {
				if !errors.Is(err, os.ErrClosed) {
					logrus.Debugf("🎮 [Touchpad] %s: %v", node, err)
				}
				return // closed by Stop, or the pad went away
			}
			if r, ok := ParseDS4Report(buf[:n]); ok {
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

// A DualShock 4-layout pad's motors and lightbar are driven through its
// hidraw node with output report 0x05, as on Windows: hid-generic (a Razer
// Raiju) gives such a pad no evdev force feedback at all. Both share one
// report, so the last motor levels and colour are kept per pad and every
// write carries both.
var ds4Out = struct {
	sync.Mutex
	pads map[string]*ds4OutState
}{pads: map[string]*ds4OutState{}}

type ds4OutState struct {
	low, high uint16
	rgb       *[3]uint8
}

// writeDS4Output changes a DS4-layout pad's output state with f and writes
// it; false if id is not such a pad or the write failed.
func writeDS4Output(id string, f func(*ds4OutState)) bool {
	node, _, _, err := ds4HIDRaw(id)
	if err != nil {
		return false
	}
	ds4Out.Lock()
	defer ds4Out.Unlock()
	st := ds4Out.pads[id]
	if st == nil {
		st = &ds4OutState{}
		ds4Out.pads[id] = st
	}
	f(st)
	rep := make([]byte, 32)
	rep[0], rep[1] = 0x05, 0x01
	rep[4] = byte(st.high >> 8) // right / fast motor
	rep[5] = byte(st.low >> 8)  // left / heavy motor
	if st.rgb != nil {
		rep[1] |= 0x02
		rep[6], rep[7], rep[8] = st.rgb[0], st.rgb[1], st.rgb[2]
	}
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

// SetGamepadLight sets the lightbar of a DualShock 4-layout pad, keeping its
// motors as they are. Other pads have no light.
func SetGamepadLight(id string, r, g, b uint8) {
	writeDS4Output(id, func(st *ds4OutState) { st.rgb = &[3]uint8{r, g, b} })
}

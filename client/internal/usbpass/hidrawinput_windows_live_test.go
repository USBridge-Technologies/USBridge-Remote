//go:build windows

package usbpass

import (
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestLiveRawInput shows what Raw Input delivers for a device the OS holds
// exclusively (a pen tablet). Skipped unless USBRIDGE_RAWINPUT_LIVE=<vid>:<pid>
// (hex, e.g. 056a:0374). Draw with the pen during the run.
func TestLiveRawInput(t *testing.T) {
	spec := os.Getenv("USBRIDGE_RAWINPUT_LIVE")
	if spec == "" {
		t.Skip("USBRIDGE_RAWINPUT_LIVE not set")
	}
	var vid, pid uint16
	if _, err := fmt.Sscanf(spec, "%x:%x", &vid, &pid); err != nil {
		t.Fatalf("bad USBRIDGE_RAWINPUT_LIVE %q: %v", spec, err)
	}

	type stat struct {
		typ     uint32
		count   int
		lens    map[int]int
		first   [][]byte
		firstAt time.Time
		lastAt  time.Time
	}
	var mu sync.Mutex
	stats := map[string]*stat{}
	src, err := startRawInput(
		[]uint16{0x0D, 0xFF00},                                // digitizers/pens, vendor-defined
		[][2]uint16{{0x01, 0x02}, {0x01, 0x04}, {0x01, 0x05}}, // mouse, joystick, gamepad
		func(ev rawInputEvent) {
			if !rawInputMatchesUSB(ev.Path, vid, pid) {
				return
			}
			mu.Lock()
			defer mu.Unlock()
			key := ev.Path
			if i := strings.Index(key, "#{"); i > 0 {
				key = key[:i]
			}
			s := stats[key]
			if s == nil {
				s = &stat{typ: ev.Type, lens: map[int]int{}, firstAt: ev.When}
				stats[key] = s
			}
			s.lastAt = ev.When
			if ev.Type == rimTypeHID {
				for _, r := range ev.Reports {
					s.count++
					s.lens[len(r)]++
					if len(s.first) < 4 {
						s.first = append(s.first, r)
					}
				}
			} else {
				s.count++
			}
		})
	if err != nil {
		t.Fatalf("startRawInput: %v", err)
	}
	t.Logf("listening for %04x:%04x raw input for 15s: DRAW WITH THE PEN NOW", vid, pid)
	time.Sleep(15 * time.Second)
	src.Stop()

	mu.Lock()
	defer mu.Unlock()
	if len(stats) == 0 {
		t.Fatal("no raw input received from the device")
	}
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		s := stats[k]
		kind := map[uint32]string{rimTypeMouse: "MOUSE", rimTypeKeyboard: "KEYBOARD", rimTypeHID: "HID"}[s.typ]
		rate := float64(s.count) / (s.lastAt.Sub(s.firstAt).Seconds() + 0.001)
		t.Logf("%s\n    type=%s reports=%d (~%.0f/s) sizes=%v", k, kind, s.count, rate, s.lens)
		for _, r := range s.first {
			t.Logf("    first: %s", strings.ToUpper(hex.EncodeToString(r)))
		}
	}
}

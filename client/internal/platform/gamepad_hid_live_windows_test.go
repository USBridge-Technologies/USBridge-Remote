//go:build windows

package platform

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

var procBeep = windows.NewLazySystemDLL("kernel32.dll").NewProc("Beep")

func beep(freq, ms uint32) { procBeep.Call(uintptr(freq), uintptr(ms)) }

func parseVIDPID(t *testing.T, spec string) (uint16, uint16) {
	t.Helper()
	var vid, pid uint64
	if _, err := fmtSscanHex(spec, &vid, &pid); err != nil {
		t.Fatal(err)
	}
	return uint16(vid), uint16(pid)
}

// Raw input reports of a pad's game collection, printing only the bytes that
// changed. Finds which bit is the PS button or the touchpad click.
//
//	USBRIDGE_HID_INPUT=1532:1007 USBRIDGE_HID_INPUT_SECS=20 go test -v -run TestLiveHIDInput ./internal/platform/
func TestLiveHIDInput(t *testing.T) {
	spec := os.Getenv("USBRIDGE_HID_INPUT")
	if spec == "" {
		t.Skip("set USBRIDGE_HID_INPUT=vvvv:pppp (hex)")
	}
	secs := 15
	if v, err := strconv.Atoi(os.Getenv("USBRIDGE_HID_INPUT_SECS")); err == nil && v > 0 {
		secs = v
	}
	vid, pid := parseVIDPID(t, spec)
	cols, err := hidCollections(vid, pid)
	if err != nil {
		t.Fatal(err)
	}
	var path string
	var inLen uint16
	for _, c := range cols {
		if c.UsagePage == 0x01 && c.Usage == 0x05 {
			path, inLen = c.Path, c.InputLen
		}
	}
	if path == "" {
		t.Fatal("no gamepad collection")
	}
	h, err := openHIDPath(path, true)
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	closed := false
	go func() {
		time.Sleep(time.Duration(secs) * time.Second)
		mu.Lock()
		closed = true
		mu.Unlock()
		windows.CloseHandle(h)
	}()
	beep(600, 200)
	buf := make([]byte, inLen)
	var base []byte
	last := map[int]byte{}
	start := time.Now()
	for {
		var n uint32
		if err := windows.ReadFile(h, buf, &n, nil); err != nil {
			break
		}
		rep := append([]byte(nil), buf[:n]...)
		if base == nil {
			base = rep
			t.Logf("baseline id=%#x len=%d: % x", rep[0], len(rep), rep[:min(len(rep), 16)])
			for i := range rep {
				last[i] = rep[i]
			}
			continue
		}
		// DS4-family report: byte 7 bit0 = PS button, bit1 = touchpad click; bytes 33..42 =
		// touchpad packet count and the two fingers' contact/x/y (4 bytes each).
		var diffs []string
		if len(rep) > 7 && rep[7]&0x03 != last[7]&0x03 {
			diffs = append(diffs, fmt.Sprintf("byte7 ps/click %02b->%02b", last[7]&0x03, rep[7]&0x03))
		}
		last[7] = rep[7]
		if len(rep) > 42 {
			now, was := rep[33:43], make([]byte, 10)
			for i := range was {
				was[i] = last[33+i]
			}
			if string(now) != string(was) {
				diffs = append(diffs, fmt.Sprintf("touch[33..42] % x", now))
			}
			for i := range now {
				last[33+i] = now[i]
			}
		}
		if len(diffs) > 0 {
			t.Logf("%5.1fs %s", time.Since(start).Seconds(), strings.Join(diffs, "; "))
		}
	}
	mu.Lock()
	defer mu.Unlock()
	_ = closed
}

// Vibrates each pad in turn so the result can be felt. Each step is announced by
// a beep: both motors, then only the large one, then only the small one.
//
//	USBRIDGE_RUMBLE_LIVE="xinput:0,winmm:0" go test -v -run TestLiveRumble ./internal/platform/
func TestLiveRumble(t *testing.T) {
	ids := os.Getenv("USBRIDGE_RUMBLE_LIVE")
	if ids == "" {
		t.Skip(`set USBRIDGE_RUMBLE_LIVE="xinput:0,winmm:0"`)
	}
	for _, id := range strings.Split(ids, ",") {
		for _, step := range []struct {
			name      string
			low, high uint16
		}{{"both motors", 40000, 40000}, {"large motor only", 50000, 0}, {"small motor only", 0, 50000}} {
			t.Logf("%s: %s", id, step.name)
			beep(1000, 150)
			time.Sleep(400 * time.Millisecond)
			SetGamepadRumble(id, step.low, step.high)
			time.Sleep(1500 * time.Millisecond)
			SetGamepadRumble(id, 0, 0)
			time.Sleep(1200 * time.Millisecond)
		}
		StopGamepadRumble(id)
		beep(500, 300)
		time.Sleep(1500 * time.Millisecond)
	}
}

// Compares rumble report variants at full power on a DS4-layout pad, to see which
// the pad feels strongest with (each step announced by a beep, then 1.5 s):
//
//	USBRIDGE_DS4_FLAGS=1532:1007 go test -v -run TestLiveDS4Flags ./internal/platform/
func TestLiveDS4Flags(t *testing.T) {
	spec := os.Getenv("USBRIDGE_DS4_FLAGS")
	if spec == "" {
		t.Skip("set USBRIDGE_DS4_FLAGS=vvvv:pppp (hex)")
	}
	vid, pid := parseVIDPID(t, spec)
	cols, err := hidCollections(vid, pid)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cols {
		if c.UsagePage != 0x01 || c.Usage != 0x05 || c.OutputLen != 32 {
			continue
		}
		w, err := openHIDWriter(c.Path)
		if err != nil {
			t.Fatal(err)
		}
		defer w.close()
		for _, v := range []struct {
			name  string
			flags byte
		}{{"flags 0x01 (rumble only), full power", 0x01}, {"flags 0x07 (rumble+lightbar+flash), full power", 0x07}, {"flags 0xF1, full power", 0xF1}} {
			t.Logf("%s", v.name)
			beep(1000, 150)
			time.Sleep(500 * time.Millisecond)
			end := time.Now().Add(1500 * time.Millisecond)
			for time.Now().Before(end) {
				r := make([]byte, 32)
				r[0], r[1], r[4], r[5] = 0x05, v.flags, 0xFF, 0xFF
				if err := w.write(r); err != nil {
					t.Fatal(err)
				}
				time.Sleep(100 * time.Millisecond)
			}
			w.write(append([]byte{0x05, v.flags}, make([]byte, 30)...))
			time.Sleep(1500 * time.Millisecond)
		}
	}
}

// The touchpad-as-mouse reader on a real pad; prints what would be sent to the host.
//
//	USBRIDGE_TOUCHPAD_LIVE=winmm:0 go test -v -run TestLiveTouchpad ./internal/platform/
func TestLiveTouchpad(t *testing.T) {
	id := os.Getenv("USBRIDGE_TOUCHPAD_LIVE")
	if id == "" {
		t.Skip("set USBRIDGE_TOUCHPAD_LIVE=winmm:N")
	}
	var mu sync.Mutex
	var sumX, sumY, moves, clicks int
	tp, err := StartGamepadTouchpad(id, func(e TouchpadEvent) {
		mu.Lock()
		defer mu.Unlock()
		if e.DX != 0 || e.DY != 0 {
			sumX += e.DX
			sumY += e.DY
			moves++
		}
		if e.ClickChanged {
			clicks++
			t.Logf("left button %v", map[bool]string{true: "DOWN", false: "UP"}[e.ClickDown])
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	beep(600, 200)
	time.Sleep(15 * time.Second)
	tp.Stop()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("moves=%d net dx=%d dy=%d click edges=%d", moves, sumX, sumY, clicks)
}

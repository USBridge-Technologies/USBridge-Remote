//go:build windows

package platform

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

// StartGamepadHID reads the HID input report of a DualShock 4-layout pad
// ("winmm:N") and hands each one to onReport, from its own goroutine. It
// returns ErrNoTouchpad for any other pad.
func StartGamepadHID(deviceID string, onReport func(DS4Report)) (*TouchpadCapture, error) {
	joyID, err := strconv.Atoi(strings.TrimPrefix(deviceID, "winmm:"))
	if err != nil || !strings.HasPrefix(deviceID, "winmm:") {
		return nil, ErrNoTouchpad
	}
	vid, pid, ok := winmmVIDPIDOf(uintptr(joyID))
	if !ok || !isDS4Family(vid, pid) {
		return nil, ErrNoTouchpad
	}
	cols, err := hidCollections(vid, pid)
	if err != nil {
		return nil, err
	}
	var path string
	var inLen uint16
	for _, c := range cols {
		if c.UsagePage == 0x01 && c.Usage == 0x05 && int(c.InputLen) >= ds4TouchMinReport {
			path, inLen = c.Path, c.InputLen
			break
		}
	}
	if path == "" {
		return nil, fmt.Errorf("%w: no HID game collection with a touchpad report (is the pad bound to WinUSB?)", ErrNoTouchpad)
	}
	h, err := openHIDPath(path, true)
	if err != nil {
		return nil, fmt.Errorf("open the pad's HID collection: %w", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, inLen)
		for {
			var n uint32
			if err := windows.ReadFile(h, buf, &n, nil); err != nil {
				return // cancelled by Stop, or the pad went away
			}
			if r, ok := ParseDS4Report(buf[:n]); ok {
				onReport(r)
			}
		}
	}()
	logrus.Infof("🎮 [Touchpad] %s (%04x:%04x): reading its HID report (touchpad, motion, battery)", deviceID, vid, pid)
	return &TouchpadCapture{stop: func() {
		windows.CancelIoEx(h, nil)
		<-done
		windows.CloseHandle(h)
		logrus.Infof("🎮 [Touchpad] stopped for %s", deviceID)
	}}, nil
}

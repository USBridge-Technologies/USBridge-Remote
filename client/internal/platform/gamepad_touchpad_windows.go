//go:build windows

package platform

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

// StartGamepadTouchpad reads the touchpad of a DualShock 4-layout pad
// ("winmm:N") from its HID input report and reports it as relative mouse
// motion and a left click. It returns ErrNoTouchpad for any other pad.
func StartGamepadTouchpad(deviceID string, onEvent func(TouchpadEvent)) (*TouchpadCapture, error) {
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
		var dec touchpadDecoder
		buf := make([]byte, inLen)
		for {
			var n uint32
			if err := windows.ReadFile(h, buf, &n, nil); err != nil {
				return // cancelled by Stop, or the pad went away
			}
			dec.feed(buf[:n], onEvent)
		}
	}()
	logrus.Infof("🎮 [Touchpad] %s (%04x:%04x) touchpad is a relative mouse", deviceID, vid, pid)
	return &TouchpadCapture{stop: func() {
		windows.CancelIoEx(h, nil)
		<-done
		windows.CloseHandle(h)
		logrus.Infof("🎮 [Touchpad] stopped for %s", deviceID)
	}}, nil
}

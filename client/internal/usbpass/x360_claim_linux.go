//go:build linux

package usbpass

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

var sysfsBusIDRe = regexp.MustCompile(`^[0-9]+-[0-9]+(\.[0-9]+)*$`)

func readSysfs(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// xboxPadKind reports which Xbox protocol the device at busID speaks, from its
// interface class triples: "gip" (Xbox One/Series, FF/47/D0), "360" (FF/5D/01),
// or "" for anything else.
func xboxPadKind(busID string) string {
	ifaces, _ := filepath.Glob("/sys/bus/usb/devices/" + busID + ":*")
	for _, dir := range ifaces {
		class := strings.ToLower(readSysfs(filepath.Join(dir, "bInterfaceClass")))
		sub := strings.ToLower(readSysfs(filepath.Join(dir, "bInterfaceSubClass")))
		proto := strings.ToLower(readSysfs(filepath.Join(dir, "bInterfaceProtocol")))
		switch {
		case class == "ff" && sub == "47" && proto == "d0":
			return "gip"
		case class == "ff" && sub == "5d" && proto == "01":
			return "360"
		}
	}
	return ""
}

// findPadEvdev opens the gamepad evdev node the kernel created for the device.
// xpad may still be (re)binding right after the previous holder let go, so it
// retries briefly.
func findPadEvdev(busID string) (*evdevPad, error) {
	var lastErr error
	for attempt := 0; attempt < 12; attempt++ {
		nodes, _ := filepath.Glob("/sys/bus/usb/devices/" + busID + ":*/input/input*/event*")
		for _, n := range nodes {
			p, err := openEvdevPad(filepath.Join("/dev/input", filepath.Base(n)))
			if err == nil {
				return p, nil
			}
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil, lastErr
}

// tryClaimX360 exports an Xbox pad as the synthetic Xbox 360 controller fed from
// the kernel's evdev node for it. handled=false with a nil error means "not an
// Xbox pad, or the kernel exposes no readable gamepad for it": take the libusb
// path.
func tryClaimX360(dev *ExportedDevice) (handled bool, err error) {
	if !x360Enabled() || !sysfsBusIDRe.MatchString(dev.BusID) {
		return false, nil
	}
	kind := xboxPadKind(dev.BusID)
	if kind == "" {
		return false, nil
	}
	pad, err := findPadEvdev(dev.BusID)
	if err != nil || pad == nil {
		logrus.Warnf("usbpass: x360: %s is an Xbox pad (%s) but no readable evdev gamepad was found (%v); falling back to the raw claim",
			dev.BusID, kind, err)
		return false, nil
	}
	pad.grab()

	b := NewX360Backend(x360SerialLinux(dev.BusID), func(c X360Command) {
		if c.Rumble {
			pad.rumble(c.Left, c.Right)
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		pad.run(b.SetState)
	}()
	b.onClose = func() {
		pad.close() // unblocks run
		<-done
	}

	b.applyTo(dev)
	logrus.Infof("usbpass: x360: %s (Xbox %s pad) exported as Xbox 360 controller %04x:%04x from %s",
		dev.BusID, kind, dev.VID, dev.PID, pad.name)
	return true, nil
}

// x360SerialLinux uses the pad's own USB serial when it has one, so the
// importer's Windows keeps one device identity across remounts.
func x360SerialLinux(busID string) string {
	s := readSysfs("/sys/bus/usb/devices/" + busID + "/serial")
	if len(s) > 16 {
		s = s[len(s)-16:]
	}
	return s
}

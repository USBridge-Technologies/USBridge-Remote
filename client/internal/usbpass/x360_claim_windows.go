//go:build windows

package usbpass

import (
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

// x360Poll is how often the local XInput pad is sampled: faster than the 4 ms
// (250 Hz) interval the synthetic endpoint advertises would only queue reports.
const x360Poll = 4 * time.Millisecond

// usbSetupClass returns the setup class name Windows gave the USB device node
// with the given instance ID ("XboxComposite", "HIDClass", ...), or "".
func usbSetupClass(instanceID string) string {
	set, err := windows.SetupDiGetClassDevsEx(nil, "USB", 0, windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES, 0, "")
	if err != nil {
		return ""
	}
	defer set.Close()
	for i := 0; ; i++ {
		data, err := set.EnumDeviceInfo(i)
		if err != nil {
			return ""
		}
		id, err := set.DeviceInstanceID(data)
		if err != nil || !strings.EqualFold(id, instanceID) {
			continue
		}
		return setupDiStringProperty(set, data, windows.SPDRP_CLASS)
	}
}

// isXboxSetupClass matches the setup classes Windows uses for Xbox pads: the
// One/Series family (GIP, xboxgip.sys) is XboxComposite, a wired 360 pad
// (xusb22.sys) is XnaComposite.
func isXboxSetupClass(class string) bool {
	c := strings.ToLower(class)
	return strings.HasPrefix(c, "xbox") || strings.HasPrefix(c, "xna")
}

// tryClaimX360 exports an Xbox pad as the synthetic Xbox 360 controller fed
// from the local XInput slot. handled=false with a nil error means "not an Xbox
// pad, or none is readable through XInput": take the HID/libusb path.
func tryClaimX360(dev *ExportedDevice) (handled bool, err error) {
	if !x360Enabled() || !strings.HasPrefix(strings.ToUpper(dev.InstanceID), "USB\\") {
		return false, nil
	}
	class := usbSetupClass(dev.InstanceID)
	if !isXboxSetupClass(class) {
		return false, nil
	}
	slot := xinputFirstConnected()
	if slot < 0 {
		logrus.Infof("usbpass: x360: %s is an Xbox pad (%s) but XInput sees no controller; falling back", dev.InstanceID, class)
		return false, nil
	}

	// The slot can change (pad replugged, another connected): follow the first
	// connected one, and rumble goes to whichever pad is being read.
	var cur = slot
	b := NewX360Backend(x360Serial(dev.InstanceID), func(c X360Command) {
		if c.Rumble {
			xinputRumble(cur, c.Left, c.Right)
		}
	})
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		tick := time.NewTicker(x360Poll)
		defer tick.Stop()
		lastProbe := time.Now()
		for {
			select {
			case <-stop:
				xinputRumble(cur, 0, 0)
				return
			case <-tick.C:
			}
			if s, ok := xinputRead(cur); ok {
				b.SetState(s)
				continue
			}
			b.SetState(X360State{}) // pad gone: release everything, do not leave a stuck input
			if time.Since(lastProbe) > 500*time.Millisecond {
				lastProbe = time.Now()
				if n := xinputFirstConnected(); n >= 0 {
					cur = n
				}
			}
		}
	}()
	b.onClose = func() {
		close(stop)
		<-done
	}

	b.applyTo(dev)
	logrus.Infof("usbpass: x360: %s (%s) exported as Xbox 360 controller %04x:%04x from XInput slot %d",
		dev.InstanceID, class, dev.VID, dev.PID, slot)
	return true, nil
}

// x360Serial derives a stable USB serial from the physical pad's instance ID so
// the importer's Windows keeps one device identity across remounts.
func x360Serial(instanceID string) string {
	if i := strings.LastIndex(instanceID, "\\"); i >= 0 && i+1 < len(instanceID) {
		s := instanceID[i+1:]
		if len(s) > 16 {
			s = s[len(s)-16:]
		}
		return s
	}
	return ""
}

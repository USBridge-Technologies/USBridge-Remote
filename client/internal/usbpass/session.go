package usbpass

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"usbridge-client/internal/models"
)

// claimTimeout bounds one libusb claim attempt. A stick that stopped
// answering on the bus (interface left with no driver, descriptors
// unreadable) blocks inside cgo forever, which froze the Devices panel with
// no error and no way out.
const claimTimeout = 20 * time.Second

var errClaimTimeout = errors.New("libusb claim timed out")

// Session owns the local USB/IP export for one (or more) mounted devices.
type Session struct {
	mu     sync.Mutex
	server *Server
	addr   string
	busIDs []string
}

var (
	sessionMu sync.Mutex
	active    *Session
)

func isGousbDisabled(err error) bool {
	return err != nil && strings.Contains(err.Error(), "gousb claim disabled")
}

func closeExported(devs []*ExportedDevice) {
	closeExportedExcept(devs, -1)
}

// closeExportedExcept skips one index whose ownership has moved to the
// watcher goroutine of an abandoned claim (see claimWithTimeout).
func closeExportedExcept(devs []*ExportedDevice, skip int) {
	for i, d := range devs {
		if i == skip {
			continue
		}
		if d != nil && d.Backend != nil {
			_ = d.Backend.Close()
			d.Backend = nil
		}
	}
}

// claimWithTimeout runs TryClaimGousb under claimTimeout. The libusb call
// cannot be cancelled, so on timeout ed is handed to a watcher goroutine that
// releases it if the claim ever completes; the caller must not touch ed again.
func claimWithTimeout(ed *ExportedDevice) error {
	done := make(chan error, 1)
	go func() { done <- TryClaimGousb(ed) }()
	select {
	case err := <-done:
		return err
	case <-time.After(claimTimeout):
		go func() {
			if err := <-done; err == nil {
				logrus.Warnf("usbpass: abandoned claim for %s completed late; releasing", ed.BusID)
				if ed.Backend != nil {
					_ = ed.Backend.Close()
				}
			}
		}()
		return errClaimTimeout
	}
}

// claimDevice claims one device, retrying once behind a pkexec grant (udev
// rule + kernel-driver unbind) when the first attempt fails, and once more
// behind a port power-cycle when the device is wedged (stopped responding
// on the bus — see docs/USB_PASSTHROUGH.md's postmortem; a leftover claim
// from a crashed/killed previous client session reliably reproduces this).
// use is the ExportedDevice the caller should keep going forward: normally
// ed itself, but a fresh one after a power-cycle retry, since ed may still
// be touched later by an abandoned watcher goroutine (see claimWithTimeout)
// and must not be reused. abandoned reports that use's own claim is stuck
// and use must not be reused either.
func claimDevice(ed *ExportedDevice, ref usbDevRef) (use *ExportedDevice, abandoned bool, err error) {
	wedged := func(id string) error {
		return fmt.Errorf("libusb claim %s timed out after %s: the device stopped responding on the bus, unplug and replug it",
			id, claimTimeout)
	}

	logrus.Infof("usbpass: claiming %s (busnum=%d devnum=%d)", ed.BusID, ed.Busnum, ed.Devnum)
	err = claimWithTimeout(ed)
	if errors.Is(err, errClaimTimeout) {
		logrus.Warnf("usbpass: claim %s timed out (wedged); power-cycling the port and retrying", ed.BusID)
		if pcErr := powerCycleUSBPort(ed.BusID); pcErr != nil {
			logrus.Warnf("usbpass: power-cycle %s failed: %v", ed.BusID, pcErr)
			return ed, true, wedged(ed.BusID)
		}
		fresh := NewExportedFromVIDPID(ed.BusID, ed.VID, ed.PID)
		if accErr := EnsureUSBAccess([]usbDevRef{{BusID: fresh.BusID, Busnum: fresh.Busnum, Devnum: fresh.Devnum}}); accErr != nil {
			return fresh, false, fmt.Errorf("USB access after power-cycle: %w", accErr)
		}
		err = claimWithTimeout(fresh)
		if errors.Is(err, errClaimTimeout) {
			return fresh, true, wedged(fresh.BusID)
		}
		if err != nil && !isGousbDisabled(err) {
			return fresh, false, fmt.Errorf("libusb claim %s after power-cycle (busnum=%d devnum=%d): %w",
				fresh.BusID, fresh.Busnum, fresh.Devnum, err)
		}
		logrus.Infof("usbpass: claim %s recovered after power-cycle", fresh.BusID)
		return fresh, false, err
	}
	if err != nil && !isGousbDisabled(err) {
		logrus.Warnf("usbpass: claim %s failed (%v); requesting unbind/grant via pkexec", ed.BusID, err)
		if !RequestUSBAccess([]usbDevRef{ref}) {
			if msg := LastUSBAccessError(); msg != "" {
				return ed, false, fmt.Errorf("USB access: %s", msg)
			}
			return ed, false, fmt.Errorf("USB access was not granted for %s", ed.BusID)
		}
		err = claimWithTimeout(ed)
		if errors.Is(err, errClaimTimeout) {
			return ed, true, wedged(ed.BusID)
		}
	}
	if err != nil && !isGousbDisabled(err) {
		return ed, false, fmt.Errorf("libusb claim %s (busnum=%d devnum=%d): %w", ed.BusID, ed.Busnum, ed.Devnum, err)
	}
	return ed, false, err
}

// ActiveBusIDs returns Linux busids currently exported by the local session
// (empty when nothing is mounted). Used by the Devices UI for the green
// "mounted" marker — gadget devices come from GetDeviceInfo, passthrough
// does not.
func ActiveBusIDs() []string {
	sessionMu.Lock()
	s := active
	sessionMu.Unlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.busIDs))
	copy(out, s.busIDs)
	return out
}

// StartSession exports the given passthrough devices on listenAddr
// (default 0.0.0.0:3240) and stores the session globally.
func StartSession(listenAddr string, devices []models.USBPassthroughDevice) (*Session, error) {
	if listenAddr == "" {
		listenAddr = "0.0.0.0:3240"
	}
	// Drop previous export/attach first so remount does not stack VHCI
	// sessions and so libusb can re-claim the stick.
	StopSession()

	var exported []*ExportedDevice
	var accessRefs []usbDevRef
	var busIDs []string
	for _, d := range devices {
		if d.Protected {
			return nil, fmt.Errorf("refusing protected device %s:%s", d.VID, d.PID)
		}
		vid, pid, err := ParseVIDPID(d.VID, d.PID)
		if err != nil {
			return nil, err
		}
		busID := d.BusID
		if busID == "" {
			busID = StableUSBIPBusID(d.InstanceID)
		}
		ed := NewExportedFromVIDPID(busID, vid, pid)
		accessRefs = append(accessRefs, usbDevRef{BusID: busID, Busnum: ed.Busnum, Devnum: ed.Devnum})
		exported = append(exported, ed)
		busIDs = append(busIDs, busID)
	}
	if len(exported) == 0 {
		return nil, fmt.Errorf("no devices to export")
	}

	// Linux: only pkexec when usbfs nodes are not openable / udev rule missing.
	// Do NOT block on pkexec just to unbind — try claim first (SetAutoDetach),
	// then RequestUSBAccess (unbind) on failure. That removes the "pkexec
	// dialog while nothing is exporting yet" race on every remount.
	if err := EnsureUSBAccess(accessRefs); err != nil {
		return nil, fmt.Errorf("USB access: %w", err)
	}
	for i, ed := range exported {
		used, abandoned, err := claimDevice(ed, accessRefs[i])
		if used != ed {
			// A wedged claim was recovered via power-cycle onto a fresh
			// ExportedDevice (ed's original claim may still complete
			// asynchronously in an abandoned watcher goroutine).
			exported[i] = used
		}
		if err != nil {
			if isGousbDisabled(err) {
				logrus.Warnf("usbpass: descriptor-only export for %s (busnum=%d devnum=%d): %v — bulk URB will EPIPE",
					used.BusID, used.Busnum, used.Devnum, err)
				continue
			}
			if abandoned {
				closeExportedExcept(exported, i)
			} else {
				closeExported(exported)
			}
			return nil, err
		}
		logrus.Infof("usbpass: live libusb claim for %s (busnum=%d devnum=%d)", used.BusID, used.Busnum, used.Devnum)
	}

	srv, err := StartExport(listenAddr, exported)
	if err != nil {
		closeExported(exported)
		return nil, err
	}
	s := &Session{server: srv, addr: listenAddr, busIDs: busIDs}
	sessionMu.Lock()
	active = s
	sessionMu.Unlock()
	return s, nil
}

// StopSession tears down the active export and kills a pending Attach.
func StopSession() {
	StopAttach()
	sessionMu.Lock()
	s := active
	active = nil
	sessionMu.Unlock()
	if s != nil {
		s.server.Stop()
	}
}

// Devices exposes the live exported devices for terminal diagnostics.
func (s *Session) Devices() []*ExportedDevice {
	s.mu.Lock()
	srv := s.server
	s.mu.Unlock()
	if srv == nil {
		return nil
	}
	return srv.Devices()
}

func (s *Session) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

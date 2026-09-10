package usbpass

import (
	"fmt"
	"strings"
	"sync"

	"github.com/sirupsen/logrus"
	"usbridge-client/internal/models"
)

// Session owns the local USB/IP export for one (or more) mounted devices.
type Session struct {
	mu     sync.Mutex
	server *Server
	addr   string
}

var (
	sessionMu sync.Mutex
	active    *Session
)

func isGousbDisabled(err error) bool {
	return err != nil && strings.Contains(err.Error(), "gousb claim disabled")
}

func closeExported(devs []*ExportedDevice) {
	for _, d := range devs {
		if d != nil && d.Backend != nil {
			_ = d.Backend.Close()
			d.Backend = nil
		}
	}
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
		busID := ed.BusID
		err := TryClaimGousb(ed)
		if err != nil && !isGousbDisabled(err) {
			logrus.Warnf("usbpass: claim %s failed (%v); requesting unbind/grant via pkexec", busID, err)
			if !RequestUSBAccess([]usbDevRef{accessRefs[i]}) {
				closeExported(exported)
				if msg := LastUSBAccessError(); msg != "" {
					return nil, fmt.Errorf("USB access: %s", msg)
				}
				return nil, fmt.Errorf("USB access was not granted for %s", busID)
			}
			err = TryClaimGousb(ed)
		}
		if err != nil {
			if isGousbDisabled(err) {
				logrus.Warnf("usbpass: descriptor-only export for %s (busnum=%d devnum=%d): %v — bulk URB will EPIPE",
					busID, ed.Busnum, ed.Devnum, err)
				continue
			}
			closeExported(exported)
			return nil, fmt.Errorf("libusb claim %s (busnum=%d devnum=%d): %w", busID, ed.Busnum, ed.Devnum, err)
		}
		logrus.Infof("usbpass: live libusb claim for %s (busnum=%d devnum=%d)", busID, ed.Busnum, ed.Devnum)
	}

	srv, err := StartExport(listenAddr, exported)
	if err != nil {
		closeExported(exported)
		return nil, err
	}
	s := &Session{server: srv, addr: listenAddr}
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

func (s *Session) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.addr
}

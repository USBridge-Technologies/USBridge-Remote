package usbpass

import (
	"fmt"
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

// StartSession exports the given passthrough devices on listenAddr
// (default 0.0.0.0:3240) and stores the session globally.
func StartSession(listenAddr string, devices []models.USBPassthroughDevice) (*Session, error) {
	if listenAddr == "" {
		listenAddr = "0.0.0.0:3240"
	}
	var exported []*ExportedDevice
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
		if err := TryClaimGousb(ed); err != nil {
			logrus.Debugf("usbpass: descriptor-only export for %s (%v)", busID, err)
		}
		exported = append(exported, ed)
	}
	if len(exported) == 0 {
		return nil, fmt.Errorf("no devices to export")
	}
	StopSession()
	srv, err := StartExport(listenAddr, exported)
	if err != nil {
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

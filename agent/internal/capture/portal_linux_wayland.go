//go:build linux

package capture

import (
	"fmt"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"
)

type PortalSession struct {
	mu           sync.RWMutex
	conn         *dbus.Conn
	sessionPath  dbus.ObjectPath
	nodeID       uint32
	dbusObj      dbus.BusObject
	initialized  bool
	initError    error
	initDone     chan struct{}
}

var (
	portalInstance *PortalSession
	portalOnce     sync.Once
)

func GetPortal() *PortalSession {
	portalOnce.Do(func() {
		portalInstance = &PortalSession{
			initDone: make(chan struct{}),
		}
	})
	return portalInstance
}

func InitPortalSession() error {
	if GetLinuxEnv() != "Wayland" {
		return nil
	}
	p := GetPortal()
	return p.Init()
}

func (p *PortalSession) Init() error {
	p.mu.Lock()
	if p.initialized {
		err := p.initError
		p.mu.Unlock()
		return err
	}
	if p.conn != nil {
		// Init already in progress
		p.mu.Unlock()
		<-p.initDone
		return p.initError
	}

	conn, err := dbus.SessionBus()
	if err != nil {
		p.initError = err
		close(p.initDone)
		p.mu.Unlock()
		return err
	}
	p.conn = conn
	p.mu.Unlock()

	go p.runInit()

	// We wait for a bit to see if it initializes quickly, but return nil if it's taking longer
	// so we don't block the app start too much. The caller can check IsInitialized().
	select {
	case <-p.initDone:
		return p.initError
	case <-time.After(2 * time.Second):
		logrus.Info("[portal] initialization taking longer than 2s, continuing in background")
		return nil
	}
}

func (p *PortalSession) runInit() {
	defer close(p.initDone)

	signals := make(chan *dbus.Signal, 100)
	p.conn.Signal(signals)

	err := p.conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	)
	if err != nil {
		p.setInitError(err)
		return
	}

	obj := p.conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")

	waitForResponse := func(reqPath dbus.ObjectPath) (map[string]dbus.Variant, error) {
		for sig := range signals {
			if sig.Path == reqPath {
				if len(sig.Body) < 2 {
					return nil, fmt.Errorf("invalid response body")
				}
				code := sig.Body[0].(uint32)
				if code != 0 {
					return nil, fmt.Errorf("user cancelled or failed (code %d)", code)
				}
				return sig.Body[1].(map[string]dbus.Variant), nil
			}
		}
		return nil, fmt.Errorf("channel closed")
	}

	// 1. CreateSession (RemoteDesktop)
	optCreate := map[string]dbus.Variant{
		"session_handle_token": dbus.MakeVariant("usbridge_session"),
		"handle_token":         dbus.MakeVariant("usbridge_req_create"),
	}
	var reqCreate dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.RemoteDesktop.CreateSession", 0, optCreate).Store(&reqCreate)
	if err != nil {
		p.setInitError(err)
		return
	}

	res, err := waitForResponse(reqCreate)
	if err != nil {
		p.setInitError(fmt.Errorf("CreateSession: %w", err))
		return
	}

	sessionHandleStr, ok := res["session_handle"].Value().(string)
	if !ok {
		p.setInitError(fmt.Errorf("invalid session_handle type"))
		return
	}
	sessionHandle := dbus.ObjectPath(sessionHandleStr)

	// 2. SelectSources (ScreenCast)
	optSources := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("usbridge_req_sources"),
		"types":        dbus.MakeVariant(uint32(1)), // 1 = Monitor
		"multiple":     dbus.MakeVariant(false),
		"cursor_mode":  dbus.MakeVariant(uint32(4)), // 4 = Metadata
	}

	var reqSources dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.ScreenCast.SelectSources", 0, sessionHandle, optSources).Store(&reqSources)
	if err != nil {
		p.setInitError(err)
		return
	}
	if _, err := waitForResponse(reqSources); err != nil {
		p.setInitError(fmt.Errorf("SelectSources: %w", err))
		return
	}

	// 3. SelectDevices (RemoteDesktop)
	optDevices := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("usbridge_req_devices"),
		"types":        dbus.MakeVariant(uint32(1 | 2 | 4)), // Keyboard | Pointer | Touchscreen
	}
	var reqDevices dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.RemoteDesktop.SelectDevices", 0, sessionHandle, optDevices).Store(&reqDevices)
	if err != nil {
		p.setInitError(err)
		return
	}
	if _, err := waitForResponse(reqDevices); err != nil {
		p.setInitError(fmt.Errorf("SelectDevices: %w", err))
		return
	}

	// 4. Start (RemoteDesktop)
	optStart := map[string]dbus.Variant{
		"handle_token": dbus.MakeVariant("usbridge_req_start"),
	}
	var reqStart dbus.ObjectPath
	err = obj.Call("org.freedesktop.portal.RemoteDesktop.Start", 0, sessionHandle, "", optStart).Store(&reqStart)
	if err != nil {
		p.setInitError(err)
		return
	}

	startRes, err := waitForResponse(reqStart)
	if err != nil {
		p.setInitError(fmt.Errorf("Start: %w", err))
		return
	}

	p.mu.Lock()
	if streamsVar, ok := startRes["streams"]; ok {
		val := streamsVar.Value()
		ids := extractAllNodeIDs(val)
		if len(ids) > 0 {
			p.nodeID = ids[0]
			logrus.Infof("[portal] selected PipeWire node ID %d", p.nodeID)
		}
	}
	p.sessionPath = sessionHandle
	p.dbusObj = obj
	p.initialized = true
	p.mu.Unlock()

	logrus.Infof("[portal] session active: %s", sessionHandle)
}

func (p *PortalSession) setInitError(err error) {
	p.mu.Lock()
	p.initError = err
	p.initialized = false
	p.mu.Unlock()
	logrus.Errorf("[portal] initialization failed: %v", err)
}

func (p *PortalSession) IsInitialized() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.initialized
}

func (p *PortalSession) SessionPath() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return string(p.sessionPath)
}

func (p *PortalSession) NodeID() uint32 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nodeID
}

func (p *PortalSession) OpenPipeWireFD() (int, error) {
	p.mu.RLock()
	obj := p.dbusObj
	session := p.sessionPath
	p.mu.RUnlock()

	if obj == nil || session == "" {
		return 0, fmt.Errorf("portal session not initialized")
	}

	var pipewireFd dbus.UnixFD
	err := obj.Call("org.freedesktop.portal.ScreenCast.OpenPipeWireRemote", 0, session, map[string]dbus.Variant{}).Store(&pipewireFd)
	if err != nil {
		return 0, fmt.Errorf("OpenPipeWireRemote failed: %w", err)
	}
	return int(pipewireFd), nil
}

// Global accessor functions for backward compatibility or ease of use
func GetPortalSession() string {
	return GetPortal().SessionPath()
}

func GetPortalPipeWireNodeID() uint32 {
	return GetPortal().NodeID()
}

func GetPortalPipeWireFD() int {
	fd, err := GetPortal().OpenPipeWireFD()
	if err != nil {
		logrus.Errorf("[portal] failed to get PipeWire FD: %v", err)
		return 0
	}
	return fd
}

func extractAllNodeIDs(raw interface{}) []uint32 {
	var ids []uint32
	switch arr := raw.(type) {
	case [][]interface{}:
		for _, item := range arr {
			if len(item) > 0 {
				if id, ok := item[0].(uint32); ok {
					ids = append(ids, id)
				}
			}
		}
	case []interface{}:
		for _, outer := range arr {
			switch item := outer.(type) {
			case []interface{}:
				if len(item) > 0 {
					if id, ok := item[0].(uint32); ok {
						ids = append(ids, id)
					}
				}
			case uint32:
				ids = append(ids, item)
			}
		}
	}
	return ids
}

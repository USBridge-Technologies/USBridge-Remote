//go:build linux

package capture

import (
	"fmt"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/sirupsen/logrus"
)

var (
	portalSessionPath dbus.ObjectPath
	portalNodeID      uint32
	portalDbusObj     dbus.BusObject
	portalMu          sync.RWMutex
)

func InitPortalSession() error {
	if GetLinuxEnv() != "Wayland" {
		return nil
	}

	portalMu.RLock()
	if portalSessionPath != "" {
		portalMu.RUnlock()
		return nil
	}
	portalMu.RUnlock()

	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}

	signals := make(chan *dbus.Signal, 100)
	conn.Signal(signals)

	err = conn.AddMatchSignal(
		dbus.WithMatchInterface("org.freedesktop.portal.Request"),
		dbus.WithMatchMember("Response"),
	)
	if err != nil {
		return err
	}

	obj := conn.Object("org.freedesktop.portal.Desktop", "/org/freedesktop/portal/desktop")

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
		return err
	}
	logrus.Infof("Portal CreateSession sent: %s", reqCreate)

	go func() {
		res, err := waitForResponse(reqCreate)
		if err != nil {
			logrus.Errorf("Portal CreateSession failed: %v", err)
			return
		}

		sessionHandleStr, ok := res["session_handle"].Value().(string)
		if !ok {
			logrus.Errorf("Portal CreateSession returned invalid session_handle type")
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
		obj.Call("org.freedesktop.portal.ScreenCast.SelectSources", 0, sessionHandle, optSources).Store(&reqSources)
		if _, err := waitForResponse(reqSources); err != nil {
			logrus.Errorf("Portal SelectSources failed: %v", err)
			return
		}

		// 3. SelectDevices (RemoteDesktop)
		optDevices := map[string]dbus.Variant{
			"handle_token": dbus.MakeVariant("usbridge_req_devices"),
			"types":        dbus.MakeVariant(uint32(1 | 2 | 4)), // Keyboard | Pointer | Touchscreen
		}
		var reqDevices dbus.ObjectPath
		obj.Call("org.freedesktop.portal.RemoteDesktop.SelectDevices", 0, sessionHandle, optDevices).Store(&reqDevices)
		if _, err := waitForResponse(reqDevices); err != nil {
			logrus.Errorf("Portal SelectDevices failed: %v", err)
			return
		}

		// 4. Start (RemoteDesktop)
		optStart := map[string]dbus.Variant{
			"handle_token": dbus.MakeVariant("usbridge_req_start"),
		}
		var reqStart dbus.ObjectPath
		obj.Call("org.freedesktop.portal.RemoteDesktop.Start", 0, sessionHandle, "", optStart).Store(&reqStart)

		startRes, err := waitForResponse(reqStart)
		if err != nil {
			logrus.Errorf("Portal Start failed: %v", err)
			return
		}

		portalMu.Lock()
		if streamsVar, ok := startRes["streams"]; ok {
			if nodeID := extractFirstNodeID(streamsVar.Value()); nodeID > 0 {
				portalNodeID = nodeID
				logrus.Infof("✅ PipeWire node ID: %d", nodeID)
			}
		}
		portalSessionPath = sessionHandle
		portalDbusObj = obj
		portalMu.Unlock()

		logrus.Infof("✅ Wayland RemoteDesktop session fully active: %s", portalSessionPath)
	}()

	return nil
}

func GetPortalSession() string {
	portalMu.RLock()
	defer portalMu.RUnlock()
	return string(portalSessionPath)
}

func GetPortalPipeWireNodeID() uint32 {
	portalMu.RLock()
	defer portalMu.RUnlock()
	return portalNodeID
}

func OpenPipeWireRemoteFD() (int, error) {
	portalMu.RLock()
	obj := portalDbusObj
	session := portalSessionPath
	portalMu.RUnlock()

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

func GetPortalPipeWireFD() int {
	fd, err := OpenPipeWireRemoteFD()
	if err != nil {
		logrus.Errorf("[portal] failed to get fresh PipeWire FD: %v", err)
		return 0
	}
	logrus.Infof("[portal] fresh PipeWire FD obtained: %d", fd)
	return fd
}


// extractFirstNodeID pulls the PipeWire node ID from the streams field of the
// portal Start response. The DBus type is a(ua{sv}) — array of (nodeID, props).
// godbus decodes struct arrays as []interface{} where each element is []interface{}.
func extractFirstNodeID(raw interface{}) uint32 {
	switch arr := raw.(type) {
	case [][]interface{}:
		if len(arr) > 0 && len(arr[0]) > 0 {
			if id, ok := arr[0][0].(uint32); ok {
				return id
			}
		}
	case []interface{}:
		if len(arr) == 0 {
			return 0
		}
		switch first := arr[0].(type) {
		case []interface{}:
			if len(first) > 0 {
				if id, ok := first[0].(uint32); ok {
					return id
				}
			}
		case uint32:
			return first
		}
	}
	return 0
}

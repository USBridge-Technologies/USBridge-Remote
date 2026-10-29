//go:build linux && !android

package netutil

import (
	"fmt"
	"os"

	"github.com/godbus/dbus/v5"
)

// Inside a snap (client/snap/snapcraft.yaml) pkexec isn't reachable, so the
// Wi-Fi tune goes through NetworkManager's D-Bus API instead, over the snap's
// network-manager plug: power saving off (802-11-wireless.powersave = 2) on
// every active Wi-Fi connection, saved to the connection and reapplied to its
// device. NetworkManager asks polkit itself, so the host's agent prompts when
// the connection isn't the user's own. iwd's periodic scan and `iw` stay out
// of reach from there.

func inSnap() bool { return os.Getenv("SNAP") != "" }

const (
	nmDest = "org.freedesktop.NetworkManager"
	nmPath = "/org/freedesktop/NetworkManager"

	nmWiFiSetting    = "802-11-wireless"
	nmPowersaveKey   = "powersave"
	nmPowersaveOff   = uint32(2) // NM_SETTING_WIRELESS_POWERSAVE_DISABLE
	nmActiveTypeWiFi = "802-11-wireless"
)

type nmSettings = map[string]map[string]dbus.Variant

// nmWiFi is one active Wi-Fi connection: its saved settings object and the
// device it runs on.
type nmWiFi struct {
	settings dbus.ObjectPath
	device   dbus.ObjectPath
}

func nmActiveWiFi(conn *dbus.Conn) ([]nmWiFi, error) {
	var actives []dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).StoreProperty(nmDest+".ActiveConnections", &actives); err != nil {
		return nil, err
	}
	var out []nmWiFi
	for _, a := range actives {
		obj := conn.Object(nmDest, a)
		var typ string
		if obj.StoreProperty(nmDest+".Connection.Active.Type", &typ) != nil || typ != nmActiveTypeWiFi {
			continue
		}
		var w nmWiFi
		if obj.StoreProperty(nmDest+".Connection.Active.Connection", &w.settings) != nil {
			continue
		}
		var devs []dbus.ObjectPath
		if obj.StoreProperty(nmDest+".Connection.Active.Devices", &devs) == nil && len(devs) > 0 {
			w.device = devs[0]
		}
		out = append(out, w)
	}
	return out, nil
}

func nmGetSettings(conn *dbus.Conn, path dbus.ObjectPath) (nmSettings, error) {
	var s nmSettings
	err := conn.Object(nmDest, path).Call(nmDest+".Settings.Connection.GetSettings", 0).Store(&s)
	return s, err
}

func nmPowersaveIsOff(s nmSettings) bool {
	v, ok := s[nmWiFiSetting][nmPowersaveKey]
	if !ok {
		return false
	}
	n, ok := v.Value().(uint32)
	return ok && n == nmPowersaveOff
}

// nmWithPowersaveOff returns the settings Update needs to turn power saving
// off: Update replaces the whole connection, so the saved secrets are merged
// back in (or a stored Wi-Fi password would be dropped), and the legacy
// ipv4/ipv6 addresses/routes arrays go when their *-data twins are there
// (NetworkManager refuses both together).
func nmWithPowersaveOff(s nmSettings, secrets map[string]nmSettings) nmSettings {
	for group, sec := range secrets {
		for _, keys := range sec {
			if s[group] == nil {
				s[group] = map[string]dbus.Variant{}
			}
			for k, v := range keys {
				s[group][k] = v
			}
		}
	}
	for _, ip := range []string{"ipv4", "ipv6"} {
		if g := s[ip]; g != nil {
			if _, ok := g["address-data"]; ok {
				delete(g, "addresses")
			}
			if _, ok := g["route-data"]; ok {
				delete(g, "routes")
			}
		}
	}
	if s[nmWiFiSetting] == nil {
		s[nmWiFiSetting] = map[string]dbus.Variant{}
	}
	s[nmWiFiSetting][nmPowersaveKey] = dbus.MakeVariant(nmPowersaveOff)
	return s
}

// nmSecretGroups are the settings that can hold a Wi-Fi connection's secrets.
var nmSecretGroups = []string{"802-11-wireless-security", "802-1x"}

func nmTurnPowersaveOff(conn *dbus.Conn, w nmWiFi) error {
	s, err := nmGetSettings(conn, w.settings)
	if err != nil {
		return fmt.Errorf("read the Wi-Fi connection: %w", err)
	}
	if nmPowersaveIsOff(s) {
		return nil
	}
	obj := conn.Object(nmDest, w.settings)
	secrets := map[string]nmSettings{}
	for _, group := range nmSecretGroups {
		if _, ok := s[group]; !ok {
			continue
		}
		var sec nmSettings
		if err := obj.Call(nmDest+".Settings.Connection.GetSecrets", 0, group).Store(&sec); err != nil {
			// Saving without them would wipe the stored password.
			return fmt.Errorf("read the Wi-Fi connection's saved password: %w", err)
		}
		secrets[group] = sec
	}
	if err := obj.Call(nmDest+".Settings.Connection.Update", 0, nmWithPowersaveOff(s, secrets)).Err; err != nil {
		return fmt.Errorf("save the Wi-Fi connection: %w", err)
	}
	if w.device == "" {
		return nil
	}
	// An empty connection reapplies the saved one. A setting the device
	// can't take live means reconnecting (Wi-Fi drops for a few seconds,
	// like the iwd restart outside the snap).
	reapplyErr := conn.Object(nmDest, w.device).Call(nmDest+".Device.Reapply", 0, nmSettings{}, uint64(0), uint32(0)).Err
	if reapplyErr == nil {
		return nil
	}
	var active dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).Call(nmDest+".ActivateConnection", 0, w.settings, w.device, dbus.ObjectPath("/")).Store(&active); err != nil {
		return fmt.Errorf("apply the Wi-Fi setting: %v; reconnect: %w", reapplyErr, err)
	}
	return nil
}

func nmWLANConnected() bool {
	conn, err := dbus.SystemBus()
	if err != nil {
		return false
	}
	ws, err := nmActiveWiFi(conn)
	return err == nil && len(ws) > 0
}

func nmWiFiTuneStatus() (applicable, done bool) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return false, false
	}
	ws, err := nmActiveWiFi(conn)
	if err != nil || len(ws) == 0 {
		return false, false
	}
	for _, w := range ws {
		s, err := nmGetSettings(conn, w.settings)
		if err != nil {
			return false, false
		}
		if !nmPowersaveIsOff(s) {
			return true, false
		}
	}
	return true, true
}

func nmApplyWiFiTune() error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("no system D-Bus: %w", err)
	}
	ws, err := nmActiveWiFi(conn)
	if err != nil {
		return fmt.Errorf("NetworkManager isn't reachable (sudo snap connect usbridge-client:network-manager): %w", err)
	}
	for _, w := range ws {
		if err := nmTurnPowersaveOff(conn, w); err != nil {
			return err
		}
	}
	if _, done := nmWiFiTuneStatus(); !done {
		return fmt.Errorf("the Wi-Fi settings were saved but are not in effect yet")
	}
	return nil
}

//go:build linux && !android

package netutil

import (
	"os"
	"testing"

	"github.com/godbus/dbus/v5"
)

func TestNMPowersaveIsOff(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    nmSettings
		want bool
	}{
		{"no wifi setting", nmSettings{}, false},
		{"default", nmSettings{nmWiFiSetting: {nmPowersaveKey: dbus.MakeVariant(uint32(0))}}, false},
		{"enabled", nmSettings{nmWiFiSetting: {nmPowersaveKey: dbus.MakeVariant(uint32(3))}}, false},
		{"disabled", nmSettings{nmWiFiSetting: {nmPowersaveKey: dbus.MakeVariant(uint32(2))}}, true},
		{"wrong type", nmSettings{nmWiFiSetting: {nmPowersaveKey: dbus.MakeVariant(int32(2))}}, false},
	} {
		if got := nmPowersaveIsOff(tc.s); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestNMWithPowersaveOffKeepsSecretsAndDropsLegacyIP(t *testing.T) {
	s := nmSettings{
		"connection":               {"id": dbus.MakeVariant("home")},
		nmWiFiSetting:              {"ssid": dbus.MakeVariant([]byte("home"))},
		"802-11-wireless-security": {"key-mgmt": dbus.MakeVariant("wpa-psk")},
		"ipv4": {
			"method":       dbus.MakeVariant("auto"),
			"addresses":    dbus.MakeVariant([][]uint32{}),
			"address-data": dbus.MakeVariant([]map[string]dbus.Variant{}),
			"routes":       dbus.MakeVariant([][]uint32{}),
			"route-data":   dbus.MakeVariant([]map[string]dbus.Variant{}),
		},
		"ipv6": {"method": dbus.MakeVariant("auto"), "addresses": dbus.MakeVariant([]interface{}{})},
	}
	secrets := map[string]nmSettings{
		"802-11-wireless-security": {"802-11-wireless-security": {"psk": dbus.MakeVariant("hunter22")}},
	}
	out := nmWithPowersaveOff(s, secrets)

	if !nmPowersaveIsOff(out) {
		t.Fatal("powersave not set to 2")
	}
	if psk, ok := out["802-11-wireless-security"]["psk"]; !ok || psk.Value() != "hunter22" {
		t.Fatalf("saved password lost: %v", out["802-11-wireless-security"])
	}
	if out["802-11-wireless-security"]["key-mgmt"].Value() != "wpa-psk" {
		t.Fatal("key-mgmt lost")
	}
	if _, ok := out[nmWiFiSetting]["ssid"]; !ok {
		t.Fatal("ssid lost")
	}
	if _, ok := out["ipv4"]["addresses"]; ok {
		t.Fatal("ipv4 addresses kept next to address-data")
	}
	if _, ok := out["ipv4"]["routes"]; ok {
		t.Fatal("ipv4 routes kept next to route-data")
	}
	if _, ok := out["ipv6"]["addresses"]; !ok {
		t.Fatal("ipv6 addresses dropped although there is no address-data")
	}
}

func TestNMWithPowersaveOffAddsWiFiSetting(t *testing.T) {
	out := nmWithPowersaveOff(nmSettings{"connection": {}}, nil)
	if !nmPowersaveIsOff(out) {
		t.Fatal("powersave not set when the 802-11-wireless setting was missing")
	}
}

// TestNMApplyWiFiTuneLive runs the snap's tune against this machine's real
// NetworkManager. It changes the active Wi-Fi connection, so it only runs
// with USBRIDGE_NM_WIFI_TEST=1 (e.g. on mac80211_hwsim radios).
func TestNMApplyWiFiTuneLive(t *testing.T) {
	if os.Getenv("USBRIDGE_NM_WIFI_TEST") != "1" {
		t.Skip("set USBRIDGE_NM_WIFI_TEST=1 to change the active Wi-Fi connection")
	}
	t.Setenv("SNAP", "/snap/usbridge-client/x1")
	if !WLANConnected() {
		t.Fatal("no active Wi-Fi connection in NetworkManager")
	}
	applicable, done := WiFiTuneStatus()
	t.Logf("before: applicable=%v done=%v", applicable, done)
	if !applicable {
		t.Fatal("tune not applicable with an active Wi-Fi connection")
	}
	if err := ApplyWiFiTune(); err != nil {
		t.Fatalf("ApplyWiFiTune: %v", err)
	}
	if applicable, done := WiFiTuneStatus(); !applicable || !done {
		t.Fatalf("after: applicable=%v done=%v", applicable, done)
	}
	if !WLANConnected() {
		t.Fatal("Wi-Fi not connected after the tune")
	}
	if err := ApplyWiFiTune(); err != nil {
		t.Fatalf("second ApplyWiFiTune (already done): %v", err)
	}
}

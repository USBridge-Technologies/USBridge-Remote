package service

import "testing"

// The list a Galaxy S25 sent over Wi-Fi (192.168.1.103/24), from logcat.
const sampleAndroidInterfaces = "dummy0 2 1500 true false false false false|fe80::cc95:fcff:fe24:4790%dummy0/64\n" +
	"rmnet_data8 71 1500 true false false false false|10.250.255.193/30\n" +
	"wlan0 44 1500 true true false false true|fe80::20fd:23ff:fe06:5fd4%wlan0/64 192.168.1.103/24\n" +
	"lo 1 65536 true false true false false|::1/128 127.0.0.1/8"

func TestParseAndroidInterfacesKeepsHostAddress(t *testing.T) {
	ifaces := parseAndroidInterfaces(sampleAndroidInterfaces)
	if len(ifaces) != 4 {
		t.Fatalf("got %d interfaces, want 4", len(ifaces))
	}
	got := map[string][]string{}
	for _, iface := range ifaces {
		for _, a := range iface.AltAddrs {
			got[iface.Name] = append(got[iface.Name], a.String())
		}
	}
	want := map[string][]string{
		"wlan0":       {"fe80::20fd:23ff:fe06:5fd4/64", "192.168.1.103/24"},
		"rmnet_data8": {"10.250.255.193/30"},
		"lo":          {"::1/128", "127.0.0.1/8"},
	}
	for name, addrs := range want {
		if len(got[name]) != len(addrs) {
			t.Fatalf("%s: got %v, want %v", name, got[name], addrs)
		}
		for i := range addrs {
			if got[name][i] != addrs[i] {
				t.Errorf("%s: got %v, want %v", name, got[name], addrs)
			}
		}
	}
	if ifaces[2].Name != "wlan0" || !ifaces[2].IsUp() {
		t.Errorf("wlan0 not parsed as up: %+v", ifaces[2])
	}
}

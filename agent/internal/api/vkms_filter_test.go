package api

import "testing"

func TestMarkVkmsConnectors(t *testing.T) {
	in := []VideoDeviceInfo{
		{Name: "card1-DP-2", Bus: "drm"},
		{Name: "card0-Virtual-1", Bus: "drm"},
	}
	out := markVkmsConnectors(in)
	if len(out) != 2 || out[0].Bus != "drm" || out[1].Bus != "virtual" {
		t.Fatalf("unexpected result: %+v", out)
	}
	if in[1].Bus != "drm" {
		t.Fatal("input must not be mutated")
	}
}

func TestDrmConnectorNameRe(t *testing.T) {
	m := drmConnectorNameRe.FindStringSubmatch("card1-DP-2")
	if m == nil || m[1] != "card1" || m[2] != "DP-2" {
		t.Fatalf("got %v", m)
	}
}

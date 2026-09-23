package gui

import "testing"

// resolveDeepLinkHost's deviceHost-over-everything-else branch only fires
// when api.BrowserIsHTTPS() is true, which is compile-time-false on this
// (desktop-native) test build -- see that function's doc comment for why
// only the wasm build ever takes it. These tests cover what's actually
// exercisable here: the pre-existing protocol-preference behavior is
// unchanged now that deviceHost is threaded through, and deviceHost itself
// is correctly ignored on a build where BrowserIsHTTPS() is false.
func TestResolveDeepLinkHost_PrefersProtocolMatch(t *testing.T) {
	cases := []struct {
		name                                  string
		protocol, internalHost, tailscaleHost string
		want                                  string
	}{
		{"tailscale protocol picks tailscale host", "tailscale", "192.168.1.5", "100.64.0.1", "100.64.0.1"},
		{"direct protocol picks internal host", "direct", "192.168.1.5", "100.64.0.1", "192.168.1.5"},
		{"no protocol falls back to tailscale if present", "", "192.168.1.5", "100.64.0.1", "100.64.0.1"},
		{"no protocol, no tailscale falls back to internal", "", "192.168.1.5", "", "192.168.1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveDeepLinkHost(tc.protocol, tc.internalHost, tc.tailscaleHost, "")
			if got != tc.want {
				t.Errorf("resolveDeepLinkHost(%q, %q, %q, \"\") = %q, want %q", tc.protocol, tc.internalHost, tc.tailscaleHost, got, tc.want)
			}
		})
	}
}

func TestResolveDeepLinkHost_DeviceHostIgnoredWhenNotBrowserHTTPS(t *testing.T) {
	// On this (desktop-native) build api.BrowserIsHTTPS() is always false,
	// so a non-empty deviceHost must never override internalHost/tailscaleHost.
	got := resolveDeepLinkHost("direct", "192.168.1.5", "", "abc123.device.usbridge.io")
	if got != "192.168.1.5" {
		t.Errorf("resolveDeepLinkHost with deviceHost set = %q, want internalHost %q (BrowserIsHTTPS() is false on desktop-native)", got, "192.168.1.5")
	}
}

func TestParseDeepLink_ExtractsDeviceHost(t *testing.T) {
	h := NewDeepLinkHandler(nil, nil)
	internalHost, tailscaleHost, deviceHost, masterKey, protocol, immediate, err := h.parseDeepLink(
		"usbridge://connect?internal_host=192.168.1.5&device_host=abc123.device.usbridge.io&master_key=secret&protocol=direct&immediate=true",
	)
	if err != nil {
		t.Fatalf("parseDeepLink: %v", err)
	}
	if internalHost != "192.168.1.5" {
		t.Errorf("internalHost = %q, want 192.168.1.5", internalHost)
	}
	if tailscaleHost != "" {
		t.Errorf("tailscaleHost = %q, want empty", tailscaleHost)
	}
	if deviceHost != "abc123.device.usbridge.io" {
		t.Errorf("deviceHost = %q, want abc123.device.usbridge.io", deviceHost)
	}
	if masterKey != "secret" {
		t.Errorf("masterKey = %q, want secret", masterKey)
	}
	if protocol != "direct" {
		t.Errorf("protocol = %q, want direct", protocol)
	}
	if !immediate {
		t.Error("immediate = false, want true")
	}
}

func TestParseDeepLink_DeviceHostOptional(t *testing.T) {
	h := NewDeepLinkHandler(nil, nil)
	_, _, deviceHost, _, _, _, err := h.parseDeepLink("usbridge://connect?internal_host=192.168.1.5&master_key=secret")
	if err != nil {
		t.Fatalf("parseDeepLink: %v", err)
	}
	if deviceHost != "" {
		t.Errorf("deviceHost = %q, want empty when the link never set it (older agent build)", deviceHost)
	}
}

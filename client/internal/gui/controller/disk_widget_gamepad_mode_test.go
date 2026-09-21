package controller

import "testing"

func TestEffectiveGamepadMode(t *testing.T) {
	tests := []struct {
		name    string
		agentOS string
		mode    string
		want    string
	}{
		{"software default", "Windows 11", "", gamepadModeMapX360},
		{"software keeps explicit xinput", "Ubuntu 24.04", gamepadModeXInput, gamepadModeXInput},
		{"software keeps explicit directinput", "macOS 15", gamepadModeDirectInput, gamepadModeDirectInput},
		{"software keeps map", "Windows 11", gamepadModeMapX360, gamepadModeMapX360},
		{"hardware default", "USBridge OS", "", gamepadModeXInput},
		{"unknown os is hardware", "", "", gamepadModeXInput},
		{"hardware has no map", "USBridge OS", gamepadModeMapX360, gamepadModeXInput},
		{"hardware keeps directinput", "USBridge OS", gamepadModeDirectInput, gamepadModeDirectInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dw := &DiskWidget{agentOS: tt.agentOS}
			if got := dw.effectiveGamepadMode(tt.mode); got != tt.want {
				t.Fatalf("effectiveGamepadMode(%q) on %q = %q, want %q", tt.mode, tt.agentOS, got, tt.want)
			}
		})
	}
}

func TestGamepadIdentityMatches(t *testing.T) {
	tests := []struct {
		name                               string
		driveVID, drivePID, devVID, devPID string
		want                               bool
	}{
		{"same", "0x1532", "0x0a29", "0x1532", "0x0a29", true},
		{"case and prefix", "0x045E", "0x028E", "045e", "028e", true},
		{"different pad", "0x045e", "0x028e", "0x1532", "0x0a29", false},
		{"agent reports none", "0x1532", "0x0a29", "", "", true},
		{"drive has none", "", "", "0x1532", "0x0a29", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gamepadIdentityMatches(tt.driveVID, tt.drivePID, tt.devVID, tt.devPID); got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

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

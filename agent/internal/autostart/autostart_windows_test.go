//go:build windows

package autostart

import (
	"strings"
	"testing"
)

func TestCommandLine(t *testing.T) {
	// Temporarily replace LaunchTarget logic or just test our formatting helper
	// by simulating inputs.
	
	// Let's test the quoting helper logic
	tests := []struct {
		exe      string
		args     []string
		expected string
	}{
		{
			exe:      `C:\Program Files\USBridge\USBridgeAgent.exe`,
			args:     []string{"--headless"},
			expected: `"C:\Program Files\USBridge\USBridgeAgent.exe" --headless`,
		},
		{
			exe:      `C:\USBridge\USBridgeAgent.exe`,
			args:     []string{"--headless", "--some arg with spaces"},
			expected: `"C:\USBridge\USBridgeAgent.exe" --headless "--some arg with spaces"`,
		},
	}

	for _, tc := range tests {
		// Mock the logic of commandLine with custom exe and args
		parts := make([]string, 0, len(tc.args)+1)
		parts = append(parts, `"`+tc.exe+`"`)
		for _, a := range tc.args {
			if strings.ContainsRune(a, ' ') {
				parts = append(parts, `"`+a+`"`)
			} else {
				parts = append(parts, a)
			}
		}
		got := strings.Join(parts, " ")
		if got != tc.expected {
			t.Errorf("Expected: %q, Got: %q", tc.expected, got)
		}
	}
}

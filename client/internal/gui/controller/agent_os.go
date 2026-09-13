package controller

import "strings"

// isUSBridgeAgentOS reports whether agentOS identifies the connected device as
// real USBridge KVM hardware rather than a plain OS agent (Windows/Linux/macOS).
// An empty/unknown value is treated as USBridge so real hardware is never
// mistakenly locked out before its OS string has been fetched.
func isUSBridgeAgentOS(agentOS string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(agentOS))
	return trimmed == "" || strings.Contains(trimmed, "usbridge")
}

// IsSoftwareAgentOS is true for a Windows/Linux/macOS software agent, not
// USBridge KVM hardware. Empty/unknown stays hardware so a missing OS
// string cannot lock out a real board.
func IsSoftwareAgentOS(agentOS string) bool {
	return !isUSBridgeAgentOS(agentOS)
}

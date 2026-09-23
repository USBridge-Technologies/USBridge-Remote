package controller

import "strings"

// isUSBridgeAgentOS reports whether agentOS identifies the connected device as
// real USBridge KVM hardware rather than a plain OS agent (Windows/Linux/macOS).
// An empty/unknown value is treated as USBridge so real hardware is never
// mistakenly locked out before its OS string has been fetched. Callers that
// already have /api/device/info (connect verification) must seed agentOS
// before the first Devices/Control paint so a software agent is not drawn
// as KVM for one frame.
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

// knownUSBridgeHardware is true only when agentOS positively identifies
// USBridge KVM. Empty/unknown stays off so Devices can keep software-agent
// cards on a first connect, then switch to KVM chrome once the saved or
// live identity lands.
func knownUSBridgeHardware(agentOS string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(agentOS)), "usbridge")
}

// MergeAgentIdentity prefers the live /api/device/info values and fills
// blanks from the last saved connection row. Used at connect so Devices and
// mouse mapping can render correctly without a second round-trip.
func MergeAgentIdentity(liveOS, liveProtocol, savedOS, savedProtocol string) (osName, protocol string) {
	osName = strings.TrimSpace(liveOS)
	protocol = strings.TrimSpace(liveProtocol)
	if osName == "" {
		osName = strings.TrimSpace(savedOS)
	}
	if protocol == "" {
		protocol = strings.TrimSpace(savedProtocol)
	}
	return osName, protocol
}

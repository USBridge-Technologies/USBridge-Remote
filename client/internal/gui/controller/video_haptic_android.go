//go:build android

package controller

import "usbridge-client/internal/service"

// triggerRmbHaptic fires a short haptic tap to confirm RMB long-press threshold.
func triggerRmbHaptic() {
	service.VKVideoAndroidHapticShortTap()
}

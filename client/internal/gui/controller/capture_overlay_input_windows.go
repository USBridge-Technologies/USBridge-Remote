//go:build windows

package controller

import "usbridge-client/internal/service"

// restoreOverlayMouseInput re-registers the Vulkan overlay's Raw Input after
// Capture stopped: the capture engine's RIDEV_REMOVE also dropped the overlay's
// registration, so absolute moves would otherwise stay dead.
func restoreOverlayMouseInput() {
	service.VKVideoReregisterRawInput()
}

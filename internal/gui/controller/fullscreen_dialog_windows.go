//go:build windows

package controller

import (
	"image"

	"usbridge-client/internal/service"

	"github.com/sirupsen/logrus"
)

// canWindowlessVKFullscreen returns true on Windows — we always attempt standalone
// VK fullscreen to avoid the Z-order race between two TOPMOST windows (GLFW + VK).
func (fd *FullscreenDialog) canWindowlessVKFullscreen() bool {
	return fd.videoWidget != nil
}

// enterWindowlessVKFullscreen creates a standalone Vulkan fullscreen window that
// covers the entire primary display. No Fyne/GLFW window is involved, so there is
// no Z-order competition and no black-screen flicker.
// Input (keyboard + mouse) is captured directly by the VK window and forwarded to
// Moonlight via the C event queues polled by startVKKeyForwarding / startVKMouseForwarding.
func (fd *FullscreenDialog) enterWindowlessVKFullscreen() {
	fd.isFullscreen = true
	fd.windowlessVKFullscreen = true

	vw := fd.videoWidget

	// Destroy any existing VK/GDI overlay on the main window.
	vw.stopMetalVideo()

	// Create the standalone fullscreen VK window.
	if !service.VKVideoCreateStandalone() {
		logrus.Error("[Win/FS] VKVideoCreateStandalone failed — cannot enter fullscreen")
		fd.windowlessVKFullscreen = false
		fd.isFullscreen = false
		// Restart main-window overlay so video continues.
		vw.startMetalVideoOnWindow(vw.parentWindow, false)
		return
	}

	// Ensure frames keep being delivered to the video widget (for VKVideoTrySubmit).
	if fd.videoClient != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			vw.handleVideoFrame(frame)
		})
	}

	// Mouse events go through the existing VK event queue → vw.touchpadWrapper.
	// Scale = 1: VK client coords are physical pixels; touchpadWrapper handles deltas.
	vw.startVKMouseForwarding(1.0)

	// Keyboard events go through the key event queue → Moonlight input directly.
	vw.startVKKeyForwarding(fd.exitFullscreen)

	logrus.Info("[Win/FS] Standalone VK fullscreen active — no Fyne window")
}

// exitWindowlessVKFullscreen tears down the standalone VK fullscreen and restores
// the Vulkan overlay on the main window.
func (fd *FullscreenDialog) exitWindowlessVKFullscreen() {
	fd.isFullscreen = false
	fd.windowlessVKFullscreen = false

	vw := fd.videoWidget
	// stopMetalVideo destroys the VK window and stops mouse+key forwarding goroutines.
	vw.stopMetalVideo()

	// Restore frame callback to the main window.
	if fd.videoClient != nil && vw != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			vw.handleVideoFrame(frame)
		})
	}

	// Restart VK overlay on the main window.
	vw.metalVideoExitFullscreen()

	logrus.Info("[Win/FS] Exited standalone VK fullscreen")
}

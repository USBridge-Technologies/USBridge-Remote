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

	// Fix absolute mouse mode: PositionToAbsolute uses touchpadSizeW/H which is
	// normally the video widget area in the main window. In standalone fullscreen
	// the VK window covers the entire primary screen, so update the size to match.
	// Store the screen dp size on VideoWidget so updateFrameContentRect (called on
	// every decoded frame) re-applies it instead of overwriting with main-window size.
	sw, sh := service.VKVideoGetDstSize() // physical screen pixels
	scale := float32(1)
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		scale = vw.parentWindow.Canvas().Scale()
	}
	screenDpW := float32(sw) / scale
	screenDpH := float32(sh) / scale
	if sw > 0 && sh > 0 && scale > 0 {
		vw.standaloneVKScreenDpW = screenDpW
		vw.standaloneVKScreenDpH = screenDpH
		vw.UpdateTouchpadAndContentRect(screenDpW, screenDpH, nil)
	}

	// Fix touchpad-mode focus steal: TouchpadWrapper.MouseDown calls
	// window.RequestFocus() which activates the main Fyne window and covers the
	// standalone VK window. Skip it while fullscreen is active.
	if vw.touchpadWrapper != nil {
		vw.touchpadWrapper.SetSkipWindowFocus(true)
	}

	// Ensure frames keep being delivered to the video widget (for VKVideoTrySubmit).
	if fd.videoClient != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			vw.handleVideoFrame(frame)
		})
	}

	// Mouse events go through the existing VK event queue → vw.touchpadWrapper.
	// Use the parent window canvas scale so physical pixel coords → correct dp coords.
	// (The scale is also re-read per-event inside dispatchVKWinMouseEvent, so this
	//  initial value is just the starting default.)
	vw.startVKMouseForwarding(scale)

	// Keyboard events go through the key event queue → Moonlight input directly.
	vw.startVKKeyForwarding(fd.exitFullscreen)

	logrus.Infof("[Win/FS] Standalone VK fullscreen active — screen=%dx%d scale=%.3f dp=%.1fx%.1f standaloneField=%.1fx%.1f touchpadSize=%.1fx%.1f contentRect=(%.1f,%.1f,%.1f,%.1f)",
		sw, sh, scale, screenDpW, screenDpH,
		vw.standaloneVKScreenDpW, vw.standaloneVKScreenDpH,
		vw.touchpadSizeW, vw.touchpadSizeH,
		vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH)
}

// exitWindowlessVKFullscreen tears down the standalone VK fullscreen and restores
// the Vulkan overlay on the main window.
func (fd *FullscreenDialog) exitWindowlessVKFullscreen() {
	fd.isFullscreen = false
	fd.windowlessVKFullscreen = false

	vw := fd.videoWidget

	// Restore normal focus behaviour on the main-window touchpad wrapper.
	if vw.touchpadWrapper != nil {
		vw.touchpadWrapper.SetSkipWindowFocus(false)
	}
	// Clear the standalone screen size so updateFrameContentRect resumes using
	// the main-window widget size for absolute position normalisation.
	vw.standaloneVKScreenDpW = 0
	vw.standaloneVKScreenDpH = 0

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

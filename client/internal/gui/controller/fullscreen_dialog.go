package controller

import (
	"image"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/mobile"
	"github.com/sirupsen/logrus"
)

// FullscreenDialog is the fullscreen mode dialog
type FullscreenDialog struct {
	parent                 fyne.Window
	videoWidget            *VideoWidget
	isFullscreen           bool
	nativeFullscreen       bool
	windowlessVKFullscreen bool // Windows: VK standalone fullscreen (no Fyne window)
	videoClient            service.VideoClient
	fullscreenWindow       fyne.Window
	virtualKeyboard        *graphics.VirtualKeyboard
	videoImage             *canvas.Image
	touchpadWrapper        *TouchpadWrapper
	nativeCapture          nativeFullscreenCapture
	lastFrame              image.Image
	frameMutex             sync.RWMutex
	originalContent        *fyne.Container
	originalTitle          string
	ui                     *view.FullscreenUI
	keyboardModifierState  atomic.Int32
	suppressRuneUntilNS    atomic.Int64
	audioMuted             bool
}

// NewFullscreenDialog creates a new fullscreen mode dialog
func NewFullscreenDialog(parent fyne.Window) *FullscreenDialog {
	return &FullscreenDialog{parent: parent}
}

// SetVideoWidget sets the reference to the video widget
func (fd *FullscreenDialog) SetVideoWidget(videoWidget *VideoWidget) {
	fd.videoWidget = videoWidget
}

// SetVideoClient sets the reference to the video service
func (fd *FullscreenDialog) SetVideoClient(videoClient service.VideoClient) {
	fd.videoClient = videoClient
}

// Show displays fullscreen mode immediately without a dialog
func (fd *FullscreenDialog) Show() {
	if fd.isFullscreen {
		fd.exitFullscreen()
		return
	}
	if fd.videoWidget == nil {
		logrus.Warn("⚠️ Video widget is not set")
		return
	}
	if !fd.videoWidget.IsStreaming() {
		logrus.Warn("⚠️ Video is not running")
		return
	}

	fd.enterFullscreen()
}

// enterFullscreen switches output into fullscreen while minimizing extra UI redraws.
func (fd *FullscreenDialog) enterFullscreen() {
	logrus.Info("🔍 Entering fullscreen mode")

	// On Windows, use a standalone VK fullscreen window instead of a Fyne window.
	// This eliminates the Z-order race between the GLFW TOPMOST window and the VK
	// TOPMOST overlay that caused a black screen in ~50% of fullscreen entries.
	if fd.canWindowlessVKFullscreen() {
		fd.enterWindowlessVKFullscreen()
		return
	}

	restartWindowPipeline := false
	if fd.videoClient != nil && fd.videoClient.SupportsNativeFullscreen() {
		restartWindowPipeline = true
		if err := fd.videoClient.StartNativeFullscreen(); err == nil {
			fd.nativeCapture = newNativeFullscreenCapture(fd.videoWidget.GetMoonlightInput, fd.exitFullscreen)
			if fd.nativeCapture != nil {
				if captureErr := fd.nativeCapture.Start(); captureErr != nil {
					logrus.Warnf("⚠️ Native fullscreen input capture unavailable, stopping native fullscreen: %v", captureErr)
					_ = fd.videoClient.StopNativeFullscreen()
					fd.nativeCapture = nil
				} else {
					fd.isFullscreen = true
					fd.nativeFullscreen = true
					logrus.Info("✅ Activated native fullscreen video sink with macOS input capture")
					return
				}
			}
		} else {
			logrus.Warnf("⚠️ Native fullscreen unavailable, using Fyne fallback: %v", err)
		}
	}
	fd.isFullscreen = true
	fd.nativeFullscreen = false
	fd.createFullscreenWindow()

	if fd.videoClient != nil && fd.videoWidget != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			fd.videoWidget.handleVideoFrame(frame)
			fd.updateVideoFrame(frame)
		})
		logrus.Info("✅ Fullscreen receives frames without redrawing the hidden main canvas")
		if restartWindowPipeline {
			go func() {
				if err := fd.videoClient.ConnectToMoonlight(); err != nil {
					logrus.Warnf("⚠️ Failed to restore the video pipeline for the Fyne fullscreen fallback: %v", err)
				}
			}()
		}
	} else {
		logrus.Warn("⚠️ Video client/widget not set — cannot start fullscreen frame delivery")
	}

	logrus.Info("✅ Fullscreen mode activated")
}

// updateVideoFrame updates the video frame in fullscreen mode
func (fd *FullscreenDialog) updateVideoFrame(frame image.Image) {
	if !fd.isFullscreen {
		return
	}

	// When native overlay (Metal/GL) is rendering, it has its own frame source.
	// Skip canvas updates entirely to prevent picture-in-picture artefacts.
	if fd.videoWidget != nil && fd.videoWidget.isNativeVideoActive() {
		return
	}

	fd.frameMutex.Lock()
	fd.lastFrame = frame
	videoImg := fd.videoImage
	touchpad := fd.touchpadWrapper
	frameCount := fd.videoWidget.frameCount
	fd.frameMutex.Unlock()

	if videoImg == nil {
		logrus.Warn("⚠️ updateVideoFrame: videoImg is nil")
		return
	}

	if frameCount%30 == 0 {
		logrus.Infof("🖼️ Fullscreen mode: updating frame %d (native inactive → canvas)", frameCount)
	}

	fyne.Do(func() {
		// Re-check nativeActive inside fyne.Do: if VK became active after we queued this
		// callback but before the main thread ran it, skip the canvas update. Without this
		// guard, a pending updateVideoFrame queued during the ~250ms transition window runs
		// AFTER onNativeReady clears the canvas, restoring a frozen Go frame behind the overlay.
		if fd.videoWidget != nil && fd.videoWidget.isNativeVideoActive() {
			return
		}
		wasNil := videoImg.Image == nil
		if videoImg.Image != frame {
			videoImg.Image = frame
			if wasNil && frame != nil {
				logrus.Infof("[Fullscreen] updateVideoFrame: fd.videoImage.Image nil→frame (native was inactive during transition)")
			}
		}
		videoImg.Refresh()
		if touchpad != nil {
			touchpad.Refresh()
		}
	})
}

// createFullscreenWindow creates the fullscreen window with video
func (fd *FullscreenDialog) createFullscreenWindow() {
	logrus.Info("🔍 Creating the fullscreen window with video")

	fd.platformInitWindow()

	// When native GPU overlay (Metal/GL) is active, Go-side currentFrame is stale
	// (set from early frames before the overlay took over). Passing it to fd.videoImage
	// causes a ghost background frame in fullscreen alongside the Metal overlay.
	var currentFrame image.Image
	if !fd.videoWidget.isNativeVideoActive() {
		currentFrame = fd.videoWidget.GetCurrentFrame()
	}
	if currentFrame != nil {
		bounds := currentFrame.Bounds()
		logrus.Infof("✅ Set the initial frame in the fullscreen window: %dx%d", bounds.Dx(), bounds.Dy())
	} else {
		logrus.Info("⚠️ No current frame for the fullscreen canvas yet (expected: native overlay or first frame)")
	}

	fd.videoImage = canvas.NewImageFromImage(currentFrame)
	fd.videoImage.FillMode = canvas.ImageFillContain
	fd.videoImage.ScaleMode = canvas.ImageScaleFastest
	fd.touchpadWrapper = NewTouchpadWrapperWithImage(fd.videoWidget, fd.videoImage)
	fd.videoWidget.platformRegisterGestureTarget()
	fd.touchpadWrapper.SetKeyHandlers(fd.handleKeyDown, fd.handleKeyUp, fd.handleKeyPress, fd.handleRunePress)
	fd.touchpadWrapper.SetWindowForFocus(fd.fullscreenWindow)
	logrus.Info("✅ TouchpadWrapper created for fullscreen mode")

	logrus.Info("⌨️ [DEBUG] Creating the virtual keyboard for fullscreen mode")
	fd.virtualKeyboard = graphics.NewVirtualKeyboard(fd.fullscreenWindow, fd.handleVirtualKeyPress, fd.handleRunePress)
	fd.platformSetupUI()

	keyboardLayout := fd.virtualKeyboard.GetKeyboardLayout()
	logrus.Infof("⌨️ [DEBUG] keyboardLayout obtained: %v, MinSize: %v", keyboardLayout != nil, keyboardLayout.MinSize())
	keyboardLayout.Hide()
	logrus.Infof("⌨️ [DEBUG] keyboardLayout.Hide() called, Visible: %v", keyboardLayout.Visible())
	fd.ui = view.NewFullscreenUI(fd.videoImage, fd.touchpadWrapper, keyboardLayout)

	fd.fullscreenWindow.SetContent(fd.ui.MainContainer)

	updatePositions := func() {
		canvasSize := fd.fullscreenWindow.Canvas().Size()
		keyboardHeight := float32(300)
		keyboardSize := fyne.NewSize(canvasSize.Width, view.MobileFooterBottomInset(keyboardHeight))
		keyboardLayout.Resize(keyboardSize)
	}

	fd.fullscreenWindow.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyEscape || string(event.Name) == "Back" {
			logrus.Infof("🔍 Exit key pressed (%s) - exiting fullscreen mode", event.Name)
			fd.exitFullscreen()
			return
		}
		// handleKeyPress already guards against Moonlight-active path (where keys go via
		// KeyDown/KeyUp instead). Canvas.SetOnTypedKey fires on every OS key-repeat, so
		// without the guard we'd get turbo: this call is only meaningful for non-Moonlight.
		fd.handleKeyPress(event)
	})

	fd.fullscreenWindow.Canvas().SetOnTypedRune(func(r rune) {
		fd.handleRunePress(r)
	})

	go func() {
		time.Sleep(150 * time.Millisecond)
		fyne.Do(updatePositions)

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		lastSize := fyne.NewSize(0, 0)
		for range ticker.C {
			if !fd.isFullscreen {
				return
			}

			fyne.Do(func() {
				if !fd.isFullscreen || fd.fullscreenWindow == nil {
					return
				}
				currentSize := fd.fullscreenWindow.Canvas().Size()
				if currentSize != lastSize {
					logrus.Infof("🔄 Screen size change detected: %v -> %v", lastSize, currentSize)
					// If the canvas grew in height — the on-screen keyboard closed.
					// Reset the IME spacer in case onUnfocused didn't fire
					// (e.g. when the keyboard is closed via the "back" button).
					if currentSize.Height > lastSize.Height && lastSize.Height > 0 && fd.virtualKeyboard != nil {
						fd.virtualKeyboard.ResetIMEState()
					}
					updatePositions()
					lastSize = currentSize
				}
			})
		}
	}()

	logrus.Infof("⌨️ [DEBUG] Overlay container created")
	logrus.Info("🔍 Fullscreen window created")
	fd.platformShow()
	logrus.Info("🔍 Fullscreen window shown")

	// Switch Metal overlay to the fullscreen window (full-window coverage).
	// Delay slightly so macOS finishes the fullscreen animation and the
	// contentView.bounds reflect the actual screen size before we create the overlay.
	if fd.videoWidget != nil {
		fsWin := fd.fullscreenWindow
		vw := fd.videoWidget
		fsImg := fd.videoImage

		// Hide/destroy the old main-window overlay immediately so it doesn't float over the new fullscreen window.
		// If we don't do this, it causes a 'picture -> black -> picture' triple blink during the 250ms delay.
		if !vw.keepNativeVideoAliveForFullscreenTransition() {
			vw.stopMetalVideo()
		}

		// Once the native overlay is live, clear the Fyne canvas so only Metal renders.
		// Without this the Go canvas shows its last frame behind the overlay (PiP).
		logrus.Infof("[Fullscreen] setting onNativeReady; fd.videoImage.Image nil=%v", fsImg != nil && fsImg.Image == nil)
		vw.onNativeReady = func() {
			logrus.Infof("[Fullscreen] onNativeReady fired — clearing fd.videoImage.Image (was nil=%v)", fsImg == nil || fsImg.Image == nil)
			if fsImg != nil {
				fsImg.Image = nil
				fsImg.Refresh()
			}
		}
		time.AfterFunc(250*time.Millisecond, func() {
			vw.metalVideoEnterFullscreen(fsWin)
		})
	}

	if fd.videoImage != nil && fd.videoImage.Image != nil {
		fd.videoImage.Refresh()
		logrus.Info("🔍 Image updated after the window was shown")
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !fd.isFullscreen || fd.fullscreenWindow == nil || fd.touchpadWrapper == nil {
			return
		}
		// On both desktop and mobile, focus on the touchpad is needed to intercept the "Back" button without a first tap.
		logrus.Infof("[Fullscreen] 500ms goroutine: calling RequestFocus (nativeActive=%v, videoImage.Image nil=%v)",
			fd.videoWidget != nil && fd.videoWidget.isNativeVideoActive(),
			fd.videoImage == nil || fd.videoImage.Image == nil)
		fyne.Do(func() {
			fd.fullscreenWindow.RequestFocus()
			fd.fullscreenWindow.Canvas().Focus(fd.touchpadWrapper)

			// Force hide system keyboard that might auto-open on Mobile
			if md, ok := fyne.CurrentDevice().(mobile.Device); ok {
				md.HideVirtualKeyboard()
			}

			// On Windows, RequestFocus calls glfwFocusWindow which can promote the Fyne GLFW
			// window above the Vulkan TOPMOST overlay when Vulkan was created in <500ms (2nd+
			// fullscreen entry is faster because the main thread is warmed up). Re-assert the
			// overlay on top after the focus request completes.
			if fd.videoWidget != nil {
				logrus.Infof("[Fullscreen] 500ms goroutine: calling ensureNativeOverlayOnTop (nativeActive=%v)",
					fd.videoWidget.isNativeVideoActive())
				fd.videoWidget.ensureNativeOverlayOnTop()
			}
		})
	}()
}

// updateLayout updates element positions in the fullscreen window
func (fd *FullscreenDialog) updateLayout() {
}

// exitFullscreen exits fullscreen mode
func (fd *FullscreenDialog) exitFullscreen() {
	if !fd.isFullscreen {
		return
	}

	logrus.Info("🔍 Exiting fullscreen mode")

	// Windows standalone VK fullscreen path.
	if fd.windowlessVKFullscreen {
		fd.exitWindowlessVKFullscreen()
		return
	}

	fd.isFullscreen = false
	if fd.nativeFullscreen {
		fd.nativeFullscreen = false
		if fd.nativeCapture != nil {
			if err := fd.nativeCapture.Stop(); err != nil {
				logrus.Warnf("⚠️ Error stopping native input capture: %v", err)
			}
			fd.nativeCapture = nil
		}
		if fd.videoClient != nil {
			if err := fd.videoClient.StopNativeFullscreen(); err != nil {
				logrus.Warnf("⚠️ Error stopping native fullscreen: %v", err)
			}
			if fd.videoWidget != nil {
				fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
					fd.videoWidget.handleVideoFrame(frame)
				})
				go func() {
					if err := fd.videoClient.ConnectToMoonlight(); err != nil {
						logrus.Warnf("⚠️ Failed to restore the windowed video pipeline after native fullscreen: %v", err)
					}
				}()
			}
		}
		logrus.Info("✅ Native fullscreen deactivated")
		return
	}

	fd.frameMutex.Lock()
	fd.videoImage = nil
	fd.touchpadWrapper = nil
	fd.frameMutex.Unlock()
	fd.ui = nil

	if fd.virtualKeyboard != nil {
		fd.virtualKeyboard.UnregisterAsIMETarget()
		fd.virtualKeyboard.Hide()
		fd.virtualKeyboard = nil
	}

	// Destroy the native overlay before the window closes, unless the platform
	// keeps a single overlay alive across fullscreen transitions (Android Vulkan).
	if fd.videoWidget != nil && !fd.videoWidget.keepNativeVideoAliveForFullscreenTransition() {
		fd.videoWidget.stopMetalVideo()
	}

	fd.platformExit()

	if fd.videoClient != nil && fd.videoWidget != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			fd.videoWidget.handleVideoFrame(frame)
		})
		logrus.Info("✅ Frame subscription restored for the main window")
		fd.videoWidget.ensureInputFocusAsync("exit-fullscreen", 150*time.Millisecond)
	}
	fd.lastFrame = nil

	// Restore Metal overlay on the main window.
	if fd.videoWidget != nil {
		fd.videoWidget.metalVideoExitFullscreen()
	}

	logrus.Info("✅ Fullscreen mode deactivated")
}

// IsFullscreen returns the fullscreen mode state
func (fd *FullscreenDialog) IsFullscreen() bool {
	return fd.isFullscreen
}

// toggleAudioMuted toggles the audio mute state
func (fd *FullscreenDialog) toggleAudioMuted() {
	fd.audioMuted = !fd.audioMuted
	if ms, ok := fd.videoClient.(interface{ SetAudioMuted(bool) }); ok {
		ms.SetAudioMuted(fd.audioMuted)
		logrus.Infof("🔊 Audio muted=%v", fd.audioMuted)
	}
}

// SetAudioMuted sets the audio mute state
func (fd *FullscreenDialog) SetAudioMuted(muted bool) {
	fd.audioMuted = muted
}

// HandleUIClick is kept for API compatibility with Windows VK overlay caller.
func (fd *FullscreenDialog) HandleUIClick(_, _ float32) bool { return false }

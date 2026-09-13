//go:build android

package controller

import (
	"image"
	"math"
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (vw *VideoWidget) isNativeVideoActive() bool {
	return service.VKVideoAndroidIsActive()
}

// getNativeFPS returns the real Vulkan render-thread FPS from the last 2-second window.
func (vw *VideoWidget) getNativeFPS() float64 {
	return service.VKVideoAndroidGetFPS()
}

// getMetalLastFrame is a no-op on Android (no Metal overlay).
func (vw *VideoWidget) getMetalLastFrame() *image.RGBA { return nil }

// ensureNativeOverlayOnTop is a no-op on Android (Vulkan SurfaceView managed by OS).
func (vw *VideoWidget) ensureNativeOverlayOnTop() {}

// keepNativeVideoAliveForFullscreenTransition reports true on Android because
// the Vulkan SurfaceView overlay is attached to the Activity, not to a Fyne
// window. Destroying and recreating it on each fullscreen toggle is wrong —
// the SurfaceView stays alive through window content changes.
func (vw *VideoWidget) keepNativeVideoAliveForFullscreenTransition() bool { return true }

// startMetalVideoOnWindow creates a Vulkan SurfaceView overlay for video.
// Falls back to the Fyne canvas render-ticker if Vulkan is unavailable.
func (vw *VideoWidget) startMetalVideoOnWindow(_ fyne.Window, fullscreen bool) {
	if fullscreen {
		return
	}

	// Wire overlay lifecycle hooks so menus/popups hide the VK SurfaceView.
	view.OnOverlayShow = func() { service.VKVideoAndroidSetHidden(true) }
	view.OnOverlayHide = func() { service.VKVideoAndroidSetHidden(false) }

	// Compute the actual pixel rect for the video area now, before creating
	// the overlay.  Passing (0,0,0,0) would cause the SurfaceView to be
	// created at 1×1, producing a 1×1 Vulkan swapchain.  All subsequent
	// frames would be presented on that invisible 1×1 surface while the Fyne
	// canvas stays frozen at the last pre-Vulkan frame.
	var px, py, pw, ph int
	done := make(chan struct{})
	fyne.Do(func() {
		x, y, w, h := vw.videoCanvasFrame()
		scale := float32(1)
		if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
			scale = vw.parentWindow.Canvas().Scale()
		}
		px = int(x * scale)
		py = int(y * scale)
		pw = int(w * scale)
		ph = int(h * scale)
		close(done)
	})
	<-done

	if service.VKVideoAndroidCreate(px, py, pw, ph) {
		logrus.Infof("[Android/VK] Vulkan overlay created at (%d,%d) %dx%d px", px, py, pw, ph)
		// Reinitialize cursor at the correct display density scale.
		vw.initAndroidCursorScale(vw.androidCursorScale())
		if view.OverlayActive() {
			service.VKVideoAndroidSetHidden(true)
		}
		if cb := vw.onNativeReady; cb != nil {
			vw.onNativeReady = nil
			cb()
		}
		// Fallthrough to startRenderTicker even with Vulkan — we need it to
		// call renderLatestFrame, which hides the Fyne canvas and tracks resizes.
	} else {
		// Fallback: Disable overlay hooks if Vulkan failed.
		view.OnOverlayShow = nil
		view.OnOverlayHide = nil
		logrus.Infof("[Android] Vulkan unavailable — using Fyne canvas fallback")
	}

	fps := 60
	if vw.videoClient != nil {
		if cfg := vw.videoClient.GetConfig(); cfg != nil && cfg.VideoFPS > 0 {
			fps = cfg.VideoFPS
		}
	}
	vw.startRenderTicker(fps)
}

func (vw *VideoWidget) stopMetalVideo() {
	vw.onNativeReady = nil
	view.OnOverlayShow = nil
	view.OnOverlayHide = nil
	service.VKVideoAndroidDestroy()
}

// HandleAppBackgrounded hides the Vulkan SurfaceView the instant the app
// leaves the foreground (registered against fyne.Lifecycle in
// main_window.go). The SurfaceView is attached directly to the Activity,
// not to any Fyne window (see keepNativeVideoAliveForFullscreenTransition's
// doc comment), so it keeps rendering completely independently of whatever
// Fyne screen is/was showing underneath -- including a screen the user has
// since navigated away from.
//
// Exists for a real race confirmed live, especially without the
// "unrestricted battery usage" exemption (Android's Doze/App Standby
// throttles background network + goroutines much harder without it): a
// ConnectToMoonlight call in flight when the app backgrounds can have its
// underlying HTTP/socket work frozen mid-flight while a *different*
// wall-clock timer (e.g. resolvePreferredVideoConfig's HTTP client
// deadline) keeps ticking in real time regardless -- so by the time the
// user reopens the app, that deadline has already fired (showing a
// connect/error screen), and moments later the previously-frozen moonlight
// connection thaws, catches up, and delivers frames -- passing
// handleVideoFrame's `isStreaming` guard (which was never told this
// attempt was superseded) and popping the video overlay up directly over
// whatever screen the user is now looking at. Hiding proactively on
// backgrounding closes that window: even if a stray frame lands while
// backgrounded or just after resuming, there's nothing to show until
// HandleAppForegrounded's reconcile explicitly decides it's still correct
// to unhide.
func (vw *VideoWidget) HandleAppBackgrounded() {
	if service.VKVideoAndroidIsActive() {
		service.VKVideoAndroidSetHidden(true)
	}
}

// HandleAppForegrounded re-validates video state the instant the app
// returns to the foreground (registered against fyne.Lifecycle in
// main_window.go) -- see HandleAppBackgrounded's doc comment for the race
// this closes the other half of.
//
// scheduleVideoReconcile only acts if desiredStreaming/isStreaming actually
// disagree (or a restart is already pending), so this is a no-op on a
// normal resume where nothing went stale while backgrounded. The overlay
// itself is only unhidden if a Fyne overlay (menu/dialog) isn't *also*
// currently covering it -- OnOverlayShow/OnOverlayHide already owns that
// signal independently (see startMetalVideoOnWindow), so deferring to
// view.OverlayActive() here avoids fighting that mechanism.
func (vw *VideoWidget) HandleAppForegrounded() {
	vw.scheduleVideoReconcile("app-resumed")
	if service.VKVideoAndroidIsActive() && !view.OverlayActive() {
		service.VKVideoAndroidSetHidden(false)
	}
}

// onIMEHeightChanged is called when the Android system IME appears/disappears.
// NavBar is ~20-50dp; real system keyboard is >150dp. Only expand for the real keyboard.
func (vw *VideoWidget) onIMEHeightChanged(imeHeightDp float32) {
	const minRealIMEDp = 100
	imeOpen := imeHeightDp > minRealIMEDp
	// System Back / GBoard ↓ often hide the soft IME without Activity.onBackPressed.
	// If our stack is still open after the open animation, collapse it with the IME.
	if !imeOpen && time.Since(vw.imeStackArmedAt) > 450*time.Millisecond {
		if vw.IsVirtualKeyboardVisible() || vw.IsSystemIMESticky() {
			logrus.Info("⌨️ System IME closed — collapsing keyboard stack")
			vw.CloseAllKeyboards()
		}
	}
	if imeOpen {
		setImeExpandHeightDp(imeHeightDp)
	} else {
		setImeExpandHeightDp(0)
	}
	vw.syncKeyboardBottomInsetFromIME(imeHeightDp)
	// Special keys in the main header: top-align so letterbox sits near the
	// IME, not as a black band under the keys. Otherwise bottom-align to IME.
	switch {
	case imeOpen && vw.specialKeysInMainHeader() && vw.IsVirtualKeyboardVisible():
		service.VKVideoAndroidSetAlignTop(true)
	case imeOpen:
		service.VKVideoAndroidSetAlignBottom(true)
	default:
		service.VKVideoAndroidSetAlignBottom(false)
		service.VKVideoAndroidSetAlignTop(false)
	}
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)

	// Invalidate again after a short delay so Vulkan picks up the layout
	// after the top safe-area inset is cleared for the keyboard stack.
	go func() {
		time.Sleep(100 * time.Millisecond)
		fyne.Do(func() {
			vw.InvalidateOverlayGeometry()
			vw.forceCanvasRefresh.Store(true)
		})
	}()
	if tw := vw.touchpadWrapper; tw != nil {
		if sz := tw.Size(); sz.Width > 0 && sz.Height > 0 {
			vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, nil)
		}
	}
	if imeOpen && (vw.IsVirtualKeyboardVisible() || vw.IsSystemIMESticky()) {
		vw.focusViewportOnVirtualCursorForKeyboard()
	}
}

// vkLastRenderedW/H track the last pixel size sent to the Vulkan overlay.
// Any change (rotation, keyboard, fullscreen) triggers a forced swapchain recreation
// so the render thread picks up the new surface dimensions immediately.
var vkLastRenderedW, vkLastRenderedH int

func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.VKVideoAndroidIsActive() {
		return
	}
	x, y, w, h := vw.videoCanvasFrame()
	if w <= 0 || h <= 0 {
		return
	}
	scale := float32(1)
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		scale = vw.parentWindow.Canvas().Scale()
	}
	pw, ph := int(w*scale), int(h*scale)
	if pw != vkLastRenderedW || ph != vkLastRenderedH {
		vkLastRenderedW, vkLastRenderedH = pw, ph
		service.VKVideoAndroidForceRecreateSwapchain()
	}
	service.VKVideoAndroidUpdateRect(int(x*scale), int(y*scale), pw, ph)
	vw.updateNativeViewportAndCursor()
}

func (vw *VideoWidget) metalVideoEnterFullscreen(_ fyne.Window) {
	// Vulkan stays VISIBLE in fullscreen — videoCanvasFrame() detects IsFullscreen()
	// and returns the full-canvas rect so the SurfaceView expands to fill the screen.
	// Force swapchain recreation so the render thread picks up the new surface
	// dimensions immediately (SetFullScreen may have resized the SurfaceView surface).
	vw.forceCanvasRefresh.Store(true)
	service.VKVideoAndroidForceRecreateSwapchain()
}

func (vw *VideoWidget) metalVideoExitFullscreen() {
	// Restoring from fullscreen requires swapchain recreation for the same reason.
	vw.forceCanvasRefresh.Store(true)
	service.VKVideoAndroidForceRecreateSwapchain()

	// touchpadSizeW/H was set to the fullscreen canvas dimensions while in fullscreen.
	// After exit the main window is smaller (has tab bar, etc.), so pan-limit
	// calculations in recalculateViewport() would use the wrong (larger) size —
	// allowing the user to drag video past the bottom boundary.
	// Trigger a reset once the main-window layout has been recalculated.
	go func() {
		time.Sleep(120 * time.Millisecond)
		fyne.Do(func() {
			if vw.touchpadWrapper != nil {
				sz := vw.touchpadWrapper.Size()
				if sz.Width > 0 && sz.Height > 0 {
					vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, nil)
				}
			}
		})
	}()
}

// updateNativeViewportAndCursor forwards the current Go viewport state (zoom/pan)
// to the Vulkan renderer and updates the virtual cursor if in cursor mode.
func (vw *VideoWidget) updateNativeViewportAndCursor() {
	if !service.VKVideoAndroidIsActive() {
		return
	}

	if isVirtualCursorLikeMode(vw.GetMouseInputMode()) {
		// Two-finger pan/zoom owns the viewport until the user moves the
		// virtual cursor again. Auto-centering while zoomed was overwriting
		// panOffset every push and snapping the picture back to center.
		if !vw.multiTouchActive && !vw.viewportManualControl {
			vw.vcMu.Lock()
			targetU := vw.virtualCursorU
			targetV := vw.virtualCursorV
			vw.vcMu.Unlock()

			vw.centerViewportOnVirtualCursor(targetU, targetV)

			// After updating pan, refresh contentRect so UV below matches.
			if tw := vw.activeViewportWrapper(); tw != nil {
				vw.UpdateTouchpadAndContentRect(vw.touchpadSizeW, vw.touchpadSizeH, nil)
			}
		}
	}

	// RustDesk-style zoom: always blit the full frame into an aspect-fit dest
	// scaled by zoomScale, then pan that dest. UV crop + stretch was deforming
	// the picture (horizontal squash) and jumping size when leaving fit mode.
	u0, v0, u1, v1 := float32(0), float32(0), float32(1), float32(1)

	scale := float32(1)
	if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
		scale = vw.parentWindow.Canvas().Scale()
	}
	if scale <= 0 {
		scale = 1
	}
	blitPanX := vw.panOffsetX * scale
	blitPanY := vw.panOffsetY * scale
	zoom := vw.zoomScale
	if zoom < 1 {
		zoom = 1
	}

	// Write viewport + cursor in a single mutex-protected call so the C render
	// thread always reads a consistent snapshot: never viewport from update N
	// with cursor from update N+1, which would flash the cursor to a wrong position.
	cursorVisible := isVirtualCursorLikeMode(vw.GetMouseInputMode())
	var uc, vc float32
	if cursorVisible {
		vw.vcMu.Lock()
		uc, vc = vw.virtualCursorU, vw.virtualCursorV
		vw.vcMu.Unlock()
	}
	service.VKVideoAndroidSetViewportAndCursor(u0, v0, u1, v1, uc, vc, cursorVisible, blitPanX, blitPanY, zoom)
}

// centerViewportOnVirtualCursor pans the viewport so the virtual cursor is
// centred on screen (RustDesk-style follow). Only effective when zoom > 1.
//
// Disabled while the user owns the viewport via two-finger pan/zoom
// (viewportManualControl / multiTouch) — callers already skip this. Even when
// armed, never wipe pan on a fitting axis (that yanked vertical letterbox pan
// back to center) and never pull a manual overflow pan toward the cursor
// unless the cursor actually moved (handled by clearing viewportManualControl).
func (vw *VideoWidget) centerViewportOnVirtualCursor(u, v float32) {
	if vw.zoomScale <= 1.001 {
		return
	}
	if vw.viewportManualControl || vw.multiTouchActive {
		return
	}
	cw := vw.baseContentRectW * vw.zoomScale
	ch := vw.baseContentRectH * vw.zoomScale

	if cw > vw.touchpadSizeW {
		idealPanX := cw * (0.5 - u)
		maxPanX := (cw - vw.touchpadSizeW) / 2
		zoneX := vw.touchpadSizeW * 0.15
		vw.panOffsetX = softClampEdgePan(idealPanX, -maxPanX, maxPanX, zoneX)
	}
	// If width still fits, leave panOffsetX alone (do not force 0).

	availH := vw.touchpadSizeH - vw.bottomInset
	if ch > availH {
		focusY := float32(0.5)
		extraUp := float32(0)
		extraDown := float32(0)
		if vw.keyboardViewportLift {
			focusY = keyboardFocusYFrac
			extraUp = availH * keyboardFocusExtraLiftFrac
			if extraUp < keyboardFocusExtraLiftMinDp {
				extraUp = keyboardFocusExtraLiftMinDp
			}
			extraDown = extraUp
		}
		idealPanY := availH*(focusY-0.5) + ch*(0.5-v)
		maxPanY := (ch - availH) / 2
		zoneY := availH * 0.15
		vw.panOffsetY = softClampEdgePan(idealPanY, -maxPanY-extraUp, maxPanY+extraDown, zoneY)
	}
	// If height still fits, leave panOffsetY alone (do not force 0).

	vw.recalculateViewport()
}

// initAndroidCursorScale rasterizes cursor-pointer.svg at the requested pixel
// size and uploads the result to the Vulkan cursor buffer.
func (vw *VideoWidget) initAndroidCursorScale(scale int) {
	if !service.VKVideoAndroidIsActive() {
		return
	}
	if scale < 1 {
		scale = 1
	}
	// SVG viewBox is 18×24; scale that by the density factor.
	w, h := 18*scale, 24*scale
	img := rasterizeSVGToNRGBA(assets.CursorPointerSVG, w, h)
	if img == nil {
		logrus.Warn("[Android/VK] cursor SVG rasterization failed")
		return
	}
	b := img.Bounds()
	service.VKVideoAndroidSetCursorPixels(img.Pix, b.Dx(), b.Dy())
}

// androidCursorScale returns the integer cursor scale factor for the current
// display density (1-4×).
func (vw *VideoWidget) androidCursorScale() int {
	if vw.parentWindow == nil {
		return 2
	}
	scale := vw.parentWindow.Canvas().Scale()
	s := int(math.Round(float64(scale)))
	if s < 1 {
		s = 1
	}
	if s > 2 {
		s = 2
	}
	return s
}

// videoCanvasFrame returns the Vulkan SurfaceView rect in window-local dp coords.
//   - Fullscreen: full canvas (Vulkan expands to fill the screen).
//   - Keyboard visible: video area above the keyboard panel.
//   - Normal: the video container's real canvas origin (not canvasH−height —
//     that assumed the container was flush with the window bottom and covers
//     Control's tab bar + AppFooter once those sit under the video).
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.parentWindow == nil {
		return
	}
	cs := vw.parentWindow.Canvas().Size()

	// In fullscreen mode the Vulkan SurfaceView covers the whole screen.
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.IsFullscreen() {
		return 0, 0, cs.Width, cs.Height
	}

	if vw.container == nil {
		return
	}
	sz := vw.container.Size()
	pos := vw.videoContainerOrigin()
	// Nudge the SurfaceView down a few dp so it clears the header hairline
	// without growing past the container bottom (height shrinks by the same).
	headerClearance := float32(8)
	// Mobile special keys replace the main header — sit flush under that band
	// (no extra black strip between keys and video).
	if vw.specialKeysInMainHeader() && vw.IsVirtualKeyboardVisible() {
		headerClearance = 0
	}
	keysH := vw.specialKeysOverlayHeightDp()
	top := headerClearance + keysH

	// With setZOrderOnTop(true) Fyne cannot paint over Vulkan pixels. Mobile
	// special keys replace the main header (above the surface); any residual
	// keysH inset is for non-header overlay paths only.
	videoTop := pos.Y + top
	if r := vw.specialKeysHeaderReserve; r > 0 && videoTop < r {
		videoTop = r
	}
	videoBottom := pos.Y + sz.Height
	if videoBottom < videoTop {
		videoBottom = videoTop
	}
	if imeH := getImeExpandHeightDp(); imeH > 0 {
		imeTop := cs.Height - imeH
		if imeTop < videoBottom {
			videoBottom = imeTop
		}
	}
	videoH := videoBottom - videoTop
	if videoH <= 0 {
		return
	}
	return pos.X, videoTop, sz.Width, videoH
}

func (vw *VideoWidget) platformSetSystemIMESticky(on bool) {
	if on == vw.systemIMESticky.Load() {
		if on {
			graphics.SetStickySystemIME(true)
			graphics.SetIMETextHandler(vw.handleNativeIMEText)
			graphics.SetIMEUserDismissedHandler(vw.CloseAllKeyboards)
		}
		return
	}
	vw.systemIMESticky.Store(on)
	if on {
		vw.imeStackArmedAt = time.Now()
		vw.ensureIMEKeyboardTarget()
		// Native EditText owns the soft keyboard. Text goes KeyboardBridge
		// onIMETextInput (LCP diff) → UTF-8 — not Fyne keyboardTyped.
		graphics.SetIMETextHandler(vw.handleNativeIMEText)
		graphics.SetIMEUserDismissedHandler(vw.CloseAllKeyboards)
		graphics.SetStickySystemIME(true)
		if vw.touchpadWrapper != nil && vw.parentWindow != nil {
			vw.parentWindow.Canvas().Focus(vw.touchpadWrapper)
		}
		if vw.specialKeysInMainHeader() && vw.IsVirtualKeyboardVisible() {
			service.VKVideoAndroidSetAlignTop(true)
		}
		vw.InvalidateOverlayGeometry()
		vw.forceCanvasRefresh.Store(true)
		// Top inset → 0 is async; remeasure Vulkan after Fyne drops the pad.
		for _, delay := range []time.Duration{100 * time.Millisecond, 280 * time.Millisecond} {
			d := delay
			time.AfterFunc(d, func() {
				fyne.Do(func() {
					if !vw.systemIMESticky.Load() {
						return
					}
					if vw.specialKeysInMainHeader() && vw.IsVirtualKeyboardVisible() {
						service.VKVideoAndroidSetAlignTop(true)
					}
					vw.InvalidateOverlayGeometry()
					vw.forceCanvasRefresh.Store(true)
					if tw := vw.touchpadWrapper; tw != nil {
						if sz := tw.Size(); sz.Width > 0 && sz.Height > 0 {
							vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, nil)
						}
					}
				})
			})
		}
		logrus.Info("⌨️ System IME sticky ON (native diff → UTF-8)")
		return
	}
	graphics.SetIMETextHandler(nil)
	graphics.SetIMEUserDismissedHandler(nil)
	graphics.SetStickySystemIME(false)
	setImeExpandHeightDp(0)
	service.VKVideoAndroidSetAlignBottom(false)
	service.VKVideoAndroidSetAlignTop(false)
	vw.InvalidateOverlayGeometry()
	vw.forceCanvasRefresh.Store(true)
	logrus.Info("⌨️ System IME sticky OFF")
}

// handleNativeIMEText applies sticky soft-IME diffs from KeyboardBridge.
func (vw *VideoWidget) handleNativeIMEText(deleteCount int, text string) {
	mi := vw.moonlightInput()
	if mi == nil {
		return
	}
	logrus.Infof("⌨️ [IME-TEXT] del=%d add=%q", deleteCount, text)
	for i := 0; i < deleteCount; i++ {
		vw.enqueueSend(func() {
			mi.SendMoonlightKey(0x08, service.LiKeyActionDown, 0)
			mi.SendMoonlightKey(0x08, service.LiKeyActionUp, 0)
		})
	}
	if text != "" {
		t := text
		vw.enqueueSend(func() { mi.SendMoonlightUtf8Text(t) })
	}
}

func (vw *VideoWidget) ensureIMEKeyboardTarget() {
	vw.ensureMobileVirtualKeyboard()
	if vw.virtualKeyboard != nil {
		vw.virtualKeyboard.RegisterAsIMETarget()
	}
}

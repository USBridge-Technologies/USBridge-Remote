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
	view.OnOverlayHide = func() { service.VKVideoAndroidSetHidden(view.VideoShouldBeHidden()) }

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
		px, py, pw, ph = vkSurfacePx(x, y, w, h, scale)
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
	if service.VKVideoAndroidIsActive() {
		service.VKVideoAndroidSetHidden(view.VideoShouldBeHidden())
	}
}

// onIMEHeightChanged is called when the Android system IME appears/disappears.
// NavBar is ~20-50dp; real system keyboard is >150dp. Only expand for the real keyboard.
func (vw *VideoWidget) onIMEHeightChanged(imeHeightDp float32) {
	const minRealIMEDp = 100
	const imeHeightSnapDp = 24
	imeOpen := imeHeightDp > minRealIMEDp
	if imeOpen {
		vw.imeConfirmedOpen.Store(true)
		rememberImeHeightDp(imeHeightDp)
	}
	// System Back / GBoard ↓ often hide the soft IME without Activity.onBackPressed.
	// Only collapse after a real IME was confirmed — a nav-bar-sized height
	// during show (or a delayed/aborted GBoard) used to flash special keys
	// and immediately CloseAllKeyboards.
	if !imeOpen && vw.imeConfirmedOpen.Load() && time.Since(vw.imeStackArmedAt) > 450*time.Millisecond {
		if vw.IsVirtualKeyboardVisible() || vw.IsSystemIMESticky() {
			logrus.Info("⌨️ System IME closed — collapsing keyboard stack")
			fyne.Do(func() { vw.CloseAllKeyboards() })
			return
		}
	}
	if !imeOpen && !vw.imeConfirmedOpen.Load() && vw.IsSystemIMESticky() &&
		time.Since(vw.imeStackArmedAt) > 350*time.Millisecond &&
		vw.imeShowRetryUsed.CompareAndSwap(false, true) {
		logrus.Info("⌨️ System IME not visible after arm — retrying sticky show")
		graphics.SetStickySystemIME(true)
	}
	if !imeOpen {
		return
	}
	cur := getImeExpandHeightDp()
	delta := imeHeightDp - cur
	if delta < 0 {
		delta = -delta
	}
	if cur > minRealIMEDp && delta < imeHeightSnapDp {
		return
	}
	if !imeCropsVideoOverlay() {
		// Landscape floating IME: still track height for dismiss, but do not
		// shrink the SurfaceView under the keyboard widget.
		setImeExpandHeightDp(0)
		vw.bottomInset = 0
		vw.keyboardViewportLift = false
		vw.applyImmediateKeyboardViewport()
		return
	}
	setImeExpandHeightDp(imeHeightDp)
	vw.syncKeyboardBottomInsetFromIME(imeHeightDp)
	vw.applyImmediateKeyboardViewport()
}

func (vw *VideoWidget) platformAfterKeyboardViewportSettle() {
	imeOpen := getImeExpandHeightDp() > 100 && imeCropsVideoOverlay()
	if imeOpen {
		// Same as the IME-only path: sit the picture on the keyboard and
		// leave letterbox under the special-keys header. AlignTop used to
		// flush the frame into the keys, which cropped the remote top and
		// left no black band to pan the desktop down into.
		service.VKVideoAndroidSetAlignBottom(true)
		service.VKVideoAndroidSetAlignTop(false)
	} else {
		service.VKVideoAndroidSetAlignBottom(false)
		service.VKVideoAndroidSetAlignTop(false)
	}
	if tw := vw.touchpadWrapper; tw != nil {
		if sz := tw.Size(); sz.Width > 0 && sz.Height > 0 {
			vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, nil)
		}
	}
}

func (vw *VideoWidget) platformSyncKeyboardBottomInsetFromIME(imeHeightDp float32) {
	vw.syncKeyboardBottomInsetFromIME(imeHeightDp)
}

// vkLastRendered* track the last pixel rect sent to the Vulkan overlay.
// Any change (rotation, keyboard, fullscreen, safe-area) triggers a forced
// swapchain recreation so the render thread picks up the new surface immediately.
var vkLastRenderedX, vkLastRenderedY, vkLastRenderedW, vkLastRenderedH int

func (vw *VideoWidget) updateMetalVideoFrame() {
	if !service.VKVideoAndroidIsActive() {
		return
	}
	// Hide first, even if the Control container already has a 0-size after
	// a tab switch (zoom + Devices used to skip this and leave Vulkan up).
	hidden := view.VideoShouldBeHidden()
	service.VKVideoAndroidSetHidden(hidden)
	if hidden {
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
	px, py, pw, ph := vkSurfacePx(x, y, w, h, scale)
	if px != vkLastRenderedX || py != vkLastRenderedY || pw != vkLastRenderedW || ph != vkLastRenderedH {
		inset := fyne.NewPos(0, 0)
		if vw.parentWindow != nil && vw.parentWindow.Canvas() != nil {
			inset = canvasInteractiveOrigin(vw.parentWindow.Canvas())
		}
		logrus.Infof("[Android/VK] overlay dp=(%.1f,%.1f %.1fx%.1f) inset=(%.1f,%.1f) px=(%d,%d %dx%d) scale=%.2f",
			x, y, w, h, inset.X, inset.Y, px, py, pw, ph, scale)
	}
	vkLastRenderedX, vkLastRenderedY = px, py
	vkLastRenderedW, vkLastRenderedH = pw, ph
	service.VKVideoAndroidUpdateRect(px, py, pw, ph)
	if tw := vw.activeViewportWrapper(); tw != nil {
		if sz := tw.Size(); sz.Width > 0 && sz.Height > 0 {
			vw.UpdateTouchpadAndContentRect(sz.Width, sz.Height, nil)
		}
	}
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
			extraUp = float32(0)
		}
		idealPanY := availH*(focusY-0.5) + ch*(0.5-v)
		maxPanY := (ch - availH) / 2
		zoneY := availH * 0.15
		vw.panOffsetY = softClampEdgePan(idealPanY, -maxPanY-extraUp, maxPanY+extraDown, zoneY)
	} else {
		vw.panOffsetY = 0
	}

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

// vkSurfacePx converts a window-canvas dp frame to SurfaceView pixels.
// Y is already in decorView space (InteractiveArea added in videoCanvasFrame).
// Top/bottom edges are rounded independently so round(y)+round(h) cannot
// overshoot the footer hairline by a pixel.
func vkSurfacePx(x, y, w, h, scale float32) (px, py, pw, ph int) {
	px = int(math.Round(float64(x * scale)))
	py = int(math.Round(float64(y * scale)))
	pw = int(math.Round(float64((x+w)*scale))) - px
	ph = int(math.Round(float64((y+h)*scale))) - py
	if pw < 1 {
		pw = 1
	}
	if ph < 1 {
		ph = 1
	}
	return
}

// videoCanvasFrame returns the Vulkan SurfaceView rect in window-local dp coords
// (same origin as Canvas.Size / the Activity decorView — not InteractiveArea).
//   - Fullscreen: full canvas (Vulkan expands to fill the screen).
//   - Keyboard visible: video area above the keyboard panel.
//   - Normal: container origin + safe-area inset so punch-hole / status-bar
//     height is included on every device.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.parentWindow == nil {
		return
	}
	c := vw.parentWindow.Canvas()
	cs := c.Size()

	// In fullscreen mode the Vulkan SurfaceView covers the whole screen.
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.IsFullscreen() {
		return 0, 0, cs.Width, cs.Height
	}

	if vw.container == nil {
		return
	}
	sz := vw.container.Size()
	abs := vw.videoContainerOrigin()
	inset := canvasInteractiveOrigin(c)
	// Keyboard stack owns the cutout band and zeros the top inset. Skip a
	// stale safe-top so Vulkan does not slide over the special keys.
	if vw.specialKeysHeaderReserve > 0 {
		inset = fyne.NewPos(inset.X, 0)
	}
	pos := overlayWindowPos(abs, inset)
	// Stop at the container bottom so the Control footer hairline stays
	// visible. A dp bleed used to cover that 1dp TopLine.
	keysH := vw.specialKeysOverlayHeightDp()
	videoTop := pos.Y + keysH
	if r := vw.specialKeysHeaderReserve; r > 0 && videoTop < r {
		videoTop = r
	}
	videoBottom := pos.Y + sz.Height
	if videoBottom < videoTop {
		videoBottom = videoTop
	}
	if imeH := getImeExpandHeightDp(); imeH > 0 && imeCropsVideoOverlay() {
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

// nativePointerOriginDp maps SurfaceView origin into TouchpadWrapper-local dp.
// Android touches are wrapper-local; Vulkan dest is SurfaceView-local. When the
// SurfaceView is inset (special keys / IME) the picture starts below the wrapper
// origin and mouse mapping must follow it.
func (vw *VideoWidget) nativePointerOriginDp() (float32, float32) {
	sx, sy, sw, sh := vw.videoCanvasFrame()
	tw := vw.activeViewportWrapper()
	if tw == nil {
		return 0, 0
	}
	sz := tw.Size()
	if sw > 0 && sh > 0 && almostEqual(sw, sz.Width) && almostEqual(sh, sz.Height) {
		return 0, 0
	}
	wx, wy := float32(0), float32(0)
	if fyne.CurrentApp() != nil {
		if drv, ok := fyne.CurrentApp().Driver().(interface {
			AbsolutePositionForObject(fyne.CanvasObject) fyne.Position
		}); ok {
			p := drv.AbsolutePositionForObject(tw)
			wx, wy = p.X, p.Y
		}
	}
	return sx - wx, sy - wy
}

func (vw *VideoWidget) platformSetSystemIMESticky(on bool) {
	if on == vw.systemIMESticky.Load() {
		if on {
			graphics.SetStickySystemIME(true)
			graphics.SetIMETextHandler(vw.handleNativeIMEText)
			graphics.SetIMEUserDismissedHandler(func() {
				fyne.Do(func() { vw.CloseAllKeyboards() })
			})
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
		graphics.SetIMEUserDismissedHandler(func() {
			fyne.Do(func() { vw.CloseAllKeyboards() })
		})
		graphics.SetStickySystemIME(true)
		if vw.touchpadWrapper != nil && vw.parentWindow != nil {
			vw.parentWindow.Canvas().Focus(vw.touchpadWrapper)
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
	logrus.Info("⌨️ System IME sticky OFF")
}

func (vw *VideoWidget) refocusStickySystemIME() bool {
	// Android sticky IME is Activity-owned; keep Fyne focus on the touchpad.
	return false
}

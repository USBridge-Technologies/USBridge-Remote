//go:build android

package controller

import (
	"bytes"
	"image"
	"math"
	"sync/atomic"
	"time"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
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

// imeExpandBits stores math.Float32bits(imeHeightDp) atomically.
// Non-zero means the system IME is open and the Vulkan SurfaceView should expand
// to cover everything above the IME (tabs, custom keyboard panel, etc.).
var imeExpandBits atomic.Int32

func setImeExpandHeightDp(h float32) {
	imeExpandBits.Store(int32(math.Float32bits(h)))
}

func getImeExpandHeightDp() float32 {
	return math.Float32frombits(uint32(imeExpandBits.Load()))
}

// onIMEHeightChanged is called when the Android system IME appears/disappears.
// NavBar is ~20-50dp; real system keyboard is >150dp. Only expand for the real keyboard.
func (vw *VideoWidget) onIMEHeightChanged(imeHeightDp float32) {
	const minRealIMEDp = 100
	imeOpen := imeHeightDp > minRealIMEDp
	if imeOpen {
		setImeExpandHeightDp(imeHeightDp)
	} else {
		setImeExpandHeightDp(0)
	}
	// Bottom-align the fitted video rect in the Vulkan swapchain while the system
	// IME is open: the SurfaceView expands upward into the tab-bar area, so
	// center-fit would leave a black gap between the video and the keyboard panel.
	service.VKVideoAndroidSetAlignBottom(imeOpen)
	vw.forceCanvasRefresh.Store(true)
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
	// Restore normal rect — videoCanvasFrame() will return to the video-area rect.
	// Force swapchain recreation so the render thread picks up the restored size.
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
		vw.vcMu.Lock()
		targetU := vw.virtualCursorU
		targetV := vw.virtualCursorV
		vw.vcMu.Unlock()

		// Center the viewport mathematically on the raw cursor with spring easing
		vw.centerViewportOnVirtualCursor(targetU, targetV)
	}

	// When the system IME is open, the Vulkan SurfaceView expands above the
	// touchpad widget by topOffset dp (the tab-bar height). Subtract that extra
	// distance from contentRectY so v0 reaches into the video content that sits
	// above the original touchpad top — filling the expanded area with real video
	// instead of leaving a black strip.
	extraTopDp := float32(0)
	if getImeExpandHeightDp() > 0 && vw.parentWindow != nil && vw.container != nil {
		cs := vw.parentWindow.Canvas().Size()
		topOffset := cs.Height - vw.container.Size().Height
		if topOffset > 0 {
			extraTopDp = topOffset
		}
	}

	// Compute visible UV rect from Go viewport state.
	cw, ch := vw.contentRectW, vw.contentRectH
	var u0, v0, u1, v1 float32
	if cw <= 0 || ch <= 0 {
		u0, v0, u1, v1 = 0, 0, 1, 1
	} else {
		u0 = clampFloat(-vw.contentRectX/cw, 0, 1)
		v0 = clampFloat(-(vw.contentRectY+extraTopDp)/ch, 0, 1)
		u1 = clampFloat((vw.touchpadSizeW-vw.contentRectX)/cw, 0, 1)
		v1 = clampFloat((vw.touchpadSizeH-vw.contentRectY)/ch, 0, 1)
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
	service.VKVideoAndroidSetViewportAndCursor(u0, v0, u1, v1, uc, vc, cursorVisible)
}

// softClampEdgePan clamps val to [lo, hi] with a smoothstep blend zone of size
// zone inside each limit. The blend zone runs from (lo) to (lo+zone) and from
// (hi-zone) to (hi). Inside these zones the output is eased toward the limit
// using a smoothstep curve whose derivative is ZERO at the hard limit — so tiny
// oscillations of val around the boundary (touch noise ε) produce only ε²/zone
// change in the output instead of ε, effectively eliminating viewport jitter.
func softClampEdgePan(val, lo, hi, zone float32) float32 {
	if val <= lo {
		return lo
	}
	if val >= hi {
		return hi
	}
	if halfRange := (hi - lo) / 2; zone > halfRange {
		zone = halfRange
	}
	if zone <= 0 {
		return val
	}
	if val < lo+zone {
		// Near lower limit: ease val toward lo.
		t := (lo + zone - val) / zone // 1 at lo, 0 at lo+zone
		t = t * t * (3 - 2*t)         // smoothstep — zero derivative at lo
		return val*(1-t) + lo*t
	}
	if val > hi-zone {
		// Near upper limit: ease val toward hi.
		t := (val - (hi - zone)) / zone // 0 at hi-zone, 1 at hi
		t = t * t * (3 - 2*t)           // smoothstep — zero derivative at hi
		return val*(1-t) + hi*t
	}
	return val
}

// centerViewportOnVirtualCursor pans the viewport so the virtual cursor is
// centred on screen (RustDesk-style follow).  Only effective when zoom > 1.
func (vw *VideoWidget) centerViewportOnVirtualCursor(u, v float32) {
	if vw.zoomScale <= 1.001 {
		return
	}
	cw := vw.baseContentRectW * vw.zoomScale
	ch := vw.baseContentRectH * vw.zoomScale

	// X: cursor-centering pan.
	var idealPanX, maxPanX float32
	if cw > vw.touchpadSizeW {
		idealPanX = cw * (0.5 - u)
		maxPanX = (cw - vw.touchpadSizeW) / 2
		zoneX := vw.touchpadSizeW * 0.15
		vw.panOffsetX = softClampEdgePan(idealPanX, -maxPanX, maxPanX, zoneX)
	} else {
		vw.panOffsetX = 0
	}

	// Y: cursor-centering pan (only when content taller than screen).
	availH := vw.touchpadSizeH - vw.bottomInset
	if ch > availH {
		idealPanY := availH/2 - (availH-ch) - v*ch
		maxPanY := ch - availH
		zoneY := availH * 0.15
		vw.panOffsetY = softClampEdgePan(idealPanY, 0, maxPanY, zoneY)
	} else {
		vw.panOffsetY = 0
	}

	vw.recalculateViewport()

	// Diagnostic: shows ideal vs actual pan and viewport mode on every cursor event.
	// "EDGE" = cursor at content boundary, contentX should be stable near 0 or tw-cw.
	// Rapid oscillation of contentX here is the jitter — compare with idealPanX vs maxPanX.
	modeX := "centered"
	if math.Abs(float64(idealPanX)) > float64(maxPanX) {
		modeX = "EDGE"
	}
	logrus.Infof("🗺️ [VP] u=%.4f idealPanX=%+.1f maxPanX=%.1f → panX=%+.1f [%s] contentX=%+.1f",
		u, idealPanX, maxPanX, vw.panOffsetX, modeX, vw.contentRectX)
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

// rasterizeSVGToNRGBA renders SVG data to an NRGBA image at the given size.
func rasterizeSVGToNRGBA(svgData []byte, w, h int) *image.NRGBA {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svgData))
	if err != nil {
		return nil
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	icon.Draw(raster, 1.0)
	return img
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
//   - Normal: full container area below the tab bar.
func (vw *VideoWidget) videoCanvasFrame() (x, y, w, h float32) {
	if vw.parentWindow == nil {
		return
	}
	cs := vw.parentWindow.Canvas().Size()

	// In fullscreen mode the Vulkan SurfaceView covers the whole screen.
	if vw.fullscreenDialog != nil && vw.fullscreenDialog.IsFullscreen() {
		return 0, 0, cs.Width, cs.Height
	}

	// When the Android system IME (letter keyboard) is open, expand the video upward
	// to fill the tab-bar area. The custom keyboard panel stays visible at the bottom.
	// Use AbsolutePositionForObject so we read the exact canvas Y of the keyboard panel
	// rather than re-deriving it from heights (which can disagree by a few dp due to
	// Fyne border-layout rounding or imeSpacer timing).
	if getImeExpandHeightDp() > 0 {
		if vw.container == nil || vw.contentContainer == nil || !vw.contentContainer.Visible() {
			return
		}
		sz := vw.container.Size()
		absPos := fyne.CurrentApp().Driver().AbsolutePositionForObject(vw.contentContainer)
		videoH := absPos.Y
		if videoH <= 0 {
			// Fallback: derive from sizes if position is not yet available.
			videoH = cs.Height
			if kh := vw.contentContainer.Size().Height; kh > 0 {
				videoH -= kh
			}
		}
		if videoH <= 0 {
			return
		}
		return 0, 0, sz.Width, videoH
	}

	if vw.container == nil {
		return
	}
	sz := vw.container.Size()
	topOffset := cs.Height - sz.Height

	videoH := sz.Height
	if vw.contentContainer != nil && vw.contentContainer.Visible() {
		if kh := vw.contentContainer.Size().Height; kh > 0 {
			videoH -= kh
		}
	}
	return 0, topOffset, sz.Width, videoH
}

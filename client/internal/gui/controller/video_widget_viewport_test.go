package controller

import (
	"testing"

	"fyne.io/fyne/v2"
)

// Regression test for the "pinch-zoom jumps to the bottom of the picture"
// bug: recalculateViewport used to default an overflowing (contentH >
// availableH) video to a bottom-anchored base position (contentY =
// availableH-contentH, panOffsetY clamped to [0, maxPanY]) instead of
// staying centered like the "fits" branch and the X axis both already do.
// The result was a visible jump the instant a pinch pushed the zoomed
// video's height past the available area, even with no explicit pan from
// the user.

func newTestViewportWidget(touchpadW, touchpadH, bottomInset float32) *VideoWidget {
	vw := &VideoWidget{}
	vw.touchpadSizeW = touchpadW
	vw.touchpadSizeH = touchpadH
	vw.bottomInset = bottomInset
	vw.baseContentRectW = touchpadW
	vw.baseContentRectH = touchpadH - bottomInset
	return vw
}

func TestRecalculateViewport_OverflowDefaultsToCentered(t *testing.T) {
	vw := newTestViewportWidget(1000, 500, 0)
	vw.zoomScale = 2 // content 2000x1000, available area 1000x500 -> overflows both axes

	vw.recalculateViewport()

	wantY := (vw.touchpadSizeH - vw.contentRectH) / 2
	if vw.contentRectY != wantY {
		t.Errorf("contentRectY = %v, want %v (vertically centered) -- got a bottom-anchored jump instead", vw.contentRectY, wantY)
	}
	if vw.panOffsetY != 0 {
		t.Errorf("panOffsetY = %v, want 0 (no explicit pan was ever applied)", vw.panOffsetY)
	}
}

func TestRecalculateViewport_PanOffsetYIsSymmetricAroundCenter(t *testing.T) {
	vw := newTestViewportWidget(1000, 500, 0)
	vw.zoomScale = 2 // contentH = 1000, availableH = 500, maxPanY = (1000-500)/2 = 250

	// Push toward the bottom edge (see the bottom of the source picture).
	vw.panOffsetY = -1000 // way past the clamp
	vw.recalculateViewport()
	if got, want := vw.panOffsetY, float32(-250); got != want {
		t.Errorf("panOffsetY clamped to %v, want %v (bottom edge)", got, want)
	}
	if got, want := vw.contentRectY, float32(500-1000); got != want { // contentY = -M = flush bottom
		t.Errorf("contentRectY = %v, want %v (bottom-flush)", got, want)
	}

	// Push toward the top edge (see the top of the source picture).
	vw.panOffsetY = 1000 // way past the clamp
	vw.recalculateViewport()
	if got, want := vw.panOffsetY, float32(250); got != want {
		t.Errorf("panOffsetY clamped to %v, want %v (top edge)", got, want)
	}
	if got, want := vw.contentRectY, float32(0); got != want { // contentY = 0 = flush top
		t.Errorf("contentRectY = %v, want %v (top-flush)", got, want)
	}

	// Never reveals empty space past either edge: the clamp bounds must
	// keep contentRectY within [-(contentH-availableH), 0].
	minY, maxY := float32(500-1000), float32(0)
	if vw.contentRectY < minY || vw.contentRectY > maxY {
		t.Errorf("contentRectY = %v out of valid range [%v, %v]", vw.contentRectY, minY, maxY)
	}
}

func TestRecalculateViewport_FitsBranchStillCenters(t *testing.T) {
	vw := newTestViewportWidget(1000, 500, 0)
	vw.zoomScale = 1 // content fits, no overflow -- untouched by this fix, still centered

	vw.recalculateViewport()

	wantY := (vw.touchpadSizeH - vw.contentRectH) / 2
	if vw.contentRectY != wantY {
		t.Errorf("contentRectY = %v, want %v (centered)", vw.contentRectY, wantY)
	}
	if vw.panOffsetY != 0 {
		t.Errorf("panOffsetY = %v, want 0", vw.panOffsetY)
	}
}

func TestRecalculateViewport_LetterboxPanMovesFittedVideo(t *testing.T) {
	// Touchpad taller than fitted content → black bars; pan may leave the
	// letterbox and go partially off-screen (min 30% still visible).
	vw := &VideoWidget{}
	vw.touchpadSizeW = 1000
	vw.touchpadSizeH = 800
	vw.baseContentRectW = 1000
	vw.baseContentRectH = 400 // centered at Y=200
	vw.zoomScale = 1
	vw.panOffsetY = -150
	vw.recalculateViewport()

	if got, want := vw.panOffsetY, float32(-150); got != want {
		t.Errorf("panOffsetY = %v, want %v (fit pan must stick)", got, want)
	}
	wantY := (800-400)/2 + (-150) // 50
	if got := vw.contentRectY; got != float32(wantY) {
		t.Errorf("contentRectY = %v, want %v", got, wantY)
	}

	// Past the old letterbox clamp, still legal: 30% of 400 = 120px must remain.
	// minY = -400*0.7 = -280 → max upward pan from center 200 is 200-(-280)=480
	vw.panOffsetY = -1000
	vw.recalculateViewport()
	if got, want := vw.contentRectY, float32(-280); got != want {
		t.Errorf("contentRectY = %v, want %v (30%% still visible off top)", got, want)
	}
	if vw.contentRectY+vw.contentRectH < 120-0.5 {
		t.Errorf("less than 30%% of video remains on screen: bottom=%v", vw.contentRectY+vw.contentRectH)
	}
}

// applyViewportGesture keeps the content point under the view centre stable
// across zoom (not the finger focus — Android focus Y drifts low vs the
// Vulkan surface and walked the picture downward while pinching).
func TestApplyViewportGesture_PreservesViewCenterOnZoom(t *testing.T) {
	const bottomInset = float32(100)
	vw := newTestViewportWidget(1000, 500, bottomInset) // availableH = 400
	vw.zoomScale = 1
	vw.recalculateViewport()
	oldX, oldY, oldW, oldH := vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH

	anchorX := float32(500)
	anchorY := float32(200) // availableH/2
	u := (anchorX - oldX) / oldW
	v := (anchorY - oldY) / oldH
	// focus args are ignored for anchoring; pass something off-centre to
	// prove we do not follow finger focus anymore.
	vw.applyViewportGesture(2.0, 800, 350, 0, 0)

	newX, newY, newW, newH := vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH
	gotX := newX + u*newW
	gotY := newY + v*newH
	if diff := gotX - anchorX; diff > 0.5 || diff < -0.5 {
		t.Errorf("view-centre X drifted: got %v, want %v", gotX, anchorX)
	}
	if diff := gotY - anchorY; diff > 0.5 || diff < -0.5 {
		t.Errorf("view-centre Y drifted: got %v, want %v", gotY, anchorY)
	}
}

// When zoomed but one axis still fits (common on portrait after moderate
// zoom), that axis must still accept pan — locking it to center made
// post-zoom drag feel broken and wiped pan from a prior 1x letterbox drag
// as soon as zoomScale crossed 1.
func TestRecalculateViewport_ZoomedFittingAxisKeepsPan(t *testing.T) {
	vw := &VideoWidget{}
	vw.touchpadSizeW = 1000
	vw.touchpadSizeH = 800
	vw.baseContentRectW = 1000
	vw.baseContentRectH = 400
	vw.zoomScale = 1.5 // content 1500x600: X overflows, Y still fits in 800
	vw.panOffsetY = -80
	vw.recalculateViewport()

	if vw.panOffsetY != -80 {
		t.Errorf("panOffsetY = %v, want -80 (zoomed fitting axis must keep pan)", vw.panOffsetY)
	}
	if vw.contentRectW <= 1000 {
		t.Fatalf("expected X overflow, got contentW=%v", vw.contentRectW)
	}
}

// An explicit two-finger drag alongside a pinch still moves the video, and
// recalculateViewport's clamp still keeps it from revealing empty space
// past either edge.
func TestApplyViewportGesture_PanDyStillMovesVideo(t *testing.T) {
	vw := newTestViewportWidget(1000, 500, 0) // availableH = 500
	vw.zoomScale = 1
	vw.recalculateViewport()

	// Zoom in and drag down by a large amount -- should clamp to the top edge (contentY = 0).
	vw.applyViewportGesture(2.0, 500, 250, 0, 10000)

	if vw.contentRectY != 0 {
		t.Errorf("contentRectY = %v, want 0 (dragged to the top-edge clamp)", vw.contentRectY)
	}
}

func TestSnapViewportAlignment_SnapsNearLeftEdgeAt1x(t *testing.T) {
	// Pillarbox: content narrower than view → distinct left/right targets.
	vw := &VideoWidget{}
	vw.touchpadSizeW = 1000
	vw.touchpadSizeH = 500
	vw.baseContentRectW = 600
	vw.baseContentRectH = 500
	vw.zoomScale = 1
	// left flush pan = -center = -(1000-600)/2 = -200
	vw.panOffsetX = -200 + 20 // within 3% of 1000
	vw.recalculateViewport()

	if !vw.snapViewportAlignment() {
		t.Fatal("expected snap near left edge")
	}
	if vw.panOffsetX != -200 {
		t.Errorf("panOffsetX = %v, want -200 (left flush)", vw.panOffsetX)
	}
}

func TestSnapViewportAlignment_DoesNotSnapVertical(t *testing.T) {
	vw := &VideoWidget{}
	vw.touchpadSizeW = 1000
	vw.touchpadSizeH = 800
	vw.baseContentRectW = 1000
	vw.baseContentRectH = 400
	vw.zoomScale = 1
	vw.panOffsetY = -10 // near vertical center, must NOT snap
	vw.recalculateViewport()

	if vw.snapViewportAlignment() {
		t.Fatal("vertical axis must not snap")
	}
	if vw.panOffsetY != -10 {
		t.Errorf("panOffsetY changed to %v, want -10", vw.panOffsetY)
	}
}

func TestSnapViewportAlignment_DoesNotSnapWhenZoomed(t *testing.T) {
	vw := newTestViewportWidget(1000, 500, 0)
	vw.zoomScale = 2
	vw.panOffsetX = 20
	vw.panOffsetY = -100
	vw.recalculateViewport()

	if vw.snapViewportAlignment() {
		t.Fatal("snap must be disabled while zoomed")
	}
	if vw.panOffsetX != 20 || vw.panOffsetY != -100 {
		t.Errorf("pan changed while zoomed: (%v,%v)", vw.panOffsetX, vw.panOffsetY)
	}
}

func TestSnapViewportAlignment_DoesNotSnapToCenterBetweenEdges(t *testing.T) {
	vw := &VideoWidget{}
	vw.touchpadSizeW = 1000
	vw.touchpadSizeH = 500
	vw.baseContentRectW = 600
	vw.baseContentRectH = 500
	vw.zoomScale = 1
	vw.panOffsetX = 0 // true center between left(-200) and right(+200)
	vw.recalculateViewport()

	if vw.snapViewportAlignment() {
		t.Fatal("center-between-edges must not magnetize")
	}
}

func TestSnapViewportAlignment_DoesNotSnapWhenFarFromEdge(t *testing.T) {
	vw := &VideoWidget{}
	vw.touchpadSizeW = 1000
	vw.touchpadSizeH = 500
	vw.baseContentRectW = 600
	vw.baseContentRectH = 500
	vw.zoomScale = 1
	vw.panOffsetX = -200 + 80 // 8% away from left — outside 3%
	vw.recalculateViewport()

	if vw.snapViewportAlignment() {
		t.Fatalf("did not expect snap, got panOffsetX=%v", vw.panOffsetX)
	}
}

func TestPlaceVirtualCursorAtViewCenter_UsesVisibleCentre(t *testing.T) {
	vw := newTestViewportWidget(1000, 500, 0)
	vw.zoomScale = 2 // content 2000x1000
	vw.panOffsetX = 500 // left-flush: viewing left side of remote
	vw.recalculateViewport()

	vw.vcMu.Lock()
	vw.placeVirtualCursorAtViewCenterLocked(0, 1, 0, 1)
	u, v := vw.virtualCursorU, vw.virtualCursorV
	vw.vcMu.Unlock()

	// Screen centre maps near the left portion of the remote frame, not 0.5.
	if u > 0.35 {
		t.Errorf("virtualCursorU = %v, want left-of-centre after left-flush pan", u)
	}
	if v < 0.4 || v > 0.6 {
		t.Errorf("virtualCursorV = %v, want ~0.5", v)
	}
	// Viewport must not have been moved by placing the cursor.
	if vw.panOffsetX != 500 {
		t.Errorf("panOffsetX changed to %v, want 500", vw.panOffsetX)
	}
}

func TestOverlayWindowPosAddsSafeAreaInset(t *testing.T) {
	// Punch-hole phone: Fyne AbsolutePosition is header-relative (40dp),
	// InteractiveArea top is the 48dp status/cutout inset. Vulkan on
	// decorView must start at 88dp, not 40dp (too high, overlapping header).
	abs := fyne.NewPos(0, 40)
	inset := fyne.NewPos(0, 48)
	got := overlayWindowPos(abs, inset)
	if got.X != 0 || got.Y != 88 {
		t.Errorf("overlayWindowPos = %v, want (0, 88)", got)
	}
}

func TestVideoTopOffsetFromCanvasSubtractsChrome(t *testing.T) {
	// iPhone Metal used canvasH−containerH for Y; that equals header+footer,
	// so the clip bottom sat on the canvas edge and covered the Control bar.
	const canvasH, containerH float32 = 800, 600
	chrome := videoChromeBelow()
	got := videoTopOffsetFromCanvas(containerH, canvasH)
	want := canvasH - containerH - chrome
	if want < 0 {
		want = 0
	}
	if got != want {
		t.Errorf("videoTopOffsetFromCanvas = %v, want %v (chrome=%v)", got, want, chrome)
	}
	oldBug := canvasH - containerH
	if chrome > 0 && got >= oldBug {
		t.Errorf("topOffset %v did not subtract chrome (old iOS bug used %v)", got, oldBug)
	}
	clipH := videoClipHeightFromCanvas(got, containerH+50, canvasH)
	if bottom := got + clipH; bottom > canvasH-chrome+0.5 {
		t.Errorf("clip bottom %.1f covers chrome (canvasH=%.0f chrome=%.0f)", bottom, canvasH, chrome)
	}
}

func TestOverlayWindowPosNoopWithoutInset(t *testing.T) {
	abs := fyne.NewPos(12, 40)
	got := overlayWindowPos(abs, fyne.NewPos(0, 0))
	if got != abs {
		t.Errorf("overlayWindowPos = %v, want %v (desktop / inset-cleared keyboard)", got, abs)
	}
}

func TestOverlayWindowPosLandscapeCutout(t *testing.T) {
	abs := fyne.NewPos(8, 36)
	inset := fyne.NewPos(44, 0) // left punch-hole in landscape
	got := overlayWindowPos(abs, inset)
	if got.X != 52 || got.Y != 36 {
		t.Errorf("overlayWindowPos = %v, want (52, 36)", got)
	}
}

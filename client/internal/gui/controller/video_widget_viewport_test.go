package controller

import "testing"

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

// applyViewportGesture must NOT anchor vertically to the pinch focus
// point -- an earlier version did, and that made the picture visibly
// crawl toward wherever the fingers happened to rest as zoom increased
// (reported live as the video "jumping down" on a real phone, where a
// natural two-hand pinch grip rarely lands exactly at screen center).
// Pinching off-center must leave the video exactly as centered as
// pinching dead-center would -- only an explicit two-finger drag (panDy)
// may move it off center.
func TestApplyViewportGesture_DoesNotAnchorVerticallyToPinchFocus(t *testing.T) {
	const bottomInset = float32(100)
	vw := newTestViewportWidget(1000, 500, bottomInset) // availableH = 400
	vw.zoomScale = 1
	vw.recalculateViewport()

	// Pinch focused low on the screen (a realistic two-hand grip on a
	// phone, well below center) with enough scale to overflow -- must
	// still land centered, not anchored toward the focus point.
	focusX, focusY := float32(500), float32(380) // near the bottom of the 1000x400 available area
	vw.applyViewportGesture(2.0, focusX, focusY, 0, 0)

	availableH := vw.touchpadSizeH - vw.bottomInset
	wantContentY := (availableH - vw.contentRectH) / 2
	if diff := vw.contentRectY - wantContentY; diff > 0.01 || diff < -0.01 {
		t.Errorf("contentRectY = %v, want ~%v (off-center pinch focus must not pull the video off center)", vw.contentRectY, wantContentY)
	}
	if vw.panOffsetY != 0 {
		t.Errorf("panOffsetY = %v, want 0 (no explicit drag happened, only a pinch)", vw.panOffsetY)
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

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

// applyViewportGesture's pinch-anchor math must reference availableH
// (touchpadSizeH minus bottomInset), not the raw touchpad height -- using
// the wrong reference frame put the anchor point off by roughly
// bottomInset from where recalculateViewport actually clamps it.
func TestApplyViewportGesture_PinchAnchorUsesAvailableHeight(t *testing.T) {
	const bottomInset = float32(100)
	vw := newTestViewportWidget(1000, 500, bottomInset) // availableH = 400
	vw.zoomScale = 1
	vw.recalculateViewport()

	// Pinch centered at the middle of the *available* area (not the raw
	// touchpad) with enough scale to overflow: the content point under
	// the finger should land back under the same on-screen Y.
	focusX, focusY := float32(500), float32(200) // middle of the 1000x400 available area
	vw.applyViewportGesture(2.0, focusX, focusY, 0, 0)

	availableH := vw.touchpadSizeH - vw.bottomInset
	wantContentY := (availableH - vw.contentRectH) / 2 // scaleFactor=2 focused at dead center -> stays centered
	if diff := vw.contentRectY - wantContentY; diff > 0.01 || diff < -0.01 {
		t.Errorf("contentRectY = %v, want ~%v (pinch centered on the available area's own center should stay centered)", vw.contentRectY, wantContentY)
	}
}

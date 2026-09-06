//go:build darwin && !ios && cgo

package service

import (
	"testing"

	"usbridge-client/internal/localui"
)

// TestBuildAIVisionOverlayImageIsTransparentExceptBoxes pins the
// assumption pushAIVisionOverlayToMetal's doc comment relies on: every
// color localui's drawing code uses is fully opaque (alpha 255, see
// draw.go), so a canvas built by buildAIVisionOverlayImage starts fully
// transparent and only the actual box/tag pixels become opaque -- exactly
// the premultiplied-alpha form metal_video_set_overlay hands to CGImage,
// no separate conversion needed. If localui ever grows a translucent
// stroke or a rounded corner with anti-aliased edges, this is the test
// that will need a real premultiply pass added on the Metal side.
func TestBuildAIVisionOverlayImageIsTransparentExceptBoxes(t *testing.T) {
	const w, h = 40, 40
	result := &localui.Result{
		Icons: []localui.Icon{{ID: "1", Bbox: localui.Box{X1: 5, Y1: 20, X2: 15, Y2: 30}}},
	}

	img := buildAIVisionOverlayImage(result, w, h)

	if got := img.Bounds().Dx(); got != w {
		t.Errorf("width = %d, want %d", got, w)
	}
	if got := img.Bounds().Dy(); got != h {
		t.Errorf("height = %d, want %d", got, h)
	}

	// Untouched background pixel: must be fully transparent, not just black.
	if c := img.RGBAAt(0, 0); c.A != 0 {
		t.Errorf("untouched pixel (0,0) = %+v, want alpha 0 (transparent)", c)
	}

	// Box border pixel (bottom-right corner, clear of the tag drawn above
	// the box -- see ai_vision_test.go's TestDrawCachedOverlayDrawsAndSkipsWhenEmpty
	// for why this specific box geometry avoids the overlap): opaque icon-red.
	if c := img.RGBAAt(15, 30); c.A != 255 || c.R != 255 || c.G != 0 || c.B != 0 {
		t.Errorf("box border pixel (15,30) = %+v, want opaque icon red {255 0 0 255}", c)
	}
}

// TestBuildAIVisionOverlayImageEmptyResultIsFullyTransparent covers the
// "detection pass found nothing this round" case: pushing this image to
// the Metal overlay layer must visually clear any previous boxes, which
// only works if every pixel is transparent (not, say, some pixels left at
// a stale nonzero alpha from a reused buffer).
func TestBuildAIVisionOverlayImageEmptyResultIsFullyTransparent(t *testing.T) {
	img := buildAIVisionOverlayImage(&localui.Result{}, 10, 10)
	for i := 3; i < len(img.Pix); i += 4 {
		if img.Pix[i] != 0 {
			t.Fatalf("pixel byte offset %d has alpha %d, want 0 (fully transparent canvas)", i, img.Pix[i])
		}
	}
}

// TestPushAIVisionOverlayToMetalNoopWhenMetalInactive guards the early
// return pushAIVisionOverlayToMetal relies on when the Metal fast path
// isn't the active renderer (e.g. the CPU-fallback decode path took over,
// or no stream is running at all, as in this test process) -- it must not
// panic or reach into the cgo boundary with no overlay layer created.
func TestPushAIVisionOverlayToMetalNoopWhenMetalInactive(t *testing.T) {
	if MetalVideoIsActive() {
		t.Skip("Metal overlay unexpectedly active in test process")
	}
	pushAIVisionOverlayToMetal(&localui.Result{
		Icons: []localui.Icon{{ID: "1", Bbox: localui.Box{X1: 0, Y1: 0, X2: 5, Y2: 5}}},
	}, 40, 40)
}

// TestAIVisionMetalHooksWiredByInit confirms metal_video_darwin.go's
// init() actually installed both hooks ai_vision.go calls through --
// SetAIVisionEnabled(false) and a completed detection pass are silent
// no-ops on macOS if these are ever left nil (see ai_vision.go's
// aiVisionMetalPush/aiVisionMetalClear doc comment).
func TestAIVisionMetalHooksWiredByInit(t *testing.T) {
	if aiVisionMetalPush == nil {
		t.Error("aiVisionMetalPush is nil -- metal_video_darwin.go's init() should have set it")
	}
	if aiVisionMetalClear == nil {
		t.Error("aiVisionMetalClear is nil -- metal_video_darwin.go's init() should have set it")
	}
}

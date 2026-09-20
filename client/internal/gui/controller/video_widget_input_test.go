package controller

import (
	"testing"

	"fyne.io/fyne/v2"
)

func TestModifierMaskForKeyName(t *testing.T) {
	tests := []struct {
		name fyne.KeyName
		want int32
	}{
		{name: fyne.KeyName("LeftControl"), want: 1},
		{name: fyne.KeyName("RightControl"), want: 1},
		{name: fyne.KeyName("LeftShift"), want: 2},
		{name: fyne.KeyName("RightShift"), want: 2},
		{name: fyne.KeyName("LeftAlt"), want: 4},
		{name: fyne.KeyName("RightAlt"), want: 4},
		{name: fyne.KeyName("LeftSuper"), want: 8},
		{name: fyne.KeyName("RightSuper"), want: 8},
		{name: fyne.KeyA, want: 0},
	}

	for _, tt := range tests {
		if got := modifierMaskForKeyName(tt.name); got != tt.want {
			t.Fatalf("modifierMaskForKeyName(%q) = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestPositionToAbsolute_PillarboxExcludesSideBars(t *testing.T) {
	// 5:4 stream in a 16:9 widget → black bars left/right (cursor lagged sideways).
	vw := &VideoWidget{}
	vw.lastVideoImgW = 1280
	vw.lastVideoImgH = 1024
	vw.UpdateTouchpadAndContentRect(1920, 1080, nil)

	left := vw.contentRectX
	right := vw.contentRectX + vw.contentRectW
	midY := vw.contentRectY + vw.contentRectH/2
	if left <= 0 || right >= 1920 {
		t.Fatalf("expected pillarbox, contentRect=(%.1f,%.1f,%.1f,%.1f)", vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH)
	}

	x, y := vw.PositionToAbsolute(left, midY)
	if x != 0 {
		t.Errorf("left picture edge x = %d, want 0 (bars still included?)", x)
	}
	if y < 16000 || y > 16800 {
		t.Errorf("left-edge y = %d, want ~16383", y)
	}

	x, _ = vw.PositionToAbsolute(right, midY)
	if x != 32767 {
		t.Errorf("right picture edge x = %d, want 32767", x)
	}

	cx, cy := vw.PositionToAbsolute(960, 540)
	if cx < 16000 || cx > 16800 || cy < 16000 || cy > 16800 {
		t.Errorf("center = (%d,%d), want ~16383", cx, cy)
	}
}

func TestPositionToAbsolute_LetterboxExcludesTopBottomBars(t *testing.T) {
	// 16:9 stream in a 16:10 widget → black bars top/bottom (cursor lagged vertically).
	vw := &VideoWidget{}
	vw.lastVideoImgW = 1920
	vw.lastVideoImgH = 1080
	vw.UpdateTouchpadAndContentRect(1920, 1200, nil)

	top := vw.contentRectY
	bottom := vw.contentRectY + vw.contentRectH
	midX := vw.contentRectX + vw.contentRectW/2
	if top <= 0 || bottom >= 1200 {
		t.Fatalf("expected letterbox, contentRect=(%.1f,%.1f,%.1f,%.1f)", vw.contentRectX, vw.contentRectY, vw.contentRectW, vw.contentRectH)
	}

	_, y := vw.PositionToAbsolute(midX, top)
	if y != 0 {
		t.Errorf("top picture edge y = %d, want 0", y)
	}
	_, y = vw.PositionToAbsolute(midX, bottom)
	if y != 32767 {
		t.Errorf("bottom picture edge y = %d, want 32767", y)
	}
}

func TestBeginVideoTraceKeepsStreamAspect(t *testing.T) {
	vw := &VideoWidget{}
	vw.lastVideoImgW = 1280
	vw.lastVideoImgH = 1024
	vw.beginVideoTrace("letterbox-mouse")
	if vw.lastVideoImgW != 1280 || vw.lastVideoImgH != 1024 {
		t.Fatalf("beginVideoTrace wiped stream size to %.0fx%.0f; native GPU mouse mapping needs it", vw.lastVideoImgW, vw.lastVideoImgH)
	}
}

func TestContainFitNorm(t *testing.T) {
	x, y, w, h := containFitNorm(1920, 1080, 1920, 1080)
	if x != 0 || y != 0 || w != 1 || h != 1 {
		t.Fatalf("same aspect = (%v,%v,%v,%v), want identity", x, y, w, h)
	}

	x, y, w, h = containFitNorm(1920, 1080, 1920, 1200) // 16:10 desktop in 16:9 stream → pillarbox
	if y != 0 || h != 1 {
		t.Fatalf("16:10 in 16:9 should pillarbox, got y=%v h=%v", y, h)
	}
	if mathAbs32(w-(1920.0/1200.0)/(1920.0/1080.0)) > 0.002 {
		t.Fatalf("pillarbox w=%v, want ~0.9", w)
	}
	if mathAbs32(x-(1-w)/2) > 0.0001 {
		t.Fatalf("pillarbox x=%v, want centered", x)
	}

	x, y, w, h = containFitNorm(1920, 1200, 1920, 1080) // 16:9 desktop in 16:10 stream → letterbox
	if x != 0 || w != 1 {
		t.Fatalf("16:9 in 16:10 should letterbox, got x=%v w=%v", x, w)
	}
	if mathAbs32(h-(1920.0/1200.0)/(1920.0/1080.0)) > 0.002 {
		t.Fatalf("letterbox h=%v, want ~0.9", h)
	}
}

func TestPositionToAbsolute_InStreamPillarbox(t *testing.T) {
	// 16:9 stream of a 16:10 monitor: bars are pixels in the frame, not client chrome.
	vw := &VideoWidget{}
	vw.lastVideoImgW = 1920
	vw.lastVideoImgH = 1080
	vw.setHostDesktopSize(1920, 1200)
	vw.UpdateTouchpadAndContentRect(1920, 1080, nil)

	fx, fy, fw, fh := vw.getFrameContentRect()
	if fy != 0 || fh != 1 || fw >= 0.999 {
		t.Fatalf("expected in-stream pillarbox, frameRect=(%.3f,%.3f,%.3f,%.3f)", fx, fy, fw, fh)
	}

	left := vw.contentRectX + vw.contentRectW*fx
	right := left + vw.contentRectW*fw
	midY := vw.contentRectY + vw.contentRectH/2

	x, y := vw.PositionToAbsolute(left, midY)
	if x != 0 {
		t.Errorf("desktop left x=%d, want 0 (still mapping the bars?)", x)
	}
	if y < 16000 || y > 16800 {
		t.Errorf("desktop left y=%d, want ~16383", y)
	}
	x, _ = vw.PositionToAbsolute(right, midY)
	if x != 32767 {
		t.Errorf("desktop right x=%d, want 32767", x)
	}

	cx, cy := vw.PositionToAbsolute(960, 540)
	if cx < 16000 || cx > 16800 || cy < 16000 || cy > 16800 {
		t.Errorf("center=(%d,%d), want ~16383", cx, cy)
	}
}

func TestPositionToAbsolute_InStreamLetterbox(t *testing.T) {
	// 16:10 stream of a 16:9 monitor: bars are top/bottom inside the frame.
	vw := &VideoWidget{}
	vw.lastVideoImgW = 1920
	vw.lastVideoImgH = 1200
	vw.setHostDesktopSize(1920, 1080)
	vw.UpdateTouchpadAndContentRect(1920, 1200, nil)

	fx, fy, fw, fh := vw.getFrameContentRect()
	if fx != 0 || fw != 1 || fh >= 0.999 {
		t.Fatalf("expected in-stream letterbox, frameRect=(%.3f,%.3f,%.3f,%.3f)", fx, fy, fw, fh)
	}

	top := vw.contentRectY + vw.contentRectH*fy
	bottom := top + vw.contentRectH*fh
	midX := vw.contentRectX + vw.contentRectW/2

	_, y := vw.PositionToAbsolute(midX, top)
	if y != 0 {
		t.Errorf("desktop top y=%d, want 0", y)
	}
	_, y = vw.PositionToAbsolute(midX, bottom)
	if y != 32767 {
		t.Errorf("desktop bottom y=%d, want 32767", y)
	}
}

func mathAbs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

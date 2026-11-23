package controller

import (
	"testing"

	"usbridge-client/internal/models"
)

func TestSelectBestResolution(t *testing.T) {
	modes := []models.VideoCaptureMode{
		{Width: 1280, Height: 720, FPS: []int{60}},
		{Width: 1920, Height: 1080, FPS: []int{60}},
		{Width: 2560, Height: 1440, FPS: []int{60}},
		{Width: 3840, Height: 2160, FPS: []int{60}},
	}

	// 1. Native fits within client HW decode ceiling -> keep native.
	w, h := SelectBestResolution(2560, 1440, modes, 3840, 2160)
	if w != 2560 || h != 1440 {
		t.Fatalf("native fits: got %dx%d, want 2560x1440", w, h)
	}

	// 2. Native unreported (0x0) -> pick highest mode fitting client HW max (not 720p!).
	w, h = SelectBestResolution(0, 0, modes, 3840, 2160)
	if w != 3840 || h != 2160 {
		t.Fatalf("unreported native: got %dx%d, want 3840x2160", w, h)
	}

	// 3. Native 4K exceeds 1080p HW decoder -> pick highest supported mode (1920x1080).
	w, h = SelectBestResolution(3840, 2160, modes, 1920, 1080)
	if w != 1920 || h != 1080 {
		t.Fatalf("clamp to 1080p mode: got %dx%d, want 1920x1080", w, h)
	}

	// 4. Native 5K MacBook Pro (5120x2880) with no capture modes on 4K HW decoder -> scale proportionally to 3840x2160.
	w, h = SelectBestResolution(5120, 2880, nil, 3840, 2160)
	if w != 3840 || h != 2160 {
		t.Fatalf("proportional scale 5K->4K: got %dx%d, want 3840x2160", w, h)
	}
}

package gui

import "testing"

func TestWindowFrameVisibleOnVirtualDesktop(t *testing.T) {
	// Primary 1920x1080 plus a left-hand 1920x1080 monitor at x=-1920.
	vx, vy, vw, vh := -1920, 0, 3840, 1080

	if !windowFrameVisible(windowFrame{X: -1400, Y: 80, W: 960, H: 640}, vx, vy, vw, vh) {
		t.Fatal("window on the left monitor should be visible")
	}
	if !windowFrameVisible(windowFrame{X: 400, Y: 80, W: 960, H: 640}, vx, vy, vw, vh) {
		t.Fatal("window on the primary monitor should be visible")
	}
	if windowFrameVisible(windowFrame{X: 5000, Y: 80, W: 960, H: 640}, vx, vy, vw, vh) {
		t.Fatal("window past the virtual desktop should not be visible")
	}
	if windowFrameVisible(windowFrame{X: 0, Y: 0, W: 0, H: 0}, vx, vy, vw, vh) {
		t.Fatal("empty frame should not be visible")
	}
}

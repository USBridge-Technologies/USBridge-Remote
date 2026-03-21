package gui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
)

func almostEqual(a, b float32) bool {
	return math.Abs(float64(a-b)) < 0.001
}

func TestDPIAwareWindowSizeUsesDesktopScale(t *testing.T) {
	got := dpiAwareWindowSize(1200, 800, 1.5, fyne.NewSize(0, 0))

	if !almostEqual(got.Width, 800) {
		t.Fatalf("width = %v, want 800", got.Width)
	}
	if !almostEqual(got.Height, 533.3333) {
		t.Fatalf("height = %v, want about 533.3333", got.Height)
	}
}

func TestDPIAwareWindowSizeRespectsContentMinimum(t *testing.T) {
	got := dpiAwareWindowSize(1200, 800, 1.5, fyne.NewSize(920, 700))

	if !almostEqual(got.Width, 920) {
		t.Fatalf("width = %v, want 920", got.Width)
	}
	if !almostEqual(got.Height, 700) {
		t.Fatalf("height = %v, want 700", got.Height)
	}
}

func TestDPIAwareWindowSizeFallsBackToDefaultConfigSize(t *testing.T) {
	got := dpiAwareWindowSize(100, 100, 0, fyne.NewSize(0, 0))

	if !almostEqual(got.Width, defaultWindowWidth) {
		t.Fatalf("width = %v, want %v", got.Width, defaultWindowWidth)
	}
	if !almostEqual(got.Height, defaultWindowHeight) {
		t.Fatalf("height = %v, want %v", got.Height, defaultWindowHeight)
	}
}

func TestClampWindowSizeToAvailableArea(t *testing.T) {
	got := clampWindowSizeToAvailableArea(fyne.NewSize(1200, 900), fyne.NewSize(1000, 700))

	if !almostEqual(got.Width, 1000) {
		t.Fatalf("width = %v, want 1000", got.Width)
	}
	if !almostEqual(got.Height, 700) {
		t.Fatalf("height = %v, want 700", got.Height)
	}
}

func TestExpandWindowSizeToPreferredArea(t *testing.T) {
	got := expandWindowSizeToPreferredArea(fyne.NewSize(800, 500), fyne.NewSize(960, 640))

	if !almostEqual(got.Width, 960) {
		t.Fatalf("width = %v, want 960", got.Width)
	}
	if !almostEqual(got.Height, 640) {
		t.Fatalf("height = %v, want 640", got.Height)
	}
}

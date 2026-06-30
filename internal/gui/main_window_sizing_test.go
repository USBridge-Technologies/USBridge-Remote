package gui

import (
	"math"
	"testing"

	"fyne.io/fyne/v2"
)

func almostEqual(a, b float32) bool {
	return math.Abs(float64(a-b)) < 0.001
}

func TestWindowSizeToLogicalRespectsContentMinimum(t *testing.T) {
	got := windowSizeToLogical(1200, 800, fyne.NewSize(1500, 900))

	if !almostEqual(got.Width, 1500) {
		t.Fatalf("width = %v, want 1500", got.Width)
	}
	if !almostEqual(got.Height, 900) {
		t.Fatalf("height = %v, want 900", got.Height)
	}
}

func TestWindowSizeToLogicalFallsBackToDefaultConfigSize(t *testing.T) {
	got := windowSizeToLogical(100, 100, fyne.NewSize(0, 0))

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

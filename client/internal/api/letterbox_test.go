package api

import (
	"image"
	"image/color"
	"testing"
)

func fillRect(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, c)
		}
	}
}

func TestCropLetterbox(t *testing.T) {
	grey := color.RGBA{120, 130, 140, 255}
	// 2560x1600 stream holding a 2560x1440 desktop: 80px bars top+bottom.
	img := image.NewRGBA(image.Rect(0, 0, 2560, 1600))
	fillRect(img, image.Rect(0, 80, 2560, 1520), grey)
	got := CropLetterbox(img)
	if got.Bounds().Dx() != 2560 || got.Bounds().Dy() != 1440 {
		t.Fatalf("crop = %v, want 2560x1440", got.Bounds())
	}
	if got.Bounds().Min.Y != 80 {
		t.Errorf("crop origin y = %d, want 80", got.Bounds().Min.Y)
	}

	// Bar on one side only (dark window at the top) must NOT be cropped.
	one := image.NewRGBA(image.Rect(0, 0, 640, 400))
	fillRect(one, image.Rect(0, 100, 640, 400), grey)
	if c := CropLetterbox(one); c.Bounds() != one.Bounds() {
		t.Errorf("one-sided dark band was cropped: %v", c.Bounds())
	}

	// No bars: untouched.
	full := image.NewRGBA(image.Rect(0, 0, 640, 400))
	fillRect(full, full.Bounds(), grey)
	if c := CropLetterbox(full); c.Bounds() != full.Bounds() {
		t.Errorf("full frame was cropped: %v", c.Bounds())
	}

	// Pillarbox (side bars) too.
	pil := image.NewRGBA(image.Rect(0, 0, 800, 400))
	fillRect(pil, image.Rect(100, 0, 700, 400), grey)
	if c := CropLetterbox(pil); c.Bounds().Dx() != 600 || c.Bounds().Dy() != 400 {
		t.Errorf("pillarbox crop = %v, want 600x400", c.Bounds())
	}
}

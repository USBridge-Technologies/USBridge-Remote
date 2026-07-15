package controller

import (
	"bytes"
	"image"
	"image/color"

	"usbridge-client/internal/gui/assets"

	"github.com/srwiley/oksvg"
	"github.com/srwiley/rasterx"
)

func newOverlayCursorImage() image.Image {
	const width = 18
	const height = 24

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	fill := color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
	border := color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0xff}

	pattern := []string{
		"110000000000000000",
		"111000000000000000",
		"111100000000000000",
		"111110000000000000",
		"111111000000000000",
		"111111100000000000",
		"111111110000000000",
		"111111111000000000",
		"111111111100000000",
		"111111111110000000",
		"111111111111000000",
		"111111111111100000",
		"111111111111110000",
		"111111111111111000",
		"111111111100000000",
		"111111011000000000",
		"111100011000000000",
		"111000001100000000",
		"110000001100000000",
		"100000000110000000",
		"000000000110000000",
		"000000000011000000",
		"000000000011000000",
		"000000000001000000",
	}

	for y, row := range pattern {
		for x, cell := range row {
			if cell != '1' {
				continue
			}
			colorToSet := fill
			for _, neighbor := range [][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
				nx := x + neighbor[0]
				ny := y + neighbor[1]
				if nx < 0 || nx >= width || ny < 0 || ny >= height || pattern[ny][nx] != '1' {
					colorToSet = border
					break
				}
			}
			img.SetNRGBA(x, y, colorToSet)
		}
	}

	return img
}

// rasterizeSVGToNRGBA renders SVG data to an NRGBA image at the given size.
// Used on Android (Vulkan cursor upload) and iOS (Metal cursor layer).
func rasterizeSVGToNRGBA(svgData []byte, w, h int) *image.NRGBA {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(svgData))
	if err != nil {
		return nil
	}
	icon.SetTarget(0, 0, float64(w), float64(h))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	scanner := rasterx.NewScannerGV(w, h, img, img.Bounds())
	raster := rasterx.NewDasher(w, h, scanner)
	icon.Draw(raster, 1.0)
	return img
}

// cursorSVGPixels rasterizes cursor-pointer.svg at scale×(18×24) and returns
// raw NRGBA bytes plus dimensions. Used by iOS Metal cursor upload.
func cursorSVGPixels(scale int) ([]byte, int, int) {
	if scale < 1 {
		scale = 1
	}
	w, h := 18*scale, 24*scale
	img := rasterizeSVGToNRGBA(assets.CursorPointerSVG, w, h)
	if img == nil {
		return nil, 0, 0
	}
	return img.Pix, w, h
}

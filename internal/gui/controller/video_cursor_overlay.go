package controller

import (
	"image"
	"image/color"
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

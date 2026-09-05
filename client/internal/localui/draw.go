package localui

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/png"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

var (
	iconColor = color.RGBA{R: 255, G: 0, B: 0, A: 255} // red
	textColor = color.RGBA{R: 0, G: 255, B: 0, A: 255} // green
	markBg    = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	markFg    = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// drawResult mirrors usbridge/modules/ui_parser/draw.go's drawResult: red
// boxes for icons, green for text, plus each element's Set-of-Mark hex tag
// -- rendered with the stdlib image/draw + golang.org/x/image/font/
// basicfont instead of gocv's OpenCV-backed drawing primitives.
func drawResult(src *rgbImage, result *Result) []byte {
	img := image.NewRGBA(image.Rect(0, 0, src.W, src.H))
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			off := (y*src.W + x) * 3
			img.SetRGBA(x, y, color.RGBA{R: src.Pix[off], G: src.Pix[off+1], B: src.Pix[off+2], A: 255})
		}
	}

	for _, icon := range result.Icons {
		drawRect(img, icon.Bbox, iconColor)
		drawMarkTag(img, icon.ID, int(icon.Bbox.X1), int(icon.Bbox.Y1))
	}
	for _, t := range result.Text {
		drawRect(img, t.Bbox, textColor)
		drawMarkTag(img, t.ID, int(t.Bbox.X1), int(t.Bbox.Y1))
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

func drawRect(img *image.RGBA, b Box, c color.RGBA) {
	x1, y1, x2, y2 := int(b.X1), int(b.Y1), int(b.X2), int(b.Y2)
	const thickness = 2
	for t := 0; t < thickness; t++ {
		hLine(img, x1, x2, y1+t, c)
		hLine(img, x1, x2, y2-t, c)
		vLine(img, y1, y2, x1+t, c)
		vLine(img, y1, y2, x2-t, c)
	}
}

func hLine(img *image.RGBA, x1, x2, y int, c color.RGBA) {
	b := img.Bounds()
	if y < b.Min.Y || y >= b.Max.Y {
		return
	}
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	for x := x1; x <= x2; x++ {
		if x >= b.Min.X && x < b.Max.X {
			img.SetRGBA(x, y, c)
		}
	}
}

func vLine(img *image.RGBA, y1, y2, x int, c color.RGBA) {
	b := img.Bounds()
	if x < b.Min.X || x >= b.Max.X {
		return
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}
	for y := y1; y <= y2; y++ {
		if y >= b.Min.Y && y < b.Max.Y {
			img.SetRGBA(x, y, c)
		}
	}
}

// drawMarkTag mirrors usbridge/modules/ui_parser/draw.go's drawMarkTag: a
// filled background box with the id in contrasting text, anchored just
// above the element's own box (or just inside it, if that would draw off
// the image's top edge).
func drawMarkTag(img *image.RGBA, id string, boxX, boxY int) {
	const charW, charH, pad = 7, 13, 2
	w := len(id)*charW + 2*pad
	h := charH + 2*pad

	x := boxX
	if x < 0 {
		x = 0
	}
	y := boxY - h
	if y < 0 {
		y = boxY
	}

	bg := image.Rect(x, y, x+w, y+h)
	draw.Draw(img, bg, &image.Uniform{C: markBg}, image.Point{}, draw.Src)

	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: markFg},
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x+pad, y+pad+10),
	}
	d.DrawString(id)
}

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
		DrawDetectionBox(img, icon.Bbox, false)
		DrawDetectionTag(img, icon.ID, icon.Bbox)
	}
	for _, t := range result.Text {
		DrawDetectionBox(img, t.Bbox, true)
		DrawDetectionTag(img, t.ID, t.Bbox)
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}

// DrawDetectionBox draws one detection's Set-of-Mark rectangle directly
// onto img -- red for a UI-element/icon box, green for a text box, same
// palette drawResult uses for the static ui.parse annotated screenshot.
// Exported so a caller compositing onto something other than a freshly
// decoded screenshot (e.g. the AI Vision live video overlay in
// internal/service/ai_vision.go, drawing straight into the RGBA frame
// buffer about to reach the native Vulkan/Metal/GL renderer) can reuse the
// exact same drawing code instead of re-implementing it.
func DrawDetectionBox(img *image.RGBA, box Box, isText bool) {
	c := iconColor
	if isText {
		c = textColor
	}
	drawRect(img, box, c)
}

// DrawDetectionTag draws one detection's Set-of-Mark hex-id tag anchored
// to its box -- see drawMarkTag and DrawDetectionBox's doc comment.
func DrawDetectionTag(img *image.RGBA, id string, box Box) {
	drawMarkTag(img, id, int(box.X1), int(box.Y1))
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

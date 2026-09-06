package localui

import (
	"bytes"
	"image"
	"image/png"
)

// rgbImage is this package's lightweight stand-in for gocv.Mat: a
// row-major RGB byte buffer (no OpenCV/cgo dependency -- everything here
// runs on plain Go slices so the client binary doesn't need to bundle
// OpenCV on top of ONNX Runtime).
type rgbImage struct {
	W, H int
	Pix  []uint8 // len == W*H*3, row-major R,G,B
}

func newRGBImage(w, h int) *rgbImage {
	return &rgbImage{W: w, H: h, Pix: make([]uint8, w*h*3)}
}

func decodeToRGB(data []byte) (*rgbImage, error) {
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	out := newRGBImage(w, h)
	// image.RGBA fast path (screen.get_image always produces this), with a
	// generic fallback via At() for any other PNG color model.
	if rgba, ok := img.(*image.RGBA); ok {
		for y := 0; y < h; y++ {
			srcOff := rgba.PixOffset(b.Min.X, b.Min.Y+y)
			dstOff := y * w * 3
			for x := 0; x < w; x++ {
				out.Pix[dstOff] = rgba.Pix[srcOff]
				out.Pix[dstOff+1] = rgba.Pix[srcOff+1]
				out.Pix[dstOff+2] = rgba.Pix[srcOff+2]
				srcOff += 4
				dstOff += 3
			}
		}
		return out, nil
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, bl, _ := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			off := (y*w + x) * 3
			out.Pix[off] = uint8(r >> 8)
			out.Pix[off+1] = uint8(g >> 8)
			out.Pix[off+2] = uint8(bl >> 8)
		}
	}
	return out, nil
}

// region returns a copy of the sub-rectangle [x1,y1)-[x2,y2) (clamped to
// img's bounds).
func (img *rgbImage) region(x1, y1, x2, y2 int) *rgbImage {
	if x1 < 0 {
		x1 = 0
	}
	if y1 < 0 {
		y1 = 0
	}
	if x2 > img.W {
		x2 = img.W
	}
	if y2 > img.H {
		y2 = img.H
	}
	w, h := x2-x1, y2-y1
	if w <= 0 || h <= 0 {
		return newRGBImage(0, 0)
	}
	out := newRGBImage(w, h)
	for y := 0; y < h; y++ {
		srcOff := ((y1+y)*img.W + x1) * 3
		dstOff := y * w * 3
		copy(out.Pix[dstOff:dstOff+w*3], img.Pix[srcOff:srcOff+w*3])
	}
	return out
}

// resize does a bilinear resize to exactly (newW, newH) -- close enough to
// OpenCV's INTER_LINEAR/INTER_CUBIC for detector/recognizer inputs that are
// themselves tolerant of minor resampling differences (YOLO's confidence
// threshold here is 0.05, SVTR is a CTC decoder over fixed 48x320 crops).
func (img *rgbImage) resize(newW, newH int) *rgbImage {
	if newW < 1 {
		newW = 1
	}
	if newH < 1 {
		newH = 1
	}
	out := newRGBImage(newW, newH)
	if img.W == 0 || img.H == 0 {
		return out
	}
	scaleX := float64(img.W) / float64(newW)
	scaleY := float64(img.H) / float64(newH)
	for y := 0; y < newH; y++ {
		sy := (float64(y)+0.5)*scaleY - 0.5
		if sy < 0 {
			sy = 0
		}
		y0 := int(sy)
		y1 := y0 + 1
		if y1 >= img.H {
			y1 = img.H - 1
		}
		fy := sy - float64(y0)
		for x := 0; x < newW; x++ {
			sx := (float64(x)+0.5)*scaleX - 0.5
			if sx < 0 {
				sx = 0
			}
			x0 := int(sx)
			x1 := x0 + 1
			if x1 >= img.W {
				x1 = img.W - 1
			}
			fx := sx - float64(x0)

			dstOff := (y*newW + x) * 3
			for c := 0; c < 3; c++ {
				p00 := float64(img.Pix[(y0*img.W+x0)*3+c])
				p01 := float64(img.Pix[(y0*img.W+x1)*3+c])
				p10 := float64(img.Pix[(y1*img.W+x0)*3+c])
				p11 := float64(img.Pix[(y1*img.W+x1)*3+c])
				top := p00 + (p01-p00)*fx
				bot := p10 + (p11-p10)*fx
				v := top + (bot-top)*fy
				out.Pix[dstOff+c] = clampU8(v)
			}
		}
	}
	return out
}

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

// toGray computes standard BT.601 luma (same coefficients OpenCV's
// COLOR_RGB2GRAY/COLOR_BGR2GRAY use -- the formula is channel-order
// agnostic as long as the right values go in the right slots).
func (img *rgbImage) toGray() []uint8 {
	out := make([]uint8, img.W*img.H)
	for i := 0; i < img.W*img.H; i++ {
		r := float64(img.Pix[i*3])
		g := float64(img.Pix[i*3+1])
		b := float64(img.Pix[i*3+2])
		out[i] = clampU8(0.299*r + 0.587*g + 0.114*b)
	}
	return out
}

func grayToRGB3(gray []uint8, w, h int) *rgbImage {
	out := newRGBImage(w, h)
	for i := 0; i < w*h; i++ {
		out.Pix[i*3] = gray[i]
		out.Pix[i*3+1] = gray[i]
		out.Pix[i*3+2] = gray[i]
	}
	return out
}

// letterboxMeta mirrors usbridge/modules/ui_parser/letterbox.go's.
type letterboxMeta struct {
	scale           float64
	padLeft, padTop int
}

// letterbox resizes src to fit inside a size x size canvas (aspect
// preserved) centered on gray (114,114,114) padding -- ultralytics' own
// default preprocessing convention, matching the original YOLO export.
func letterboxRGB(src *rgbImage, size int) (*rgbImage, letterboxMeta) {
	w, h := src.W, src.H
	scale := float64(size) / float64(h)
	if wScale := float64(size) / float64(w); wScale < scale {
		scale = wScale
	}
	nw := int(float64(w)*scale + 0.5)
	nh := int(float64(h)*scale + 0.5)
	resized := src.resize(nw, nh)

	padLeft := (size - nw) / 2
	padTop := (size - nh) / 2

	out := newRGBImage(size, size)
	for i := range out.Pix {
		out.Pix[i] = 114
	}
	for y := 0; y < nh; y++ {
		srcOff := y * nw * 3
		dstOff := ((y+padTop)*size + padLeft) * 3
		copy(out.Pix[dstOff:dstOff+nw*3], resized.Pix[srcOff:srcOff+nw*3])
	}
	return out, letterboxMeta{scale: scale, padLeft: padLeft, padTop: padTop}
}

func (lm letterboxMeta) toOriginal(b Box) Box {
	return Box{
		X1: (b.X1 - float64(lm.padLeft)) / lm.scale,
		Y1: (b.Y1 - float64(lm.padTop)) / lm.scale,
		X2: (b.X2 - float64(lm.padLeft)) / lm.scale,
		Y2: (b.Y2 - float64(lm.padTop)) / lm.scale,
	}
}

// toNCHWFloat converts an RGB image to a flat NCHW float32 slice, values
// scaled to [0,1] (plain /255, matching the mean=0/std=255 convention the
// device's .rknn models were converted with -- see
// usbridge/modules/ui_parser/letterbox.go's matToNHWCFloat doc comment;
// confirmed empirically against these same freshly-exported ONNX graphs
// that plain /255, not ImageNet mean/std, is the right normalization here).
func (img *rgbImage) toNCHWFloat() []float32 {
	n := img.W * img.H
	out := make([]float32, n*3)
	for i := 0; i < n; i++ {
		out[i] = float32(img.Pix[i*3]) / 255.0       // R plane
		out[n+i] = float32(img.Pix[i*3+1]) / 255.0   // G plane
		out[2*n+i] = float32(img.Pix[i*3+2]) / 255.0 // B plane
	}
	return out
}

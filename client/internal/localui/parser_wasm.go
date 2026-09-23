//go:build js && wasm

package localui

// The real half of this package's wasm build: icon_detect (YOLOv8 UI-
// element detector) running via onnxruntime-web in the browser tab itself
// (client/web/ai_vision.js), instead of onnxruntime_go's cgo binding every
// other platform uses. See stub_wasm.go's top doc comment for the split
// between this and what stays unimplemented (dbnet+svtr OCR).
//
// Pre/post-processing below -- decodeToRGB, letterbox, /255 NCHW packing,
// YOLO decode+NMS, Set-of-Mark ID assignment -- is ported line-for-line
// from the native build's image.go/yolo.go/tile.go/marks.go rather than
// reimplemented, so both platforms run exactly the same math on exactly
// the same model and only actually disagree where the underlying
// runtime/hardware does. The one deliberate difference: no CLAHE
// (clahe.go's contrast-boost preprocessing) -- yolo.go's own doc comment
// already treats it as an approximation-grade nicety ("close enough...
// YOLO here runs at a permissive 0.05 confidence threshold, not exact
// color fidelity"), not worth a second from-scratch port for a first
// version of the browser overlay.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"syscall/js"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const iconInputSize = 640

// Parser holds nothing but the model URL passed to
// window.usbridgeAIVision.loadModel -- the actual onnxruntime-web session
// lives entirely on the JS side (ai_vision.js's own module-level
// sessionPromise), reused across every Parser (there's only ever one, see
// api.SetLocalUIParser) and every ParseIconsOnly call.
type Parser struct {
	modelURL string
}

// NewParser resolves window.usbridgeAIVision (defined by client/web/
// ai_vision.js, loaded as a <script type="module"> from index.html) and
// blocks until its loadModel(cfg.IconONNXPath) promise settles -- the same
// "loading takes hundreds of ms to seconds, do it once up front" contract
// InitLocalUIParseFromConfig's own doc comment describes, except here
// "loading" can also mean fetching ~100MB+ over the network the first time
// (see ai_vision.js's own top doc comment on why that fetch is lazy, not
// at page load). cfg.DBNetONNXPath/SVTRONNXPath/SharedLibPath/UseGPU are
// silently ignored -- there is no OCR stage and no separate shared-library
// path to pick a backend from on this platform (executionProviders is
// ai_vision.js's own fixed webgpu-then-wasm list).
func NewParser(cfg Config) (*Parser, error) {
	bridge := js.Global().Get("usbridgeAIVision")
	if bridge.IsUndefined() || bridge.IsNull() {
		return nil, fmt.Errorf("window.usbridgeAIVision is not defined -- ai_vision.js failed to load")
	}
	if cfg.IconONNXPath == "" {
		return nil, fmt.Errorf("no icon_detect model path configured")
	}
	if _, err := awaitPromise(bridge.Call("loadModel", cfg.IconONNXPath)); err != nil {
		return nil, fmt.Errorf("loadModel(%s): %w", cfg.IconONNXPath, err)
	}
	return &Parser{modelURL: cfg.IconONNXPath}, nil
}

// ParseIconsOnly mirrors the native Parser's method of the same name
// (parser.go): decode -> letterbox to 640x640 -> /255 NCHW -> run
// icon_detect -> decode YOLO output -> map boxes back to original-image
// coordinates -> assign Set-of-Mark IDs. The only platform-specific piece
// is runInference itself, which crosses into JS instead of calling a cgo
// session.Run.
func (p *Parser) ParseIconsOnly(imgBytes []byte) (icons []Icon, err error) {
	original, err := decodeToRGB(imgBytes)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if original.W == 0 || original.H == 0 {
		return nil, fmt.Errorf("decode image: empty result")
	}

	letterboxed, meta := letterboxRGB(original, iconInputSize)
	tensor := letterboxed.toNCHWFloat()

	raw, err := p.runInference(tensor)
	if err != nil {
		return nil, fmt.Errorf("icon_detect inference: %w", err)
	}

	for _, icon := range decodeYOLO(raw) {
		icon.Bbox = meta.toOriginal(icon.Bbox)
		icons = append(icons, icon)
	}
	assignMarkIDs(icons, nil)
	return icons, nil
}

// runInference crosses into window.usbridgeAIVision.runInference (see
// ai_vision.js) and back: Go's []float32 tensor -> a JS Float32Array (raw
// bytes copied in bulk via js.CopyBytesToJS, not element-by-element --
// 3*640*640 individual syscall/js calls per pass would dwarf the actual
// inference cost) -> awaited promise -> the raw output Float32Array copied
// back the same way.
func (p *Parser) runInference(tensor []float32) ([]float32, error) {
	bridge := js.Global().Get("usbridgeAIVision")
	input := float32sToJS(tensor)
	result, err := awaitPromise(bridge.Call("runInference", input))
	if err != nil {
		return nil, err
	}
	return jsToFloat32s(result), nil
}

// float32sToJS packs data as raw little-endian bytes (WASM/JS are always
// little-endian, so this is a plain reinterpretation, not a real
// conversion) into a fresh JS Float32Array backed by its own ArrayBuffer.
func float32sToJS(data []float32) js.Value {
	buf := make([]byte, len(data)*4)
	for i, v := range data {
		binary.LittleEndian.PutUint32(buf[i*4:], math.Float32bits(v))
	}
	jsBytes := js.Global().Get("Uint8Array").New(len(buf))
	js.CopyBytesToJS(jsBytes, buf)
	return js.Global().Get("Float32Array").New(jsBytes.Get("buffer"))
}

// jsToFloat32s is float32sToJS's inverse -- v is a JS Float32Array
// (ai_vision.js's runInference resolves with outputs[name].data, itself a
// Float32Array view). Constructs a Uint8Array over exactly v's own
// byteOffset/byteLength within its buffer, not the whole underlying
// buffer, since onnxruntime-web may return a view into a larger allocation.
func jsToFloat32s(v js.Value) []float32 {
	n := v.Get("length").Int()
	byteOffset := v.Get("byteOffset").Int()
	byteLength := n * 4
	jsBytes := js.Global().Get("Uint8Array").New(v.Get("buffer"), byteOffset, byteLength)
	raw := make([]byte, byteLength)
	js.CopyBytesToGo(raw, jsBytes)
	out := make([]float32, n)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
	}
	return out
}

// awaitPromise blocks the calling goroutine until a JS Promise settles --
// same pattern (and same reasoning: Go's wasm scheduler is cooperative, so
// blocking one goroutine on a channel here doesn't block the JS event loop
// a js.FuncOf callback needs to run on) as internal/webrtcweb/
// client_wasm.go's own awaitPromise. Not shared directly across packages
// for a ~20 line helper neither package otherwise depends on the other for.
func awaitPromise(promise js.Value) (js.Value, error) {
	resultCh := make(chan js.Value, 1)
	errCh := make(chan error, 1)

	var thenFunc, catchFunc js.Func
	thenFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer thenFunc.Release()
		defer catchFunc.Release()
		if len(args) > 0 {
			resultCh <- args[0]
		} else {
			resultCh <- js.Undefined()
		}
		return nil
	})
	catchFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer thenFunc.Release()
		defer catchFunc.Release()
		msg := "promise rejected"
		if len(args) > 0 {
			if m := args[0].Get("message"); !m.IsUndefined() {
				msg = m.String()
			} else {
				msg = args[0].String()
			}
		}
		errCh <- fmt.Errorf("%s", msg)
		return nil
	})
	promise.Call("then", thenFunc).Call("catch", catchFunc)

	select {
	case v := <-resultCh:
		return v, nil
	case err := <-errCh:
		return js.Value{}, err
	}
}

// ---- ported from image.go (native build, excluded here by its own
// !(js && wasm) build tag) -- see this file's top doc comment. ----

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
				if v < 0 {
					v = 0
				} else if v > 255 {
					v = 255
				}
				out.Pix[dstOff+c] = uint8(v + 0.5)
			}
		}
	}
	return out
}

type letterboxMeta struct {
	scale           float64
	padLeft, padTop int
}

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

func (img *rgbImage) toNCHWFloat() []float32 {
	n := img.W * img.H
	out := make([]float32, n*3)
	for i := 0; i < n; i++ {
		out[i] = float32(img.Pix[i*3]) / 255.0
		out[n+i] = float32(img.Pix[i*3+1]) / 255.0
		out[2*n+i] = float32(img.Pix[i*3+2]) / 255.0
	}
	return out
}

// ---- ported from yolo.go/tile.go ----

const (
	yoloConfThresh = 0.05
	yoloIOUThresh  = 0.1
)

func decodeYOLO(raw []float32) []Icon {
	const numAnchors = 8400
	if len(raw) != 5*numAnchors {
		return nil
	}
	cx := raw[0*numAnchors : 1*numAnchors]
	cy := raw[1*numAnchors : 2*numAnchors]
	w := raw[2*numAnchors : 3*numAnchors]
	h := raw[3*numAnchors : 4*numAnchors]
	conf := raw[4*numAnchors : 5*numAnchors]

	var boxes []Box
	var scores []float32
	for i := 0; i < numAnchors; i++ {
		if conf[i] <= yoloConfThresh {
			continue
		}
		boxes = append(boxes, Box{
			X1: float64(cx[i] - w[i]/2),
			Y1: float64(cy[i] - h[i]/2),
			X2: float64(cx[i] + w[i]/2),
			Y2: float64(cy[i] + h[i]/2),
		})
		scores = append(scores, conf[i])
	}
	if len(boxes) == 0 {
		return nil
	}

	keep := nmsIndices(boxes, scores, yoloIOUThresh)
	icons := make([]Icon, 0, len(keep))
	for _, idx := range keep {
		icons = append(icons, Icon{Bbox: boxes[idx], Confidence: float64(scores[idx])})
	}
	return icons
}

func nmsIndices(boxes []Box, scores []float32, iouThresh float64) []int {
	order := make([]int, len(boxes))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool { return scores[order[a]] > scores[order[b]] })

	var kept []int
	for _, idx := range order {
		redundant := false
		for _, k := range kept {
			if boxIoU(boxes[idx], boxes[k]) > iouThresh {
				redundant = true
				break
			}
		}
		if !redundant {
			kept = append(kept, idx)
		}
	}
	return kept
}

func boxIoU(a, b Box) float64 {
	x1 := math.Max(a.X1, b.X1)
	y1 := math.Max(a.Y1, b.Y1)
	x2 := math.Min(a.X2, b.X2)
	y2 := math.Min(a.Y2, b.Y2)
	interW := x2 - x1
	interH := y2 - y1
	if interW <= 0 || interH <= 0 {
		return 0
	}
	inter := interW * interH
	areaA := (a.X2 - a.X1) * (a.Y2 - a.Y1)
	areaB := (b.X2 - b.X1) * (b.Y2 - b.Y1)
	union := areaA + areaB - inter
	if union <= 0 {
		return 0
	}
	return inter / union
}

// ---- ported from marks.go ----

// assignMarkIDs mirrors the native build's marks.go: "00".."FF" Set-of-Mark
// hex tags, icons first then text (text is always nil here -- no OCR stage
// on this platform, see stub_wasm.go), matching the device's tagging
// exactly so an agent sees identical IDs regardless of which backend
// answered ui.parse.
func assignMarkIDs(icons []Icon, text []TextRegion) {
	n := 0
	next := func() string {
		id := fmt.Sprintf("%02X", n)
		n++
		return id
	}
	for i := range icons {
		icons[i].ID = next()
	}
	for i := range text {
		text[i].ID = next()
	}
}

// ---- ported from draw.go ----

var (
	iconWasmColor = color.RGBA{R: 255, G: 0, B: 0, A: 255} // red
	textWasmColor = color.RGBA{R: 0, G: 255, B: 0, A: 255} // green
	markWasmBg    = color.RGBA{R: 0, G: 0, B: 0, A: 255}
	markWasmFg    = color.RGBA{R: 255, G: 255, B: 255, A: 255}
)

// DrawDetectionBox mirrors the native build's draw.go exactly (see that
// file's doc comment) -- ai_vision.go's buildAIVisionOverlayImage/
// drawCachedOverlay call this unconditionally on every platform.
func DrawDetectionBox(img *image.RGBA, box Box, isText bool) {
	c := iconWasmColor
	if isText {
		c = textWasmColor
	}
	drawWasmRect(img, box, c)
}

// DrawDetectionTag mirrors the native build's draw.go exactly.
func DrawDetectionTag(img *image.RGBA, id string, box Box) {
	drawWasmMarkTag(img, id, int(box.X1), int(box.Y1))
}

func drawWasmRect(img *image.RGBA, b Box, c color.RGBA) {
	x1, y1, x2, y2 := int(b.X1), int(b.Y1), int(b.X2), int(b.Y2)
	const thickness = 2
	for t := 0; t < thickness; t++ {
		hWasmLine(img, x1, x2, y1+t, c)
		hWasmLine(img, x1, x2, y2-t, c)
		vWasmLine(img, y1, y2, x1+t, c)
		vWasmLine(img, y1, y2, x2-t, c)
	}
}

func hWasmLine(img *image.RGBA, x1, x2, y int, c color.RGBA) {
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

func vWasmLine(img *image.RGBA, y1, y2, x int, c color.RGBA) {
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

func drawWasmMarkTag(img *image.RGBA, id string, boxX, boxY int) {
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
	draw.Draw(img, bg, &image.Uniform{C: markWasmBg}, image.Point{}, draw.Src)

	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: markWasmFg},
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x+pad, y+pad+10),
	}
	d.DrawString(id)
}

//go:build js && wasm

package localui

// The real half of this package's wasm build: icon_detect (YOLOv8 UI-
// element detector), dbnet (text-region detector) and svtr (text
// recognizer) all running via onnxruntime-web in the browser tab itself
// (client/web/ai_vision.js), instead of onnxruntime_go's cgo binding every
// other platform uses. See stub_wasm.go's top doc comment.
//
// Pre/post-processing below -- decodeToRGB, letterbox, CLAHE, /255 NCHW
// packing, YOLO decode+NMS, DBNet blob extraction+tiling, SVTR CTC
// decode, Set-of-Mark ID assignment, label association, zoom hints -- is
// ported line-for-line from the native build's image.go/clahe.go/yolo.go/
// dbnet.go/tile.go/svtr.go/labels.go/density.go/parser.go rather than
// reimplemented, so both platforms run exactly the same math on exactly
// the same models and only actually disagree where the underlying
// runtime/hardware does. The one deliberate simplification: SVTR crops run
// one at a time (batch size 1) instead of native's svtrBatchSize-at-a-time
// batched Run() -- that batching exists to amortize cgo's per-call
// overhead, which doesn't apply the same way across a JS/wasm boundary,
// and a live preview overlay only ever OCRs a handful of near-icon boxes
// per pass (see ai_vision.go's ParseFastNearIconsStaged usage), not the
// hundreds a dense full-screenshot ui.parse call might.

import (
	_ "embed"
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"strings"
	"syscall/js"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

const (
	iconInputSize = 640
	dbnetMapSize  = 960
)

// Parser holds the recognized-text dictionary (loaded once) -- the actual
// onnxruntime-web sessions live entirely on the JS side (ai_vision.js's
// own module-level sessions Map, keyed by "icon"/"dbnet"/"svtr"), reused
// across every Parser (there's only ever one, see api.SetLocalUIParser)
// and every Parse* call.
type Parser struct {
	dict []string
}

// NewParser resolves window.usbridgeAIVision (defined by client/web/
// ai_vision.js, loaded as a <script type="module"> from index.html) and
// blocks until all three models' loadModel promises settle -- the same
// "loading takes hundreds of ms to seconds, do it once up front" contract
// InitLocalUIParseFromConfig's own doc comment describes, except here
// "loading" can also mean fetching MBs over the network the first time
// (see ai_vision.js's own top doc comment on why that fetch is lazy, not
// at page load). cfg.SharedLibPath/UseGPU are silently ignored -- there is
// no separate shared-library path to pick a backend from on this platform
// (executionProviders is ai_vision.js's own fixed webgpu-then-wasm list).
func NewParser(cfg Config) (*Parser, error) {
	bridge := js.Global().Get("usbridgeAIVision")
	if bridge.IsUndefined() || bridge.IsNull() {
		return nil, fmt.Errorf("window.usbridgeAIVision is not defined -- ai_vision.js failed to load")
	}
	models := []struct {
		name string
		path string
		dims []int
	}{
		{"icon", cfg.IconONNXPath, []int{1, 3, iconInputSize, iconInputSize}},
		{"dbnet", cfg.DBNetONNXPath, []int{1, 3, dbnetMapSize, dbnetMapSize}},
		{"svtr", cfg.SVTRONNXPath, []int{1, 3, svtrHeight, svtrWidth}},
	}
	for _, m := range models {
		if m.path == "" {
			return nil, fmt.Errorf("no %s model path configured", m.name)
		}
		if _, err := awaitPromise(bridge.Call("loadModel", m.name, m.path, dimsToJS(m.dims))); err != nil {
			return nil, fmt.Errorf("loadModel(%s, %s): %w", m.name, m.path, err)
		}
	}
	return &Parser{dict: loadSVTRDict()}, nil
}

func dimsToJS(dims []int) js.Value {
	arr := make([]interface{}, len(dims))
	for i, d := range dims {
		arr[i] = d
	}
	return js.ValueOf(arr)
}

// runInference crosses into window.usbridgeAIVision.runInference (see
// ai_vision.js) and back: Go's []float32 tensor -> a JS Float32Array (raw
// bytes copied in bulk via js.CopyBytesToJS, not element-by-element -- a
// per-element syscall/js call per float would dwarf the actual inference
// cost) -> awaited promise -> the raw output Float32Array copied back the
// same way.
func (p *Parser) runInference(name string, tensor []float32, dims []int) ([]float32, error) {
	bridge := js.Global().Get("usbridgeAIVision")
	input := float32sToJS(tensor)
	result, err := awaitPromise(bridge.Call("runInference", name, input, dimsToJS(dims)))
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

// ==== Parse* public API -- mirrors parser.go's exactly, all delegating to
// the shared parse() core below. ====

// ParseIconsOnly decodes imgBytes and runs icon_detect ONLY -- no dbnet, no
// svtr, no marked PNG. See parser.go's ParseIconsOnly doc comment (native
// build) for the full rationale; behavior here is identical.
func (p *Parser) ParseIconsOnly(imgBytes []byte) (icons []Icon, err error) {
	original, err := decodeToRGB(imgBytes)
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}
	if original.W == 0 || original.H == 0 {
		return nil, fmt.Errorf("decode image: empty result")
	}
	icons, err = p.runIconStage(original)
	if err != nil {
		return nil, err
	}
	assignMarkIDs(icons, nil)
	return icons, nil
}

func (p *Parser) Parse(imgBytes []byte) (markedPNG []byte, result *Result, err error) {
	return p.parse(imgBytes, true, nil, nil, nil)
}

func (p *Parser) ParseFast(imgBytes []byte) (result *Result, err error) {
	_, result, err = p.parse(imgBytes, false, nil, nil, nil)
	return result, err
}

func (p *Parser) ParseFastNearIcons(imgBytes []byte) (result *Result, err error) {
	_, result, err = p.parse(imgBytes, false, nil, filterBoxesNearIcons, nil)
	return result, err
}

func (p *Parser) ParseFastNearIconsStaged(imgBytes []byte, onTextBoxes func(boxes []Box)) (result *Result, err error) {
	_, result, err = p.parse(imgBytes, false, nil, filterBoxesNearIcons, onTextBoxes)
	return result, err
}

func (p *Parser) ParseStaged(imgBytes []byte, onIcons func(icons []Icon)) (result *Result, err error) {
	_, result, err = p.parse(imgBytes, false, onIcons, nil, nil)
	return result, err
}

// parse mirrors parser.go's own parse() exactly -- see that function's doc
// comment for the full phase-by-phase rationale.
func (p *Parser) parse(imgBytes []byte, drawMarked bool, onIcons func(icons []Icon), textFilter func(icons []Icon, boxes []Box) []Box, onTextBoxes func(boxes []Box)) (markedPNG []byte, result *Result, err error) {
	original, err := decodeToRGB(imgBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("decode image: %w", err)
	}
	if original.W == 0 || original.H == 0 {
		return nil, nil, fmt.Errorf("decode image: empty result")
	}

	res := &Result{ImageWidth: original.W, ImageHeight: original.H, Backend: "local-onnx-web"}

	res.Icons, err = p.runIconStage(original)
	if err != nil {
		return nil, nil, err
	}

	if onIcons != nil {
		iconsCopy := append([]Icon(nil), res.Icons...)
		assignMarkIDs(iconsCopy, nil)
		onIcons(iconsCopy)
	}

	tiles := tileRects(original.W, original.H, dbnetMapSize, dbnetTileOverlap)
	if tiles == nil {
		tiles = []rect{{X1: 0, Y1: 0, X2: original.W, Y2: original.H}}
	}

	var allTextBoxes []Box
	for _, t := range tiles {
		boxes, err := p.detectTextInTile(original, t)
		if err != nil {
			return nil, nil, err
		}
		allTextBoxes = append(allTextBoxes, boxes...)
	}
	textBoxes := mergeOverlappingBoxes(allTextBoxes, 0.3)

	if textFilter != nil {
		textBoxes = textFilter(res.Icons, textBoxes)
	}

	if onTextBoxes != nil {
		boxesCopy := append([]Box(nil), textBoxes...)
		onTextBoxes(boxesCopy)
	}

	res.Text = p.recognizeSVTR(original, textBoxes)

	associateLabels(res.Icons, res.Text)
	assignMarkIDs(res.Icons, res.Text)
	res.ZoomHints = findZoomHints(res.Icons)

	if drawMarked {
		markedPNG = drawResultWasm(original, res)
	}

	return markedPNG, res, nil
}

// runIconStage mirrors parser.go's own exactly: letterbox to 640x640
// (gray114 pad), /255 NCHW, icon_detect inference, YOLO decode+NMS,
// coordinate un-mapping. No IDs assigned, no labels -- callers number/
// associate as appropriate (see ParseIconsOnly/parse's onIcons handling).
func (p *Parser) runIconStage(original *rgbImage) ([]Icon, error) {
	letterboxed, meta := letterboxRGB(original, iconInputSize)
	tensor := letterboxed.toNCHWFloat()

	raw, err := p.runInference("icon", tensor, []int{1, 3, iconInputSize, iconInputSize})
	if err != nil {
		return nil, fmt.Errorf("icon_detect inference: %w", err)
	}

	var icons []Icon
	for _, icon := range decodeYOLO(raw) {
		icon.Bbox = meta.toOriginal(icon.Bbox)
		icons = append(icons, icon)
	}
	return icons, nil
}

// detectTextInTile mirrors parser.go's own exactly.
func (p *Parser) detectTextInTile(original *rgbImage, t rect) ([]Box, error) {
	tileImg, dbLBMeta := prepareDBNetTile(original, t, dbnetMapSize)
	dbInput := tileImg.toNCHWFloat()

	dbOut, err := p.runInference("dbnet", dbInput, []int{1, 3, dbnetMapSize, dbnetMapSize})
	if err != nil {
		return nil, fmt.Errorf("paddle_dbnet inference: %w", err)
	}

	var boxes []Box
	for _, boxLocal := range decodeDBNet(dbOut) {
		boxTile := dbLBMeta.toOriginal(boxLocal)
		boxes = append(boxes, Box{
			X1: boxTile.X1 + float64(t.X1),
			Y1: boxTile.Y1 + float64(t.Y1),
			X2: boxTile.X2 + float64(t.X1),
			Y2: boxTile.Y2 + float64(t.Y1),
		})
	}
	return boxes, nil
}

func prepareDBNetTile(original *rgbImage, t rect, size int) (*rgbImage, letterboxMeta) {
	crop := original.region(t.X1, t.Y1, t.X2, t.Y2)
	if crop.W == size && crop.H == size {
		return applyGrayCLAHE(crop), letterboxMeta{scale: 1}
	}
	lb, meta := letterboxRGB(crop, size)
	return applyGrayCLAHE(lb), meta
}

// recognizeSVTR is parser.go's batchRecognizeSVTR, minus the batching --
// see this file's top doc comment for why a JS/wasm boundary doesn't need
// it the same way cgo's per-call overhead does.
func (p *Parser) recognizeSVTR(original *rgbImage, textBoxes []Box) []TextRegion {
	var out []TextRegion
	for _, box := range textBoxes {
		var texts []string
		var confSum float64
		for _, crop := range planSVTRCrops(box) {
			cropImg, ok := safeCrop(original, crop)
			if !ok {
				continue
			}
			tensor := preprocessSVTRCrop(cropImg)
			logits, err := p.runInference("svtr", tensor, []int{1, 3, svtrHeight, svtrWidth})
			if err != nil {
				continue
			}
			text, conf := ctcGreedyDecodeSVTR(logits, p.dict)
			if text == "" {
				continue
			}
			texts = append(texts, text)
			confSum += conf
		}
		if len(texts) == 0 {
			continue
		}
		out = append(out, TextRegion{Bbox: box, Text: joinChunks(texts), Confidence: confSum / float64(len(texts))})
	}
	return out
}

const maxSVTRAspect = 6.0

// planSVTRCrops mirrors parser.go's own exactly.
func planSVTRCrops(box Box) []Box {
	w := box.X2 - box.X1
	h := box.Y2 - box.Y1
	if h <= 0 || w/h <= maxSVTRAspect {
		return []Box{box}
	}

	chunkW := maxSVTRAspect * h
	overlap := chunkW * 0.15
	n := int(w/(chunkW-overlap)) + 1

	var chunks []Box
	for i := 0; i < n; i++ {
		x1 := box.X1 + float64(i)*(chunkW-overlap)
		x2 := x1 + chunkW
		if x2 > box.X2 {
			x2 = box.X2
		}
		if x2-x1 < h {
			continue
		}
		chunks = append(chunks, Box{X1: x1, Y1: box.Y1, X2: x2, Y2: box.Y2})
	}
	return chunks
}

func safeCrop(img *rgbImage, box Box) (*rgbImage, bool) {
	x1, y1 := int(box.X1), int(box.Y1)
	x2, y2 := int(box.X2), int(box.Y2)
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
	if x2-x1 < 2 || y2-y1 < 2 {
		return nil, false
	}
	return img.region(x1, y1, x2, y2), true
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

func clampU8(v float64) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v + 0.5)
}

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

// ---- ported from clahe.go ----

func claheGray(gray []uint8, w, h int, clipLimit float64, tilesX, tilesY int) []uint8 {
	tileW := (w + tilesX - 1) / tilesX
	tileH := (h + tilesY - 1) / tilesY

	mappings := make([][][256]uint8, tilesY)
	for ty := 0; ty < tilesY; ty++ {
		mappings[ty] = make([][256]uint8, tilesX)
		for tx := 0; tx < tilesX; tx++ {
			x0, y0 := tx*tileW, ty*tileH
			x1, y1 := minInt(x0+tileW, w), minInt(y0+tileH, h)

			var hist [256]int
			count := 0
			for y := y0; y < y1; y++ {
				row := y * w
				for x := x0; x < x1; x++ {
					hist[gray[row+x]]++
					count++
				}
			}
			if count == 0 {
				for i := 0; i < 256; i++ {
					mappings[ty][tx][i] = uint8(i)
				}
				continue
			}

			clipAt := int(clipLimit * float64(count) / 256.0)
			if clipAt < 1 {
				clipAt = 1
			}
			excess := 0
			for i := 0; i < 256; i++ {
				if hist[i] > clipAt {
					excess += hist[i] - clipAt
					hist[i] = clipAt
				}
			}
			redist := excess / 256
			rem := excess - redist*256
			for i := 0; i < 256; i++ {
				hist[i] += redist
				if i < rem {
					hist[i]++
				}
			}

			var cdf [256]int
			running := 0
			for i := 0; i < 256; i++ {
				running += hist[i]
				cdf[i] = running
			}
			scale := 255.0 / float64(count)
			for i := 0; i < 256; i++ {
				mappings[ty][tx][i] = clampU8(float64(cdf[i]) * scale)
			}
		}
	}

	out := make([]uint8, w*h)
	centerX := func(tx int) float64 { return float64(tx)*float64(tileW) + float64(tileW)/2 }
	centerY := func(ty int) float64 { return float64(ty)*float64(tileH) + float64(tileH)/2 }

	for y := 0; y < h; y++ {
		fy := float64(y)
		ty0 := int((fy - float64(tileH)/2) / float64(tileH))
		if ty0 < 0 {
			ty0 = 0
		}
		ty1 := ty0 + 1
		if ty1 >= tilesY {
			ty1 = tilesY - 1
		}
		if ty0 >= tilesY {
			ty0 = tilesY - 1
		}
		denomY := centerY(ty1) - centerY(ty0)
		wy := 0.0
		if denomY > 0 {
			wy = (fy - centerY(ty0)) / denomY
		}
		wy = clampF(wy, 0, 1)

		for x := 0; x < w; x++ {
			fx := float64(x)
			tx0 := int((fx - float64(tileW)/2) / float64(tileW))
			if tx0 < 0 {
				tx0 = 0
			}
			tx1 := tx0 + 1
			if tx1 >= tilesX {
				tx1 = tilesX - 1
			}
			if tx0 >= tilesX {
				tx0 = tilesX - 1
			}
			denomX := centerX(tx1) - centerX(tx0)
			wx := 0.0
			if denomX > 0 {
				wx = (fx - centerX(tx0)) / denomX
			}
			wx = clampF(wx, 0, 1)

			v := gray[y*w+x]
			v00 := float64(mappings[ty0][tx0][v])
			v01 := float64(mappings[ty0][tx1][v])
			v10 := float64(mappings[ty1][tx0][v])
			v11 := float64(mappings[ty1][tx1][v])
			top := v00 + (v01-v00)*wx
			bot := v10 + (v11-v10)*wx
			out[y*w+x] = clampU8(top + (bot-top)*wy)
		}
	}
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func applyGrayCLAHE(src *rgbImage) *rgbImage {
	gray := src.toGray()
	eq := claheGray(gray, src.W, src.H, 2.0, 8, 8)
	return grayToRGB3(eq, src.W, src.H)
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

type rect struct{ X1, Y1, X2, Y2 int }

const dbnetTileOverlap = 150

func tileRects(imgW, imgH, tileSize, overlap int) []rect {
	if imgW < tileSize || imgH < tileSize {
		return nil
	}
	xs := axisStarts(imgW, tileSize, overlap)
	ys := axisStarts(imgH, tileSize, overlap)
	rects := make([]rect, 0, len(xs)*len(ys))
	for _, y := range ys {
		for _, x := range xs {
			rects = append(rects, rect{X1: x, Y1: y, X2: x + tileSize, Y2: y + tileSize})
		}
	}
	return rects
}

func axisStarts(dim, tileSize, overlap int) []int {
	step := tileSize - overlap
	if step <= 0 {
		step = tileSize
	}
	var starts []int
	x := 0
	for {
		if x+tileSize >= dim {
			starts = append(starts, dim-tileSize)
			break
		}
		starts = append(starts, x)
		x += step
	}
	return starts
}

func mergeOverlappingBoxes(boxes []Box, iouThresh float64) []Box {
	if len(boxes) <= 1 {
		return boxes
	}
	order := make([]int, len(boxes))
	for i := range order {
		order[i] = i
	}
	area := func(b Box) float64 { return (b.X2 - b.X1) * (b.Y2 - b.Y1) }
	for i := 0; i < len(order); i++ {
		for j := i + 1; j < len(order); j++ {
			if area(boxes[order[j]]) > area(boxes[order[i]]) {
				order[i], order[j] = order[j], order[i]
			}
		}
	}

	var kept []Box
	for _, idx := range order {
		b := boxes[idx]
		redundant := false
		for _, k := range kept {
			if boxIoU(b, k) > iouThresh {
				redundant = true
				break
			}
		}
		if !redundant {
			kept = append(kept, b)
		}
	}
	return kept
}

func joinChunks(parts []string) string {
	out := parts[0]
	for _, p := range parts[1:] {
		out += " " + p
	}
	return out
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- ported from dbnet.go ----

const (
	dbnetThresh      = 0.2
	dbnetMinSide     = 2
	dbnetUnclipRatio = 1.5
	dbnetMinAvgScore = 0.45
)

func decodeDBNet(raw []float32) []Box {
	if len(raw) != dbnetMapSize*dbnetMapSize {
		return nil
	}
	size := dbnetMapSize

	mask := make([]bool, size*size)
	for i, v := range raw {
		mask[i] = v > dbnetThresh
	}

	dilated := dilateRect(mask, size, size, 21, 3)
	boxes := connectedComponentBoxes(dilated, size, size)

	var out []Box
	for _, b := range boxes {
		bw := float64(b.X2 - b.X1)
		bh := float64(b.Y2 - b.Y1)
		if bw < dbnetMinSide || bh < dbnetMinSide {
			continue
		}
		score := blobAvgScore(raw, mask, size, b)
		if score < dbnetMinAvgScore {
			continue
		}
		area := bw * bh
		perimeter := 2 * (bw + bh)
		if perimeter == 0 {
			continue
		}
		d := area * dbnetUnclipRatio / perimeter

		x1 := float64(b.X1) - d
		y1 := float64(b.Y1) - d
		x2 := float64(b.X2) + d
		y2 := float64(b.Y2) + d
		if x1 < 0 {
			x1 = 0
		}
		if y1 < 0 {
			y1 = 0
		}
		if x2 > float64(size) {
			x2 = float64(size)
		}
		if y2 > float64(size) {
			y2 = float64(size)
		}
		out = append(out, Box{X1: x1, Y1: y1, X2: x2, Y2: y2})
	}
	return out
}

func blobAvgScore(raw []float32, mask []bool, size int, b intBox) float64 {
	var sum float64
	var n int
	for y := b.Y1; y < b.Y2; y++ {
		row := y * size
		for x := b.X1; x < b.X2; x++ {
			i := row + x
			if mask[i] {
				sum += float64(raw[i])
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

func dilateRect(mask []bool, w, h, kw, kh int) []bool {
	halfW, halfH := kw/2, kh/2
	tmp := make([]bool, w*h)
	for y := 0; y < h; y++ {
		row := y * w
		for x := 0; x < w; x++ {
			set := false
			for dx := -halfW; dx <= halfW && !set; dx++ {
				xx := x + dx
				if xx < 0 || xx >= w {
					continue
				}
				if mask[row+xx] {
					set = true
				}
			}
			tmp[row+x] = set
		}
	}
	out := make([]bool, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			set := false
			for dy := -halfH; dy <= halfH && !set; dy++ {
				yy := y + dy
				if yy < 0 || yy >= h {
					continue
				}
				if tmp[yy*w+x] {
					set = true
				}
			}
			out[y*w+x] = set
		}
	}
	return out
}

type intBox struct{ X1, Y1, X2, Y2 int }

func connectedComponentBoxes(mask []bool, w, h int) []intBox {
	visited := make([]bool, w*h)
	var boxes []intBox
	queue := make([]int, 0, 1024)

	for start := 0; start < w*h; start++ {
		if !mask[start] || visited[start] {
			continue
		}
		visited[start] = true
		queue = queue[:0]
		queue = append(queue, start)
		x1, y1 := start%w, start/w
		x2, y2 := x1, y1

		for len(queue) > 0 {
			p := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			px, py := p%w, p/w
			if px < x1 {
				x1 = px
			}
			if px > x2 {
				x2 = px
			}
			if py < y1 {
				y1 = py
			}
			if py > y2 {
				y2 = py
			}
			for dy := -1; dy <= 1; dy++ {
				ny := py + dy
				if ny < 0 || ny >= h {
					continue
				}
				for dx := -1; dx <= 1; dx++ {
					nx := px + dx
					if nx < 0 || nx >= w || (dx == 0 && dy == 0) {
						continue
					}
					np := ny*w + nx
					if mask[np] && !visited[np] {
						visited[np] = true
						queue = append(queue, np)
					}
				}
			}
		}
		boxes = append(boxes, intBox{X1: x1, Y1: y1, X2: x2 + 1, Y2: y2 + 1})
	}
	return boxes
}

// ---- ported from svtr.go ----

//go:embed cyrillic_dict.txt
var svtrDictRaw string

func loadSVTRDict() []string {
	lines := strings.Split(strings.TrimRight(svtrDictRaw, "\n"), "\n")
	chars := make([]string, len(lines))
	for i, l := range lines {
		chars[i] = strings.TrimRight(l, "\r")
	}
	return chars
}

const (
	svtrHeight     = 48
	svtrWidth      = 320
	svtrTimeSteps  = 40
	svtrNumClasses = 165
)

func preprocessSVTRCrop(crop *rgbImage) []float32 {
	w, h := crop.W, crop.H
	resizedW := svtrWidth
	if h > 0 {
		rw := int(math.Ceil(float64(svtrHeight) * float64(w) / float64(h)))
		if rw < svtrWidth {
			resizedW = rw
		}
	}
	if resizedW < 1 {
		resizedW = 1
	}
	resized := crop.resize(resizedW, svtrHeight)

	canvas := newRGBImage(svtrWidth, svtrHeight)
	for y := 0; y < svtrHeight; y++ {
		var lastPixel [3]uint8
		for x := 0; x < svtrWidth; x++ {
			var px [3]uint8
			if x < resizedW {
				srcOff := (y*resizedW + x) * 3
				px = [3]uint8{resized.Pix[srcOff], resized.Pix[srcOff+1], resized.Pix[srcOff+2]}
				lastPixel = px
			} else {
				px = lastPixel
			}
			dstOff := (y*svtrWidth + x) * 3
			canvas.Pix[dstOff] = px[0]
			canvas.Pix[dstOff+1] = px[1]
			canvas.Pix[dstOff+2] = px[2]
		}
	}
	return canvas.toNCHWFloat()
}

func ctcGreedyDecodeSVTR(logits []float32, dict []string) (string, float64) {
	if len(logits) != svtrTimeSteps*svtrNumClasses {
		return "", 0
	}
	var text []rune
	var confSum float64
	var confCount int
	prevIdx := -1
	for t := 0; t < svtrTimeSteps; t++ {
		row := logits[t*svtrNumClasses : (t+1)*svtrNumClasses]
		best, bestV := 0, row[0]
		for c := 1; c < svtrNumClasses; c++ {
			if row[c] > bestV {
				best, bestV = c, row[c]
			}
		}
		isBlank := best == 0 || best > len(dict)
		if !isBlank && best != prevIdx {
			text = append(text, []rune(dict[best-1])...)
			confSum += float64(bestV)
			confCount++
		}
		prevIdx = best
	}
	avgConf := 0.0
	if confCount > 0 {
		avgConf = confSum / float64(confCount)
	}
	return string(text), avgConf
}

// ---- ported from labels.go ----

const (
	labelMaxLineGap = 45.0
	labelMaxHOffset = 70.0
	nearIconGap     = 80.0
)

func associateLabels(icons []Icon, texts []TextRegion) {
	if len(icons) == 0 || len(texts) == 0 {
		return
	}
	claimed := make([]bool, len(texts))

	type candidate struct {
		iconIdx int
		textIdx int
		dist    float64
	}
	var insideCandidates []candidate
	for i, icon := range icons {
		for j, t := range texts {
			if overlapsH(icon.Bbox, t.Bbox) && overlapsV(icon.Bbox, t.Bbox) {
				insideCandidates = append(insideCandidates, candidate{i, j, hCenterDist(icon.Bbox, t.Bbox)})
			}
		}
	}
	sort.Slice(insideCandidates, func(a, b int) bool { return insideCandidates[a].dist < insideCandidates[b].dist })
	insideLabel := make(map[int]int)
	for _, c := range insideCandidates {
		if claimed[c.textIdx] {
			continue
		}
		if _, have := insideLabel[c.iconIdx]; have {
			continue
		}
		insideLabel[c.iconIdx] = c.textIdx
		claimed[c.textIdx] = true
	}
	for iconIdx, textIdx := range insideLabel {
		icons[iconIdx].Label = texts[textIdx].Text
	}

	for i := range icons {
		if icons[i].Label != "" {
			continue
		}
		cursor := icons[i].Bbox
		var lines []string
		for {
			bestIdx := -1
			bestDist := labelMaxLineGap + 1
			for j, t := range texts {
				if claimed[j] {
					continue
				}
				if t.Bbox.Y1 < cursor.Y2 {
					continue
				}
				gap := t.Bbox.Y1 - cursor.Y2
				if gap > labelMaxLineGap {
					continue
				}
				if hCenterDist(icons[i].Bbox, t.Bbox) > labelMaxHOffset {
					continue
				}
				if gap < bestDist {
					bestDist = gap
					bestIdx = j
				}
			}
			if bestIdx < 0 {
				break
			}
			claimed[bestIdx] = true
			lines = append(lines, texts[bestIdx].Text)
			cursor = texts[bestIdx].Bbox
			if len(lines) >= 3 {
				break
			}
		}
		if len(lines) > 0 {
			icons[i].Label = joinChunks(lines)
		}
	}
}

func overlapsH(a, b Box) bool { return a.X1 < b.X2 && a.X2 > b.X1 }
func overlapsV(a, b Box) bool { return a.Y1 < b.Y2 && a.Y2 > b.Y1 }

func hCenterDist(a, b Box) float64 {
	ac := (a.X1 + a.X2) / 2
	bc := (b.X1 + b.X2) / 2
	if ac > bc {
		return ac - bc
	}
	return bc - ac
}

func filterBoxesNearIcons(icons []Icon, boxes []Box) []Box {
	if len(icons) == 0 {
		return boxes
	}
	var out []Box
	for _, b := range boxes {
		for _, icon := range icons {
			expanded := Box{
				X1: icon.Bbox.X1 - nearIconGap,
				Y1: icon.Bbox.Y1 - nearIconGap,
				X2: icon.Bbox.X2 + nearIconGap,
				Y2: icon.Bbox.Y2 + nearIconGap,
			}
			if overlapsH(b, expanded) && overlapsV(b, expanded) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}

// ---- ported from density.go ----

const (
	zoomHintMaxIconSide  = 56.0
	zoomHintGapRatio     = 0.6
	zoomHintMinCluster   = 3
	zoomHintPadding      = 12.0
	zoomHintMaxHintCount = 6
)

func findZoomHints(icons []Icon) []Box {
	type node struct {
		box     Box
		visited bool
	}
	var candidates []node
	for _, ic := range icons {
		w := ic.Bbox.X2 - ic.Bbox.X1
		h := ic.Bbox.Y2 - ic.Bbox.Y1
		if w <= 0 || h <= 0 || w > zoomHintMaxIconSide || h > zoomHintMaxIconSide {
			continue
		}
		candidates = append(candidates, node{box: ic.Bbox})
	}
	if len(candidates) < zoomHintMinCluster {
		return nil
	}

	packed := func(a, b Box) bool {
		gapX := gapBetween(a.X1, a.X2, b.X1, b.X2)
		gapY := gapBetween(a.Y1, a.Y2, b.Y1, b.Y2)
		if gapX < 0 {
			gapX = 0
		}
		if gapY < 0 {
			gapY = 0
		}
		avgSize := ((a.X2 - a.X1) + (a.Y2 - a.Y1) + (b.X2 - b.X1) + (b.Y2 - b.Y1)) / 4
		if avgSize <= 0 {
			return false
		}
		return (gapX <= avgSize*zoomHintGapRatio && gapY <= avgSize*2) ||
			(gapY <= avgSize*zoomHintGapRatio && gapX <= avgSize*2)
	}

	var hints []Box
	for i := range candidates {
		if candidates[i].visited {
			continue
		}
		queue := []int{i}
		candidates[i].visited = true
		var cluster []Box
		for len(queue) > 0 {
			cur := queue[len(queue)-1]
			queue = queue[:len(queue)-1]
			cluster = append(cluster, candidates[cur].box)
			for j := range candidates {
				if candidates[j].visited {
					continue
				}
				if packed(candidates[cur].box, candidates[j].box) {
					candidates[j].visited = true
					queue = append(queue, j)
				}
			}
		}
		if len(cluster) < zoomHintMinCluster {
			continue
		}
		x1, y1, x2, y2 := cluster[0].X1, cluster[0].Y1, cluster[0].X2, cluster[0].Y2
		for _, b := range cluster[1:] {
			x1 = math.Min(x1, b.X1)
			y1 = math.Min(y1, b.Y1)
			x2 = math.Max(x2, b.X2)
			y2 = math.Max(y2, b.Y2)
		}
		hints = append(hints, Box{X1: x1 - zoomHintPadding, Y1: y1 - zoomHintPadding, X2: x2 + zoomHintPadding, Y2: y2 + zoomHintPadding})
		if len(hints) >= zoomHintMaxHintCount {
			break
		}
	}
	return hints
}

func gapBetween(a1, a2, b1, b2 float64) float64 {
	if b1 > a2 {
		return b1 - a2
	}
	if a1 > b2 {
		return a1 - b2
	}
	return -1
}

// ---- ported from marks.go ----

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

// drawResultWasm mirrors the native build's draw.go's drawResult -- builds
// Parse's marked PNG (see local_ui_intercept.go's tryLocalUIParse, which
// returns it to the MCP caller).
func drawResultWasm(src *rgbImage, result *Result) []byte {
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

package localui

import (
	"fmt"
	"sync"
)

const iconInputSize = 640

// Parser is the client-side (ONNX Runtime) counterpart to the device's
// ui_parser.Parser -- same three-model pipeline, same output JSON shape,
// run locally instead of over the network+NPU. See package doc comment.
type Parser struct {
	mu    sync.Mutex
	icon  *session
	dbnet *session
	svtr  *session
	dict  []string
	gpu   bool // whether icon/dbnet actually ended up on the OpenVINO GPU EP
}

// Config describes where to find the ONNX models and the ONNX Runtime
// shared library, and whether to try the OpenVINO GPU execution provider.
type Config struct {
	IconONNXPath  string
	DBNetONNXPath string
	SVTRONNXPath  string
	SharedLibPath string // path to libonnxruntime.so; "" uses the system default search path
	UseGPU        bool   // try OpenVINO GPU EP for icon_detect+dbnet (SVTR stays CPU -- see NewParser)
}

// NewParser loads all three ONNX models. icon_detect and dbnet are the
// expensive, GPU-worthwhile passes (see runtime.go's benchmarked
// rationale); SVTR runs dozens of times per call on tiny 48x320 crops
// where OpenVINO's GPU dispatch overhead measured slower than plain CPU,
// so it's always CPU regardless of Config.UseGPU.
func NewParser(cfg Config) (*Parser, error) {
	if err := initRuntime(cfg.SharedLibPath); err != nil {
		return nil, fmt.Errorf("init onnxruntime: %w", err)
	}

	icon, err := newSession(cfg.IconONNXPath, []int64{1, 3, iconInputSize, iconInputSize}, []int64{1, 5, 8400}, cfg.UseGPU)
	if err != nil {
		return nil, fmt.Errorf("load icon_detect: %w", err)
	}
	dbnet, err := newSession(cfg.DBNetONNXPath, []int64{1, 3, dbnetMapSize, dbnetMapSize}, []int64{1, 1, dbnetMapSize, dbnetMapSize}, cfg.UseGPU)
	if err != nil {
		icon.Close()
		return nil, fmt.Errorf("load paddle_dbnet: %w", err)
	}
	svtr, err := newSession(cfg.SVTRONNXPath, []int64{1, 3, svtrHeight, svtrWidth}, []int64{1, svtrTimeSteps, svtrNumClasses}, false)
	if err != nil {
		icon.Close()
		dbnet.Close()
		return nil, fmt.Errorf("load paddle_svtr: %w", err)
	}

	return &Parser{icon: icon, dbnet: dbnet, svtr: svtr, dict: loadSVTRDict(), gpu: cfg.UseGPU}, nil
}

func (p *Parser) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.icon.Close()
	p.dbnet.Close()
	p.svtr.Close()
}

// Parse runs the full local pipeline on a PNG-encoded screenshot (as
// produced by the device's screen.get_image) and returns the annotated PNG
// plus the structured result, in the same shape as the device's own
// ui.parse response (see types.go).
func (p *Parser) Parse(imgBytes []byte) (markedPNG []byte, result *Result, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	original, err := decodeToRGB(imgBytes)
	if err != nil {
		return nil, nil, fmt.Errorf("decode image: %w", err)
	}
	if original.W == 0 || original.H == 0 {
		return nil, nil, fmt.Errorf("decode image: empty result")
	}

	res := &Result{ImageWidth: original.W, ImageHeight: original.H, Backend: backendLabel(p.gpu)}

	// -- Icons --
	clahe := applyCLAHE(original)
	claheLB, iconLBMeta := letterboxRGB(clahe, iconInputSize)
	iconInput := claheLB.toNCHWFloat()

	iconOut, err := p.icon.run(iconInput)
	if err != nil {
		return nil, nil, fmt.Errorf("icon_detect inference: %w", err)
	}
	for _, icon := range decodeYOLO(iconOut) {
		icon.Bbox = iconLBMeta.toOriginal(icon.Bbox)
		res.Icons = append(res.Icons, icon)
	}

	// -- Text (tiled DBNet, see tile.go) --
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

	for _, boxOrig := range textBoxes {
		text, conf, ok := p.recognizeBox(original, boxOrig)
		if !ok || text == "" {
			continue
		}
		res.Text = append(res.Text, TextRegion{Bbox: boxOrig, Text: text, Confidence: conf})
	}

	associateLabels(res.Icons, res.Text)
	assignMarkIDs(res.Icons, res.Text)

	markedPNG = drawResult(original, res)
	return markedPNG, res, nil
}

func backendLabel(gpu bool) string {
	if gpu {
		return "local-onnx-openvino-gpu"
	}
	return "local-onnx-cpu"
}

func (p *Parser) detectTextInTile(original *rgbImage, t rect) ([]Box, error) {
	tileImg, dbLBMeta := prepareDBNetTile(original, t, dbnetMapSize)
	dbInput := tileImg.toNCHWFloat()

	dbOut, err := p.dbnet.run(dbInput)
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

const maxSVTRAspect = 6.0

func (p *Parser) recognizeBox(original *rgbImage, box Box) (string, float64, bool) {
	w := box.X2 - box.X1
	h := box.Y2 - box.Y1
	if h <= 0 || w/h <= maxSVTRAspect {
		return p.recognizeCrop(original, box)
	}

	chunkW := maxSVTRAspect * h
	overlap := chunkW * 0.15
	n := int(w/(chunkW-overlap)) + 1

	var parts []string
	var confSum float64
	var confCount int
	for i := 0; i < n; i++ {
		x1 := box.X1 + float64(i)*(chunkW-overlap)
		x2 := x1 + chunkW
		if x2 > box.X2 {
			x2 = box.X2
		}
		if x2-x1 < h {
			continue
		}
		text, conf, ok := p.recognizeCrop(original, Box{X1: x1, Y1: box.Y1, X2: x2, Y2: box.Y2})
		if !ok || text == "" {
			continue
		}
		parts = append(parts, text)
		confSum += conf
		confCount++
	}
	if confCount == 0 {
		return "", 0, false
	}
	return joinChunks(parts), confSum / float64(confCount), true
}

func (p *Parser) recognizeCrop(original *rgbImage, box Box) (string, float64, bool) {
	crop, ok := safeCrop(original, box)
	if !ok {
		return "", 0, false
	}
	svtrInput := preprocessSVTRCrop(crop)
	svtrOut, err := p.svtr.run(svtrInput)
	if err != nil {
		return "", 0, false
	}
	text, conf := ctcGreedyDecodeSVTR(svtrOut, p.dict)
	return text, conf, true
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

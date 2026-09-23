//go:build js && wasm

// Package localui's real implementation (every other file in this package)
// is built around github.com/yalue/onnxruntime_go, a cgo binding with no
// wasm build at all ("build constraints exclude all Go files") -- and a
// local ONNX inference accelerator makes no sense for a browser tab anyway
// (there is no local screen-capture/heavy-hardware story to accelerate
// here; ui.parse just keeps forwarding to the device, same as any other
// platform where local ui.parse isn't set up, and the AI-Vision live
// overlay in internal/service/ai_vision.go stays permanently disabled since
// GetLocalUIParser() never has anything to return). This stub mirrors the
// real package's exported surface field-for-field/signature-for-signature
// (verified against types.go/parser.go/draw.go/onnx.go) so every consumer
// across internal/api and internal/service compiles unchanged under wasm,
// with NewParser always failing so InitLocalUIParseFromConfig's existing
// "optional accelerator, never a hard dependency" fallback (log and keep
// forwarding) kicks in automatically.
package localui

import (
	"fmt"
	"image"
)

// Box mirrors the real package's Box exactly.
type Box struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// Icon mirrors the real package's Icon exactly.
type Icon struct {
	ID         string  `json:"id"`
	Bbox       Box     `json:"bbox"`
	Confidence float64 `json:"confidence"`
	Label      string  `json:"label,omitempty"`
}

// TextRegion mirrors the real package's TextRegion exactly.
type TextRegion struct {
	ID         string  `json:"id"`
	Bbox       Box     `json:"bbox"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

// Result mirrors the real package's Result exactly -- ai_vision.go reads
// Icons/Text directly (not just json.Marshal), so field names/types have to
// match, not just be opaque.
type Result struct {
	Icons       []Icon       `json:"ui_elements"`
	Text        []TextRegion `json:"text"`
	ImageWidth  int          `json:"image_width"`
	ImageHeight int          `json:"image_height"`
	Backend     string       `json:"_backend,omitempty"`
}

// Config mirrors the real package's Config -- same field names so
// local_ui_init.go's struct literal compiles unchanged.
type Config struct {
	IconONNXPath  string
	DBNetONNXPath string
	SVTRONNXPath  string
	SharedLibPath string
	UseGPU        bool
}

// Parser mirrors the real package's Parser opaquely; NewParser never
// actually produces one on this platform, so none of its methods are ever
// reached at runtime -- they only need to type-check.
type Parser struct{}

// DefaultRuntimeLibName returns "" -- there is no ONNX Runtime shared
// library to resolve a name for under wasm.
func DefaultRuntimeLibName() string { return "" }

// errNotSupported is returned by every entry point that would otherwise
// touch ONNX Runtime.
var errNotSupported = fmt.Errorf("local ui.parse is not supported in the browser build")

// NewParser always fails: local ui.parse acceleration is not available in
// the browser build.
func NewParser(cfg Config) (*Parser, error) { return nil, errNotSupported }

// Close is a no-op; Parser is never actually constructed on this platform.
func (p *Parser) Close() {}

// Parse/ParseFast/ParseFastNearIcons/ParseFastNearIconsStaged/ParseStaged/
// ParseIconsOnly are all unreachable (NewParser always errors first) but
// must exist, with the real package's exact signatures, for
// internal/api/local_ui_intercept.go and internal/service/ai_vision.go's
// call sites to compile.
func (p *Parser) Parse(imgBytes []byte) (markedPNG []byte, result *Result, err error) {
	return nil, nil, errNotSupported
}

func (p *Parser) ParseFast(imgBytes []byte) (result *Result, err error) {
	return nil, errNotSupported
}

func (p *Parser) ParseFastNearIcons(imgBytes []byte) (result *Result, err error) {
	return nil, errNotSupported
}

func (p *Parser) ParseFastNearIconsStaged(imgBytes []byte, onTextBoxes func(boxes []Box)) (result *Result, err error) {
	return nil, errNotSupported
}

func (p *Parser) ParseStaged(imgBytes []byte, onIcons func(icons []Icon)) (result *Result, err error) {
	return nil, errNotSupported
}

func (p *Parser) ParseIconsOnly(imgBytes []byte) (icons []Icon, err error) {
	return nil, errNotSupported
}

// DrawDetectionBox/DrawDetectionTag are unreachable under wasm (nothing
// ever produces a non-nil *Result to draw), but ai_vision.go's
// buildAIVisionOverlayImage/drawCachedOverlay call them unconditionally, so
// they need real (no-op) bodies rather than not existing at all.
func DrawDetectionBox(img *image.RGBA, box Box, isText bool) {}
func DrawDetectionTag(img *image.RGBA, id string, box Box)   {}

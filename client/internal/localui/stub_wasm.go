//go:build js && wasm

// Package localui's real implementation (every other file in this package)
// is built around github.com/yalue/onnxruntime_go, a cgo binding with no
// wasm build at all ("build constraints exclude all Go files"). This file
// mirrors that real package's exported surface field-for-field/
// signature-for-signature (verified against types.go/parser.go/draw.go/
// onnx.go) so every consumer across internal/api and internal/service
// compiles unchanged under wasm -- just the plain data types Parser,
// NewParser, and every Parse* method (including the OCR/dbnet+svtr stage)
// all have real bodies here too, in parser_wasm.go, backed by
// onnxruntime-web running in the browser tab itself (client/web/
// ai_vision.js) instead of onnxruntime_go's cgo binding.
package localui

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
	ZoomHints   []Box        `json:"zoom_hints,omitempty"`
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

// DefaultRuntimeLibName returns "" -- there is no ONNX Runtime shared
// library to resolve a name for under wasm (onnxruntime-web ships its own
// WASM/WebGPU runtime, fetched by client/web/ai_vision.js, not loaded as a
// named shared library the way onnxruntime_go's cgo binding does).
func DefaultRuntimeLibName() string { return "" }

// Close releases nothing -- there's no session handle/native resource on
// this platform to release; the underlying onnxruntime-web sessions
// (ai_vision.js) are process-lifetime for as long as the tab is open.
func (p *Parser) Close() {}

// Parser, NewParser, every Parse* method, and DrawDetectionBox/
// DrawDetectionTag (real bodies -- ai_vision.go's buildAIVisionOverlayImage/
// drawCachedOverlay call these unconditionally regardless of platform) all
// live in parser_wasm.go.

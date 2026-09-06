// Package localui is the client-side ("heavy hardware") mirror of the
// device's modules/ui_parser Go package: the same Set-of-Mark UI-parsing
// pipeline (YOLOv8 icon/element detector + PaddleOCR-style DBNet text
// detector + SVTR text recognizer), but run locally via ONNX Runtime
// instead of on the device's RK3566 NPU.
//
// Why this exists: at 1920x1080 the device's ui.parse call tiles DBNet into
// 6 native-resolution passes on a single-core ~0.5-1 TOPS NPU, taking
// ~20s end to end (see mcp_proxy.go's mcpProxyTimeout doc comment). The
// same three models, exported straight from their original PaddleOCR/
// ultralytics sources to ONNX and run on this machine's CPU or Intel iGPU
// (OpenVINO execution provider), complete the full pipeline in well under
// 2s -- see runtime.go's provider selection.
//
// Types here deliberately mirror usbridge/modules/ui_parser/types.go's JSON
// shape field-for-field, so the MCP proxy's interceptor (see
// api/mcp_proxy.go) can return a localui.Result in place of the device's
// own ui.parse response without an MCP client ever seeing a difference.
package localui

// Box is a pixel-space bounding box in the original (un-letterboxed) image.
type Box struct {
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
	X2 float64 `json:"x2"`
	Y2 float64 `json:"y2"`
}

// Icon is one YOLOv8 UI-element detection -- see
// usbridge/modules/ui_parser/types.go's Icon for the full field-by-field
// rationale (ID/Label semantics are identical here).
type Icon struct {
	ID         string  `json:"id"`
	Bbox       Box     `json:"bbox"`
	Confidence float64 `json:"confidence"`
	Label      string  `json:"label,omitempty"`
}

// TextRegion is one DBNet-detected + SVTR-recognized text box.
type TextRegion struct {
	ID         string  `json:"id"`
	Bbox       Box     `json:"bbox"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
}

// Result is the full parse output, same JSON shape as the device's
// ui_parser.Result.
type Result struct {
	Icons       []Icon       `json:"ui_elements"`
	Text        []TextRegion `json:"text"`
	ImageWidth  int          `json:"image_width"`
	ImageHeight int          `json:"image_height"`
	// Backend records where this result came from, purely informational --
	// not present on the device's own response, added so a caller/log can
	// tell a locally-computed ui.parse apart from a device one. The MCP
	// proxy interceptor only ever adds this field; it never expects one back.
	Backend string `json:"_backend,omitempty"`

	// ZoomHints -- see usbridge/modules/ui_parser/types.go's Result.ZoomHints
	// and density.go's findZoomHints. Ported so a caller sees identical
	// zoom_hints behavior whether the device or this local offload
	// answered ui.parse; the client never intercepts the ui.zoom tool
	// itself (it always forwards to the device, which is the only place
	// that can actually re-crop+re-detect a live screen region).
	ZoomHints []Box `json:"zoom_hints,omitempty"`
}

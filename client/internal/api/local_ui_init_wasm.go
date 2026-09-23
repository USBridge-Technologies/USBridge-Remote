//go:build js && wasm

package api

// init overrides local_ui_init.go's localUIDefaultModelDir: the native
// resolveLocalUIPath chain it defaults to (os.Executable()/os.Stat against
// an installed app bundle) means nothing under wasm, where the "model
// directory" is really a relative web path that ai_vision.js's
// ensureSession fetch()es -- see stub_wasm.go's real Parser implementation,
// which joins this with "icon_detect.onnx" into the URL it hands
// window.usbridgeAIVision.loadModel. Matches scripts/build_web.sh's
// web/models/icon_detect.onnx build output location.
func init() {
	localUIDefaultModelDir = func() string { return "models" }
}

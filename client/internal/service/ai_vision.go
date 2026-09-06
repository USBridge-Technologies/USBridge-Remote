package service

import (
	"bytes"
	"image"
	"image/png"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	usbapi "usbridge-client/internal/api"
	"usbridge-client/internal/localui"
)

// AI Vision is an optional live overlay, off by default: when turned on
// from the video settings popup (next to resolution/bitrate), it burns
// the same Set-of-Mark detection an agent's ui.parse MCP call would get --
// red/green boxes around UI elements and text, each tagged with its hex
// id -- directly into the live video frame, right before that frame
// reaches the native Vulkan/Metal/GL renderer. The operator sees exactly
// what an agent sees and how it would address each element, overlaid on
// the moving picture instead of a frozen screenshot.
//
// It reuses internal/localui's client-side ONNX pipeline (the same one
// backing the local ui.parse MCP offload, see internal/api/local_ui_init.go)
// rather than shipping a second copy of the models. That pipeline runs in
// the neighborhood of a second or more per frame -- nowhere close to video
// frame rate -- so detection does NOT run every frame: aiVisionInterval
// paces how often a fresh pass is kicked off, and every frame in between
// keeps drawing the most recently completed result. Disabled, the whole
// feature costs one atomic load per frame (see ApplyAIVisionOverlay).
const aiVisionInterval = 2 * time.Second

var (
	aiVisionEnabled atomic.Bool
	aiVisionBusy    atomic.Bool
	aiVisionLastRun atomic.Int64 // UnixNano of the last detection *kickoff*

	aiVisionMu     sync.RWMutex
	aiVisionResult *localui.Result
)

// SetAIVisionEnabled turns the live detection overlay on or off. Wired to
// the "AI Vision" checkbox in the video settings popup (see
// gui/view/video_start_dialog.go) -- takes effect immediately, independent
// of the Start/Apply button, since it only affects local rendering and
// touches nothing on the device. Disabling drops the cached result right
// away so a stale overlay never lingers after the checkbox is unticked.
func SetAIVisionEnabled(enabled bool) {
	wasEnabled := aiVisionEnabled.Swap(enabled)
	if !enabled {
		aiVisionMu.Lock()
		aiVisionResult = nil
		aiVisionMu.Unlock()
	}
	if wasEnabled != enabled {
		logrus.Infof("🔎 [AI Vision] %s", map[bool]string{true: "enabled", false: "disabled"}[enabled])
	}
}

// AIVisionEnabled reports the live overlay checkbox's current state.
func AIVisionEnabled() bool {
	return aiVisionEnabled.Load()
}

// ApplyAIVisionOverlay is called once per decoded frame from the native
// decode path (see moonlight_cgo_linux.go / moonlight_cgo_apple.go's
// deliver_frame, via the goAIVisionOverlay cgo export in
// moonlight_cgo_wrapper.go) on the tightly-packed RGBA buffer about to be
// handed to the GPU. It sits on the hot path, so the disabled case -- the
// default -- must stay a single atomic load; everything else here (kicking
// off a detection pass, drawing boxes) only runs while the checkbox is on.
func ApplyAIVisionOverlay(rgba []byte, w, h, stride int) {
	if !aiVisionEnabled.Load() {
		return
	}
	maybeKickDetection(rgba, w, h, stride)
	drawCachedOverlay(rgba, w, h, stride)
}

// maybeKickDetection copies the current frame and hands it to the local
// ui.parse detector on a background goroutine, at most once per
// aiVisionInterval and never while a previous pass is still running (a
// slow CPU can take longer than the interval -- in that case we simply
// keep showing the last completed result instead of piling up goroutines
// or falling behind on GPU-owned memory).
func maybeKickDetection(rgba []byte, w, h, stride int) {
	now := time.Now().UnixNano()
	if now-aiVisionLastRun.Load() < int64(aiVisionInterval) {
		return
	}
	if !aiVisionBusy.CompareAndSwap(false, true) {
		return
	}
	aiVisionLastRun.Store(now)

	parser := usbapi.GetLocalUIParser()
	if parser == nil {
		aiVisionBusy.Store(false)
		logrus.Warn("🔎 [AI Vision] enabled but the local ui.parse models aren't loaded (Settings ▸ Local ui.parse offload) -- nothing to overlay yet")
		return
	}

	// frame takes its own copy of the pixels: rgba is only valid for the
	// duration of this call (the C caller frees/reuses it right after).
	frame := snapshotRGBA(rgba, w, h, stride)

	go func() {
		defer aiVisionBusy.Store(false)
		var buf bytes.Buffer
		if err := png.Encode(&buf, frame); err != nil {
			logrus.Warnf("🔎 [AI Vision] frame encode failed: %v", err)
			return
		}
		_, result, err := parser.Parse(buf.Bytes())
		if err != nil {
			logrus.Warnf("🔎 [AI Vision] detection pass failed: %v", err)
			return
		}
		aiVisionMu.Lock()
		aiVisionResult = result
		aiVisionMu.Unlock()
	}()
}

// snapshotRGBA copies a possibly stride-padded RGBA buffer into a
// standalone *image.RGBA that the encoder and background goroutine can
// hold onto safely past this call.
func snapshotRGBA(rgba []byte, w, h, stride int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rowBytes := w * 4
	for y := 0; y < h; y++ {
		srcOff := y * stride
		dstOff := y * img.Stride
		if srcOff+rowBytes > len(rgba) {
			break
		}
		copy(img.Pix[dstOff:dstOff+rowBytes], rgba[srcOff:srcOff+rowBytes])
	}
	return img
}

// drawCachedOverlay burns the most recently completed detection's boxes
// and Set-of-Mark hex tags directly into the live RGBA buffer, in place,
// by wrapping it as an *image.RGBA with zero copy (image.RGBA is just a
// {Pix []byte, Stride int, Rect} view) and reusing localui's own drawing
// code, so the live overlay renders pixel-identical to a ui.parse
// annotated screenshot.
func drawCachedOverlay(rgba []byte, w, h, stride int) {
	aiVisionMu.RLock()
	result := aiVisionResult
	aiVisionMu.RUnlock()
	if result == nil {
		return
	}

	img := &image.RGBA{Pix: rgba, Stride: stride, Rect: image.Rect(0, 0, w, h)}
	for _, icon := range result.Icons {
		localui.DrawDetectionBox(img, icon.Bbox, false)
		localui.DrawDetectionTag(img, icon.ID, icon.Bbox)
	}
	for _, t := range result.Text {
		localui.DrawDetectionBox(img, t.Bbox, true)
		localui.DrawDetectionTag(img, t.ID, t.Bbox)
	}
}

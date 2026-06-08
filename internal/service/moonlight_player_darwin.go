//go:build darwin

package service

import (
	"image"
	"os"

	"github.com/sirupsen/logrus"
)

// usesVideoToolbox reports that Darwin uses VideoToolbox for H.264 decode
// instead of a GStreamer subprocess. Used by moonlight_service.go to route
// the "stream stopped" notification through the CGO onStop callback.
func usesVideoToolbox() bool { return true }

// startMoonlightGStreamer on Darwin registers the VideoToolbox frame callback
// and returns immediately — no subprocess is launched.
//
// The VT decode path is driven by vt_dr_submit in moonlight_cgo.go:
//   dr_submit → VTDecompressionSession → vt_callback → goVTFrame → vtFrameCallback → onFrame
//
// pipeRead is closed immediately because VT writes directly to the Go callback;
// the pipe created by moonlight_service.go is not used for video on this platform.
func startMoonlightGStreamer(
	pipeRead *os.File,
	width, height int,
	stopCh <-chan struct{},
	onFrame func(image.Image),
	onStop func(error),
) error {
	_ = pipeRead.Close()

	vtFrameCallbackMu.Lock()
	vtFrameCallback = onFrame
	vtFrameCallbackMu.Unlock()

	logrus.Infof("🍎 [Moonlight/VT] VideoToolbox decoder active — %dx%d, no GStreamer subprocess", width, height)
	return nil
}

// startMoonlightAudio on Darwin is a no-op: audio is handled directly by
// CoreAudio AudioQueue inside ar_decode (moonlight_cgo.go, #ifdef __APPLE__).
// The AudioQueue is created in ar_init when moonlight-common-c sets up the audio
// stream, and disposed in ar_cleanup when the stream ends — no subprocess needed.
func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	_ = pipeRead.Close()
	logrus.Info("🔊 [Moonlight/Audio] CoreAudio path — AudioQueue started natively in ar_init (no GStreamer subprocess)")
	return nil
}

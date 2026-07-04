//go:build ios

package service

import (
	"image"
	"os"

	"github.com/sirupsen/logrus"
)

// usesVideoToolbox reports true — iOS uses VideoToolbox for H.264 decode.
func usesVideoToolbox() bool { return true }

// startMoonlightGStreamer on iOS registers the VideoToolbox frame callback.
// VT decode runs inside platform_dr_submit (moonlight_cgo_apple.go):
//
//	dr_submit → VTDecompressionSession → vt_callback → goVTFrame → vtFrameCallback
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

	logrus.Infof("🍎 [Moonlight/VT] iOS VideoToolbox decoder active — %dx%d", width, height)
	return nil
}

// startMoonlightAudio on iOS is a no-op: audio is handled directly by
// CoreAudio AudioQueue in platform_ar_decode (moonlight_cgo_apple.go).
func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	_ = pipeRead.Close()
	logrus.Info("🔊 [Moonlight/Audio] iOS CoreAudio path — AudioQueue started natively in platform_ar_init")
	return nil
}

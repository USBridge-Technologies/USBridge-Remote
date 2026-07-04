//go:build android

package service

import (
	"image"
	"os"

	"github.com/sirupsen/logrus"
)

// usesVideoToolbox reports true — Android uses AMediaCodec hardware decoder
// which delivers frames via the same goVTFrame → vtFrameCallback path.
func usesVideoToolbox() bool { return true }

// startMoonlightGStreamer on Android registers the AMediaCodec frame callback.
// AMediaCodec decode runs inside dr_submit (moonlight_cgo_android.go):
//
//	dr_submit → AMediaCodec → goVTFrame → vtFrameCallback
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

	logrus.Infof("🤖 [Moonlight/Video] Android AMediaCodec decoder active — %dx%d", width, height)
	return nil
}

// startMoonlightAudio on Android is a no-op: audio is handled by
// AAudio in ar_decode (moonlight_cgo_android.go).
func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	_ = pipeRead.Close()
	logrus.Info("🔊 [Moonlight/Audio] Android AAudio path — low-latency native output")
	return nil
}

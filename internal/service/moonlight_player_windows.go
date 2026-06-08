//go:build windows

package service

import (
	"image"
	"os"

	"github.com/sirupsen/logrus"
)

// usesVideoToolbox reports true — Windows uses libavcodec with D3D11VA hardware
// acceleration which delivers frames via the same goVTFrame → vtFrameCallback path.
func usesVideoToolbox() bool { return true }

// startMoonlightGStreamer on Windows registers the libavcodec frame callback.
// D3D11VA decode runs inside dr_submit (moonlight_cgo_windows.go):
//   dr_submit → libavcodec (D3D11VA) → goVTFrame → vtFrameCallback
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

	logrus.Infof("🪟 [Moonlight/Video] Windows libavcodec D3D11VA decoder active — %dx%d", width, height)
	return nil
}

// startMoonlightAudio on Windows is a no-op: audio is handled directly by
// WASAPI in ar_decode (moonlight_cgo_windows.go).
func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	_ = pipeRead.Close()
	logrus.Info("🔊 [Moonlight/Audio] Windows WASAPI path — PCM written natively in ar_decode")
	return nil
}

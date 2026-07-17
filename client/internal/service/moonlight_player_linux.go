//go:build linux && !android

package service

import (
	"image"
	"os"

	"github.com/sirupsen/logrus"
)

// usesVideoToolbox reports true — Linux uses the libavcodec hardware decoder
// which delivers frames via the same goVTFrame → vtFrameCallback path as macOS.
func usesVideoToolbox() bool { return true }

// startMoonlightVideoDecoder on Linux registers the libavcodec frame callback
// and returns immediately — no subprocess is launched.
// libavcodec (VA-API / NVDEC / software fallback) runs inside the moonlight-common-c
// dr_submit callback thread, delivering RGBA frames directly via goVTFrame.
func startMoonlightVideoDecoder(
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

	logrus.Infof("🐧 [Moonlight/Video] libavcodec decoder active — %dx%d (VA-API/NVDEC/SW)", width, height)
	return nil
}

// startMoonlightAudio on Linux is a no-op: audio is handled directly by
// ALSA in platform_ar_decode (moonlight_cgo_linux.go).
// The ALSA device is opened in platform_ar_init when moonlight-common-c
// sets up the audio stream, and closed in platform_ar_cleanup when it ends.
func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	_ = pipeRead.Close()
	logrus.Info("🔊 [Moonlight/Audio] ALSA path — PCM written natively in platform_ar_decode (no subprocess)")
	return nil
}

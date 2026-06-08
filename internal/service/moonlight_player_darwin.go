//go:build darwin

package service

import (
	"bufio"
	"fmt"
	"image"
	"os"
	"os/exec"
	"syscall"

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

func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	gstPath, err := findDarwinGStreamerTool("gst-launch-1.0")
	if err != nil || gstPath == "" {
		return fmt.Errorf("gst-launch-1.0 not found (%v) — install via: brew install gstreamer gst-plugins-good", err)
	}

	args := []string{
		"-q",
		"fdsrc", "fd=3",
		"!", "audio/x-raw,format=S16LE,rate=48000,channels=2,layout=interleaved",
		"!", "audioconvert",
		"!", "autoaudiosink", "sync=false",
	}

	cmd := exec.Command(gstPath, args...)
	cmd.Env = (&GStreamerService{}).getGStreamerEnv()
	cmd.ExtraFiles = []*os.File{pipeRead} // → fd=3

	stderr, err2 := cmd.StderrPipe()
	if err2 != nil {
		return fmt.Errorf("audio stderr pipe: %v", err2)
	}

	if err2 := cmd.Start(); err2 != nil {
		return fmt.Errorf("audio gst-launch-1.0 start: %v", err2)
	}
	_ = pipeRead.Close()

	logrus.Infof("🔊 [Moonlight/Audio] started PID=%d — S16LE 48kHz stereo → autoaudiosink", cmd.Process.Pid)

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			logrus.Warnf("🔊 [Moonlight/Audio stderr] %s", sc.Text())
		}
	}()

	go func() {
		<-stopCh
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}()

	go func() {
		err := cmd.Wait()
		logrus.Infof("🔊 [Moonlight/Audio] process exited: %v", err)
		if onStop != nil {
			onStop(err)
		}
	}()

	return nil
}

//go:build linux && !android

package service

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/sirupsen/logrus"
)

func startMoonlightAudio(
	pipeRead *os.File,
	stopCh <-chan struct{},
	onStop func(error),
) error {
	gstPath := (&GStreamerService{}).findLinuxGStreamerTool("gst-launch-1.0")
	if gstPath == "" {
		return fmt.Errorf("gst-launch-1.0 not found — install gstreamer1.0-tools gstreamer1.0-plugins-good")
	}

	args := []string{
		"-q",
		"fdsrc", "fd=3",
		"!", "audio/x-raw,format=S16LE,rate=48000,channels=2,layout=interleaved",
		"!", "audioconvert",
		"!", "autoaudiosink", "sync=false",
	}

	cmd := exec.Command(gstPath, args...)
	cmd.ExtraFiles = []*os.File{pipeRead} // → fd=3

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("audio stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("audio gst-launch-1.0 start: %v", err)
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

func startMoonlightGStreamer(
	pipeRead *os.File,
	width, height int,
	stopCh <-chan struct{},
	onFrame func(image.Image),
	onStop func(error),
) error {
	gstPath := (&GStreamerService{}).findLinuxGStreamerTool("gst-launch-1.0")
	if gstPath == "" {
		return fmt.Errorf("gst-launch-1.0 not found — install via: sudo apt install gstreamer1.0-tools gstreamer1.0-libav gstreamer1.0-plugins-good")
	}

	// fdsrc reads H.264 Annex-B from fd=3 (ExtraFiles[0]).
	// avdec_h264 is software decode from gst-libav, available everywhere.
	args := []string{
		"-q",
		"fdsrc", "fd=3",
		"!", "h264parse", "config-interval=-1",
		"!", "video/x-h264,stream-format=avc,alignment=au",
		"!", "avdec_h264",
		"!", "videoscale",
		"!", fmt.Sprintf("video/x-raw,width=%d,height=%d", width, height),
		"!", "videoconvert",
		"!", "video/x-raw,format=RGBA",
		"!", "fdsink", "fd=1", "sync=false",
	}

	logrus.Infof("🌕 [Moonlight/GStreamer] pipeline: gst-launch-1.0 %v", args)

	cmd := exec.Command(gstPath, args...)
	cmd.ExtraFiles = []*os.File{pipeRead} // → fd=3 in child (stdin=0,stdout=1,stderr=2,ExtraFiles[0]=3)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("stdout pipe: %v", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("stderr pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gst-launch-1.0 start: %v", err)
	}
	// Child inherited fd via ExtraFiles; close the parent's copy so EOF propagates when pipeWrite closes.
	_ = pipeRead.Close()

	logrus.Infof("🌕 [Moonlight/GStreamer] started PID=%d %dx%d — waiting for avdec_h264 frames on stdout", cmd.Process.Pid, width, height)

	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			logrus.Warnf("🌕 [Moonlight/GStreamer stderr] %s", sc.Text())
		}
	}()

	go func() {
		<-stopCh
		logrus.Info("🌕 [Moonlight/GStreamer] stop signal — SIGTERM")
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}()

	var pool sync.Pool
	go func() {
		frameSize := width * height * 4
		reader := bufio.NewReaderSize(stdout, frameSize*2)
		var finalErr error
		frameCount := 0

		for {
			select {
			case <-stopCh:
				goto done
			default:
			}

			img := framePoolGet(&pool, width, height)
			_, err := io.ReadFull(reader, img.Pix)
			if err != nil {
				framePoolPut(&pool, img)
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					logrus.Errorf("🌕 [Moonlight/GStreamer] frame read error after %d frames: %v", frameCount, err)
					finalErr = fmt.Errorf("frame read: %v", err)
				} else {
					logrus.Infof("🌕 [Moonlight/GStreamer] stream ended (EOF) after %d frames", frameCount)
				}
				goto done
			}

			frameCount++
			if frameCount == 1 {
				logrus.Info("🌕 [Moonlight/GStreamer] ✅ first RGBA frame received from avdec_h264 — video is flowing!")
			}

			if onFrame != nil {
				onFrame(img)
			} else {
				framePoolPut(&pool, img)
			}
		}
	done:
		_ = cmd.Process.Signal(syscall.SIGKILL)
		_ = cmd.Wait()
		if onStop != nil {
			onStop(finalErr)
		}
	}()

	return nil
}

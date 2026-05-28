//go:build darwin

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

// startMoonlightGStreamer launches a gst-launch-1.0 fdsrc pipeline that reads
// raw Annex-B H.264 from pipeRead (written by LiStartConnection's submitDecodeUnit
// callback), decodes with vtdec, and calls onFrame with each decoded RGBA image.
// Returns immediately; pipeline runs in goroutines.
// onStop is called once when the pipeline exits (nil = clean, err = problem).
func startMoonlightGStreamer(
	pipeRead *os.File,
	width, height int,
	stopCh <-chan struct{},
	onFrame func(image.Image),
	onStop func(error),
) error {
	gstPath, err := findDarwinGStreamerTool("gst-launch-1.0")
	if err != nil || gstPath == "" {
		return fmt.Errorf("gst-launch-1.0 not found (%v) — install via: brew install gstreamer gst-plugins-good gst-plugins-bad", err)
	}

	// fdsrc reads from fd=3 (ExtraFiles[0] is the pipe read end).
	// h264parse → vtdec → videoscale → videoconvert → fdsink writes RGBA to stdout.
	args := []string{
		"-q",
		"fdsrc", "fd=3",
		"!", "h264parse", "config-interval=-1",
		"!", "video/x-h264,stream-format=avc,alignment=au",
		"!", "vtdec",
		"!", "videoscale",
		"!", fmt.Sprintf("video/x-raw,width=%d,height=%d", width, height),
		"!", "videoconvert",
		"!", "video/x-raw,format=RGBA",
		"!", "fdsink", "fd=1", "sync=false",
	}

	logrus.Infof("🌕 [Moonlight/GStreamer] pipeline: gst-launch-1.0 %v", args)

	cmd := exec.Command(gstPath, args...)
	cmd.Env = (&GStreamerService{}).getGStreamerEnv()
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

	logrus.Infof("🌕 [Moonlight/GStreamer] started PID=%d %dx%d — waiting for vtdec frames on stdout", cmd.Process.Pid, width, height)

	// Forward stderr — vtdec errors, pipeline warnings, negotiation failures all appear here.
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			logrus.Warnf("🌕 [Moonlight/GStreamer stderr] %s", sc.Text())
		}
	}()

	// Stop on explicit disconnect signal.
	go func() {
		<-stopCh
		logrus.Info("🌕 [Moonlight/GStreamer] stop signal — SIGTERM")
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}()

	// Read decoded RGBA frames from stdout and deliver them.
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
				logrus.Info("🌕 [Moonlight/GStreamer] ✅ first RGBA frame received from vtdec — video is flowing!")
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

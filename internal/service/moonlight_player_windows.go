//go:build windows

package service

import (
	"fmt"
	"image"
	"io"
	"os"
	"time"

	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
	"github.com/sirupsen/logrus"
)

// usesVideoToolbox reports false — Windows uses GStreamer for H.264 decode.
func usesVideoToolbox() bool { return false }

func startMoonlightAudio(pipeRead *os.File, stopCh <-chan struct{}, onStop func(error)) error {
	_ = pipeRead.Close()
	logrus.Warn("🔊 [Moonlight/Audio] audio not yet implemented on Windows")
	return nil
}

func startMoonlightGStreamer(
	pipeRead *os.File,
	width, height int,
	stopCh <-chan struct{},
	onFrame func(image.Image),
	onStop func(error),
) error {
	gst.Init(nil)

	// Try hardware decode first, fall back to software.
	// appsrc receives raw Annex-B H.264 from the LiStartConnection pipe.
	// appsink delivers decoded RGBA frames to the onFrame callback.
	hwPipeline := fmt.Sprintf(
		"appsrc name=src format=bytes ! h264parse config-interval=-1 ! "+
			"video/x-h264,stream-format=avc,alignment=au ! d3d11h264dec ! d3d11download ! "+
			"videoscale ! video/x-raw,width=%d,height=%d ! "+
			"videoconvert ! video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		width, height,
	)
	swPipeline := fmt.Sprintf(
		"appsrc name=src format=bytes ! h264parse config-interval=-1 ! "+
			"video/x-h264,stream-format=avc,alignment=au ! avdec_h264 ! "+
			"videoscale ! video/x-raw,width=%d,height=%d ! "+
			"videoconvert ! video/x-raw,format=RGBA ! "+
			"appsink name=sink sync=false max-buffers=1 drop=true",
		width, height,
	)

	var pipeline *gst.Pipeline
	var pipelineStr string

	if pl, err := gst.NewPipelineFromString(hwPipeline); err == nil {
		// Test if hardware decoder can enter PLAYING.
		if err2 := pl.SetState(gst.StatePlaying); err2 == nil {
			pl.SetState(gst.StateNull)
			pipeline = pl
			pipelineStr = "d3d11h264dec (hardware)"
		} else {
			pl.SetState(gst.StateNull)
		}
	}

	if pipeline == nil {
		pl, err := gst.NewPipelineFromString(swPipeline)
		if err != nil {
			return fmt.Errorf("create GStreamer pipeline: %v", err)
		}
		pipeline = pl
		pipelineStr = "avdec_h264 (software)"
	}

	logrus.Infof("🌕 [Moonlight/GStreamer] Windows pipeline: %s %dx%d", pipelineStr, width, height)

	// Get appsrc.
	srcElem, err := pipeline.GetElementByName("src")
	if err != nil {
		return fmt.Errorf("get appsrc element: %v", err)
	}
	src := app.SrcFromElement(srcElem)

	// Get appsink and attach frame callback.
	sinkElem, err := pipeline.GetElementByName("sink")
	if err != nil {
		return fmt.Errorf("get appsink element: %v", err)
	}
	sink := app.SinkFromElement(sinkElem)

	frameCount := 0
	frameSize := width * height * 4

	sink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
			sample := sink.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}
			buf := sample.GetBuffer()
			if buf == nil {
				return gst.FlowOK
			}
			mapInfo := buf.Map(gst.MapRead)
			if mapInfo == nil {
				return gst.FlowOK
			}
			data := mapInfo.Bytes()
			var img *image.RGBA
			if len(data) >= frameSize {
				img = image.NewRGBA(image.Rect(0, 0, width, height))
				copy(img.Pix, data[:frameSize])
			}
			buf.Unmap()

			frameCount++
			if frameCount == 1 {
				logrus.Infof("🌕 [Moonlight/GStreamer] ✅ first RGBA frame received (%s) — video is flowing!", pipelineStr)
			}
			if img != nil && onFrame != nil {
				onFrame(img)
			}
			return gst.FlowOK
		},
	})

	if err := pipeline.SetState(gst.StatePlaying); err != nil {
		return fmt.Errorf("start pipeline: %v", err)
	}

	// Goroutine: read H.264 Annex-B from pipe and push to appsrc.
	// EOF on pipe means LiStartConnection stopped — send EOS to pipeline.
	// pipeRead ownership is taken here; close when the loop exits.
	go func() {
		defer pipeRead.Close()
		buf := make([]byte, 65536)
		for {
			n, err := pipeRead.Read(buf)
			if n > 0 {
				gstBuf := gst.NewBufferFromBytes(buf[:n])
				src.PushBuffer(gstBuf)
			}
			if err != nil {
				if err != io.EOF {
					logrus.Errorf("🌕 [Moonlight/GStreamer] pipe read error: %v", err)
				}
				src.EndStream()
				return
			}
		}
	}()

	// Goroutine: forward stop signal.
	go func() {
		<-stopCh
		logrus.Info("🌕 [Moonlight/GStreamer] stop signal — sending EOS")
		src.EndStream()
	}()

	// Goroutine: wait for EOS/error on the pipeline bus, then clean up.
	go func() {
		var finalErr error
		bus := pipeline.GetBus()
		for {
			msg := bus.TimedPop(time.Second)
			if msg == nil {
				select {
				case <-stopCh:
					goto done
				default:
					continue
				}
			}
			switch msg.Type() {
			case gst.MessageEOS:
				logrus.Infof("🌕 [Moonlight/GStreamer] EOS after %d frames", frameCount)
				goto done
			case gst.MessageError:
				gerr := msg.ParseError()
				logrus.Errorf("🌕 [Moonlight/GStreamer] pipeline error: %v", gerr)
				finalErr = fmt.Errorf("pipeline error: %v", gerr)
				goto done
			}
		}
	done:
		pipeline.SetState(gst.StateNull)
		if onStop != nil {
			onStop(finalErr)
		}
	}()

	return nil
}

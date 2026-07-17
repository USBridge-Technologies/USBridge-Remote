//go:build !android && !ios && !windows
// +build !android,!ios,!windows

// GStreamer-based desktop QR camera capture, used on macOS/Linux. Windows
// uses Media Foundation directly instead (qr_camera_scanner_windows.go) so
// the Windows build doesn't need to bundle GStreamer at all.

package controller

import (
	"fmt"
	"image"
	"runtime"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/sirupsen/logrus"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// QRCameraScanner scans QR codes from a live desktop camera feed.
type QRCameraScanner struct {
	parent    fyne.Window
	qrScanner *QRScanner

	pipeline *gst.Pipeline
	appsink  *app.Sink
	stopChan chan struct{}

	popup *widget.PopUp
}

// macOS uses avfvideosrc to trigger the native camera permission flow
// reliably. Other non-Windows desktop targets rely on autovideosrc.
func getCameraPipelineStr() string {
	if runtime.GOOS == "darwin" {
		return "avfvideosrc ! videoconvert ! video/x-raw,format=RGBA,width=640,height=480 ! appsink name=sink sync=false max-buffers=2 drop=true"
	}
	return "autovideosrc ! videoconvert ! video/x-raw,format=RGBA,width=640,height=480 ! appsink name=sink sync=false max-buffers=2 drop=true"
}

func newQRCameraScanner(parent fyne.Window, qs *QRScanner) (*QRCameraScanner, error) {
	gst.Init(nil)

	pipelineStr := getCameraPipelineStr()
	logrus.Infof("QR camera pipeline: %s", pipelineStr)
	pipeline, err := gst.NewPipelineFromString(pipelineStr)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipeline: %w", err)
	}

	sinkEl, err := pipeline.GetElementByName("sink")
	if err != nil {
		pipeline.SetState(gst.StateNull)
		return nil, fmt.Errorf("appsink: %w", err)
	}

	return &QRCameraScanner{
		parent:    parent,
		qrScanner: qs,
		pipeline:  pipeline,
		appsink:   app.SinkFromElement(sinkEl),
		stopChan:  make(chan struct{}),
	}, nil
}

func (q *QRCameraScanner) Run() {
	videoImg := canvas.NewImageFromImage(nil)
	videoImg.FillMode = canvas.ImageFillContain
	videoImg.ScaleMode = canvas.ImageScaleSmooth
	videoImg.SetMinSize(fyne.NewSize(320, 240))

	var closeUI func()
	closeScanner := func() {
		q.Stop()
		if closeUI != nil {
			closeUI()
		}
	}

	popup := showEmbeddedQRScannerPopup(q.parent, videoImg, closeScanner)
	q.popup = popup
	closeUI = func() {
		popup.Hide()
		q.popup = nil
	}
	popup.Show()

	qrReader := qrcode.NewQRCodeReader()
	firstFrameLogged := false

	q.appsink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
			defer func() {
				if recovered := recover(); recovered != nil {
					logrus.Errorf("QR scanner sample callback panic: %v", recovered)
				}
			}()

			select {
			case <-q.stopChan:
				return gst.FlowEOS
			default:
			}

			sample := sink.PullSample()
			if sample == nil {
				return gst.FlowEOS
			}

			img := q.processSample(sample)
			if img != nil {
				if !firstFrameLogged {
					firstFrameLogged = true
					logrus.Info("QR camera: first frame received")
				}
				fyne.Do(func() {
					videoImg.Image = img
					videoImg.Refresh()
				})

				if contents, ok := decodeQRImage(qrReader, img); ok {
					logrus.Infof("QR detected: %s", contents)

					fyne.Do(func() {
						q.Stop()
						if closeUI != nil {
							closeUI()
						}
						q.qrScanner.parseAndApply(contents, q.parent)
					})
					return gst.FlowEOS
				}
			}
			return gst.FlowOK
		},
	})

	go func() {
		logrus.Info("Starting QR camera pipeline")
		if err := q.pipeline.SetState(gst.StatePlaying); err != nil {
			logrus.Errorf("Error starting camera: %v", err)
			fyne.Do(func() {
				if closeUI != nil {
					closeUI()
				}
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorStartingCamera, err), q.parent)
			})
		}
	}()
}

func (q *QRCameraScanner) Stop() {
	select {
	case <-q.stopChan:
		return
	default:
		close(q.stopChan)
	}

	if q.pipeline != nil {
		q.pipeline.SetState(gst.StateNull)
		q.pipeline = nil
	}

	if q.popup != nil {
		q.popup.Hide()
		q.popup = nil
	}
}

func (q *QRCameraScanner) processSample(sample *gst.Sample) image.Image {
	buffer := sample.GetBuffer()
	if buffer == nil {
		return nil
	}

	caps := sample.GetCaps()
	if caps == nil {
		return nil
	}

	structure := caps.GetStructureAt(0)
	width, _ := structure.GetValue("width")
	height, _ := structure.GetValue("height")
	if width == nil || height == nil {
		logrus.Warn("QR scanner sample missing width/height in caps")
		return nil
	}

	w, ok := numericToInt(width)
	if !ok {
		logrus.Warnf("QR scanner width has unsupported type: %T", width)
		return nil
	}
	h, ok := numericToInt(height)
	if !ok {
		logrus.Warnf("QR scanner height has unsupported type: %T", height)
		return nil
	}

	mapInfo := buffer.Map(gst.MapRead)
	if mapInfo == nil {
		logrus.Warn("QR scanner failed to map sample buffer")
		return nil
	}

	data := mapInfo.Bytes()
	expectedSize := w * h * 4
	if len(data) < expectedSize {
		buffer.Unmap()
		return nil
	}

	dataCopy := make([]byte, expectedSize)
	copy(dataCopy, data[:expectedSize])
	buffer.Unmap()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, dataCopy)
	return img
}

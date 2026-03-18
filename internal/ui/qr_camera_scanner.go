//go:build !android
// +build !android

package ui

import (
	"fmt"
	"image"
	"runtime"

	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/sirupsen/logrus"
	"github.com/tinyzimmer/go-gst/gst"
	"github.com/tinyzimmer/go-gst/gst/app"
)

// QRCameraScanner сканирует QR с камеры в реальном времени (desktop)
type QRCameraScanner struct {
	parent    fyne.Window
	qrScanner *QRScanner

	pipeline *gst.Pipeline
	appsink  *app.Sink
	stopChan chan struct{}
}

// getCameraPipelineStr возвращает pipeline для текущей платформы
// Windows: ksvideosrc — один вариант, работает и в MSYS2, и в dist от scripts/build_windows.sh (libgstwinks.dll)
// Linux/macOS: autovideosrc (v4l2/avf)
func getCameraPipelineStr() string {
	if runtime.GOOS == "windows" {
		return "ksvideosrc ! videoconvert ! video/x-raw,format=RGBA,width=640,height=480 ! queue max-size-buffers=2 leaky=downstream ! appsink name=sink sync=false max-buffers=2 drop=true"
	}
	return "autovideosrc ! videoconvert ! video/x-raw,format=RGBA,width=640,height=480 ! appsink name=sink sync=false max-buffers=2 drop=true"
}

// newQRCameraScanner создаёт сканер камеры
func newQRCameraScanner(parent fyne.Window, qs *QRScanner) (*QRCameraScanner, error) {
	gst.Init(nil)

	pipelineStr := getCameraPipelineStr()
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

// Run показывает окно камеры и сканирует до обнаружения QR или закрытия
func (q *QRCameraScanner) Run() {
	videoImg := canvas.NewImageFromImage(nil)
	videoImg.FillMode = canvas.ImageFillContain
	videoImg.SetMinSize(fyne.NewSize(640, 480))

	statusLabel := widget.NewLabel(i18n.Current.PointCameraAtQR)
	closeBtn := widget.NewButton(i18n.Current.Close, func() {
		q.Stop()
	})

	content := container.NewBorder(
		nil, nil, nil, nil,
		container.NewVBox(
			videoImg,
			statusLabel,
			closeBtn,
		),
	)

	scanWindow := fyne.CurrentApp().NewWindow(i18n.Current.QRScanning)
	scanWindow.SetContent(content)
	scanWindow.Resize(fyne.NewSize(680, 560))
	scanWindow.CenterOnScreen()

	qrReader := qrcode.NewQRCodeReader()

	q.appsink.SetCallbacks(&app.SinkCallbacks{
		NewSampleFunc: func(sink *app.Sink) gst.FlowReturn {
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
				fyne.Do(func() {
					videoImg.Image = img
					videoImg.Refresh()
				})

				// Сканируем QR
				bmp, err := gozxing.NewBinaryBitmapFromImage(img)
				if err != nil {
					return gst.FlowOK
				}

				result, err := qrReader.Decode(bmp, nil)
				if err == nil {
					contents := result.GetText()
					logrus.Infof("✅ QR detected: %s", contents)

					// ВАЖНО: Не вызывать q.Stop() из callback GStreamer - deadlock!
					// Вся очистка через fyne.Do на главном потоке
					fyne.Do(func() {
						q.Stop()
						scanWindow.Close()
						q.qrScanner.parseAndApply(contents, q.parent)
					})
					return gst.FlowEOS
				}
			}
			return gst.FlowOK
		},
	})

	scanWindow.SetOnClosed(func() {
		q.Stop()
	})

	scanWindow.Show()

	// На Windows SetState(Playing) может блокировать main thread при deadlock с dshow
	// Запускаем в goroutine — окно уже показано, UI не зависает
	go func() {
		if err := q.pipeline.SetState(gst.StatePlaying); err != nil {
			logrus.Errorf("Error starting camera: %v", err)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf(i18n.Current.ErrorStartingCamera, err), q.parent)
				scanWindow.Close()
			})
		}
	}()
}

// Stop останавливает сканер
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
}

// processSample конвертирует GStreamer sample в image.Image
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
		return nil
	}

	w := width.(int)
	h := height.(int)
	mapInfo := buffer.Map(gst.MapRead)
	if mapInfo == nil {
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

//go:build !android
// +build !android

package controller

import (
	"fmt"
	"image"
	"image/color"
	"runtime"
	"time"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing"
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

// Windows uses ksvideosrc. macOS uses avfvideosrc to trigger the native
// camera permission flow reliably. Other desktop targets rely on autovideosrc.
func getCameraPipelineStr() string {
	if runtime.GOOS == "windows" {
		return "ksvideosrc ! videoconvert ! video/x-raw,format=RGBA,width=640,height=480 ! queue max-size-buffers=2 leaky=downstream ! appsink name=sink sync=false max-buffers=2 drop=true"
	}
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

	if runtime.GOOS == "windows" {
		popup := q.showEmbeddedScanner(videoImg, closeScanner)
		q.popup = popup
		closeUI = func() {
			popup.Hide()
			q.popup = nil
		}
		popup.Show()
		q.watchPopupSize(popup)
	} else {
		content := container.NewBorder(
			nil, nil, nil, nil,
			container.NewVBox(
				videoImg,
				widget.NewLabel(i18n.Current.PointCameraAtQR),
				widget.NewButton(i18n.Current.Close, closeScanner),
			),
		)

		scanWindow := fyne.CurrentApp().NewWindow(i18n.Current.QRScanning)
		scanWindow.SetContent(content)
		scanWindow.Resize(fyne.NewSize(680, 560))
		scanWindow.CenterOnScreen()
		scanWindow.SetOnClosed(func() {
			q.Stop()
		})
		closeUI = func() {
			scanWindow.Close()
		}
		scanWindow.Show()
	}

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

				bmp, err := gozxing.NewBinaryBitmapFromImage(img)
				if err != nil {
					return gst.FlowOK
				}

				result, err := qrReader.Decode(bmp, nil)
				if err == nil {
					contents := result.GetText()
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
				dialog.ShowError(fmt.Errorf(i18n.Current.ErrorStartingCamera, err), q.parent)
			})
		}
	}()
}

func (q *QRCameraScanner) showEmbeddedScanner(videoImg *canvas.Image, onClose func()) *widget.PopUp {
	title := canvas.NewText("Scan device qr code", design.ColorTextLight)
	title.TextSize = 18
	title.TextStyle.Bold = true
	title.Alignment = fyne.TextAlignCenter

	closeBtn := view.NewHeaderActionButton(onClose)
	closeBtn.ApplySpec(view.HeaderActionButtonSpec{
		Fill:        color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10},
		Foreground:  design.ColorTextLight,
		Stroke:      color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x18},
		StrokeWidth: 1,
		Icon:        theme.WindowCloseIcon(),
	})

	header := container.New(&qrScannerHeaderLayout{}, title, closeBtn)

	videoBg := canvas.NewRectangle(color.NRGBA{R: 0x08, G: 0x08, B: 0x08, A: 0xf2})
	videoBg.CornerRadius = 14

	videoBorder := canvas.NewRectangle(color.Transparent)
	videoBorder.CornerRadius = 14
	videoBorder.StrokeColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x18}
	videoBorder.StrokeWidth = 1

	videoCard := container.NewStack(
		videoBg,
		view.NewInset(container.NewMax(videoImg), 14, 14, 14, 14),
		videoBorder,
	)

	cardBg := canvas.NewRectangle(color.NRGBA{R: 0x22, G: 0x22, B: 0x22, A: 0xe9})
	cardBg.CornerRadius = 18
	cardBg.StrokeColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x22}
	cardBg.StrokeWidth = 1

	card := container.NewStack(
		cardBg,
		container.New(&qrScannerCardLayout{}, header, videoCard),
	)

	dim := canvas.NewRectangle(color.NRGBA{R: 0x06, G: 0x08, B: 0x0b, A: 0xbb})
	content := container.New(&embeddedScannerOverlayLayout{}, dim, card)

	popup := widget.NewModalPopUp(content, q.parent.Canvas())
	popup.Resize(q.parent.Canvas().Size())
	return popup
}

type embeddedScannerOverlayLayout struct{}

func (l *embeddedScannerOverlayLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	dim := objects[0]
	panel := objects[1]
	dim.Move(fyne.NewPos(0, 0))
	dim.Resize(size)

	panelSize := qrScannerPanelSize(size)
	panel.Move(fyne.NewPos((size.Width-panelSize.Width)/2, (size.Height-panelSize.Height)/2))
	panel.Resize(panelSize)
}

func (l *embeddedScannerOverlayLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

type qrScannerCardLayout struct{}

func (l *qrScannerCardLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	header := objects[0]
	video := objects[1]

	padding := clampFloat32(minFloat32(size.Width, size.Height)*0.04, 18, 26)
	headerHeight := float32(40)
	headerWidth := size.Width - padding*2
	if headerWidth < 0 {
		headerWidth = 0
	}

	header.Move(fyne.NewPos(padding, padding))
	header.Resize(fyne.NewSize(headerWidth, headerHeight))

	videoTop := padding + headerHeight + 14
	availableWidth := size.Width - padding*2
	availableHeight := size.Height - videoTop - padding
	if availableWidth < 0 {
		availableWidth = 0
	}
	if availableHeight < 0 {
		availableHeight = 0
	}

	videoWidth := availableWidth
	videoHeight := videoWidth * 3 / 4
	if videoHeight > availableHeight {
		videoHeight = availableHeight
		videoWidth = videoHeight * 4 / 3
	}
	if videoWidth > availableWidth {
		videoWidth = availableWidth
		videoHeight = videoWidth * 3 / 4
	}
	if videoWidth < 0 {
		videoWidth = 0
	}
	if videoHeight < 0 {
		videoHeight = 0
	}

	video.Move(fyne.NewPos((size.Width-videoWidth)/2, videoTop+(availableHeight-videoHeight)/2))
	video.Resize(fyne.NewSize(videoWidth, videoHeight))
}

func (l *qrScannerCardLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(320, 280)
}

type qrScannerHeaderLayout struct{}

func (l *qrScannerHeaderLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	title := objects[0]
	closeBtn := objects[1]

	closeSize := fyne.NewSize(40, 40)
	closeX := size.Width - closeSize.Width
	if closeX < 0 {
		closeX = 0
	}
	closeBtn.Move(fyne.NewPos(closeX, 0))
	closeBtn.Resize(closeSize)

	title.Move(fyne.NewPos(0, 0))
	title.Resize(size)
}

func (l *qrScannerHeaderLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	titleMin := objects[0].MinSize()
	closeMin := objects[1].MinSize()
	height := maxFloat32(titleMin.Height, closeMin.Height)
	width := maxFloat32(titleMin.Width, closeMin.Width*2)
	return fyne.NewSize(width, height)
}

func qrScannerPanelSize(canvasSize fyne.Size) fyne.Size {
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 32)
	maxWidth := canvasSize.Width - margin*2
	maxHeight := canvasSize.Height - margin*2
	if maxWidth <= 0 {
		maxWidth = canvasSize.Width
	}
	if maxHeight <= 0 {
		maxHeight = canvasSize.Height
	}

	padding := clampFloat32(minFloat32(maxWidth, maxHeight)*0.04, 18, 26)
	headerHeight := float32(40)
	gap := float32(14)
	videoMaxWidth := maxFloat32(0, maxWidth-padding*2)
	videoMaxHeight := maxFloat32(0, maxHeight-padding*2-headerHeight-gap)

	videoWidth := minFloat32(680, videoMaxWidth)
	videoHeight := videoWidth * 3 / 4
	if videoHeight > videoMaxHeight {
		videoHeight = videoMaxHeight
		videoWidth = videoHeight * 4 / 3
	}
	if videoWidth > videoMaxWidth {
		videoWidth = videoMaxWidth
		videoHeight = videoWidth * 3 / 4
	}

	panelMinWidth := minFloat32(320, maxWidth)
	panelMinHeight := minFloat32(260, maxHeight)
	panelWidth := clampFloat32(videoWidth+padding*2, panelMinWidth, maxWidth)
	panelHeight := clampFloat32(padding*2+headerHeight+gap+videoHeight, panelMinHeight, maxHeight)
	return fyne.NewSize(panelWidth, panelHeight)
}

func (q *QRCameraScanner) watchPopupSize(popup *widget.PopUp) {
	go func() {
		lastSize := q.parent.Canvas().Size()
		for {
			select {
			case <-q.stopChan:
				return
			default:
			}

			currentSize := q.parent.Canvas().Size()
			if currentSize != lastSize {
				lastSize = currentSize
				fyne.Do(func() {
					if q.popup != popup || popup == nil {
						return
					}

					popup.Resize(currentSize)
				})
			}

			time.Sleep(120 * time.Millisecond)
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

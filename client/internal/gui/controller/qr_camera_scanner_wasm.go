//go:build js && wasm

// Browser-camera QR scanning via getUserMedia, mirroring the desktop
// platforms' V4L2/AVFoundation/Media-Foundation captures
// (qr_camera_scanner_linux.go et al.): same QRCameraScanner shape, same
// showEmbeddedQRScannerPopup/decodeQRImage helpers from
// qr_camera_scanner_ui.go, same qrScanner.parseAndApply hand-off — only the
// actual frame source differs, since there's no cgo under GOOS=js. Frames
// come from an offscreen <video>+<canvas> pair driven via syscall/js
// instead of a platform capture API.
package controller

import (
	"fmt"
	"image"
	"syscall/js"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/sirupsen/logrus"
)

// qrCameraFrameInterval trades scan latency for CPU: decoding every frame
// at 30-60fps is unnecessary for a QR code that isn't moving; the desktop
// captures scan every frame because V4L2/AVFoundation delivery is already
// throttled by the driver, but a <video> element has no such natural
// backpressure under wasm, so this polls explicitly instead.
const qrCameraFrameInterval = 150 * time.Millisecond

// QRCameraScanner scans QR codes from the browser's camera via
// getUserMedia.
type QRCameraScanner struct {
	parent    fyne.Window
	qrScanner *QRScanner

	videoEl  js.Value
	canvasEl js.Value
	ctx2d    js.Value
	stream   js.Value

	stopChan chan struct{}
	popup    *widget.PopUp
}

func newQRCameraScanner(parent fyne.Window, qs *QRScanner) (*QRCameraScanner, error) {
	mediaDevices := js.Global().Get("navigator").Get("mediaDevices")
	if mediaDevices.IsUndefined() || mediaDevices.Get("getUserMedia").IsUndefined() {
		return nil, fmt.Errorf("camera access (getUserMedia) is unavailable in this browser")
	}
	return &QRCameraScanner{
		parent:    parent,
		qrScanner: qs,
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

	go q.openAndCapture(videoImg, closeUI)
}

// openAndCapture requests camera access (a real permission prompt in the
// browser chrome — same UX every getUserMedia site shows), waits for the
// stream, then drives the same decode loop shape as the desktop captures.
func (q *QRCameraScanner) openAndCapture(videoImg *canvas.Image, closeUI func()) {
	constraints := js.Global().Get("Object").New()
	videoConstraints := js.Global().Get("Object").New()
	videoConstraints.Set("facingMode", "environment") // prefer the rear/world camera on phones; desktops only have one anyway
	constraints.Set("video", videoConstraints)
	constraints.Set("audio", false)

	mediaDevices := js.Global().Get("navigator").Get("mediaDevices")
	streamPromise := mediaDevices.Call("getUserMedia", constraints)
	stream, err := awaitJSPromise(streamPromise)
	if err != nil {
		logrus.Errorf("[QRCamera/wasm] getUserMedia failed: %v", err)
		select {
		case <-q.stopChan:
		default:
			fyne.Do(func() {
				if closeUI != nil {
					closeUI()
				}
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorStartingCamera, err), q.parent)
			})
		}
		return
	}
	q.stream = stream

	doc := js.Global().Get("document")
	video := doc.Call("createElement", "video")
	video.Set("autoplay", true)
	video.Set("muted", true)
	video.Set("playsInline", true)
	video.Set("srcObject", stream)
	q.videoEl = video

	// Wait for the video element to report real dimensions (loadedmetadata)
	// before sizing the capture canvas -- videoWidth/videoHeight read 0
	// until then.
	metadataCh := make(chan struct{})
	var onLoaded js.Func
	onLoaded = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer onLoaded.Release()
		close(metadataCh)
		return nil
	})
	video.Call("addEventListener", "loadedmetadata", onLoaded)
	select {
	case <-metadataCh:
	case <-time.After(10 * time.Second):
		logrus.Warn("[QRCamera/wasm] timed out waiting for camera metadata")
	case <-q.stopChan:
		return
	}

	width := video.Get("videoWidth").Int()
	height := video.Get("videoHeight").Int()
	if width == 0 || height == 0 {
		width, height = 640, 480
	}

	canvasEl := doc.Call("createElement", "canvas")
	canvasEl.Set("width", width)
	canvasEl.Set("height", height)
	q.canvasEl = canvasEl
	q.ctx2d = canvasEl.Call("getContext", "2d")

	logrus.Infof("[QRCamera/wasm] camera stream open: %dx%d", width, height)
	q.captureLoop(videoImg, width, height, closeUI)
}

func (q *QRCameraScanner) captureLoop(videoImg *canvas.Image, width, height int, closeUI func()) {
	qrReader := qrcode.NewQRCodeReader()
	ticker := time.NewTicker(qrCameraFrameInterval)
	defer ticker.Stop()

	for {
		select {
		case <-q.stopChan:
			return
		case <-ticker.C:
		}

		q.ctx2d.Call("drawImage", q.videoEl, 0, 0, width, height)
		imageData := q.ctx2d.Call("getImageData", 0, 0, width, height)
		jsPixels := imageData.Get("data") // Uint8ClampedArray, RGBA

		img := image.NewRGBA(image.Rect(0, 0, width, height))
		js.CopyBytesToGo(img.Pix, jsPixels)

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
			return
		}
	}
}

func (q *QRCameraScanner) Stop() {
	select {
	case <-q.stopChan:
		return
	default:
		close(q.stopChan)
	}

	if !q.stream.IsUndefined() && !q.stream.IsNull() {
		tracks := q.stream.Call("getTracks")
		length := tracks.Get("length").Int()
		for i := 0; i < length; i++ {
			tracks.Index(i).Call("stop")
		}
	}

	if q.popup != nil {
		q.popup.Hide()
		q.popup = nil
	}
}

// awaitJSPromise blocks the calling goroutine until a JS Promise settles.
// Small local twin of client/internal/webrtcweb's awaitPromise — kept
// separate rather than shared across packages for a ~20-line helper with no
// other coupling between the two.
func awaitJSPromise(promise js.Value) (js.Value, error) {
	resultCh := make(chan js.Value, 1)
	errCh := make(chan error, 1)

	var thenFunc, catchFunc js.Func
	thenFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer thenFunc.Release()
		defer catchFunc.Release()
		if len(args) > 0 {
			resultCh <- args[0]
		} else {
			resultCh <- js.Undefined()
		}
		return nil
	})
	catchFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer thenFunc.Release()
		defer catchFunc.Release()
		msg := "promise rejected"
		if len(args) > 0 {
			if name := args[0].Get("name"); !name.IsUndefined() {
				msg = name.String()
				if m := args[0].Get("message"); !m.IsUndefined() && m.String() != "" {
					msg += ": " + m.String()
				}
			}
		}
		errCh <- fmt.Errorf("%s", msg)
		return nil
	})
	promise.Call("then", thenFunc).Call("catch", catchFunc)

	select {
	case v := <-resultCh:
		return v, nil
	case err := <-errCh:
		return js.Value{}, err
	case <-time.After(60 * time.Second):
		return js.Value{}, fmt.Errorf("timed out waiting for JS promise (camera permission prompt not answered?)")
	}
}

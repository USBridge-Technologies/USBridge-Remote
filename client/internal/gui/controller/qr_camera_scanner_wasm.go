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
	// Without explicit resolution hints, some phone browsers hand back a
	// low-res, low-framerate profile by default (optimized for video calls,
	// not for reading a small printed pattern) -- ask for a real capture
	// resolution. "ideal" is a soft constraint: getUserMedia still succeeds
	// on hardware that can't hit it, just picks the closest it can.
	videoConstraints.Set("width", map[string]interface{}{"ideal": 1280})
	videoConstraints.Set("height", map[string]interface{}{"ideal": 720})
	// Continuous autofocus is a Chrome-only capability (advanced constraint,
	// silently ignored elsewhere) -- default camera behavior on many phones
	// otherwise locks focus at first frame, which is exactly wrong for a
	// handheld QR scan where distance keeps changing.
	videoConstraints.Set("advanced", []interface{}{
		map[string]interface{}{"focusMode": "continuous"},
	})
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
	// Off-screen but still attached to the document and laid out (1x1,
	// clipped) rather than display:none -- confirmed live: a <video> that's
	// never inserted into the DOM (or is display:none) never actually
	// decodes frames in Chrome/Firefox even though srcObject/play() report
	// success and the browser's own getUserMedia permission-prompt preview
	// shows a live picture (that preview is the browser chrome's own
	// element, unrelated to this one). videoWidth/videoHeight and drawImage
	// only start producing real pixel data once the element is part of a
	// rendered (if invisible) layout.
	style := video.Get("style")
	style.Set("position", "fixed")
	style.Set("left", "-9999px")
	style.Set("width", "1px")
	style.Set("height", "1px")
	video.Set("srcObject", stream)
	doc.Get("body").Call("appendChild", video)
	q.videoEl = video
	playPromise := video.Call("play")
	if !playPromise.IsUndefined() {
		playPromise.Call("catch", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if len(args) > 0 {
				logrus.Warnf("[QRCamera/wasm] video.play() rejected: %v", args[0])
			}
			return nil
		}))
	}

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

	srcWidth := video.Get("videoWidth").Int()
	srcHeight := video.Get("videoHeight").Int()
	if srcWidth == 0 || srcHeight == 0 {
		srcWidth, srcHeight = 1280, 720
	}

	// Draw into a downscaled capture canvas rather than the source
	// resolution: confirmed live on Android Chrome that piping a full
	// 1280x720 frame through getImageData -> CopyBytesToGo -> a new
	// image.NewRGBA on every tick is what actually made the preview feel
	// laggy (that's ~3.6MB copied and re-allocated per frame), not the
	// camera itself. A QR code held at arm's length doesn't need native
	// resolution to decode -- <canvas>'s own drawImage does the downscale
	// on the GPU/native side for free, which is far cheaper than reading
	// back full-res pixels into Go just to throw most of them away.
	width, height := qrCameraCaptureDimensions(srcWidth, srcHeight)

	canvasEl := doc.Call("createElement", "canvas")
	canvasEl.Set("width", width)
	canvasEl.Set("height", height)
	q.canvasEl = canvasEl
	q.ctx2d = canvasEl.Call("getContext", "2d")

	logrus.Infof("[QRCamera/wasm] camera stream open: source=%dx%d capture=%dx%d", srcWidth, srcHeight, width, height)
	q.captureLoop(videoImg, width, height, closeUI)
}

// qrCameraCaptureDimensions downscales to a capped width while preserving
// aspect ratio. Capped at the getUserMedia "ideal" request above (720p) --
// this only ever kicks in if the camera handed back something even
// higher-res than requested, so in practice it's a no-op on most hardware;
// the real cost saving here is skipping the decode on 2 out of 3 ticks
// below, not downscaling pixels the QR decoder actually needs. Confirmed
// live: without an explicit width/height getUserMedia constraint at all,
// phone browsers can negotiate a much lower-res, denoised/compressed
// video-call-oriented profile that's too soft at the pixel level for a
// printed QR code's fine modules to survive -- that's the actual root
// cause of "scans on native, doesn't scan on web", not this downscale.
func qrCameraCaptureDimensions(srcWidth, srcHeight int) (width, height int) {
	const maxWidth = 1280
	if srcWidth <= maxWidth {
		return srcWidth, srcHeight
	}
	scale := float64(maxWidth) / float64(srcWidth)
	return maxWidth, int(float64(srcHeight) * scale)
}

// qrCameraDecodeEveryNTicks: the QR decode itself (pattern-finding across
// the whole frame) costs meaningfully more than the drawImage+readback
// above, especially at the higher end of phone hardware variance --
// running it every tick was the other half of the "laggy" complaint.
// Refreshing the visible preview every tick still feels smooth (~6-7fps at
// qrCameraFrameInterval); decoding at a third of that rate is still fast
// enough that scanning a held-up code feels instant, without spending CPU
// on pattern-matching against near-duplicate consecutive frames.
const qrCameraDecodeEveryNTicks = 3

func (q *QRCameraScanner) captureLoop(videoImg *canvas.Image, width, height int, closeUI func()) {
	qrReader := qrcode.NewQRCodeReader()
	ticker := time.NewTicker(qrCameraFrameInterval)
	defer ticker.Stop()

	tickCount := 0
	for {
		select {
		case <-q.stopChan:
			return
		case <-ticker.C:
		}
		tickCount++

		q.ctx2d.Call("drawImage", q.videoEl, 0, 0, width, height)

		// Readback (getImageData -> Go pixels) happens every tick: Fyne's
		// canvas.Image needs real Go-side image.Image bytes to texture-
		// upload, there's no way to hand it the HTML canvas directly, so
		// the preview needs this regardless of whether this tick also
		// decodes. Only the zxing pattern-search below is skipped on most
		// ticks -- it's the more expensive of the two, and consecutive
		// frames from a held phone rarely differ enough to make decoding
		// every single one worth the extra CPU.
		imageData := q.ctx2d.Call("getImageData", 0, 0, width, height)
		jsPixels := imageData.Get("data") // Uint8ClampedArray, RGBA

		img := image.NewRGBA(image.Rect(0, 0, width, height))
		js.CopyBytesToGo(img.Pix, jsPixels)

		fyne.Do(func() {
			videoImg.Image = img
			videoImg.Refresh()
		})

		if tickCount%qrCameraDecodeEveryNTicks != 0 {
			continue
		}

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

	if !q.videoEl.IsUndefined() && !q.videoEl.IsNull() {
		q.videoEl.Set("srcObject", js.Null())
		if parent := q.videoEl.Get("parentNode"); !parent.IsNull() && !parent.IsUndefined() {
			parent.Call("removeChild", q.videoEl)
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

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

	videoEl js.Value
	stream  js.Value

	// previewCanvas/previewCtx are the cheap, small target the on-screen
	// preview is refreshed from every tick. decodeCanvas/decodeCtx are a
	// second, separate canvas drawn (and read back) only on the ticks that
	// actually run the zxing decode -- see the doc comments on
	// qrCameraPreviewMaxWidth/qrCameraDecodeMaxWidth for why these are two
	// different resolutions instead of one shared canvas.
	previewCanvas, decodeCanvas js.Value
	previewCtx, decodeCtx       js.Value

	// barcodeDetector/useNativeDetector: when the browser implements the
	// Shape Detection API's BarcodeDetector for qr_code (Chrome/Edge,
	// desktop and Android -- on Android specifically this is backed by
	// the same on-device ML Kit model the native Camera/Lens app uses,
	// real NPU/GPU-accelerated detection, not just "faster JS"), decode
	// ticks call it directly on the live <video> element instead of the
	// gozxing software path below -- see detectNative's docs. Firefox and
	// Safari implement neither as of this writing, so useNativeDetector
	// stays false there and every tick falls through to gozxing exactly
	// as before this existed.
	barcodeDetector   js.Value
	useNativeDetector bool

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
	// on hardware that can't hit it, just picks the closest it can. 1080p
	// (up from the previous 720p ask) because qrCameraDecodeMaxWidth below
	// no longer throws that extra resolution away before decoding -- a
	// printed QR code held at any real-world distance needs every module
	// (the little black/white squares) to still span multiple source
	// pixels after capture, and 720p was themselves the thing making
	// smaller/farther codes unreadable, independent of any of this file's
	// own downscaling.
	videoConstraints.Set("width", map[string]interface{}{"ideal": 1920})
	videoConstraints.Set("height", map[string]interface{}{"ideal": 1080})
	// Continuous autofocus is a Chrome-only capability (advanced constraint,
	// silently ignored elsewhere) -- default camera behavior on many phones
	// otherwise locks focus at first frame, which is exactly wrong for a
	// handheld QR scan where distance keeps changing. resizeMode:"none"
	// (also Chrome-only, also silently ignored elsewhere) opts out of the
	// browser's own additional software downscale/crop step some
	// implementations apply on top of the sensor's native output when
	// picking a resolution to hand back -- confirmed live as a real,
	// separate source of softness from anything this file does to the
	// frame afterwards.
	videoConstraints.Set("advanced", []interface{}{
		map[string]interface{}{"focusMode": "continuous"},
		map[string]interface{}{"resizeMode": "none"},
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
	applyContinuousFocus(stream)

	q.barcodeDetector, q.useNativeDetector = setUpNativeBarcodeDetector()
	if q.useNativeDetector {
		logrus.Info("[QRCamera/wasm] using native BarcodeDetector (hardware/OS-accelerated qr_code detection)")
	} else {
		logrus.Info("[QRCamera/wasm] native BarcodeDetector unavailable for qr_code -- using gozxing (software) decode")
	}

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

	// Two canvases at two different resolutions, not one shared one: the
	// previous version drew+read back a single canvas that was downscaled
	// for *both* the on-screen preview and the actual zxing decode, which
	// meant making the preview cheap enough to feel smooth (small canvas)
	// directly cost decode quality (same small canvas) -- confirmed live
	// as the actual "downscales the QR code into unreadable mush" report:
	// a code that's perfectly legible held up to the eye was often already
	// below zxing's module-per-pixel threshold once shrunk to fit that one
	// shared low-res canvas. Splitting them lets the preview stay cheap
	// (previewCanvas, refreshed every tick, small) while the decode
	// canvas gets close to the camera's actual delivered resolution
	// (drawn+read back only on the ticks that decode, a third as often --
	// see qrCameraDecodeEveryNTicks) so the QR reader gets to see real
	// module detail instead of a pre-shrunk approximation of it.
	previewWidth, previewHeight := qrCameraScaledDimensions(srcWidth, srcHeight, qrCameraPreviewMaxWidth)
	decodeWidth, decodeHeight := qrCameraScaledDimensions(srcWidth, srcHeight, qrCameraDecodeMaxWidth)

	previewCanvas := doc.Call("createElement", "canvas")
	previewCanvas.Set("width", previewWidth)
	previewCanvas.Set("height", previewHeight)
	q.previewCanvas = previewCanvas
	q.previewCtx = previewCanvas.Call("getContext", "2d")

	decodeCanvas := doc.Call("createElement", "canvas")
	decodeCanvas.Set("width", decodeWidth)
	decodeCanvas.Set("height", decodeHeight)
	q.decodeCanvas = decodeCanvas
	q.decodeCtx = decodeCanvas.Call("getContext", "2d")
	// Smoothing/interpolation blurs exactly the sharp black/white module
	// edges zxing's binarizer depends on to tell a module boundary from
	// noise -- worth it on the preview (looks nicer, decode quality
	// doesn't matter there) but actively counterproductive on the canvas
	// that's actually being decoded, even though decodeWidth/Height is
	// normally close enough to srcWidth/Height that drawImage barely
	// scales at all.
	q.decodeCtx.Set("imageSmoothingEnabled", false)

	logrus.Infof("[QRCamera/wasm] camera stream open: source=%dx%d preview=%dx%d decode=%dx%d", srcWidth, srcHeight, previewWidth, previewHeight, decodeWidth, decodeHeight)
	q.captureLoop(videoImg, previewWidth, previewHeight, decodeWidth, decodeHeight, closeUI)
}

// qrCameraPreviewMaxWidth caps the cheap, every-tick preview canvas --
// this is what makes the on-screen feed feel smooth (small
// drawImage+getImageData+CopyBytesToGo+image.NewRGBA every 150ms), and has
// no bearing on decode quality since qrCameraDecodeMaxWidth below is now a
// fully separate canvas.
const qrCameraPreviewMaxWidth = 480

// qrCameraDecodeMaxWidth caps the canvas the QR reader actually sees.
// 1920 matches the getUserMedia "ideal" request above -- in practice this
// is a no-op on most phone hardware (drawImage draws source-resolution
// video into a same-size canvas, ~1:1), it only kicks in as a genuine
// downscale on hardware that hands back something even higher-res than
// requested. Only paid on the minority of ticks that decode (see
// qrCameraDecodeEveryNTicks), not every tick like the preview, so the
// extra readback cost here is bounded.
const qrCameraDecodeMaxWidth = 1920

// setUpNativeBarcodeDetector feature-detects the Shape Detection API's
// window.BarcodeDetector and confirms it actually supports the "qr_code"
// format (Chrome/Edge implement the interface but a given UA/OS build could
// in principle only back other 1D formats), returning a ready-to-use
// detector instance and true when both hold. The canonical, spec-documented
// way to check format support is the constructor's own static async
// getSupportedFormats() -- not just "does the constructor throw," which is
// under-specified across implementations -- so this awaits that once, up
// front, rather than probing per-tick. Returns the zero js.Value and false
// on any browser that lacks the API entirely (Firefox, Safari as of this
// writing) or doesn't list qr_code, in which case every caller falls
// through to the existing gozxing (software) decode path unchanged.
func setUpNativeBarcodeDetector() (js.Value, bool) {
	ctor := js.Global().Get("BarcodeDetector")
	if ctor.IsUndefined() || ctor.IsNull() {
		return js.Value{}, false
	}
	getSupported := ctor.Get("getSupportedFormats")
	if getSupported.IsUndefined() {
		return js.Value{}, false
	}

	result, err := awaitJSPromise(ctor.Call("getSupportedFormats"))
	if err != nil {
		logrus.Debugf("[QRCamera/wasm] BarcodeDetector.getSupportedFormats() failed: %v", err)
		return js.Value{}, false
	}
	supportsQR := false
	for i := 0; i < result.Get("length").Int(); i++ {
		if result.Index(i).String() == "qr_code" {
			supportsQR = true
			break
		}
	}
	if !supportsQR {
		return js.Value{}, false
	}

	opts := js.Global().Get("Object").New()
	opts.Set("formats", []interface{}{"qr_code"})
	detector := ctor.New(opts)
	return detector, true
}

// detectNative runs one native BarcodeDetector pass directly on the live
// <video> element (no manual drawImage/getImageData/CopyBytesToGo round
// trip needed -- the browser's own implementation reads the frame itself,
// which is both simpler and cheaper than the gozxing path below it).
// Returns (contents, true, nil) on a match, ("", false, nil) when the
// detector ran fine but found nothing this frame (the common case, not an
// error), and ("", false, err) only when the call itself failed -- callers
// treat that last case as "this browser's native detector isn't reliable
// after all" and fall back to gozxing for the rest of the session.
func (q *QRCameraScanner) detectNative() (contents string, found bool, err error) {
	defer func() {
		// detect() throwing synchronously (rather than rejecting its
		// Promise) would otherwise panic this goroutine -- defensive only,
		// no implementation is known to do this for a live <video> source,
		// but the fallback path exists precisely for surprises like this.
		if r := recover(); r != nil {
			err = fmt.Errorf("BarcodeDetector.detect() panicked: %v", r)
		}
	}()

	result, callErr := awaitJSPromise(q.barcodeDetector.Call("detect", q.videoEl))
	if callErr != nil {
		return "", false, callErr
	}
	if result.Get("length").Int() == 0 {
		return "", false, nil
	}
	rawValue := result.Index(0).Get("rawValue")
	if rawValue.IsUndefined() || rawValue.IsNull() {
		return "", false, nil
	}
	return rawValue.String(), true, nil
}

// applyContinuousFocus re-requests continuous autofocus directly on the
// live video track via applyConstraints, on top of the "advanced"
// getUserMedia constraint already asked for above. Confirmed live as
// necessary, not redundant: on several Android Chrome/WebView builds the
// focusMode passed inside the initial getUserMedia() call is silently
// dropped (the camera locks focus at whatever it happened to be on at
// first frame -- exactly the "не фокусировалась вообще, видела мазанину"
// [never focused at all, just saw a blur] symptom, no error, nothing to
// catch), while the identical constraint applied to the already-live
// MediaStreamTrack via applyConstraints() after the fact reliably takes.
// getCapabilities()/focusMode is Chrome-only and only appears on tracks
// whose hardware actually exposes focus control -- both silently absent
// (empty/undefined) everywhere else, so this is a no-op there rather than
// an error.
func applyContinuousFocus(stream js.Value) {
	tracks := stream.Call("getVideoTracks")
	if tracks.Get("length").Int() == 0 {
		return
	}
	track := tracks.Index(0)
	getCaps := track.Get("getCapabilities")
	if getCaps.IsUndefined() {
		return
	}
	caps := track.Call("getCapabilities")
	focusModes := caps.Get("focusMode")
	if focusModes.IsUndefined() || focusModes.IsNull() {
		return
	}
	supportsContinuous := false
	for i := 0; i < focusModes.Get("length").Int(); i++ {
		if focusModes.Index(i).String() == "continuous" {
			supportsContinuous = true
			break
		}
	}
	if !supportsContinuous {
		return
	}
	applyConstraints := track.Get("applyConstraints")
	if applyConstraints.IsUndefined() {
		return
	}
	promise := track.Call("applyConstraints", map[string]interface{}{
		"advanced": []interface{}{
			map[string]interface{}{"focusMode": "continuous"},
		},
	})
	if !promise.IsUndefined() {
		promise.Call("catch", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if len(args) > 0 {
				logrus.Warnf("[QRCamera/wasm] applyConstraints(focusMode=continuous) rejected: %v", args[0])
			}
			return nil
		}))
	}
}

// qrCameraScaledDimensions downscales to a capped width while preserving
// aspect ratio; returns the source dimensions unchanged if already under
// the cap.
func qrCameraScaledDimensions(srcWidth, srcHeight, maxWidth int) (width, height int) {
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

func (q *QRCameraScanner) captureLoop(videoImg *canvas.Image, previewWidth, previewHeight, decodeWidth, decodeHeight int, closeUI func()) {
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

		// Cheap preview path, every tick: Fyne's canvas.Image needs real
		// Go-side image.Image bytes to texture-upload (there's no way to
		// hand it the HTML canvas directly), but only at previewWidth/
		// Height -- small enough that this readback+copy+realloc stays
		// smooth at qrCameraFrameInterval.
		q.previewCtx.Call("drawImage", q.videoEl, 0, 0, previewWidth, previewHeight)
		previewData := q.previewCtx.Call("getImageData", 0, 0, previewWidth, previewHeight)
		previewPixels := previewData.Get("data") // Uint8ClampedArray, RGBA

		previewImg := image.NewRGBA(image.Rect(0, 0, previewWidth, previewHeight))
		js.CopyBytesToGo(previewImg.Pix, previewPixels)

		fyne.Do(func() {
			videoImg.Image = previewImg
			videoImg.Refresh()
		})

		if tickCount%qrCameraDecodeEveryNTicks != 0 {
			continue
		}

		if q.useNativeDetector {
			contents, found, err := q.detectNative()
			if err != nil {
				// Real failure (not just "nothing found this frame"):
				// this browser advertised qr_code support via
				// getSupportedFormats() but detect() itself isn't
				// actually working -- disable native detection for the
				// rest of this scan session rather than retrying a
				// broken call every tick, and let this same tick fall
				// through to gozxing below instead of wasting it.
				logrus.Warnf("[QRCamera/wasm] native detect() failed, falling back to gozxing for the rest of this session: %v", err)
				q.useNativeDetector = false
			} else if found {
				logrus.Infof("QR detected (native): %s", contents)
				fyne.Do(func() {
					q.Stop()
					if closeUI != nil {
						closeUI()
					}
					q.qrScanner.parseAndApply(contents, q.parent)
				})
				return
			} else {
				continue
			}
		}

		// gozxing (software) fallback path -- only reached when this
		// browser has no working native BarcodeDetector for qr_code (see
		// useNativeDetector above). Draws the same live video frame
		// again, this time into decodeCanvas (see its own doc comment on
		// why this is a second, separate, higher-res canvas rather than
		// reusing previewImg here).
		q.decodeCtx.Call("drawImage", q.videoEl, 0, 0, decodeWidth, decodeHeight)
		decodeData := q.decodeCtx.Call("getImageData", 0, 0, decodeWidth, decodeHeight)
		decodePixels := decodeData.Get("data")

		decodeImg := image.NewRGBA(image.Rect(0, 0, decodeWidth, decodeHeight))
		js.CopyBytesToGo(decodeImg.Pix, decodePixels)

		if contents, ok := decodeQRImage(qrReader, decodeImg); ok {
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

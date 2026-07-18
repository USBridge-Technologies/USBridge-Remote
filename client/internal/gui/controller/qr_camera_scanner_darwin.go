//go:build darwin && !ios

// AVFoundation-based desktop QR camera capture for macOS. Replaces the
// gstreamer avfvideosrc pipeline used previously (qr_camera_scanner.go,
// now Linux-only) so the macOS build doesn't need to bundle GStreamer at
// all — capture goes straight through the OS-supplied AVFoundation
// framework, mirroring qr_camera_scanner_windows.go's use of Media
// Foundation on Windows.

package controller

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AVFoundation -framework CoreVideo -framework CoreMedia -framework Foundation

#include "avcamera_impl_darwin.h"
extern void goAVCameraLog(char *msg, int level);
*/
import "C"

import (
	"fmt"
	"image"
	"unsafe"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/widget"
	"github.com/makiuchi-d/gozxing/qrcode"
	"github.com/sirupsen/logrus"
)

//export goAVCameraLog
func goAVCameraLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 2:
		logrus.Errorf("[QRCamera/AV] %s", text)
	case 1:
		logrus.Warnf("[QRCamera/AV] %s", text)
	default:
		logrus.Infof("[QRCamera/AV] %s", text)
	}
}

const (
	avCameraRequestWidth  = 640
	avCameraRequestHeight = 480
)

// QRCameraScanner scans QR codes from a live desktop camera feed using
// AVFoundation directly (no GStreamer).
type QRCameraScanner struct {
	parent    fyne.Window
	qrScanner *QRScanner

	width, height int
	stopChan      chan struct{}

	popup *widget.PopUp
}

func newQRCameraScanner(parent fyne.Window, qs *QRScanner) (*QRCameraScanner, error) {
	var outW, outH C.int
	if C.av_camera_open(C.int(avCameraRequestWidth), C.int(avCameraRequestHeight), &outW, &outH) == 0 {
		return nil, fmt.Errorf("failed to open camera (AVFoundation)")
	}

	return &QRCameraScanner{
		parent:    parent,
		qrScanner: qs,
		width:     int(outW),
		height:    int(outH),
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

	go q.captureLoop(videoImg, closeUI)
}

// captureLoop blocks on av_camera_read_frame in a background goroutine,
// updating the preview and feeding every frame to the QR decoder until a
// code is found or the scanner is stopped.
func (q *QRCameraScanner) captureLoop(videoImg *canvas.Image, closeUI func()) {
	logrus.Info("Starting QR camera capture (AVFoundation)")

	qrReader := qrcode.NewQRCodeReader()
	frameBuf := make([]byte, q.width*q.height*4)
	firstFrameLogged := false

	defer C.av_camera_close()

	for {
		select {
		case <-q.stopChan:
			return
		default:
		}

		result := C.av_camera_read_frame((*C.uint8_t)(unsafe.Pointer(&frameBuf[0])), C.int(len(frameBuf)))
		switch result {
		case 0: // end of stream / fatal error
			select {
			case <-q.stopChan:
			default:
				fyne.Do(func() {
					if closeUI != nil {
						closeUI()
					}
					view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorStartingCamera, fmt.Errorf("camera stream ended")), q.parent)
				})
			}
			return
		case 2: // no sample this call, try again
			continue
		}

		img := image.NewRGBA(image.Rect(0, 0, q.width, q.height))
		copy(img.Pix, frameBuf)

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

	if q.popup != nil {
		q.popup.Hide()
		q.popup = nil
	}
}

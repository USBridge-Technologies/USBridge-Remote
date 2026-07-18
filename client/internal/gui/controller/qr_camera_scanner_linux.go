//go:build linux && !android

// V4L2-based desktop QR camera capture for Linux. Replaces the gstreamer
// autovideosrc pipeline used previously so the Linux build doesn't need to
// bundle GStreamer at all — capture goes straight through the kernel's V4L2
// ioctl interface, mirroring qr_camera_scanner_darwin.go (AVFoundation) and
// qr_camera_scanner_windows.go (Media Foundation).
//
// Only YUYV-capable devices are supported for now; MJPEG-only cameras (some
// higher-megapixel UVC webcams) fail to open with a clear log message
// instead of producing garbage frames — see v4l2camera_impl_linux.c.

package controller

/*
#include "v4l2camera_impl_linux.h"
extern void goV4L2CameraLog(char *msg, int level);
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

//export goV4L2CameraLog
func goV4L2CameraLog(msg *C.char, level C.int) {
	text := C.GoString(msg)
	switch int(level) {
	case 2:
		logrus.Errorf("[QRCamera/V4L2] %s", text)
	case 1:
		logrus.Warnf("[QRCamera/V4L2] %s", text)
	default:
		logrus.Infof("[QRCamera/V4L2] %s", text)
	}
}

const (
	v4l2CameraRequestWidth  = 640
	v4l2CameraRequestHeight = 480
)

// QRCameraScanner scans QR codes from a live desktop camera feed using V4L2
// directly (no GStreamer).
type QRCameraScanner struct {
	parent    fyne.Window
	qrScanner *QRScanner

	width, height int
	stopChan      chan struct{}

	popup *widget.PopUp
}

func newQRCameraScanner(parent fyne.Window, qs *QRScanner) (*QRCameraScanner, error) {
	var outW, outH C.int
	if C.v4l2_camera_open(C.int(v4l2CameraRequestWidth), C.int(v4l2CameraRequestHeight), &outW, &outH) == 0 {
		return nil, fmt.Errorf("failed to open camera (V4L2)")
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

// captureLoop blocks on v4l2_camera_read_frame in a background goroutine,
// updating the preview and feeding every frame to the QR decoder until a
// code is found or the scanner is stopped.
func (q *QRCameraScanner) captureLoop(videoImg *canvas.Image, closeUI func()) {
	logrus.Info("Starting QR camera capture (V4L2)")

	qrReader := qrcode.NewQRCodeReader()
	frameBuf := make([]byte, q.width*q.height*4)
	firstFrameLogged := false

	defer C.v4l2_camera_close()

	for {
		select {
		case <-q.stopChan:
			return
		default:
		}

		result := C.v4l2_camera_read_frame((*C.uint8_t)(unsafe.Pointer(&frameBuf[0])), C.int(len(frameBuf)))
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

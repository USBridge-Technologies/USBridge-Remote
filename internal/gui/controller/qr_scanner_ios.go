//go:build ios

package controller

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework AVFoundation -framework UIKit

#include <stdlib.h>

void ios_launch_qr_scanner(void);
void ios_qr_get_status(int *status_out, char *buf, int buflen);
void ios_qr_clear(void);
*/
import "C"

import (
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (qs *QRScanner) ShowCameraScannerNative(parent fyne.Window) {
	qs.window = parent
	session := qs.beginScanSession()

	logrus.Info("📷 iOS: launching native QR scanner (AVFoundation)")

	C.ios_launch_qr_scanner()

	go qs.pollQRResultIOS(parent, session)
}

func (qs *QRScanner) pollQRResultIOS(parent fyne.Window, session uint64) {
	buf := make([]byte, 4096)

	for i := 0; i < 600; i++ { // 60 second timeout
		time.Sleep(100 * time.Millisecond)

		if !qs.isScanSessionActive(session) {
			logrus.Debug("📷 iOS [POLL] session changed, stopping")
			return
		}

		var status C.int
		C.ios_qr_get_status(&status, (*C.char)(unsafe.Pointer(&buf[0])), 4096)

		switch int(status) {
		case 0:
			continue
		case 1:
			result := C.GoString((*C.char)(unsafe.Pointer(&buf[0])))
			C.ios_qr_clear()
			logrus.Infof("📷 iOS [POLL] QR scanned: %s", result)
			if !qs.tryHandleScanResult(session) {
				return
			}
			fyne.Do(func() {
				qs.parseAndApply(result, parent)
			})
			return
		case 2:
			C.ios_qr_clear()
			logrus.Info("📷 iOS [POLL] cancelled")
			return
		}
	}

	logrus.Warn("📷 iOS [POLL] timeout")
	C.ios_qr_clear()
}

//go:build ios

package controller

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (qs *QRScanner) ShowCameraScannerNative(parent fyne.Window) {
	logrus.Warn("📷 iOS: camera QR scanner not yet implemented")
}

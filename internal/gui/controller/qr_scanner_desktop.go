//go:build !android
// +build !android

package controller

import (
	"fmt"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/sirupsen/logrus"
)

// ShowCameraScannerNative показывает окно камеры для сканирования QR (desktop)
func (qs *QRScanner) ShowCameraScannerNative(parent fyne.Window) {
	qs.window = parent

	logrus.Info("📷 Desktop: запуск сканера QR-кода")

	// Создание pipeline (gst.Init, NewPipelineFromString) тяжёлое — делаем в фоне, чтобы не тормозить UI
	go func() {
		scanner, err := newQRCameraScanner(parent, qs)
		fyne.Do(func() {
			if err != nil {
				logrus.Warnf("Камера недоступна: %v", err)
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorStartingCamera, err), parent)
				return
			}
			scanner.Run()
		})
	}()
}

//go:build !android && !ios
// +build !android,!ios

package controller

import (
	"fmt"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// ShowCameraScannerNative shows camera window for scanning QR (desktop)
func (qs *QRScanner) ShowCameraScannerNative(parent fyne.Window) {
	qs.window = parent

	logrus.Info("📷 Desktop: starting QR code scanner")

	// Pipeline creation (gst.Init, NewPipelineFromString) is heavy - doing it in background to not block UI
	go func() {
		scanner, err := newQRCameraScanner(parent, qs)
		fyne.Do(func() {
			if err != nil {
				logrus.Warnf("Camera is unavailable: %v", err)
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorStartingCamera, err), parent)
				return
			}
			scanner.Run()
		})
	}()
}

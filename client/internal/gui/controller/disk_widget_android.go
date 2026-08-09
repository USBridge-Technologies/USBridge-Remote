//go:build android
// +build android

package controller

import (
	"fmt"
	"runtime"
	"time"

	"usbridge-client/androidbridge"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// pickImageForDiskList triggers a file pick via SAF and adds it to the image list
// Global handler for SAF (to avoid visibility issues between packages)
var globalSAFSuccessHandler func(uri string, fd int, size int64)
var globalSAFErrorHandler func(err string)

// CallGlobalSAFSuccess is called by external packages to pass through the SAF result
func CallGlobalSAFSuccess(uri string, fd int, size int64) {
	if globalSAFSuccessHandler != nil {
		globalSAFSuccessHandler(uri, fd, size)
	} else {
		logrus.Warn("⚠️ [CONTROLLER] globalSAFSuccessHandler is nil")
	}
}

// CallGlobalSAFError is called by external packages to pass through a SAF error
func CallGlobalSAFError(err string) {
	if globalSAFErrorHandler != nil {
		globalSAFErrorHandler(err)
	} else {
		logrus.Warn("⚠️ [CONTROLLER] globalSAFErrorHandler is nil")
	}
}

func (dw *DiskWidget) pickImageForDiskList() {
	fmt.Println("🚀 [SAF-CONTROLLER] pickImageForDiskList ENTER")
	logrus.Info("🚀 [SAF-CONTROLLER] pickImageForDiskList ENTER")
	if dw.window == nil {
		fmt.Println("⚠️ [SAF-CONTROLLER] dw.window is nil")
		logrus.Warn("⚠️ [SAF-CONTROLLER] dw.window is nil")
		return
	}

	fmt.Println("📁 [SAF] Starting image picker for disk list...")
	logrus.Info("📁 [SAF] Starting image picker for disk list...")

	// Set up the local handler
	successHandler := func(uri string, fileName string, fd int, size int64) {
		logrus.Infof("✅ [SAF-LOCAL-HANDLER] Success for uri=%s, fileName=%s, fd=%d, size=%d", uri, fileName, fd, size)

		if fileName == "" {
			fileName = "Image"
		}

		fyne.Do(func() {
			logrus.Infof("📍 [SAF-UI-UPDATE] Handling selected image: %s", fileName)
			dw.handleSelectedImage(selectedImage{
				FileName: fileName,
				URI:      uri,
			})
			dw.combineDrives()
			dw.requestDevicesRefresh()
			logrus.Info("✅ [SAF-UI-UPDATE] UI refresh requested")
		})
	}

	errorHandler := func(error string) {
		logrus.Errorf("❌ [SAF-LOCAL-HANDLER] Error: %s", error)
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorSelectingFile, error), dw.window)
		})
	}

	// Register the handlers both in androidbridge and globally
	globalSAFSuccessHandler = func(uri string, fd int, size int64) {
		successHandler(uri, "", fd, size)
	}
	globalSAFErrorHandler = errorHandler
	androidbridge.SetSAFCallbacks(func(uri string, fd int, size int64) {
		successHandler(uri, "", fd, size)
	}, errorHandler)

	// Trigger the native picker via JNI
	if dw.safHelper != nil {
		go func() {
			err := dw.safHelper.TriggerSAFPicker()
			if err != nil {
				logrus.Errorf("❌ [SAF] Failed to trigger picker: %v", err)
				fyne.Do(func() {
					view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorSelectingFile, err), dw.window)
				})
				return
			}

			// Start polling for the result from Java (since Go callbacks may not fire due to gomobile bind)
			logrus.Info("⏳ [SAF-POLL] Starting result polling from Java...")

			// Limit the polling duration (e.g. 2 minutes)
			timeout := time.After(2 * time.Minute)
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()

			for {
				select {
				case <-timeout:
					logrus.Warn("⚠️ [SAF-POLL] Polling timed out")
					return
				case <-ticker.C:
					logrus.Info("🔄 [SAF-POLL] Ticker tick, calling PollSAFResult...")
					uri, fileName, fd, size, hasResult := dw.safHelper.PollSAFResult()
					if hasResult {
						logrus.Infof("🎉 [SAF-POLL-SUCCESS] Result fetched from Java! uri=%s, fileName=%s, fd=%d, size=%d", uri, fileName, fd, size)
						successHandler(uri, fileName, fd, size)
						return
					}
				}
			}
		}()
	} else {
		logrus.Error("❌ [SAF] SAFHelper is nil")
	}
}

// Initialization for Android
func init() {
	if runtime.GOOS == "android" {
		logrus.Info("🤖 Android: SAF file picker integration via JNI is enabled")
		logrus.Info("📱 Using JNI integration for SAF")

		// Inject global handlers into the androidbridge package
		// This works around gomobile bind creating its own instance of the package
		androidbridge.GlobalSuccessHandler = CallGlobalSAFSuccess
		androidbridge.GlobalErrorHandler = CallGlobalSAFError
		logrus.Info("🔗 [ANDROID-INIT] SAF global handlers injected into androidbridge")
	}
}

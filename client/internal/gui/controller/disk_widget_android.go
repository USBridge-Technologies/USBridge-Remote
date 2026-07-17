//go:build android
// +build android

package controller

import (
	"fmt"
	"runtime"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/nbdbridge"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// NBD state for Android
var (
	nbdSelectedUri  string
	nbdSelectedFd   int
	nbdSelectedSize int64
	nbdAddr         = "127.0.0.1:10809" // Default address
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

	// Register the handlers both in nbdbridge and globally
	globalSAFSuccessHandler = func(uri string, fd int, size int64) {
		successHandler(uri, "", fd, size)
	}
	globalSAFErrorHandler = errorHandler
	nbdbridge.SetSAFCallbacks(func(uri string, fd int, size int64) {
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

// handleAddImageAndroid handles adding an image via SAF on Android
func (dw *DiskWidget) handleAddImageAndroid() {
	if dw.window == nil {
		logrus.Warn("⚠️ Window is not set")
		return
	}

	// Show the NBD management dialog
	dw.showNbdDialog()
}

// showNbdDialog shows the NBD server management dialog
func (dw *DiskWidget) showNbdDialog() {
	// Status label
	statusLabel := widget.NewLabel(getNbdStatusText())
	statusLabel.Wrapping = fyne.TextWrapWord

	// Address entry
	addrEntry := widget.NewEntry()
	addrEntry.SetText(nbdAddr)
	addrEntry.SetPlaceHolder("127.0.0.1:10809")

	// LAN mode checkbox
	lanMode := widget.NewCheck(i18n.Current.NBDAllowLAN, func(checked bool) {
		if checked {
			addrEntry.SetText("0.0.0.0:10809")
		} else {
			addrEntry.SetText("127.0.0.1:10809")
		}
	})

	// File info label
	fileInfoLabel := widget.NewLabel(i18n.Current.NBDImageNotSelected)
	if nbdSelectedUri != "" {
		fileInfoLabel.SetText(fmt.Sprintf(i18n.Current.NBDImageSelected,
			nbdSelectedUri, nbdSelectedSize/1024/1024))
	}

	// Pick file button - uses SAF via NbdBridge.startSAFPicker()
	pickBtn := widget.NewButton(i18n.Current.NBDSelectImage, func() {
		logrus.Info("📁 Installing SAF callbacks and preparing the picker...")

		successHandler := func(uri string, fileName string, fd int, size int64) {
			nbdSelectedUri = uri
			nbdSelectedFd = fd
			nbdSelectedSize = size

			displayName := fileName
			if displayName == "" {
				displayName = uri
			}
			logrus.Infof("✅ Image selected: %s, fd=%d, size=%d", displayName, fd, size)

			fyne.Do(func() {
				fileInfoLabel.SetText(fmt.Sprintf(i18n.Current.NBDImageSelected,
					displayName, size/1024/1024))
				view.ShowInfoDialog(i18n.Current.FileSelected,
					fmt.Sprintf(i18n.Current.NBDImageSelectedGB, displayName, float64(size)/1024/1024/1024),
					dw.window)
			})
		}

		nbdbridge.SetSAFCallbacks(
			func(uri string, fd int, size int64) { successHandler(uri, "", fd, size) },
			func(error string) {
				logrus.Errorf("❌ File selection failed: %s", error)
				fyne.Do(func() {
					view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorSelectingFile, error), dw.window)
				})
			},
		)

		globalSAFSuccessHandler = func(uri string, fd int, size int64) {
			successHandler(uri, "", fd, size)
		}
		globalSAFErrorHandler = func(error string) {
			logrus.Errorf("❌ File selection failed: %s", error)
			fyne.Do(func() {
				view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorSelectingFile, error), dw.window)
			})
		}

		// Call the SAF picker via NbdBridge (through JNI)
		logrus.Info("📱 Triggering SAF picker via JNI...")
		if dw.safHelper != nil {
			go func() {
				err := dw.safHelper.TriggerSAFPicker()
				if err != nil {
					logrus.Errorf("❌ Failed to trigger SAF picker: %v", err)
					fyne.Do(func() {
						view.ShowErrorDialog(fmt.Errorf(i18n.Current.ErrorSelectingFile, err), dw.window)
					})
					return
				}

				// Poll for the result
				timeout := time.After(2 * time.Minute)
				ticker := time.NewTicker(500 * time.Millisecond)
				defer ticker.Stop()

				for {
					select {
					case <-timeout:
						return
					case <-ticker.C:
						uri, fileName, fd, size, hasResult := dw.safHelper.PollSAFResult()
						if hasResult {
							successHandler(uri, fileName, fd, size)
							return
						}
					}
				}
			}()
		} else {
			logrus.Error("❌ SAFHelper is nil, cannot trigger picker")
		}
	})

	// Start button
	startBtn := widget.NewButton(i18n.Current.NBDStartServer, func() {
		if nbdSelectedFd <= 0 {
			fyne.Do(func() {
				view.ShowErrorDialog(fmt.Errorf("%s", i18n.Current.NBDImageNotSelected), dw.window)
			})
			return
		}

		addr := addrEntry.Text
		if addr == "" {
			addr = nbdAddr
		}
		nbdAddr = addr

		// Start NBD with foreground service via JNI (readOnly=false: allow writes for RW/overlay)
		dw.startNbdServiceJNI(nbdSelectedFd, nbdSelectedSize, addr, false)

		// Update status
		fyne.Do(func() {
			statusLabel.SetText(getNbdStatusText())
		})
	})

	// Stop button
	stopBtn := widget.NewButton(i18n.Current.NBDStopServer, func() {
		dw.stopNbdService()
		fyne.Do(func() {
			statusLabel.SetText(getNbdStatusText())
		})
	})

	// Refresh status button
	refreshBtn := widget.NewButton(i18n.Current.NBDRefreshStatus, func() {
		fyne.Do(func() {
			statusLabel.SetText(getNbdStatusText())
		})
	})

	// Instructions
	instructions := widget.NewLabel("📖 " + i18n.Current.NBDInstructions)
	instructions.Wrapping = fyne.TextWrapWord

	// Layout
	content := container.NewVBox(
		widget.NewLabel(i18n.Current.NBDServerManagement),
		widget.NewSeparator(),
		statusLabel,
		widget.NewSeparator(),
		fileInfoLabel,
		pickBtn,
		widget.NewSeparator(),
		widget.NewLabel(i18n.Current.NBDListenAddress),
		addrEntry,
		lanMode,
		widget.NewSeparator(),
		container.NewGridWithColumns(2, startBtn, stopBtn),
		refreshBtn,
		widget.NewSeparator(),
		instructions,
	)

	// Create dialog
	d := dialog.NewCustom(i18n.Current.NBDServerForAndroid, i18n.Current.Close, content, dw.window)
	d.Resize(fyne.NewSize(500, 700))
	d.Show()
}

// startNbdServiceJNI starts NBD via a foreground service using JNI
// readOnly: false = RW (writes allowed, for overlay); true = read-only
func (dw *DiskWidget) startNbdServiceJNI(fd int, size int64, addr string, readOnly bool) {
	logrus.Infof("🚀 Starting NBD service via JNI: fd=%d, size=%d, addr=%s, readOnly=%v", fd, size, addr, readOnly)

	// Start NBD backend (readOnly=false so the host can write to the loop without an I/O error)
	err := nbdbridge.StartNBD(fd, size, addr, readOnly)
	if err != nil {
		logrus.Errorf("❌ Failed to start NBD backend: %v", err)
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.NBDStartFailed, err), dw.window)
		})
		return
	}

	// TODO: Start foreground service via app.RunOnJVM calling NbdBridge.startNbdService()

	logrus.Info("✅ NBD server started")
	fyne.Do(func() {
		view.ShowInfoDialog(
			i18n.Current.NBDStarted,
			fmt.Sprintf(i18n.Current.NBDStartedInstructions, addr),
			dw.window,
		)
	})
}

// stopNbdService stops the NBD service
func (dw *DiskWidget) stopNbdService() {
	logrus.Info("🛑 Stopping NBD service")

	err := nbdbridge.StopNBD()
	if err != nil {
		logrus.Errorf("❌ Failed to stop NBD: %v", err)
		fyne.Do(func() {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.NBDStopError, err), dw.window)
		})
		return
	}

	logrus.Info("✅ NBD server stopped")
	fyne.Do(func() {
		view.ShowInfoDialog(i18n.Current.NBDStopped, i18n.Current.NBDStoppedSuccess, dw.window)
	})
}

// getNbdStatusText returns the current NBD status
func getNbdStatusText() string {
	status := nbdbridge.Status()

	if status == "stopped" {
		return "🔴 " + i18n.Current.NBDStatusStopped
	} else if len(status) >= 7 && status[:7] == "serving" {
		return "🟢 " + fmt.Sprintf(i18n.Current.NBDStatusRunning, status[8:])
	} else if len(status) >= 5 && status[:5] == "error" {
		return "🔴 " + fmt.Sprintf(i18n.Current.NBDStatusError, status[7:])
	}

	return "⚪ " + status
}

// Initialization for Android
func init() {
	if runtime.GOOS == "android" {
		logrus.Info("🤖 Android: NBD integration with SAF via JNI is enabled")
		logrus.Info("📱 Using JNI integration for SAF and foreground service")

		// Inject global handlers into the nbdbridge package
		// This works around gomobile bind creating its own instance of the package
		nbdbridge.GlobalSuccessHandler = CallGlobalSAFSuccess
		nbdbridge.GlobalErrorHandler = CallGlobalSAFError
		logrus.Info("🔗 [ANDROID-INIT] SAF global handlers injected into nbdbridge")
	}
}

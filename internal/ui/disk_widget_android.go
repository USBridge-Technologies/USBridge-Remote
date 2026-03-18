//go:build android
// +build android

package ui

import (
	"fmt"
	"runtime"

	"usbridge-client/internal/ui/i18n"
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

// handleAddImageAndroid обрабатывает добавление образа через SAF на Android
func (dw *DiskWidget) handleAddImageAndroid() {
	if dw.window == nil {
		logrus.Warn("⚠️ Window is not set")
		return
	}

	// Показываем диалог с NBD функциональностью
	dw.showNbdDialog()
}

// showNbdDialog показывает диалог управления NBD сервером
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

	// Pick file button - использует SAF через NbdBridge.startSAFPicker()
	pickBtn := widget.NewButton(i18n.Current.NBDSelectImage, func() {
		logrus.Info("📁 Installing SAF callbacks and preparing the picker...")

		// Устанавливаем callbacks
		nbdbridge.SetSAFCallbacks(
			// Success callback
			func(uri string, fd int, size int64) {
				nbdSelectedUri = uri
				nbdSelectedFd = fd
				nbdSelectedSize = size

				logrus.Infof("✅ Image selected: %s, fd=%d, size=%d", uri, fd, size)

				fyne.Do(func() {
					fileInfoLabel.SetText(fmt.Sprintf(i18n.Current.NBDImageSelected,
						uri, size/1024/1024))
					dialog.ShowInformation(i18n.Current.FileSelected,
						fmt.Sprintf(i18n.Current.NBDImageSelectedGB, uri, float64(size)/1024/1024/1024),
						dw.window)
				})
			},
			// Error callback
			func(error string) {
				logrus.Errorf("❌ File selection failed: %s", error)
				fyne.Do(func() {
					dialog.ShowError(fmt.Errorf(i18n.Current.ErrorSelectingFile, error), dw.window)
				})
			},
		)

		// Показываем инструкцию как вызвать SAF
		// TODO: Автоматический вызов через app.RunOnJVM
		fyne.Do(func() {
			dialog.ShowInformation(
				i18n.Current.SAFFilePicker,
				i18n.Current.SAFInstructions,
				dw.window,
			)
		})

		// Вызов SAF пикера через NbdBridge (gomobile экспортированный метод)
		// NbdBridge.startSAFPicker() откроет SAF и вызовет Nbdbridge.onSAFSuccess()
		logrus.Info("📱 Waiting for user to trigger SAF picker...")
	})

	// Start button
	startBtn := widget.NewButton(i18n.Current.NBDStartServer, func() {
		if nbdSelectedFd <= 0 {
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf("%s", i18n.Current.NBDImageNotSelected), dw.window)
			})
			return
		}

		addr := addrEntry.Text
		if addr == "" {
			addr = nbdAddr
		}
		nbdAddr = addr

		// Start NBD with foreground service via JNI (readOnly=false: разрешаем запись для RW/overlay)
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

// startNbdServiceJNI запускает NBD через foreground service с использованием JNI
// readOnly: false = RW (запись возможна, для overlay); true = только чтение
func (dw *DiskWidget) startNbdServiceJNI(fd int, size int64, addr string, readOnly bool) {
	logrus.Infof("🚀 Starting NBD service via JNI: fd=%d, size=%d, addr=%s, readOnly=%v", fd, size, addr, readOnly)

	// Start NBD backend (readOnly=false чтобы хост мог писать в loop без I/O error)
	err := nbdbridge.StartNBD(fd, size, addr, readOnly)
	if err != nil {
		logrus.Errorf("❌ Failed to start NBD backend: %v", err)
		fyne.Do(func() {
			dialog.ShowError(fmt.Errorf(i18n.Current.NBDStartFailed, err), dw.window)
		})
		return
	}

	// TODO: Start foreground service via app.RunOnJVM calling NbdBridge.startNbdService()

	logrus.Info("✅ NBD server started")
	fyne.Do(func() {
		dialog.ShowInformation(
			i18n.Current.NBDStarted,
			fmt.Sprintf(i18n.Current.NBDStartedInstructions, addr),
			dw.window,
		)
	})
}

// stopNbdService останавливает NBD service
func (dw *DiskWidget) stopNbdService() {
	logrus.Info("🛑 Stopping NBD service")

	err := nbdbridge.StopNBD()
	if err != nil {
		logrus.Errorf("❌ Failed to stop NBD: %v", err)
		fyne.Do(func() {
			dialog.ShowError(fmt.Errorf(i18n.Current.NBDStopError, err), dw.window)
		})
		return
	}

	logrus.Info("✅ NBD server stopped")
	fyne.Do(func() {
		dialog.ShowInformation(i18n.Current.NBDStopped, i18n.Current.NBDStoppedSuccess, dw.window)
	})
}

// getNbdStatusText возвращает текущий статус NBD
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

// Инициализация для Android
func init() {
	if runtime.GOOS == "android" {
		logrus.Info("🤖 Android: NBD integration with SAF via JNI is enabled")
		logrus.Info("📱 Using JNI integration for SAF and foreground service")
	}
}

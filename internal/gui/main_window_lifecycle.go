package gui

import (
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// createBackupFlashTab создает вкладку Backup Flash
func (mw *MainWindow) createBackupFlashTab() fyne.CanvasObject {
	return mw.backupWidget.GetContainer()
}

// setupEventHandlers настраивает обработчики событий
func (mw *MainWindow) setupEventHandlers() {
	mw.window.SetCloseIntercept(func() {
		mw.handleClose()
	})
}

// handleHostChanged обрабатывает изменение IP адреса
func (mw *MainWindow) handleHostChanged(host string) {
	if host == "" {
		return
	}

	tempClient := api.NewUSBClient(host, mw.config.USBPort, mw.config.APITimeout)
	mw.gstreamerService.UpdateHost(host)
	mw.diskWidget.UpdateClient(tempClient)
	mw.videoWidget.UpdateClient(tempClient)
	if mw.backupWidget != nil {
		mw.backupWidget.UpdateClient(tempClient)
	}

	logrus.Infof("🔄 Обновлен хост: %s", host)
}

// showConnectionManager показывает менеджер подключений
func (mw *MainWindow) showConnectionManager() {
	fyne.Do(func() {
		if mw.connectionContent == nil {
			logrus.Warn("⚠️ showConnectionManager: connectionContent is nil")
			return
		}
		if mw.deviceButtonsPanel != nil {
			mw.deviceButtonsPanel.Hide()
		}
		mw.window.SetContent(mw.connectionContent)
		if content := mw.window.Content(); content != nil {
			content.Refresh()
			mw.window.Canvas().Refresh(content)
		}
	})
}

// showMainContent показывает основной интерфейс
func (mw *MainWindow) showMainContent() {
	fyne.Do(func() {
		if mw.mainContent == nil {
			logrus.Warn("⚠️ showMainContent: mainContent is nil")
			return
		}
		mw.window.SetContent(mw.mainContent)
		if content := mw.window.Content(); content != nil {
			content.Refresh()
			mw.window.Canvas().Refresh(content)
		}
	})
	mw.updateDeviceButtonsVisibility()
}

// handleClose обрабатывает закрытие приложения
func (mw *MainWindow) handleClose() {
	if mw.videoWidget != nil && mw.videoWidget.ExitFullscreenIfNeeded() {
		logrus.Info("🔍 handleClose: обнаружен полноэкранный режим, выходим из него")
		return
	}

	if mw.isConnected {
		mw.handleDisconnect()
	}
	mw.window.Close()
}

// Show показывает окно
func (mw *MainWindow) Show() {
	go func() {
		time.Sleep(200 * time.Millisecond)

		fyne.Do(func() {
			mw.createInterface()
			mw.connectionManager = controller.NewConnectionManager(mw.app, mw.window, mw.hostEntry, mw.tokenEntry, mw.protocolSelect, mw.handleConnectionFromManager)
			mw.recreateContainers()
			mw.setupEventHandlers()
			mw.setDefaultValues()
			mw.showConnectionManager()
			mw.updateStatusBar()
			mw.deepLinkHandler = NewDeepLinkHandler(mw.handleConnectionFromManager, mw.handleSaveFromDeepLink)
			mw.checkDeepLink()
			mw.startDeepLinkMonitoring()
			mw.connectionManager.SetLanguageChangeCallback(mw.reloadUI)
		})
	}()

	mw.window.ShowAndRun()
}

// reloadUI перезагружает весь UI с новым языком
func (mw *MainWindow) reloadUI() {
	logrus.Info("🔄 Reloading UI with new language...")

	currentHost := mw.hostEntry.Text
	currentToken := mw.tokenEntry.Text
	wasConnected := mw.isConnected

	mw.createInterface()
	mw.hostEntry.SetText(currentHost)
	mw.tokenEntry.SetText(currentToken)

	mw.connectionManager = controller.NewConnectionManager(mw.app, mw.window, mw.hostEntry, mw.tokenEntry, mw.protocolSelect, mw.handleConnectionFromManager)
	mw.connectionManager.SetLanguageChangeCallback(mw.reloadUI)
	mw.recreateContainers()
	mw.window.SetTitle(i18n.Current.AppTitle)

	if wasConnected {
		if mw.pcpanelWidget != nil && mw.usbClient != nil {
			mw.pcpanelWidget.SetClient(mw.usbClient)
		}
		mw.showMainContent()
	} else {
		mw.showConnectionManager()
	}

	mw.updateStatusBar()
	logrus.Info("✅ UI reloaded successfully")
}

// checkDeepLink проверяет наличие deep link при запуске
func (mw *MainWindow) checkDeepLink() {
	if mw.deepLinkHandler != nil {
		mw.deepLinkHandler.CheckAndHandleDeepLink(mw.window)
	}
}

// startDeepLinkMonitoring запускает мониторинг deep links в фоне
func (mw *MainWindow) startDeepLinkMonitoring() {
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for range ticker.C {
			if mw.deepLinkHandler != nil {
				mw.deepLinkHandler.CheckAndHandleDeepLink(mw.window)
			}
		}
	}()
}

package gui

import (
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// createBackupFlashTab creates the Backup Flash tab.
func (mw *MainWindow) createBackupFlashTab() fyne.CanvasObject {
	return mw.backupWidget.GetContainer()
}

// setupEventHandlers configures window event handlers.
func (mw *MainWindow) setupEventHandlers() {
	mw.window.SetCloseIntercept(func() {
		mw.handleClose()
	})
}

// handleHostChanged updates the current host.
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

	logrus.Infof("Host updated: %s", host)
}

// showConnectionManager displays the connection manager.
func (mw *MainWindow) showConnectionManager() {
	fyne.Do(func() {
		if mw.connectionContent == nil {
			logrus.Warn("showConnectionManager: connectionContent is nil")
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

// showMainContent displays the main interface.
func (mw *MainWindow) showMainContent() {
	fyne.Do(func() {
		if mw.mainContent == nil {
			logrus.Warn("showMainContent: mainContent is nil")
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

// handleClose handles app shutdown.
func (mw *MainWindow) handleClose() {
	if !mw.shutdownInProgress.CompareAndSwap(false, true) {
		logrus.Info("[shutdown] handleClose: shutdown already in progress")
		return
	}

	logrus.Infof("[shutdown] handleClose: entered connected=%v wg_running=%v frp_running=%v", mw.isConnected, mw.wgService != nil && mw.wgService.IsRunning(), mw.frpService != nil && mw.frpService.IsRunning())
	if mw.videoWidget != nil && mw.videoWidget.ExitFullscreenIfNeeded() {
		logrus.Info("handleClose: fullscreen active, exit it first")
		mw.shutdownInProgress.Store(false)
		return
	}

	needsDisconnect := mw.isConnected ||
		mw.usbClient != nil ||
		(mw.videoWidget != nil && mw.videoWidget.IsStreaming()) ||
		(mw.nbdServer != nil && mw.nbdServer.IsRunning()) ||
		(mw.wgService != nil && mw.wgService.IsRunning()) ||
		(mw.frpService != nil && mw.frpService.IsRunning())

	if needsDisconnect {
		logrus.Info("[shutdown] handleClose: calling handleDisconnect")
		mw.handleDisconnect()
	}

	if mw.app != nil {
		logrus.Info("[shutdown] handleClose: quitting app")
		mw.app.Quit()
		return
	}

	logrus.Info("[shutdown] handleClose: closing window")
	mw.window.Close()
}

// Show displays the window.
func (mw *MainWindow) Show() {
	go func() {
		time.Sleep(200 * time.Millisecond)

		fyne.Do(func() {
			mw.createInterface()
			mw.connectionManager = controller.NewConnectionManager(mw.app, mw.window, mw.hostEntry, mw.tokenEntry, mw.protocolSelect, mw.handleConnectionFromManager)
			mw.recreateContainers()
			mw.connectionManager.SetConnectionsStateCallback(mw.updateConnectionFooterVisibility)
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

// reloadUI reloads all UI with the current language.
func (mw *MainWindow) reloadUI() {
	logrus.Info("Reloading UI with new language...")

	currentHost := mw.hostEntry.Text
	currentToken := mw.tokenEntry.Text
	wasConnected := mw.isConnected

	mw.createInterface()
	mw.hostEntry.SetText(currentHost)
	mw.tokenEntry.SetText(currentToken)

	mw.connectionManager = controller.NewConnectionManager(mw.app, mw.window, mw.hostEntry, mw.tokenEntry, mw.protocolSelect, mw.handleConnectionFromManager)
	mw.connectionManager.SetLanguageChangeCallback(mw.reloadUI)
	mw.recreateContainers()
	mw.connectionManager.SetConnectionsStateCallback(mw.updateConnectionFooterVisibility)
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
	logrus.Info("UI reloaded successfully")
}

// checkDeepLink checks for a deep link on app start.
func (mw *MainWindow) checkDeepLink() {
	if mw.deepLinkHandler != nil {
		mw.deepLinkHandler.CheckAndHandleDeepLink(mw.window)
	}
}

// startDeepLinkMonitoring starts background deep-link monitoring.
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

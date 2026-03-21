package gui

import (
	"strings"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// createInterface инициализирует поля адресной строки.
func (mw *MainWindow) createInterface() {
	mw.hostEntry = widget.NewEntry()
	mw.hostEntry.SetPlaceHolder(i18n.Current.ServerAddress)

	mw.tokenEntry = widget.NewEntry()
	mw.tokenEntry.SetPlaceHolder(i18n.Current.Token)
	mw.tokenEntry.Password = true

	mw.protocolSelect = widget.NewSelect([]string{
		models.ConnectionProtocolAuto,
		models.ConnectionProtocolQUIC,
		models.ConnectionProtocolWireGuard,
	}, nil)
	mw.protocolSelect.SetSelected(mw.config.ConnectionProtocol)

	mw.connectionBtn = widget.NewButton(i18n.Current.ConnectButton, mw.handleConnectionToggle)
	mw.connectionBtn.Importance = widget.HighImportance

	mw.sdStorageProgress = view.NewStorageProgressBar()
	mw.sdStorageProgress.Hide()

	if mw.backupWidget != nil {
		mw.backupWidget.UpdateHostEntry(mw.hostEntry)
	}

	mw.refreshConnectionControls()
}

func (mw *MainWindow) refreshConnectionControls() {
	if mw.connectionBtn == nil || mw.protocolSelect == nil {
		return
	}

	if mw.isConnected {
		text, importance := protocolButtonState(mw.connectedProtocol)
		mw.connectionBtn.SetText(text)
		mw.connectionBtn.Importance = importance
		mw.protocolSelect.Hide()
	} else {
		mw.connectionBtn.SetText(i18n.Current.ConnectButton)
		mw.connectionBtn.Importance = widget.HighImportance
		mw.protocolSelect.Show()
	}

	mw.connectionBtn.Refresh()
	mw.protocolSelect.Refresh()
}

// setDefaultValues устанавливает начальные значения для полей.
func (mw *MainWindow) setDefaultValues() {
	mw.hostEntry.SetText("")
	mw.tokenEntry.SetText("")
}

// recreateContainers пересоздает контейнеры с менеджером подключений.
func (mw *MainWindow) recreateContainers() {
	storageUpdate := func(usedPct float64, available, total int64) {
		fyne.Do(func() {
			if mw.sdStorageProgress == nil {
				return
			}
			if total <= 0 {
				mw.sdStorageProgress.Hide()
				return
			}
			mw.sdStorageProgress.SetValue(usedPct)
			used := total - available
			if used < 0 {
				used = 0
			}
			mw.sdStorageProgress.SetSizeText(models.FormatStorageSizeOnly(used, total))
			mw.sdStorageProgress.Show()
		})
	}
	if mw.diskWidget != nil {
		mw.diskWidget.SetOnStorageInfoUpdate(storageUpdate)
		if mw.videoWidget != nil {
			mw.diskWidget.SetOnMouseTypeChanged(mw.videoWidget.SetMouseInputMode)
		}
	}
	if mw.backupWidget != nil {
		mw.backupWidget.SetOnStorageInfoUpdate(storageUpdate)
	}

	statusBar := mw.createStatusBar()
	addressBar := mw.createAddressBar()

	mw.tabs = container.NewAppTabs(
		container.NewTabItem(i18n.Current.TabDevices, mw.diskWidget.GetContainer()),
		container.NewTabItem(i18n.Current.TabControl, mw.videoWidget.GetContainer()),
		container.NewTabItem(i18n.Current.TabSnapshots, mw.createBackupFlashTab()),
	)
	mw.tabs.OnSelected = func(tab *container.TabItem) {
		mw.updateDeviceButtonsVisibility()
	}

	mw.mainContent = container.NewBorder(addressBar, statusBar, nil, nil, mw.tabs)
	mw.connectionContent = container.NewBorder(
		addressBar,
		nil,
		nil,
		nil,
		mw.connectionManager.GetContainer(),
	)

	mw.window.SetContent(mw.connectionContent)
}

// createAddressBar создает адресную строку.
func (mw *MainWindow) createAddressBar() *fyne.Container {
	mw.pcpanelWidget = controller.NewPCPanelWidget(mw.window)
	protocolPanel := view.NewOutlinedControl(mw.protocolSelect, 132, 40)
	connectPanel := container.NewGridWrap(fyne.NewSize(132, 40), mw.connectionBtn)
	rightPart := container.NewHBox(
		mw.pcpanelWidget.GetContainer(),
		mw.sdStorageProgress,
		mw.statusPanel,
		protocolPanel,
		view.NewInset(container.NewWithoutLayout(), 12, 0, 0, 0),
		connectPanel,
	)
	row := container.New(
		layout.NewBorderLayout(nil, nil, nil, rightPart),
		mw.hostEntry,
		rightPart,
	)
	return view.NewHeaderBand("", row)
}

// createStatusBar создает строку состояния.
func (mw *MainWindow) createStatusBar() *fyne.Container {
	mw.connectionIcon = widget.NewButton("🔌", func() {})
	mw.connectionIcon.Importance = widget.LowImportance
	mw.nbdIcon = widget.NewButton("💿", func() {})
	mw.nbdIcon.Importance = widget.LowImportance
	mw.videoIcon = widget.NewButton("📺", func() {})
	mw.videoIcon.Importance = widget.LowImportance
	mw.keyboardIcon = widget.NewButton("⌨️", func() {
		if mw.videoWidget != nil {
			mw.videoWidget.HandleVirtualKeyboard()
		}
	})
	mw.keyboardIcon.Importance = widget.LowImportance
	mw.mouseIcon = widget.NewButton("🖱️", func() {})
	mw.mouseIcon.Importance = widget.LowImportance
	mw.rndisIcon = widget.NewButton("🌐", func() {})
	mw.rndisIcon.Importance = widget.LowImportance
	mw.cdromIcon = widget.NewButton("💿", func() {})
	mw.cdromIcon.Importance = widget.LowImportance
	mw.backupIcon = widget.NewButton("🛡️", func() {})
	mw.backupIcon.Importance = widget.LowImportance
	mw.snapshotIcon = widget.NewButton("📸", func() {})
	mw.snapshotIcon.Importance = widget.LowImportance

	mw.statusPanel = container.NewHBox()

	mountBtn, unmountBtn, addImageBtn := mw.diskWidget.GetButtons()
	mw.deviceButtonsPanel = container.NewHBox(mountBtn, addImageBtn, unmountBtn)
	mw.deviceButtonsPanel.Hide()

	return container.NewBorder(nil, nil, mw.deviceButtonsPanel, nil, nil)
}

// updateDeviceButtonsVisibility обновляет видимость кнопок устройств.
func (mw *MainWindow) updateDeviceButtonsVisibility() {
	if mw.tabs == nil || mw.deviceButtonsPanel == nil {
		return
	}

	fyne.Do(func() {
		if mw.tabs.SelectedIndex() == 0 {
			mw.deviceButtonsPanel.Show()
		} else {
			mw.deviceButtonsPanel.Hide()
		}
		mw.deviceButtonsPanel.Refresh()
	})
}

// updateStatusBar обновляет панель статусов.
func (mw *MainWindow) updateStatusBar() {
	keyboardConnected := false
	mouseConnected := false
	rndisConnected := false
	cdromConnected := false
	backupConnected := false
	snapshotConnected := false

	nbdConnected := false
	if mw.nbdServer.IsRunning() {
		clients := mw.nbdServer.GetClients()
		nbdConnected = len(clients) > 0
	}

	videoStreaming := mw.videoWidget != nil && mw.videoWidget.IsStreaming()

	if mw.usbClient != nil {
		deviceInfo, err := mw.usbClient.GetDeviceInfo()
		if err == nil {
			logrus.Infof("🔍 updateStatusBar: найдено %d устройств", len(deviceInfo.Devices))
			for _, device := range deviceInfo.Devices {
				logrus.Infof("🔍 Устройство: Type=%s, Status=%s, Name=%s, ProductName=%s",
					device.Type, device.Status, device.Name, device.ProductName)
				if device.Status == "connected" {
					if device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:") {
						keyboardConnected = true
					}
					if device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:") {
						mouseConnected = true
					}
					if device.Type == "rndis" || strings.HasPrefix(device.Type, "rndis:") {
						rndisConnected = true
					}
					if device.Type == "local" && !strings.Contains(device.Name, "data") {
						cdromConnected = true
					}
					if device.Type == "mtp" && strings.Contains(device.Name, "data") && !strings.Contains(device.ProductName, "snapshot") {
						backupConnected = true
					}
					if device.Type == "nbd" || (device.Type == "mtp" && (strings.Contains(device.ProductName, "snapshot") || strings.Contains(device.Name, "snapshot"))) {
						snapshotConnected = true
						logrus.Infof("📸 Найден снапшот: Type=%s, Name=%s, ProductName=%s", device.Type, device.Name, device.ProductName)
					}
				}
			}
		}

		if mw.backupWidget != nil && !snapshotConnected {
			snapshotsResp, err := mw.usbClient.GetSnapshots()
			if err == nil {
				for _, snapshot := range snapshotsResp.Snapshots {
					if snapshot.Connected {
						snapshotConnected = true
						logrus.Infof("📸 Найден подключенный снапшот через API снапшотов: %s", snapshot.Name)
						break
					}
				}
			}
		}
	}

	logrus.Infof("🔍 Статусы: keyboard=%v, mouse=%v, rndis=%v, cdrom=%v, backup=%v, snapshot=%v",
		keyboardConnected, mouseConnected, rndisConnected, cdromConnected, backupConnected, snapshotConnected)

	fyne.Do(func() {
		var allIcons []fyne.CanvasObject

		if nbdConnected {
			mw.nbdIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.nbdIcon)
		}
		if videoStreaming {
			mw.videoIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.videoIcon)
		}
		if keyboardConnected {
			mw.keyboardIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.keyboardIcon)
		}
		if mouseConnected {
			mw.mouseIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.mouseIcon)
		}
		if rndisConnected {
			mw.rndisIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.rndisIcon)
		}
		if cdromConnected {
			mw.cdromIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.cdromIcon)
		}
		if backupConnected {
			mw.backupIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.backupIcon)
		}
		if snapshotConnected {
			mw.snapshotIcon.Importance = widget.HighImportance
			allIcons = append(allIcons, mw.snapshotIcon)
		}

		if len(allIcons) > 0 {
			mw.statusPanel.Objects = allIcons
		} else {
			mw.statusPanel.Objects = []fyne.CanvasObject{}
		}
		mw.statusPanel.Refresh()
	})
}

package gui

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const (
	addressBarGap       float32 = 4
	addressBarControlH  float32 = 36
	addressBarActionBtn float32 = 36
)

// createInterface инициализирует поля адресной строки.
func (mw *MainWindow) createInterface() {
	mw.hostEntry = widget.NewEntry()
	mw.hostEntry.SetPlaceHolder(i18n.Current.ServerAddress)
	mw.hostEntry.OnChanged = func(string) {
		mw.refreshConnectionControls()
	}

	mw.tokenEntry = widget.NewEntry()
	mw.tokenEntry.SetPlaceHolder(i18n.Current.Token)
	mw.tokenEntry.Password = true

	mw.protocolSelect = widget.NewSelect([]string{
		models.ConnectionProtocolAuto,
		models.ConnectionProtocolQUIC,
		models.ConnectionProtocolWireGuard,
	}, nil)
	mw.protocolSelect.OnChanged = func(value string) {
		if mw.protocolDropdown != nil {
			mw.protocolDropdown.SetSelected(value)
		}
	}

	mw.protocolDropdown = view.NewHeaderDropdown(mw.protocolSelect.Options, mw.config.ConnectionProtocol, func(value string) {
		mw.protocolSelect.SetSelected(value)
	})
	mw.protocolSelect.SetSelected(mw.config.ConnectionProtocol)

	mw.connectionBtn = view.NewHeaderActionButton(mw.handleConnectionToggle)

	mw.sdStorageProgress = view.NewStorageProgressBar()
	mw.sdStorageProgress.Hide()

	if mw.backupWidget != nil {
		mw.backupWidget.UpdateHostEntry(mw.hostEntry)
	}

	mw.refreshConnectionControls()
}

func (mw *MainWindow) refreshConnectionControls() {
	if mw.connectionBtn == nil || mw.protocolSelect == nil || mw.protocolDropdown == nil {
		return
	}

	if mw.isConnected {
		text, fill, foreground := protocolButtonState(mw.connectedProtocol)
		mw.connectionBtn.ApplySpec(view.HeaderActionButtonSpec{
			Disabled:   mw.isConnectionPending,
			Fill:       fill,
			Foreground: foreground,
			Stroke:     color.Transparent,
			Text:       text,
		})
		mw.protocolDropdown.Hide()
	} else {
		hasHost := strings.TrimSpace(mw.hostEntry.Text) != ""
		spec := view.HeaderActionButtonSpec{
			Disabled:   mw.isConnectionPending || !hasHost,
			Fill:       design.ColorAccent,
			Foreground: design.ColorBackground,
			Icon:       assets.ServerConnectBold,
			Stroke:     color.Transparent,
		}

		if !hasHost {
			spec.Fill = color.Transparent
			spec.Foreground = design.ColorAccentHover
			spec.Icon = assets.ServerConnectGlow
			spec.Stroke = design.ColorAccentHover
			spec.StrokeWidth = 1
		}
		if mw.isConnectionLoading {
			spec.Disabled = true
			spec.Fill = design.ColorAccent
			spec.Foreground = design.ColorBackground
			spec.Icon = nil
			spec.Stroke = color.Transparent
			spec.SpinnerFrames = assets.LoadingGrayFrames
		}

		mw.connectionBtn.ApplySpec(spec)
		mw.protocolDropdown.SetDisabled(mw.isConnectionPending)
		mw.protocolDropdown.Show()
	}

	mw.protocolDropdown.Refresh()
	if mw.connectionManager != nil {
		mw.connectionManager.SetConnectionPending(mw.isConnectionPending && !mw.isConnected)
	}
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

	mw.createStatusBar()
	addressBar := mw.createAddressBar()
	mainFooter := mw.createDeviceFooterBar()
	connectionFooter := mw.createConnectionFooterBar()

	mw.tabs = container.NewAppTabs(
		container.NewTabItem(i18n.Current.TabDevices, mw.diskWidget.GetContainer()),
		container.NewTabItem(i18n.Current.TabControl, mw.videoWidget.GetContainer()),
		container.NewTabItem(i18n.Current.TabSnapshots, mw.createBackupFlashTab()),
	)
	mw.tabs.OnSelected = func(tab *container.TabItem) {
		mw.updateDeviceButtonsVisibility()
	}

	mw.mainContent = container.NewBorder(addressBar, mainFooter, nil, nil, mw.tabs)
	mw.connectionContent = container.NewBorder(
		addressBar,
		connectionFooter,
		nil,
		nil,
		mw.connectionManager.GetContainer(),
	)

	mw.window.SetContent(mw.connectionContent)
}

// createAddressBar создает адресную строку.
func (mw *MainWindow) createAddressBar() *fyne.Container {
	mw.pcpanelWidget = controller.NewPCPanelWidget(mw.window)
	hostField := view.NewFixedHeight(mw.hostEntry, addressBarControlH)
	protocolPanel := view.NewOutlinedControl(mw.protocolDropdown, 0, addressBarControlH)
	connectPanel := container.NewGridWrap(fyne.NewSize(addressBarActionBtn, addressBarControlH), mw.connectionBtn)
	utilityPart := newCollapsingBox(
		mw.pcpanelWidget.GetContainer(),
		mw.sdStorageProgress,
		mw.statusPanel,
	)
	rightPart := container.NewHBox(
		newOptionalLeadingGap(addressBarGap, utilityPart),
		headerGapSpacer(addressBarGap),
		protocolPanel,
		headerGapSpacer(addressBarGap),
		connectPanel,
	)
	row := container.New(
		layout.NewBorderLayout(nil, nil, nil, rightPart),
		hostField,
		rightPart,
	)
	return view.NewHeaderBand("", row)
}

func headerGapSpacer(width float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(width, 1))
	return spacer
}

func (mw *MainWindow) createConnectionFooterBar() *fyne.Container {
	var langBtn fyne.CanvasObject
	langBtn = view.NewFooterIconButton(assets.LanguageIconDim, assets.LanguageIcon, fyne.NewSize(14, 14), func() {
		if mw.connectionManager != nil {
			mw.connectionManager.ShowLanguageMenu(langBtn)
		}
	})

	helpBtn := view.NewFooterIconButton(assets.QuestionIconDim, assets.QuestionIcon, fyne.NewSize(13, 13), func() {
		if mw.connectionManager != nil {
			mw.connectionManager.OpenQuickStartDocs()
		}
	})

	discordBtn := view.NewFooterIconButton(assets.DiscordIconDim, assets.DiscordIcon, fyne.NewSize(14, 14), func() {
		if mw.connectionManager != nil {
			mw.connectionManager.OpenDiscordInvite()
		}
	})

	row := container.NewCenter(container.NewHBox(helpBtn, discordBtn, langBtn))

	bg := canvas.NewRectangle(design.ColorGray950)

	bar := container.NewStack(
		bg,
		view.NewInset(row, 6, 8, 2, 2),
	)

	mw.connectionFooterBar = bar
	return bar
}

func (mw *MainWindow) createDeviceFooterBar() *fyne.Container {
	bg := canvas.NewRectangle(design.ColorGray950)
	bar := container.NewStack(
		bg,
		view.NewInset(container.NewCenter(mw.deviceButtonsPanel), 6, 8, 2, 2),
	)
	mw.deviceFooterBar = bar
	mw.deviceFooterBar.Hide()
	return bar
}

func (mw *MainWindow) updateConnectionFooterVisibility(hasConnections bool) {
	if mw.connectionFooterBar == nil {
		return
	}

	fyne.Do(func() {
		_ = hasConnections
		mw.connectionFooterBar.Show()

		if content := mw.window.Content(); content != nil {
			content.Refresh()
			mw.window.Canvas().Refresh(content)
		}
	})
}

type collapsingBoxLayout struct{}
type optionalLeadingGapLayout struct {
	gap float32
}

func newCollapsingBox(objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(&collapsingBoxLayout{}, objects...)
}

func newOptionalLeadingGap(width float32, content fyne.CanvasObject) *fyne.Container {
	return container.New(&optionalLeadingGapLayout{gap: width}, headerGapSpacer(width), content)
}

func (l *collapsingBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, obj := range objects {
		if !hasVisibleContent(obj) {
			obj.Resize(fyne.NewSize(0, 0))
			continue
		}
		min := obj.MinSize()
		obj.Move(fyne.NewPos(x, (size.Height-min.Height)/2))
		obj.Resize(min)
		x += min.Width
	}
}

func (l *collapsingBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width := float32(0)
	height := float32(0)
	for _, obj := range objects {
		if !hasVisibleContent(obj) {
			continue
		}
		min := obj.MinSize()
		width += min.Width
		if min.Height > height {
			height = min.Height
		}
	}
	return fyne.NewSize(width, height)
}

func (l *optionalLeadingGapLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) != 2 {
		return
	}

	gapObj := objects[0]
	content := objects[1]
	if !hasVisibleContent(content) {
		gapObj.Resize(fyne.NewSize(0, 0))
		content.Resize(fyne.NewSize(0, 0))
		return
	}

	contentMin := content.MinSize()
	gapObj.Move(fyne.NewPos(0, 0))
	gapObj.Resize(fyne.NewSize(l.gap, size.Height))
	content.Move(fyne.NewPos(l.gap, (size.Height-contentMin.Height)/2))
	content.Resize(contentMin)
}

func (l *optionalLeadingGapLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) != 2 || !hasVisibleContent(objects[1]) {
		return fyne.NewSize(0, 0)
	}

	contentMin := objects[1].MinSize()
	return fyne.NewSize(l.gap+contentMin.Width, contentMin.Height)
}

func hasVisibleContent(obj fyne.CanvasObject) bool {
	if obj == nil || !obj.Visible() {
		return false
	}
	if c, ok := obj.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if hasVisibleContent(child) {
				return true
			}
		}
		return false
	}
	return true
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
	mw.deviceButtonsPanel = container.NewHBox(addImageBtn, mountBtn, unmountBtn)
	mw.deviceButtonsPanel.Hide()

	return container.NewBorder(nil, nil, mw.deviceButtonsPanel, nil, nil)
}

// updateDeviceButtonsVisibility обновляет видимость кнопок устройств.
func (mw *MainWindow) updateDeviceButtonsVisibility() {
	if mw.tabs == nil || mw.deviceButtonsPanel == nil || mw.deviceFooterBar == nil {
		return
	}

	fyne.Do(func() {
		if mw.tabs.SelectedIndex() == 0 {
			mw.deviceButtonsPanel.Show()
			mw.deviceFooterBar.Show()
		} else {
			mw.deviceButtonsPanel.Hide()
			mw.deviceFooterBar.Hide()
		}
		mw.deviceButtonsPanel.Refresh()
		mw.deviceFooterBar.Refresh()
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

package gui

import (
	"fmt"
	"image/color"
	"math"
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
	fynetheme "fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const (
	addressBarGap        float32 = 4
	addressBarControlH   float32 = 36
	addressBarActionBtn  float32 = 36
	addressBarHostHideAt float32 = 180
	statusIconSize       float32 = 18
)

type tabsTheme struct {
	base fyne.Theme
}

func (t *tabsTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case fynetheme.ColorNameHover, fynetheme.ColorNamePressed, fynetheme.ColorNameFocus:
		return color.Transparent
	default:
		return t.base.Color(name, variant)
	}
}

func (t *tabsTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *tabsTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *tabsTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}

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
		mw.diskWidget.SetOnButtonsChanged(mw.refreshDeviceFooterButtons)
		if mw.videoWidget != nil {
			mw.diskWidget.SetOnMouseTypeChanged(mw.videoWidget.SetMouseInputMode)
			mw.diskWidget.SetOnVideoConfigRequested(func(devicePath string) {
				mw.videoWidget.ShowVideoDeviceSettings(devicePath, mw.tabs != nil && mw.tabs.SelectedIndex() == 1, false)
			})
			mw.diskWidget.SetOnVideoConnect(func(devicePath string) {
				mw.videoWidget.StartVideoDeviceAsync(devicePath)
			})
			mw.diskWidget.SetOnVideoDisconnect(func() {
				mw.videoWidget.StopVideoAsync()
			})
			mw.videoWidget.SetOnFPSChanged(func(fps float64) {
				mw.currentVideoFPS = fps
				mw.updateVideoIconLabel()
			})
		}
	}
	if mw.backupWidget != nil {
		mw.backupWidget.SetOnStorageInfoUpdate(storageUpdate)
	}

	mw.createStatusBar()
	mainAddressBar := mw.createMainAddressBar()
	connectionAddressBar := mw.createConnectionAddressBar()
	mainFooter := mw.createDeviceFooterBar()
	connectionFooter := mw.createConnectionFooterBar()
	devicesTabTitle := "Devices"
	controlTabTitle := "Control"
	snapshotsTabTitle := "Snapshots"

	mw.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon(devicesTabTitle, assets.USBTabIcon, container.NewThemeOverride(mw.diskWidget.GetContainer(), design.NewBrandTheme())),
		container.NewTabItemWithIcon(controlTabTitle, assets.MonitorTabIcon, container.NewThemeOverride(mw.videoWidget.GetContainer(), design.NewBrandTheme())),
		container.NewTabItemWithIcon(snapshotsTabTitle, assets.SnapshotsTabIcon, container.NewThemeOverride(mw.createBackupFlashTab(), design.NewBrandTheme())),
	)
	mw.applyTabVisualState(0)
	mw.tabs.OnSelected = func(tab *container.TabItem) {
		mw.applyTabVisualState(mw.tabs.SelectedIndex())
		mw.updateDeviceButtonsVisibility()
		if tab != nil && tab.Text == controlTabTitle {
			if mw.videoWidget != nil && !mw.videoWidget.IsStreaming() {
				mw.videoWidget.StartConfiguredVideoAsync()
			}
			return
		}
		if mw.videoWidget != nil && mw.videoWidget.IsStreaming() {
			mw.videoWidget.StopVideoAsync()
		}
	}

	deviceFooterOverlay := container.NewBorder(nil, mainFooter, nil, nil, nil)
	tabsWithTheme := container.NewThemeOverride(mw.tabs, &tabsTheme{base: design.NewBrandTheme()})
	mw.mainContent = container.NewBorder(
		mainAddressBar,
		nil,
		nil,
		nil,
		container.NewStack(tabsWithTheme, deviceFooterOverlay),
	)
	mw.connectionContent = container.NewBorder(
		connectionAddressBar,
		connectionFooter,
		nil,
		nil,
		mw.connectionManager.GetContainer(),
	)

	mw.window.SetContent(mw.connectionContent)
}

func (mw *MainWindow) applyTabVisualState(activeIndex int) {
	if mw == nil || mw.tabs == nil || len(mw.tabs.Items) < 3 {
		return
	}

	mw.tabs.Items[0].Icon = assets.USBTabIcon
	mw.tabs.Items[1].Icon = assets.MonitorTabIcon
	mw.tabs.Items[2].Icon = assets.SnapshotsTabIcon

	switch activeIndex {
	case 0:
		mw.tabs.Items[0].Icon = assets.USBTabIconActive
	case 1:
		mw.tabs.Items[1].Icon = assets.MonitorTabIconActive
	case 2:
		mw.tabs.Items[2].Icon = assets.SnapshotsTabIconActive
	}
	mw.tabs.Refresh()
}

// createAddressBar создает адресную строку.
func (mw *MainWindow) createConnectionAddressBar() *fyne.Container {
	hostField := view.NewFixedHeight(mw.hostEntry, addressBarControlH)
	protocolPanel := view.NewOutlinedControl(mw.protocolDropdown, 0, addressBarControlH)
	connectPanel := container.NewGridWrap(fyne.NewSize(addressBarActionBtn, addressBarControlH), mw.connectionBtn)
	rightPart := container.NewHBox(
		protocolPanel,
		headerGapSpacer(addressBarGap),
		connectPanel,
	)
	row := container.New(&addressBarResponsiveLayout{
		hideHostAt: addressBarHostHideAt,
		keepHostShown: func() bool {
			return true
		},
	}, hostField, rightPart)
	return view.NewHeaderBand("", row)
}

func (mw *MainWindow) createMainAddressBar() *fyne.Container {
	if mw.pcpanelWidget == nil {
		mw.pcpanelWidget = controller.NewPCPanelWidget(mw.window)
	}
	if mw.mainExitBtn == nil {
		mw.mainExitBtn = view.NewHeaderActionButton(mw.handleConnectionToggle)
		mw.mainExitBtn.ApplySpec(view.HeaderActionButtonSpec{
			Fill:       design.ColorSurfaceLight,
			Foreground: design.ColorTextLight,
			Stroke:     color.Transparent,
			Icon:       assets.ExitIcon,
		})
	}
	exitPanel := container.NewGridWrap(fyne.NewSize(addressBarActionBtn, addressBarControlH), mw.mainExitBtn)
	row := container.New(
		&distributedVisibleLayout{minGap: 12},
		mw.pcpanelWidget.GetContainer(),
		mw.sdStorageProgress,
		mw.statusPanel,
		exitPanel,
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

	row := container.NewHBox(helpBtn, discordBtn, langBtn)

	bg := canvas.NewRectangle(design.ColorGray950)

	bar := container.NewStack(
		bg,
		view.NewInset(row, 6, 8, 2, 2),
	)

	mw.connectionFooterBar = bar
	return bar
}

func (mw *MainWindow) createDeviceFooterBar() *fyne.Container {
	bar := view.NewInset(container.NewCenter(mw.deviceButtonsPanel), 6, 8, 2, 2)
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
type addressBarResponsiveLayout struct {
	hideHostAt    float32
	keepHostShown func() bool
}
type distributedVisibleLayout struct {
	minGap float32
}

func newCollapsingBox(objects ...fyne.CanvasObject) *fyne.Container {
	return container.New(&collapsingBoxLayout{}, objects...)
}

func newOptionalLeadingGap(width float32, content fyne.CanvasObject) *fyne.Container {
	return container.New(&optionalLeadingGapLayout{gap: width}, headerGapSpacer(width), content)
}

func newHeaderStatusIcon(resource fyne.Resource) fyne.CanvasObject {
	icon := canvas.NewImageFromResource(resource)
	icon.FillMode = canvas.ImageFillContain
	return container.NewGridWrap(fyne.NewSize(statusIconSize, statusIconSize), icon)
}

func newProtocolIndicator(protocol string) fyne.CanvasObject {
	iconRes := assets.ConnectionStatusIcon
	textColor := design.ColorTextMuted
	if strings.TrimSpace(protocol) != "" {
		iconRes = assets.ConnectionStatusIconActive
		textColor = design.ColorAccent
	}

	label := canvas.NewText(strings.TrimSpace(protocol), textColor)
	label.TextSize = 14
	label.TextStyle.Bold = true

	return container.NewHBox(
		newHeaderStatusIcon(iconRes),
		headerGapSpacer(4),
		container.NewCenter(label),
	)
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func (l *distributedVisibleLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := make([]fyne.CanvasObject, 0, len(objects))
	totalWidth := float32(0)
	maxHeight := float32(0)
	for _, obj := range objects {
		if !hasVisibleContent(obj) {
			obj.Move(fyne.NewPos(0, 0))
			obj.Resize(fyne.NewSize(0, 0))
			continue
		}
		min := obj.MinSize()
		totalWidth += min.Width
		if min.Height > maxHeight {
			maxHeight = min.Height
		}
		visible = append(visible, obj)
	}
	if len(visible) == 0 {
		return
	}
	if len(visible) == 1 {
		min := visible[0].MinSize()
		visible[0].Move(fyne.NewPos(0, maxFloat32(0, (size.Height-min.Height)/2)))
		visible[0].Resize(min)
		return
	}

	gap := l.minGap
	if extra := size.Width - totalWidth; extra > 0 {
		distributed := extra / float32(len(visible)-1)
		if distributed > gap {
			gap = distributed
		}
	}

	x := float32(0)
	for _, obj := range visible {
		min := obj.MinSize()
		obj.Move(fyne.NewPos(x, maxFloat32(0, (size.Height-min.Height)/2)))
		obj.Resize(min)
		x += min.Width + gap
	}
}

func (l *distributedVisibleLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width := float32(0)
	height := float32(0)
	count := 0
	for _, obj := range objects {
		if !hasVisibleContent(obj) {
			continue
		}
		min := obj.MinSize()
		width += min.Width
		if min.Height > height {
			height = min.Height
		}
		count++
	}
	if count > 1 {
		width += float32(count-1) * l.minGap
	}
	return fyne.NewSize(width, height)
}

func (l *addressBarResponsiveLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	host := objects[0]
	right := objects[1]
	rightMin := right.MinSize()
	rightWidth := minFloat32(size.Width, rightMin.Width)
	rightX := size.Width - rightWidth
	if rightX < 0 {
		rightX = 0
	}

	right.Move(fyne.NewPos(rightX, maxFloat32(0, (size.Height-rightMin.Height)/2)))
	right.Resize(fyne.NewSize(rightWidth, minFloat32(size.Height, rightMin.Height)))

	hostWidth := size.Width - rightWidth
	keepShown := l.keepHostShown != nil && l.keepHostShown()
	if !keepShown && hostWidth < l.hideHostAt {
		host.Hide()
		host.Move(fyne.NewPos(0, 0))
		host.Resize(fyne.NewSize(0, 0))
		return
	}

	host.Show()
	hostMin := host.MinSize()
	host.Move(fyne.NewPos(0, maxFloat32(0, (size.Height-hostMin.Height)/2)))
	host.Resize(fyne.NewSize(hostWidth, minFloat32(size.Height, hostMin.Height)))
}

func (l *addressBarResponsiveLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	hostMin := objects[0].MinSize()
	rightMin := objects[1].MinSize()
	height := maxFloat32(hostMin.Height, rightMin.Height)
	return fyne.NewSize(rightMin.Width, height)
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
	mw.videoIcon.OnTapped = func() {
		mw.showVideoMenu()
	}
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

	mountBtn, unmountBtn, _ := mw.diskWidget.GetButtons()
	mw.deviceMountBtn = mountBtn
	mw.deviceUnmountBtn = unmountBtn
	mw.deviceVideoBtn = view.NewDeviceActionButton(i18n.Current.VideoStreamActiveButton, nil, func() {
		if mw.tabs != nil && len(mw.tabs.Items) > 1 {
			mw.tabs.Select(mw.tabs.Items[1])
		}
	})
	mw.deviceVideoBtn.SetColors(design.ColorAccent, design.ColorAccentHover, design.ColorBackground, design.ColorBackground)
	mw.deviceVideoBtn.Hide()
	mw.deviceButtonsPanel = container.NewHBox()
	mw.refreshDeviceFooterButtons()
	mw.deviceButtonsPanel.Hide()
	mw.updateVideoIconLabel()

	return container.NewBorder(nil, nil, mw.deviceButtonsPanel, nil, nil)
}

func (mw *MainWindow) refreshDeviceFooterButtons() {
	if mw.deviceButtonsPanel == nil {
		return
	}

	buildGap := func() fyne.CanvasObject {
		gap := canvas.NewRectangle(color.Transparent)
		gap.SetMinSize(fyne.NewSize(16, 1))
		return gap
	}

	objects := make([]fyne.CanvasObject, 0, 5)
	appendVisible := func(obj fyne.CanvasObject) {
		if obj == nil || !obj.Visible() {
			return
		}
		if len(objects) > 0 {
			objects = append(objects, buildGap())
		}
		objects = append(objects, obj)
	}

	appendVisible(mw.deviceMountBtn)
	appendVisible(mw.deviceUnmountBtn)
	appendVisible(mw.deviceVideoBtn)

	mw.deviceButtonsPanel.Objects = objects
	mw.deviceButtonsPanel.Refresh()
	if mw.deviceFooterBar != nil {
		mw.deviceFooterBar.Refresh()
	}
}

func buildHeaderStatusIndicators(protocol string, keyboardConnected, mouseConnected bool) []fyne.CanvasObject {
	indicatorGap := func() fyne.CanvasObject {
		gap := canvas.NewRectangle(color.Transparent)
		gap.SetMinSize(fyne.NewSize(10, 1))
		return gap
	}

	protocolText, _, _ := protocolButtonState(protocol)
	items := []fyne.CanvasObject{
		newHeaderStatusIcon(func() fyne.Resource {
			if keyboardConnected {
				return assets.KeyboardIconActive
			}
			return assets.KeyboardIcon
		}()),
		indicatorGap(),
		newHeaderStatusIcon(func() fyne.Resource {
			if mouseConnected {
				return assets.MouseIconActive
			}
			return assets.MouseIcon
		}()),
		indicatorGap(),
		newProtocolIndicator(protocolText),
	}

	return items
}

// updateDeviceButtonsVisibility обновляет видимость кнопок устройств.
func (mw *MainWindow) updateDeviceButtonsVisibility() {
	if mw.tabs == nil || mw.deviceButtonsPanel == nil || mw.deviceFooterBar == nil {
		return
	}

	fyne.Do(func() {
		mw.refreshDeviceFooterButtons()
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

	videoStreaming := mw.videoWidget != nil && mw.videoWidget.IsStreaming()

	if mw.usbClient != nil {
		deviceInfo, err := mw.usbClient.GetDeviceInfo()
		if err == nil {
			logrus.Debugf("🔍 updateStatusBar: найдено %d устройств", len(deviceInfo.Devices))
			for _, device := range deviceInfo.Devices {
				logrus.Debugf("🔍 Устройство: Type=%s, Status=%s, Name=%s, ProductName=%s",
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
						logrus.Debugf("📸 Найден снапшот: Type=%s, Name=%s, ProductName=%s", device.Type, device.Name, device.ProductName)
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
						logrus.Debugf("📸 Найден подключенный снапшот через API снапшотов: %s", snapshot.Name)
						break
					}
				}
			}
		}
	}

	logrus.Debugf("🔍 Статусы: keyboard=%v, mouse=%v, rndis=%v, cdrom=%v, backup=%v, snapshot=%v",
		keyboardConnected, mouseConnected, rndisConnected, cdromConnected, backupConnected, snapshotConnected)

	fyne.Do(func() {
		if mw.deviceVideoBtn != nil {
			if videoStreaming {
				mw.deviceVideoBtn.Show()
				mw.deviceVideoBtn.Enable()
			} else {
				mw.deviceVideoBtn.Hide()
			}
			mw.refreshDeviceFooterButtons()
		}

		mw.statusPanel.Objects = buildHeaderStatusIndicators(
			mw.connectedProtocol,
			keyboardConnected,
			mouseConnected,
		)
		mw.statusPanel.Refresh()
	})
}

func (mw *MainWindow) updateVideoIconLabel() {
	if mw.videoIcon == nil {
		return
	}

	label := "0 FPS"
	if mw.currentVideoFPS > 0 {
		rounded := math.Round(mw.currentVideoFPS*10) / 10
		if rounded == math.Trunc(rounded) {
			label = fmt.Sprintf("%.0f FPS", rounded)
		} else {
			label = fmt.Sprintf("%.1f FPS", rounded)
		}
	}

	fyne.Do(func() {
		mw.videoIcon.SetText(label)
		mw.videoIcon.Refresh()
	})
}

func (mw *MainWindow) showVideoMenu() {
	if mw.videoIcon == nil || mw.videoWidget == nil {
		return
	}

	items := []view.StyledMenuItem{
		{
			Label: i18n.Current.SettingsAction,
			OnTap: func() {
				mw.videoWidget.ShowCurrentVideoSettings(false)
			},
		},
	}

	if mw.videoWidget.IsStreaming() {
		items = append(items, view.StyledMenuItem{
			Label: i18n.Current.FullscreenAction,
			OnTap: func() {
				mw.videoWidget.ShowFullscreen()
			},
		})
	}

	view.ShowStyledMenu(mw.videoIcon, items)
}

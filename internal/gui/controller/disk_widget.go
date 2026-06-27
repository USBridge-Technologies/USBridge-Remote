package controller

import (
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// DiskWidget виджет управления устройствами и ISO
type DiskWidget struct {
	container   *fyne.Container
	devicesList *view.DevicesListView
	ui          *view.DiskWidgetUI
	mountBtn    *widget.Button
	unmountBtn  *widget.Button
	window      fyne.Window
	app         fyne.App

	// Компактные кнопки для статус бара
	compactMountBtn   *view.DeviceActionButton
	compactUnmountBtn *view.DeviceActionButton

	// Данные
	localDrives    []*models.LocalDrive
	localFiles     []*models.DiskInfo
	videoDevices   []models.SystemDevice
	audioDevices   []models.SystemDevice
	gamepadDevices []platform.GamepadDevice
	sdSpaceInfo    *models.ISOSpaceInfo

	// Gamepad capture
	activeCaptures    map[string]*platform.GamepadCapture
	moonlightProvider moonlightProvider

	onStorageInfoUpdate func(usedPct float64, available, total int64)
	userImages          []*models.DiskInfo
	allDrives           []DriveItem
	mountedDevices      []*models.DeviceInfo
	selectedDrive       *DriveItem
	selectedItems       map[int]bool
	selectedItemsMu     sync.RWMutex
	devicesTraceBudget  int
	lastDrivesTraceSig  string
	preferredMouseMode    string
	observedMouseMode     string
	preferredDisplayIndex int // 0-based display index for absolute mouse (0 = first)
	preferredDisplayCount int // total display count for absolute mouse (0/1 = single)

	loadingLocalDrives    atomic.Bool
	loadingLocalFiles     atomic.Bool
	loadingVideoDevices   atomic.Bool
	loadingAudioDevices   atomic.Bool
	loadingMountedInfo    atomic.Bool
	devicesRefreshPending atomic.Bool
	devicesRefreshQueued  atomic.Bool
	userOperationInFlight atomic.Bool
	apiMountInProgress    atomic.Bool
	audioAutoStarted      atomic.Bool
	audioConnectGen       atomic.Uint64 // incremented on every manual audio connect to cancel in-flight auto-start
	pendingAudioPath      atomic.Value  // string: effective audio path while switch is in-flight; cleared after onAudioConnect returns
	imagePickerInFlight   atomic.Bool
	// pendingCombine guards the scheduleCombine debounce timer.
	pendingCombine atomic.Bool

	refreshMu          sync.Mutex
	lastDevicesRefresh time.Time

	// UI cache — reused across refreshes to avoid recreating row widgets.
	rowsCache  map[string]fyne.CanvasObject
	cardsCache map[string]fyne.CanvasObject

	// Сервисы
	nbdServers   map[string]service.NBDRunner
	usbClient    *api.USBClient
	updateStatus func()
	frpService   *service.FRPService

	// Конфигурация
	config         *models.AppConfig
	scanPaths      []string
	supportedTypes []string

	onMouseTypeChanged      func(mouseType string)
	onMouseModeReconfigured func()
	onVideoConfigRequested  func(devicePath string)
	onVideoConnect          func(devicePath string)
	onVideoDisconnect       func()
	onAudioConnect          func(devicePath string)
	onAudioDisconnect       func()
	onUSBAudioConnect       func(mode string)
	onButtonsChanged        func()

	safHelper *platform.SAFHelper

	agentOS string
}

// MaxDevicesToMount максимальное количество устройств для одновременного выбора
const MaxDevicesToMount = 5

var rndisModeOptions = []string{"auto", "wifirouter", "etherouter", "etherbridge"}

const (
	gamepadModeDirectInput = "directinput"
	gamepadModeXInput      = "xinput"
)

func normalizeGamepadMode(mode string) string {
	if strings.ToLower(mode) == gamepadModeXInput {
		return gamepadModeXInput
	}
	return gamepadModeDirectInput
}

func gamepadModeLabel(mode string) string {
	if mode == gamepadModeXInput {
		return i18n.Current.DeviceXInput
	}
	return i18n.Current.DeviceDirectInput
}

func normalizeRNDISMode(mode string) string {
	switch strings.ToLower(mode) {
	case "auto", "wifirouter", "etherouter", "etherbridge":
		return strings.ToLower(mode)
	case "router":
		return "auto"
	case "bridge":
		return "etherbridge"
	default:
		return "auto"
	}
}

// DriveItem объединяет локальные устройства из API, локальные файлы и клавиатуру
type DriveItem struct {
	Name           string
	Size           string
	Source         string // "api", "local", "user", "keyboard", "mouse", "rndis", "gamepad", "video", "audio", "usbaudio"
	IsMounted      bool
	LocalDrive     *models.LocalDrive
	DiskInfo       *models.DiskInfo
	IsKeyboard     bool
	IsMouse        bool
	MouseType      string
	IsRNDIS        bool
	RNDISMode      string
	IsVideo        bool
	VideoDevice    *models.SystemDevice
	IsGamepad        bool
	GamepadID        string
	GamepadMode      string
	GamepadVendorID  string
	GamepadProductID string
	IsAudio        bool
	AudioDevice    *models.SystemDevice
	IsUSBAudio     bool
	USBAudioMode   string // "uac1" or "uac2"
	ReadOnly       bool
	UploadProgress float64
	UploadSpeed    float64
	IsUploading    bool
	IsMounting     bool
}

// NewDiskWidget создает новый виджет устройств
func NewDiskWidget(usbClient *api.USBClient, updateStatus func(), app fyne.App, config *models.AppConfig) *DiskWidget {
	supportedTypes := []string{".iso", ".img", ".vmdk", ".vdi", ".qcow", ".qcow2", ".raw", ".vmi"}
	scanPaths := []string{"./isos", "/home/user/isos", "/mnt/isos"}
	if config != nil {
		if len(config.SupportedTypes) > 0 {
			supportedTypes = config.SupportedTypes
		}
		if len(config.ScanPaths) > 0 {
			scanPaths = config.ScanPaths
		}
	}
	dw := &DiskWidget{
		nbdServers:         make(map[string]service.NBDRunner),
		usbClient:          usbClient,
		updateStatus:       updateStatus,
		app:                app,
		config:             config,
		localDrives:        make([]*models.LocalDrive, 0),
		localFiles:         make([]*models.DiskInfo, 0),
		videoDevices:       make([]models.SystemDevice, 0),
		audioDevices:       make([]models.SystemDevice, 0),
		userImages:         make([]*models.DiskInfo, 0),
		allDrives:          make([]DriveItem, 0),
		mountedDevices:     make([]*models.DeviceInfo, 0),
		selectedItems:      make(map[int]bool),
		devicesTraceBudget: 20,
		preferredMouseMode:   defaultMouseMode(),
		preferredDisplayCount: 1,
		scanPaths:          scanPaths,
		supportedTypes:     supportedTypes,
		safHelper:          platform.GetSAFHelper(app),
		rowsCache:          make(map[string]fyne.CanvasObject),
		cardsCache:         make(map[string]fyne.CanvasObject),
	}

	if runtime.GOOS == "android" && dw.safHelper != nil {
		logrus.Info("📱 [ANDROID-INIT] SAF global context will be initialized on window set")
	}

	dw.loadDisplayConfig()
	dw.createInterface()
	dw.startPeriodicRefresh()
	go dw.loadGamepadDevices()

	return dw
}

// SetWindow устанавливает окно для диалогов
func (dw *DiskWidget) SetWindow(window fyne.Window) {
	dw.window = window
	if runtime.GOOS == "android" && dw.safHelper != nil {
		go func() {
			logrus.Info("📱 [ANDROID-INIT] Initializing SAF global context and loading saved images")
			dw.safHelper.SetContext()

			dw.updateUIAsync(func() {
				dw.loadUserImagesFromPreferences()
				dw.loadLocalDrives()
				dw.loadLocalFiles()
				dw.loadVideoDevices()
				dw.combineDrives()
				dw.loadMountedDevices()
			})
		}()
	}
}

// createInterface создает интерфейс виджета
func (dw *DiskWidget) createInterface() {
	dw.ui = view.NewDiskWidgetUI(nil, dw.buildDeviceCards, nil)
	dw.devicesList = dw.ui.DevicesList

	dw.mountBtn = widget.NewButton(i18n.Current.MountButton, dw.handleMount)
	dw.unmountBtn = widget.NewButton(i18n.Current.UnmountButton, dw.handleUnmount)
}

func (dw *DiskWidget) openQuickStartDocs() {
	const docsURL = "https://www.usbridge.io/docs/getting-started/quick-start-guide/"

	uri, err := url.Parse(docsURL)
	if err != nil {
		logrus.Errorf("failed to parse docs URL %q: %v", docsURL, err)
		return
	}

	app := dw.app
	if app == nil {
		app = fyne.CurrentApp()
	}
	if app == nil {
		logrus.Errorf("failed to open docs URL: fyne app is nil")
		return
	}

	go func() {
		if err := openExternalURL(app, uri); err != nil {
			logrus.Errorf("failed to open docs URL %q: %v", docsURL, err)
		}
	}()
}

func (dw *DiskWidget) getDriveUniqueID(drive DriveItem) string {
	switch {
	case drive.IsKeyboard:
		return "keyboard"
	case drive.IsMouse:
		return "mouse"
	case drive.IsRNDIS:
		return "rndis"
	case drive.IsGamepad:
		if drive.GamepadID != "" {
			return "gamepad:" + drive.GamepadID
		}
		return "gamepad"
	case drive.IsVideo && drive.VideoDevice != nil:
		return "video:" + drive.VideoDevice.Path
	case drive.IsAudio && drive.AudioDevice != nil:
		return "audio:" + drive.AudioDevice.Path
	case drive.IsUSBAudio:
		return "usbaudio"
	case drive.LocalDrive != nil:
		return "api:" + drive.LocalDrive.Name + ":" + drive.LocalDrive.SourceType
	case drive.DiskInfo != nil:
		return drive.Source + ":" + drive.DiskInfo.Path
	default:
		return "raw:" + drive.Name
	}
}

// SetOnStorageInfoUpdate устанавливает callback для обновления прогрессбара в main window
func (dw *DiskWidget) SetOnStorageInfoUpdate(fn func(usedPct float64, available, total int64)) {
	dw.onStorageInfoUpdate = fn
}

func (dw *DiskWidget) GetISODirectory() string {
	if dw.sdSpaceInfo == nil {
		return ""
	}
	return dw.sdSpaceInfo.ISODirectory
}

// SetOnMouseTypeChanged устанавливает callback при смене типа манипулятора после запуска устройства.
func (dw *DiskWidget) SetOnMouseTypeChanged(fn func(mouseType string)) {
	dw.onMouseTypeChanged = fn
}

func (dw *DiskWidget) SetOnMouseModeReconfigured(fn func()) {
	dw.onMouseModeReconfigured = fn
}

// GetMouseMode возвращает текущий выбранный режим указателя.
func (dw *DiskWidget) GetMouseMode() string {
	if mode := normalizeMouseMode(dw.preferredMouseMode); mode != "" {
		return mode
	}
	for _, drive := range dw.allDrives {
		if drive.IsMouse {
			return normalizeMouseMode(drive.MouseType)
		}
	}
	return defaultMouseMode()
}

// GetDisplayConfig возвращает текущую конфигурацию дисплея для абсолютного режима мыши.
func (dw *DiskWidget) GetDisplayConfig() (displayIndex, displayCount int) {
	idx := dw.preferredDisplayIndex
	cnt := dw.preferredDisplayCount
	if cnt < 1 {
		cnt = 1
	}
	return idx, cnt
}

// SetDisplayConfig сохраняет конфигурацию дисплея и записывает в preferences.
func (dw *DiskWidget) SetDisplayConfig(displayIndex, displayCount int) {
	if displayCount < 1 {
		displayCount = 1
	}
	dw.preferredDisplayIndex = displayIndex
	dw.preferredDisplayCount = displayCount
	if dw.app != nil {
		dw.app.Preferences().SetInt("mouse.display.index", displayIndex)
		dw.app.Preferences().SetInt("mouse.display.count", displayCount)
	}
}

// loadDisplayConfig загружает конфигурацию дисплея из preferences.
func (dw *DiskWidget) loadDisplayConfig() {
	if dw.app == nil {
		return
	}
	dw.preferredDisplayIndex = dw.app.Preferences().IntWithFallback("mouse.display.index", 0)
	dw.preferredDisplayCount = dw.app.Preferences().IntWithFallback("mouse.display.count", 1)
}

// GetRNDISMode возвращает текущий выбранный режим RNDIS.
func (dw *DiskWidget) GetRNDISMode() string {
	for _, drive := range dw.allDrives {
		if drive.IsRNDIS {
			return normalizeRNDISMode(drive.RNDISMode)
		}
	}
	return normalizeRNDISMode("auto")
}

// SetMouseMode применяет режим указателя через ту же логику, что и экран устройств.
func (dw *DiskWidget) SetMouseMode(mode string) {
	if dw == nil || dw.controlsLocked() {
		return
	}
	mode = normalizeMouseMode(mode)
	for i := range dw.allDrives {
		if dw.allDrives[i].IsMouse {
			dw.applyMouseModeSelection(i, mode)
			return
		}
	}
	dw.combineDrives()
	for i := range dw.allDrives {
		if dw.allDrives[i].IsMouse {
			dw.applyMouseModeSelection(i, mode)
			return
		}
	}
}

// SetRNDISMode применяет режим сетевой карты так же, как в карточке устройства.
func (dw *DiskWidget) SetRNDISMode(mode string) {
	if dw == nil || dw.controlsLocked() {
		return
	}
	mode = normalizeRNDISMode(mode)
	updated := false
	for i := range dw.allDrives {
		if dw.allDrives[i].IsRNDIS {
			dw.allDrives[i].RNDISMode = mode
			updated = true
		}
	}
	if updated {
		dw.requestDevicesRefresh()
	}
}

func (dw *DiskWidget) SetOnVideoConfigRequested(fn func(devicePath string)) {
	dw.onVideoConfigRequested = fn
}

func (dw *DiskWidget) SetOnVideoConnect(fn func(devicePath string)) {
	dw.onVideoConnect = fn
}

func (dw *DiskWidget) SetOnVideoDisconnect(fn func()) {
	dw.onVideoDisconnect = fn
}

func (dw *DiskWidget) SetOnAudioConnect(fn func(devicePath string)) {
	dw.onAudioConnect = fn
}

func (dw *DiskWidget) SetOnAudioDisconnect(fn func()) {
	dw.onAudioDisconnect = fn
}

func (dw *DiskWidget) SetOnUSBAudioConnect(fn func(mode string)) {
	dw.onUSBAudioConnect = fn
}

func (dw *DiskWidget) setPreferredAudioDevice(device models.SystemDevice) {
	if strings.TrimSpace(device.Path) == "" {
		return
	}
	dw.audioConnectGen.Add(1) // cancel any in-flight auto-start goroutine
	go func() {
		// Set pending path before fyne.Do to close the race window where combineDrives
		// could fire between the optimistic UI update and the pending path being set,
		// causing the inference block to re-select UAC from stale server state.
		dw.pendingAudioPath.Store(device.Path)
		usbAudioWasMounted := false
		fyne.Do(func() {
			for i := range dw.allDrives {
				if dw.allDrives[i].IsAudio && dw.allDrives[i].AudioDevice != nil {
					dw.allDrives[i].IsMounted = dw.allDrives[i].AudioDevice.Path == device.Path
				}
				if dw.allDrives[i].IsUSBAudio && dw.allDrives[i].IsMounted {
					usbAudioWasMounted = true
					dw.allDrives[i].IsMounted = false
				}
			}
			dw.requestDevicesRefresh()
		})
		logrus.Infof("💾 [AUDIO-PREFS] Selected device: %s (%s)", device.Name, device.Path)
		if usbAudioWasMounted {
			dw.disconnectUSBAudioGadget()
		}
		if dw.onAudioDisconnect != nil {
			dw.onAudioDisconnect()
		}
		if dw.onAudioConnect != nil {
			dw.onAudioConnect(device.Path)
		}
		dw.pendingAudioPath.Store("")
	}()
}

func (dw *DiskWidget) selectUSBAudio(mode string) {
	dw.audioConnectGen.Add(1) // cancel any in-flight auto-start goroutine
	go func() {
		// Set pending path before fyne.Do to close the race window (same as setPreferredAudioDevice).
		dw.pendingAudioPath.Store("uac")
		fyne.Do(func() {
			for i := range dw.allDrives {
				if dw.allDrives[i].IsAudio && dw.allDrives[i].AudioDevice != nil {
					dw.allDrives[i].IsMounted = false
				}
				if dw.allDrives[i].IsUSBAudio {
					dw.allDrives[i].IsMounted = true
				}
			}
			dw.requestDevicesRefresh()
		})
		logrus.Infof("💾 [USB-AUDIO] Selecting USB Audio Codec mode=%s", mode)
		if dw.onAudioDisconnect != nil {
			dw.onAudioDisconnect()
		}
		if dw.onUSBAudioConnect != nil {
			// Blocks until USB gadget teardown+rebuild completes (synchronous handler).
			dw.onUSBAudioConnect(mode)
		}
		// Brief wait for PulseAudio's udev-detect to register the recreated UAC card.
		time.Sleep(400 * time.Millisecond)
		// Start capturing audio from the UAC gadget so Sunshine streams it.
		if dw.onAudioConnect != nil {
			dw.onAudioConnect("uac")
		}
		dw.pendingAudioPath.Store("")
	}()
}

// disconnectUSBAudioGadget sends a batch-start without the usbaudio item,
// effectively removing the USB audio gadget from the host while keeping other devices.
// Must be called from a goroutine (not the Fyne event thread).
func (dw *DiskWidget) disconnectUSBAudioGadget() {
	if dw.usbClient == nil {
		return
	}
	var keepDrives []DriveItem
	fyne.Do(func() {
		for _, drive := range dw.allDrives {
			if drive.IsMounted && !drive.IsUSBAudio && !drive.IsVideo && !drive.IsAudio {
				keepDrives = append(keepDrives, drive)
			}
		}
	})
	var keepRequests []models.DeviceStartRequest
	for _, drive := range keepDrives {
		req, err := dw.buildDeviceRequestForDrive(drive, true)
		if err != nil || req == nil {
			logrus.Warnf("⚠️ [USB-AUDIO-DISC] Skip %s: %v", drive.Name, err)
			continue
		}
		keepRequests = append(keepRequests, *req)
	}
	if len(keepRequests) == 0 {
		// No other gadget devices remain — stop the gadget entirely instead of
		// sending {"devices":null} which the server rejects with 400.
		if err := dw.usbClient.StopAllDevices(); err != nil {
			logrus.Errorf("⚠️ [USB-AUDIO-DISC] Failed to stop USB gadget: %v", err)
		}
		return
	}
	if _, err := dw.startDevicesWithRetry(models.DeviceStartBatchRequest(keepRequests), false); err != nil {
		logrus.Errorf("⚠️ [USB-AUDIO-DISC] Failed to disconnect USB audio gadget: %v", err)
	}
}

func (dw *DiskWidget) isPreferredAudioDrive(drive DriveItem) bool {
	if drive.AudioDevice == nil {
		return false
	}
	for _, candidate := range dw.allDrives {
		if candidate.IsAudio && candidate.AudioDevice != nil && candidate.IsMounted {
			return candidate.AudioDevice.Path == drive.AudioDevice.Path
		}
	}
	return false
}

func (dw *DiskWidget) SetOnButtonsChanged(fn func()) {
	dw.onButtonsChanged = fn
}

func (dw *DiskWidget) controlsLocked() bool {
	return dw.userOperationInFlight.Load() || dw.apiMountInProgress.Load()
}

func (dw *DiskWidget) setUserOperationInFlight(inFlight bool) {
	dw.userOperationInFlight.Store(inFlight)
	dw.updateButtons()
	dw.requestDevicesRefresh()
}

func (dw *DiskWidget) setAPIMountInProgress(inFlight bool) {
	dw.apiMountInProgress.Store(inFlight)
	dw.updateButtons()
	dw.requestDevicesRefresh()
}

func (dw *DiskWidget) setPreferredVideoDevice(device models.SystemDevice) {
	if strings.TrimSpace(device.Path) == "" {
		return
	}
	go func() {
		cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
		cfg.DevicePath = device.Path
		cfg.DeviceName = device.Name
		saveVideoDeviceConfig(cfg)
		logrus.Infof("💾 [VIDEO-PREFS] Saved preferred device: %s (%s)", device.Name, device.Path)
	}()
}

// selectVideoDevice saves the preferred device and, if video is currently
// streaming, reconnects to the new device — mirrors setPreferredAudioDevice.
func (dw *DiskWidget) selectVideoDevice(device models.SystemDevice) {
	if strings.TrimSpace(device.Path) == "" {
		return
	}
	go func() {
		// Optimistic UI: mark the selected device as mounted, clear others.
		fyne.Do(func() {
			for i := range dw.allDrives {
				if dw.allDrives[i].IsVideo && dw.allDrives[i].VideoDevice != nil {
					dw.allDrives[i].IsMounted = dw.allDrives[i].VideoDevice.Path == device.Path
				}
			}
			dw.requestDevicesRefresh()
		})
		cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
		cfg.DevicePath = device.Path
		cfg.DeviceName = device.Name
		saveVideoDeviceConfig(cfg)
		logrus.Infof("💾 [VIDEO-SELECT] Selected device: %s (%s)", device.Name, device.Path)
		if dw.onVideoDisconnect != nil {
			dw.onVideoDisconnect()
		}
		if dw.onVideoConnect != nil {
			dw.onVideoConnect(device.Path)
		}
	}()
}

func (dw *DiskWidget) isPreferredVideoDrive(drive DriveItem) bool {
	if drive.VideoDevice == nil {
		return false
	}
	selectedPath := strings.TrimSpace(selectedVideoDevicePath())
	if selectedPath != "" {
		return selectedPath == drive.VideoDevice.Path
	}
	for _, candidate := range dw.allDrives {
		if candidate.IsVideo && candidate.VideoDevice != nil {
			return candidate.VideoDevice.Path == drive.VideoDevice.Path
		}
	}
	return false
}

func (dw *DiskWidget) applyMouseModeSelection(rowID int, newMode string) {
	if rowID < 0 || rowID >= len(dw.allDrives) {
		return
	}
	newMode = normalizeMouseMode(newMode)
	drive := &dw.allDrives[rowID]
	if !drive.IsMouse || drive.MouseType == newMode {
		return
	}

	previousMode := drive.MouseType
	previousPreferredMode := dw.preferredMouseMode
	dw.preferredMouseMode = newMode
	drive.MouseType = newMode
	if dw.onMouseTypeChanged != nil {
		dw.onMouseTypeChanged(newMode)
	}
	if !dw.isMouseMountedActual() {
		dw.requestDevicesRefresh()
		return
	}

	if dw.hasMountedStorageDevicesActual() {
		message := "Changing mouse mode will rebuild the USB gadget and reconnect mounted disks. Continue?"
		view.ShowConfirmYesLeftDanger(i18n.Current.Confirmation, message, func(ok bool) {
			if !ok {
				dw.preferredMouseMode = previousPreferredMode
				if rowID < len(dw.allDrives) && dw.allDrives[rowID].IsMouse {
					dw.allDrives[rowID].MouseType = previousMode
					dw.requestDevicesRefresh()
				}
				if dw.onMouseTypeChanged != nil {
					dw.onMouseTypeChanged(previousPreferredMode)
				}
				return
			}
			dw.reconfigureMountedDevicesForMouseMode(newMode)
		}, dw.window)
		return
	}

	dw.reconfigureMountedDevicesForMouseMode(newMode)
}

func (dw *DiskWidget) isMouseMountedActual() bool {
	for _, device := range dw.mountedDevices {
		if device.Status != "connected" {
			continue
		}
		if isMouseDeviceType(device.Type) {
			return true
		}
	}
	return false
}

func (dw *DiskWidget) hasMountedStorageDevicesActual() bool {
	for _, device := range dw.mountedDevices {
		if device.Status != "connected" {
			continue
		}
		switch device.Type {
		case "local", "nbd", "mtp":
			return true
		}
	}
	return false
}

func (dw *DiskWidget) hasMountedStorageDevices() bool {
	for _, drive := range dw.allDrives {
		if !drive.IsMounted || drive.IsVideo || drive.IsAudio || drive.IsKeyboard || drive.IsMouse || drive.IsRNDIS {
			continue
		}
		return true
	}
	return false
}

// Refresh сбрасывает UI-кэш и запускает все загрузчики данных заново.
func (dw *DiskWidget) Refresh() {
	dw.refreshMu.Lock()
	dw.rowsCache = make(map[string]fyne.CanvasObject)
	dw.cardsCache = make(map[string]fyne.CanvasObject)
	dw.refreshMu.Unlock()

	dw.loadLocalDrives()
	dw.loadLocalFiles()
	dw.loadVideoDevices()
	dw.loadAudioDevices()
	dw.loadMountedDevices()
	go dw.loadGamepadDevices()
}

// GetContainer возвращает контейнер виджета
func (dw *DiskWidget) GetContainer() fyne.CanvasObject {
	return dw.ui.Container
}

// GetButtons возвращает компактные кнопки управления для размещения в statusBar
func (dw *DiskWidget) GetButtons() (mount, unmount, addImage fyne.CanvasObject) {
	if dw.compactMountBtn == nil {
		dw.compactMountBtn = view.NewDeviceActionButton(i18n.Current.ConnectButton, nil, dw.handleMount)
		dw.compactMountBtn.SetColors(design.ColorAccent, design.ColorAccentHover, design.ColorBackground, design.ColorBackground)
		dw.compactUnmountBtn = view.NewDeviceActionButton(i18n.Current.DisconnectButton, nil, dw.handleUnmount)
		dw.updateButtons()
	}
	addImageBtn := widget.NewButton(i18n.Current.AddImageButton, dw.handleAddImage)
	return dw.compactMountBtn, dw.compactUnmountBtn, addImageBtn
}

// setButtonsEnabled включает/выключает кнопки управления во время операций
func (dw *DiskWidget) setButtonsEnabled(enabled bool) {
	if enabled {
		dw.updateButtons()
	} else {
		fyne.Do(func() {
			dw.mountBtn.Disable()
			dw.unmountBtn.Disable()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Disable()
			}
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Disable()
			}
		})
	}
}

// setMountingStateByExportNames устанавливает IsMounting для устройств по именам экспортов
func (dw *DiskWidget) setMountingStateByExportNames(exportNames map[string]bool, mounting bool) {
	for i := range dw.allDrives {
		name := ""
		if dw.allDrives[i].DiskInfo != nil {
			name = dw.allDrives[i].DiskInfo.Name
		} else if dw.allDrives[i].LocalDrive != nil {
			name = dw.allDrives[i].LocalDrive.Name
		} else {
			name = dw.allDrives[i].Name
		}
		if exportNames[name] || exportNames[filepath.Base(name)] {
			dw.allDrives[i].IsMounting = mounting
		}
	}
}

// updateUIAsync безопасно обновляет UI из горутины
func (dw *DiskWidget) updateUIAsync(updateFunc func()) {
	fyne.Do(updateFunc)
}

// updateStatusAsync логирует статусное сообщение
func (dw *DiskWidget) updateStatusAsync(status string) {
	logrus.Info(status)
}

// showErrorAsync показывает ошибку из горутины
func (dw *DiskWidget) showErrorAsync(err error) {
	logrus.Errorf("Ошибка: %v", err)
	dw.updateUIAsync(func() {
		if dw.window != nil {
			view.ShowErrorDialog(err, dw.window)
		}
	})
}

// showWarningAsync показывает предупреждение из горутины
func (dw *DiskWidget) showWarningAsync(title, message string) {
	dw.updateUIAsync(func() {
		if dw.window != nil {
			view.ShowInfoDialog(title, message, dw.window)
		}
	})
}

// SetFRPService устанавливает FRP сервис для проверки перед монтированием NBD
func (dw *DiskWidget) SetFRPService(frp *service.FRPService) {
	dw.frpService = frp
}

// UpdateClient обновляет USB клиент. На отключение — немедленно очищает данные;
// на подключение — запускает полный цикл загрузки.
func (dw *DiskWidget) UpdateClient(usbClient *api.USBClient) {
	dw.usbClient = usbClient
	dw.agentOS = ""
	dw.audioAutoStarted.Store(false)
	if usbClient == nil {
		fyne.Do(func() {
			dw.localDrives = nil
			dw.mountedDevices = nil
			dw.audioDevices = nil
			dw.sdSpaceInfo = nil
			dw.updateSDStorageInfo()
			dw.stopAllGamepadCaptures()
			dw.combineDrives()
			dw.requestDevicesRefresh()
		})
		return
	}
	dw.Refresh()
}

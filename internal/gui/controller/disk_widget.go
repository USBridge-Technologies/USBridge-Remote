package controller

import (
	"fmt"
	"image/color"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
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
	localDrives    []*models.LocalDrive // Устройства из API
	localFiles     []*models.DiskInfo   // Локальные файлы из папки isos
	videoDevices   []models.SystemDevice
	gamepadDevices []platform.GamepadDevice // Геймпады в системе
	sdSpaceInfo    *models.ISOSpaceInfo     // Информация о месте на SD-карте (при монтировании)

	// Gamepad capture
	activeCaptures   map[string]*platform.GamepadCapture
	moonlightProvider moonlightProvider

	// Callback для обновления общего прогрессбара в main window (место на флешке)
	onStorageInfoUpdate   func(usedPct float64, available, total int64)
	userImages            []*models.DiskInfo // Образы, добавленные пользователем
	allDrives             []DriveItem        // Объединенный список
	mountedDevices        []*models.DeviceInfo
	selectedDrive         *DriveItem
	selectedItems         map[int]bool // Множественный выбор элементов
	selectedItemsMu       sync.RWMutex // Мутекс для защиты selectedItems
	devicesTraceBudget    int
	lastDrivesTraceSig    string
	preferredMouseMode    string
	observedMouseMode     string
	loadingLocalDrives    atomic.Bool
	loadingLocalFiles     atomic.Bool
	loadingVideoDevices   atomic.Bool
	loadingMountedInfo    atomic.Bool
	devicesRefreshPending atomic.Bool
	devicesRefreshQueued  atomic.Bool
	userOperationInFlight atomic.Bool
	apiMountInProgress    atomic.Bool
	imagePickerInFlight   atomic.Bool
	refreshMu             sync.Mutex
	lastDevicesRefresh    time.Time
	
	// UI Cache
	rowsCache  map[string]fyne.CanvasObject
	cardsCache map[string]fyne.CanvasObject

	// Сервисы
	nbdServers   map[string]service.NBDRunner // Карта NBD (go-nbd или qemu-nbd) по именам экспортов
	usbClient    *api.USBClient
	updateStatus func()
	frpService   *service.FRPService // Для проверки FRP перед монтированием NBD

	// Конфигурация
	config         *models.AppConfig
	scanPaths      []string
	supportedTypes []string

	// Callback: тип манипулятора при запуске мыши (touchpad/absolute) — для синхронизации с VideoWidget
	onMouseTypeChanged      func(mouseType string)
	onMouseModeReconfigured func()
	onVideoConfigRequested  func(devicePath string)
	onVideoConnect          func(devicePath string)
	onVideoDisconnect       func()
	onButtonsChanged        func()

	// SAF helper для Android
	safHelper *platform.SAFHelper

	// Состояние устройства (ОС агента)
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
	Source         string // "api", "local", "user", "keyboard", "mouse", "rndis", "gamepad"
	IsMounted      bool
	LocalDrive     *models.LocalDrive // Для устройств из API
	DiskInfo       *models.DiskInfo   // Для локальных файлов
	IsKeyboard     bool               // Для клавиатуры
	IsMouse        bool               // Для мыши
	MouseType      string             // "mouse" (touchpad) или "absolute", только для мыши
	IsRNDIS        bool               // Для сетевой карты
	RNDISMode      string             // "auto", "wifirouter", "etherouter" или "etherbridge", только для RNDIS
	IsVideo        bool               // Для видеоустройства /dev/video*
	VideoDevice    *models.SystemDevice
	IsGamepad      bool   // Для геймпада
	GamepadID      string // Уникальный идентификатор геймпада (платформенный)
	GamepadMode    string // "directinput" (default) или "xinput"
	ReadOnly       bool   // Для образов vdi/vmdk/qcow2: true=RO, false=RW через overlay
	UploadProgress float64 // Прогресс загрузки 0-100
	UploadSpeed    float64 // Скорость загрузки МБ/с
	IsUploading    bool    // Идет ли загрузка
	IsMounting     bool    // Идёт монтирование (202 Accepted)
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
		userImages:         make([]*models.DiskInfo, 0),
		allDrives:          make([]DriveItem, 0),
		mountedDevices:     make([]*models.DeviceInfo, 0),
		selectedItems:      make(map[int]bool),
		devicesTraceBudget: 20,
		preferredMouseMode: defaultMouseMode(),
		scanPaths:          scanPaths,
		supportedTypes:     supportedTypes,
		safHelper:          platform.GetSAFHelper(app),
		rowsCache:          make(map[string]fyne.CanvasObject),
		cardsCache:         make(map[string]fyne.CanvasObject),
	}

	if runtime.GOOS == "android" && dw.safHelper != nil {
		logrus.Info("📱 [ANDROID-INIT] SAF global context will be initialized on window set")
	}

	dw.createInterface()

	// Запускаем периодическое обновление состояния
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
	case drive.LocalDrive != nil:
		return "api:" + drive.LocalDrive.Name + ":" + drive.LocalDrive.SourceType
	case drive.DiskInfo != nil:
		return drive.Source + ":" + drive.DiskInfo.Path
	default:
		return "raw:" + drive.Name
	}
}

func (dw *DiskWidget) buildDeviceCards() []fyne.CanvasObject {
	sections := dw.groupDriveIndexes()
	cards := make([]fyne.CanvasObject, 0, len(sections))

	for _, section := range sections {
		if len(section.indexes) == 0 && section.key != "storage" {
			continue
		}

		rows := make([]fyne.CanvasObject, 0, len(section.indexes))
		for _, driveIndex := range section.indexes {
			drive := dw.allDrives[driveIndex]
			id := dw.getDriveUniqueID(drive)
			
			rowObj, ok := dw.rowsCache[id]
			if !ok {
				rowObj = view.NewDiskRowTemplate()
				dw.rowsCache[id] = rowObj
			}
			dw.configureDriveRow(driveIndex, rowObj)
			rows = append(rows, rowObj)
		}

		fill, border, badge := sectionPalette(section.key)
		var sectionAction fyne.CanvasObject
		var sectionTrailingAction fyne.CanvasObject
		if section.key == "storage" {
			sectionAction = view.NewDeviceSectionAddButton(dw.handleAddImage)
			sectionTrailingAction = view.NewFooterIconButton(assets.QuestionIconDim, assets.QuestionIcon, fyne.NewSize(13, 13), func() {
				dw.openQuickStartDocs()
			})
		}

		// Re-use or create section card
		// Using eyebrow as part of key because it contains the section title
		card, ok := dw.cardsCache[section.key]
		if !ok {
			card = view.NewDeviceSectionCard(
				section.title,
				section.title,
				section.description,
				formatSectionCount(len(section.indexes)),
				fill,
				border,
				badge,
				rows,
				sectionAction,
				sectionTrailingAction,
			)
			dw.cardsCache[section.key] = card
		} else {
			// Update existing card with new rows
			view.UpdateDeviceSectionCard(card, rows, formatSectionCount(len(section.indexes)))
		}

		cards = append(cards, card)
	}

	return cards
}

func (dw *DiskWidget) configureDriveRow(id int, obj fyne.CanvasObject) {
	if id < 0 || id >= len(dw.allDrives) {
		return
	}

	drive := dw.allDrives[id]
	row := view.ResolveDiskRowWidgets(obj)
	if row == nil {
		return
	}

	checkbox := row.Checkbox
	captureSelector := row.CaptureSelector
	prefixIcon := row.PrefixIcon
	nameLabel := row.NameLabel
	statusLabel := row.StatusLabel
	statusDot := row.StatusDot
	roRwBtn := row.RORWButton
	modeSelect := row.ModeSelect
	uploadBtn := row.UploadButton
	deleteBtn := row.DeleteButton
	settingsBtn := row.SettingsButton
	modeRowIconText := row.ModeIcon
	modeTitleLabel := row.ModeTitleLabel

	if checkbox == nil || prefixIcon == nil || nameLabel == nil || statusLabel == nil || statusDot == nil {
		return
	}

	videoUnavailable := drive.IsVideo && drive.VideoDevice != nil && !drive.VideoDevice.Connected && !drive.IsMounted
	controlsLocked := dw.controlsLocked()
	checked := false
	if !drive.IsVideo {
		dw.selectedItemsMu.RLock()
		checked = dw.selectedItems[id]
		dw.selectedItemsMu.RUnlock()
	}

	// Подготовка состояния иконок и цветов ЗАРАНЕЕ
	baseTextColor := design.ColorTextLight
	if drive.IsMounted {
		baseTextColor = design.ColorAccent
	}

	var iconRes fyne.Resource
	useStorageIcon := false
	switch drive.Source {
	case "api":
		useStorageIcon = true
		if drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp" {
			iconRes = assets.SDCardIcon
		} else {
			iconRes = assets.DiscIcon
		}
	case "local", "user":
		useStorageIcon = true
		iconRes = assets.FolderIcon
	case "keyboard":
		if drive.IsMounted {
			iconRes = assets.KeyboardIconActive
		} else {
			iconRes = assets.KeyboardIcon
		}
	case "mouse":
		if drive.IsMounted {
			iconRes = assets.MouseIconActive
		} else {
			iconRes = assets.MouseIcon
		}
	case "rndis":
		if drive.IsMounted {
			iconRes = assets.NetworkIconActive
		} else {
			iconRes = assets.NetworkIcon
		}
	case "gamepad":
		if drive.IsMounted {
			iconRes = assets.GamepadIconActive
		} else {
			iconRes = assets.GamepadIcon
		}
	case "video":
		if drive.IsMounted {
			iconRes = assets.CameraIconActive
		} else {
			iconRes = assets.CameraIcon
		}
	default:
		iconRes = assets.DiscIcon
	}

	if useStorageIcon && drive.IsMounted {
		if iconRes == assets.FolderIcon {
			iconRes = assets.FolderIconActive
		} else if iconRes == assets.SDCardIcon {
			iconRes = assets.SDCardIconActive
		} else {
			iconRes = assets.DiscIconActive
		}
	}

	var statusColor color.Color
	if drive.IsMounting {
		statusColor = color.NRGBA{R: 0xc7, G: 0x9b, B: 0x52, A: 0xff}
	} else if drive.IsMounted {
		statusColor = design.ColorAccent
	} else if videoUnavailable {
		statusColor = design.ColorBorder
	} else {
		statusColor = color.NRGBA{R: 0x86, G: 0x86, B: 0x86, A: 0xff}
	}

	nameText := dw.deviceRowText(drive)
	if drive.Source == "mouse" {
		switch normalizeMouseMode(drive.MouseType) {
		case mouseModeTouchScreen:
			nameText = fmt.Sprintf("TCH %s", nameText)
		case mouseModeAbsolute:
			nameText = fmt.Sprintf("ABS %s", nameText)
		default:
			nameText = fmt.Sprintf("PTR %s", nameText)
		}
	}

	overlayCapable := false
	if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil {
		overlayCapable = service.IsOverlayCapableExtension(strings.ToLower(filepath.Ext(drive.DiskInfo.Path)))
	} else if drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType != "mtp" {
		overlayCapable = service.IsOverlayCapableExtension(strings.ToLower(filepath.Ext(drive.LocalDrive.Name)))
	}

	// Единый пакет обновлений UI
	fyne.Do(func() {
		prefixIcon.Resource = iconRes
		prefixIcon.SetMinSize(fyne.NewSize(18, 18))
		if drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType == "mtp" {
			prefixIcon.SetMinSize(fyne.NewSize(16, 16))
		}
		prefixIcon.Show()
		prefixIcon.Refresh()

		nameLabel.SetColor(baseTextColor)
		nameLabel.SetText(nameText)
		nameLabel.Show()

		statusDot.FillColor = statusColor
		statusDot.Refresh()
		statusLabel.Hide()

		modeRowIconText.Hide()
		modeTitleLabel.Hide()

		if drive.Source == "mouse" || drive.Source == "rndis" || drive.Source == "gamepad" {
			modeSelect.Show()
			modeSelect.SetDisabled(controlsLocked)
			switch drive.Source {
			case "rndis":
				modeSelect.SetOptions(rndisModeOptions)
				modeSelect.SetSelected(normalizeRNDISMode(drive.RNDISMode))
			case "gamepad":
				modeSelect.SetOptions([]string{i18n.Current.DeviceDirectInput, i18n.Current.DeviceXInput})
				modeSelect.SetSelected(gamepadModeLabel(normalizeGamepadMode(drive.GamepadMode)))
			default: // mouse
				mode := normalizeMouseMode(drive.MouseType)
				if mode == mouseModeAbsolute {
					modeSelect.SetSelected(i18n.Current.DeviceAbsolute)
				} else {
					modeSelect.SetSelected(i18n.Current.DeviceTouchPad)
				}
				modeSelect.SetOptions([]string{i18n.Current.DeviceTouchPad, i18n.Current.DeviceAbsolute})
			}
		} else {
			modeSelect.Hide()
		}

		if drive.IsVideo {
			checkbox.Hide()
			if captureSelector != nil {
				captureSelector.Show()
				captureSelector.SetSelected(dw.isPreferredVideoDrive(drive))
				captureSelector.SetDisabled(controlsLocked || videoUnavailable)
			}
			settingsBtn.Show()
			if controlsLocked || videoUnavailable {
				settingsBtn.Disable()
			} else {
				settingsBtn.Enable()
			}
		} else {
			if captureSelector != nil {
				captureSelector.Hide()
			}
			checkbox.SetChecked(checked)
			checkbox.Show()
			if controlsLocked || drive.IsMounting || videoUnavailable {
				checkbox.Disable()
			} else {
				checkbox.Enable()
			}
			settingsBtn.Hide()
		}

		if drive.Source == "user" && drive.DiskInfo != nil && !drive.IsMounting {
			uploadBtn.Show()
			if drive.IsUploading || controlsLocked {
				uploadBtn.SetIcons(assets.UploadIconMuted, assets.UploadIconMuted, assets.UploadIconMuted)
				if drive.IsUploading {
					uploadBtn.SetText(fmt.Sprintf("%.0f%%", drive.UploadProgress))
				} else {
					uploadBtn.SetText("")
				}
				uploadBtn.SetDisabled(true)
			} else {
				uploadBtn.SetIcons(assets.UploadIcon, assets.UploadIcon, assets.UploadIconMuted)
				uploadBtn.SetText("")
				uploadBtn.SetDisabled(false)
			}
		} else {
			uploadBtn.Hide()
		}

		shouldShowDelete := false
		if !drive.IsMounting {
			if drive.Source == "user" {
				shouldShowDelete = true
			} else if drive.Source == "api" || drive.Source == "local" {
				isBackupFlash := drive.LocalDrive != nil && drive.LocalDrive.Name == "data" && drive.LocalDrive.SourceType == "mtp"
				if !isBackupFlash {
					shouldShowDelete = true
				}
			}
		}

		if shouldShowDelete {
			deleteBtn.Show()
			deleteBtn.SetDisabled(controlsLocked)
		} else {
			deleteBtn.Hide()
		}

		if overlayCapable && !drive.IsMounting {
			roRwBtn.Show()
			if drive.ReadOnly {
				roRwBtn.SetText("RO")
			} else {
				roRwBtn.SetText("RW")
			}
			if controlsLocked {
				roRwBtn.Disable()
			} else {
				roRwBtn.Enable()
			}
		} else {
			roRwBtn.Hide()
		}
	})

	// Колбэки
	if drive.IsVideo && drive.VideoDevice != nil {
		deviceCopy := *drive.VideoDevice
		if captureSelector != nil {
			captureSelector.SetOnTapped(func() {
				if dw.controlsLocked() || videoUnavailable {
					return
				}
				dw.setPreferredVideoDevice(deviceCopy)
				dw.requestDevicesRefresh()
			})
		}
		settingsBtn.SetOnTapped(func() {
			dw.setPreferredVideoDevice(deviceCopy)
			if dw.onVideoConfigRequested != nil {
				dw.onVideoConfigRequested(deviceCopy.Path)
			}
			dw.requestDevicesRefresh()
		})
	} else {
		if captureSelector != nil {
			captureSelector.SetOnTapped(nil)
		}
		checkbox.OnChanged = func(checked bool) {
			if dw.controlsLocked() || drive.IsMounting || videoUnavailable {
				return
			}
			if checked {
				if dw.countSelectedGadgetItems() >= MaxDevicesToMount {
					fyne.Do(func() { checkbox.SetChecked(false) })
					if dw.window != nil {
						dialog.ShowInformation(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
					}
					return
				}
			}
			dw.selectedItemsMu.Lock()
			dw.selectedItems[id] = checked
			dw.selectedItemsMu.Unlock()
			dw.updateButtons()
		}
		settingsBtn.SetOnTapped(nil)
	}

	if drive.Source == "rndis" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() {
				return
			}
			if rowID < len(dw.allDrives) {
				dw.allDrives[rowID].RNDISMode = normalizeRNDISMode(s)
			}
		}
	} else if drive.Source == "gamepad" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() || rowID >= len(dw.allDrives) {
				return
			}
			mode := gamepadModeDirectInput
			if s == i18n.Current.DeviceXInput {
				mode = gamepadModeXInput
			}
			dw.allDrives[rowID].GamepadMode = mode
		}
	} else if drive.Source == "mouse" {
		rowID := id
		modeSelect.OnSelected = func(s string) {
			if dw.controlsLocked() || rowID >= len(dw.allDrives) {
				return
			}
			newMode := mouseModeTouchPad
			if s == i18n.Current.DeviceAbsolute {
				newMode = mouseModeAbsolute
			}
			dw.applyMouseModeSelection(rowID, newMode)
		}
	}

	if drive.Source == "user" {
		rowID := id
		deleteBtn.SetOnTapped(func() {
			if !dw.controlsLocked() {
				dw.removeUserImage(rowID)
			}
		})
		uploadBtn.SetOnTapped(func() {
			if !dw.controlsLocked() {
				dw.handleUploadImage(rowID)
			}
		})
	} else {
		deleteBtn.SetOnTapped(nil)
		uploadBtn.SetOnTapped(nil)
		if !drive.IsMounting && (drive.Source == "api" || drive.Source == "local") {
			isBackupFlash := drive.LocalDrive != nil && drive.LocalDrive.Name == "data" && drive.LocalDrive.SourceType == "mtp"
			if !isBackupFlash {
				rowID := id
				var filename string
				if drive.LocalDrive != nil {
					filename = drive.LocalDrive.Name
				} else if drive.DiskInfo != nil {
					filename = drive.DiskInfo.Name
				} else {
					filename = drive.Name
				}
				deleteBtn.SetOnTapped(func() {
					if !dw.controlsLocked() {
						dw.handleDeleteImageFromDevice(rowID, filename)
					}
				})
			}
		}
	}

	if overlayCapable && !drive.IsMounting {
		rowID := id
		roRwBtn.OnTapped = func() {
			if !dw.controlsLocked() && rowID < len(dw.allDrives) {
				dw.allDrives[rowID].ReadOnly = !dw.allDrives[rowID].ReadOnly
				dw.requestDevicesRefresh()
			}
		}
	} else {
		roRwBtn.OnTapped = nil
	}
}

func (dw *DiskWidget) handleDeleteImageFromDevice(driveIndex int, filename string) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		return
	}
	drive := dw.allDrives[driveIndex]
	if dw.window != nil {
		fyne.Do(func() {
			view.ShowDeleteImageConfirm(
				drive.Name,
				func(confirmed bool) {
					if confirmed {
						go dw.deleteImageFromDevice(filename, drive.Name)
					}
				},
				dw.window,
			)
		})
	}
}

func (dw *DiskWidget) deviceRowText(drive DriveItem) string {
	if drive.IsVideo {
		return dw.captureDeviceTitle(drive)
	}

	if drive.Source == "user" || drive.Source == "local" {
		if drive.DiskInfo != nil {
			title := strings.TrimSpace(drive.DiskInfo.Name)
			if title == "" && strings.TrimSpace(drive.DiskInfo.Path) != "" {
				title = filepath.Base(filepath.Clean(drive.DiskInfo.Path))
			}
			if title == "" {
				title = drive.Name
			}
			return title
		}
	}

	if drive.Source == "api" && drive.LocalDrive != nil {
		name := strings.TrimSpace(dw.localizedAPIDriveName(drive.LocalDrive))
		if name == "" {
			name = drive.Name
		}
		return name
	}

	return drive.Name
}

func (dw *DiskWidget) localizedAPIDriveName(drive *models.LocalDrive) string {
	if drive == nil {
		return ""
	}
	if drive.Name == "data" && drive.SourceType == "mtp" {
		return i18n.Current.BackupFlashName
	}
	return drive.Name
}

func (dw *DiskWidget) captureDeviceTitle(drive DriveItem) string {
	if drive.VideoDevice == nil || len(dw.videoDevices) <= 1 {
		return i18n.Current.CaptureDevice
	}

	for index, device := range dw.videoDevices {
		if device.Path == drive.VideoDevice.Path {
			return fmt.Sprintf("%s (%d)", i18n.Current.CaptureDevice, index+1)
		}
	}
	return i18n.Current.CaptureDevice
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

// SetOnMouseTypeChanged устанавливает callback при смене типа манипулятора (мышь/тачскрин) после запуска устройства.
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
	// Сохраняем асинхронно, так как это может вовлекать дисковое I/O
	go func() {
		cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
		cfg.DevicePath = device.Path
		cfg.DeviceName = device.Name
		saveVideoDeviceConfig(cfg)
		logrus.Infof("💾 [VIDEO-PREFS] Saved preferred device: %s (%s)", device.Name, device.Path)
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
		if !drive.IsMounted || drive.IsVideo || drive.IsKeyboard || drive.IsMouse || drive.IsRNDIS {
			continue
		}
		return true
	}
	return false
}

// createButtonBar создает панель кнопок

// Refresh обновляет виджет
func (dw *DiskWidget) Refresh() {
	// Сбрасываем кэш, чтобы гарантировать чистое обновление при ручном запросе (например, смена языка или реконнект)
	dw.refreshMu.Lock()
	dw.rowsCache = make(map[string]fyne.CanvasObject)
	dw.cardsCache = make(map[string]fyne.CanvasObject)
	dw.refreshMu.Unlock()

	dw.loadLocalDrives()
	dw.loadLocalFiles()
	dw.loadVideoDevices()
	dw.loadMountedDevices()
	go dw.loadGamepadDevices()
}

func (dw *DiskWidget) requestDevicesRefresh() {
	if dw == nil || dw.devicesList == nil {
		return
	}

	// Проверяем, изменилось ли что-то в данных, прежде чем планировать обновление UI.
	// Если данные не изменились, игнорируем запрос на обновление, чтобы избежать моргания.
	signature := dw.computeDrivesSignature()
	if signature == dw.lastDrivesTraceSig {
		return
	}

	if !dw.devicesRefreshPending.CompareAndSwap(false, true) {
		return
	}

	delay := dw.nextDevicesRefreshDelay()
	runRefresh := func() {
		fyne.Do(func() {
			defer dw.devicesRefreshPending.Store(false)
			if dw.devicesList != nil {
				// Еще раз проверяем сигнатуру прямо перед отрисовкой
				currentSig := dw.computeDrivesSignature()
				if currentSig == dw.lastDrivesTraceSig && !dw.lastDevicesRefresh.IsZero() {
					return
				}
				dw.lastDrivesTraceSig = currentSig

				dw.markDevicesRefresh()
				dw.devicesList.Refresh()
			}
		})
	}
	if delay <= 0 {
		runRefresh()
		return
	}

	time.AfterFunc(delay, runRefresh)
}

func (dw *DiskWidget) computeDrivesSignature() string {
	if dw == nil {
		return ""
	}
	drives := dw.allDrives
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("total=%d|api=%d|local=%d|user=%d|video=%d|mounted=%d|os=%s|op=%v|mnt=%v",
		len(drives), len(dw.localDrives), len(dw.localFiles), len(dw.userImages), len(dw.videoDevices),
		len(dw.mountedDevices), dw.agentOS, dw.userOperationInFlight.Load(), dw.apiMountInProgress.Load()))

	for i := range drives {
		drive := drives[i]
		// Включаем в сигнатуру все поля, влияющие на визуальное состояние строки в списке
		builder.WriteString(fmt.Sprintf("|%d:%s:%t:%t:%t:%t:%s:%s:%s",
			i, drive.Source, drive.IsMounted, drive.IsMounting, drive.IsUploading, drive.ReadOnly,
			drive.Name, drive.RNDISMode, drive.MouseType))

		if drive.IsUploading {
			// Округляем прогресс до 2% для уменьшения количества рефрешей (и моргания)
			builder.WriteString(fmt.Sprintf(":up%.0f", drive.UploadProgress/2.0))
		}
		if drive.IsVideo && drive.VideoDevice != nil {
			builder.WriteString(fmt.Sprintf(":vc%t", drive.VideoDevice.Connected))
		}
	}
	return builder.String()
}

func (dw *DiskWidget) nextDevicesRefreshDelay() time.Duration {
	const mobileRefreshInterval = 120 * time.Millisecond

	if !fyne.CurrentDevice().IsMobile() {
		return 0
	}

	dw.refreshMu.Lock()
	defer dw.refreshMu.Unlock()

	if dw.lastDevicesRefresh.IsZero() {
		return 0
	}

	elapsed := time.Since(dw.lastDevicesRefresh)
	if elapsed >= mobileRefreshInterval {
		return 0
	}
	return mobileRefreshInterval - elapsed
}

func (dw *DiskWidget) markDevicesRefresh() {
	dw.refreshMu.Lock()
	dw.lastDevicesRefresh = time.Now()
	dw.refreshMu.Unlock()
}

// GetContainer возвращает контейнер виджета
func (dw *DiskWidget) GetContainer() fyne.CanvasObject {
	return dw.ui.Container
}

// GetButtons возвращает компактные кнопки управления для размещения в statusBar
func (dw *DiskWidget) GetButtons() (mount, unmount, addImage fyne.CanvasObject) {
	// Создаем компактные кнопки для статус бара (только если еще не созданы)
	if dw.compactMountBtn == nil {
		dw.compactMountBtn = view.NewDeviceActionButton(i18n.Current.ConnectButton, nil, dw.handleMount)
		dw.compactMountBtn.SetColors(design.ColorAccent, design.ColorAccentHover, design.ColorBackground, design.ColorBackground)
		dw.compactUnmountBtn = view.NewDeviceActionButton(i18n.Current.DisconnectButton, nil, dw.handleUnmount)
		dw.updateButtons() // начальное состояние (Connect скрыт если ничего не выбрано)
	}

	// Кнопка добавления ISO/IMG образа
	addImageBtn := widget.NewButton(i18n.Current.AddImageButton, dw.handleAddImage)

	return dw.compactMountBtn, dw.compactUnmountBtn, addImageBtn
}

// setButtonsEnabled включает/выключает кнопки управления (блокирует Mount и Unmount во время операций)
func (dw *DiskWidget) setButtonsEnabled(enabled bool) {
	if enabled {
		dw.updateButtons() // Восстанавливаем правильное состояние кнопок
	} else {
		// Блокируем кнопки Mount и Unmount во время подключения/отключения устройств
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
	// В Fyne используем fyne.Do для обновления UI из горутин
	fyne.Do(updateFunc)
}

// updateStatusAsync безопасно обновляет статус из горутины
func (dw *DiskWidget) updateStatusAsync(status string) {
	logrus.Info(status)
}

// showErrorAsync безопасно показывает ошибку из горутины
func (dw *DiskWidget) showErrorAsync(err error) {
	logrus.Errorf("Ошибка: %v", err)
	dw.updateUIAsync(func() {
		if dw.window != nil {
			view.ShowErrorDialog(err, dw.window)
		}
	})
}

// showWarningAsync безопасно показывает предупреждение из горутины
func (dw *DiskWidget) showWarningAsync(title, message string) {
	dw.updateUIAsync(func() {
		if dw.window != nil {
			dialog.ShowInformation(title, message, dw.window)
		}
	})
}

// startPeriodicRefresh запускает периодическое обновление состояния
func (dw *DiskWidget) startPeriodicRefresh() {
	go func() {
		ticker := time.NewTicker(10 * time.Second) // Обновляем каждые 10 секунд
		defer ticker.Stop()

		for range ticker.C {
			if !dw.devicesRefreshQueued.CompareAndSwap(false, true) {
				continue
			}
			dw.updateUIAsync(func() {
				defer dw.devicesRefreshQueued.Store(false)
				dw.Refresh()
			})
		}
	}()
}

// getLocalIP получает локальный IP адрес для NBD сервера
func (dw *DiskWidget) getLocalIP() (string, error) {
	if dw.usbClient == nil {
		return "", fmt.Errorf("USB клиент не инициализирован")
	}

	// Получаем хост из USB клиента
	// USB клиент создается с baseURL вида "http://host:port"
	// Извлекаем хост из baseURL
	baseURL := dw.usbClient.GetBaseURL()
	if baseURL == "" {
		return "", fmt.Errorf("не удается получить базовый URL USB клиента")
	}

	// Извлекаем хост из URL (убираем "http://" и ":port")
	host := strings.TrimPrefix(baseURL, "http://")
	if idx := strings.Index(host, ":"); idx != -1 {
		host = host[:idx]
	}

	// Пытаемся подключиться к USBridge 2 чтобы определить локальный IP
	conn, err := net.Dial("udp", host+":8080")
	if err != nil {
		return "", fmt.Errorf("не удается определить локальный IP: %v", err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String(), nil
}

// getAvailablePort получает свободный порт для NBD сервера
func (dw *DiskWidget) getAvailablePort() (int, error) {
	basePort := 10809
	maxAttempts := 100

	logrus.Infof("🔍 Поиск свободного порта начиная с %d...", basePort)

	for i := 0; i < maxAttempts; i++ {
		port := basePort + i

		// Проверяем, не используется ли порт уже нашим сервером
		portInUse := false
		for exportName, server := range dw.nbdServers {
			if server.IsRunning() {
				// Получаем порт сервера из конфигурации
				if server.GetServerStatus()["server_port"] == port {
					logrus.Debugf("🔍 Порт %d уже используется сервером для экспорта %s", port, exportName)
					portInUse = true
					break
				}
			}
		}

		if !portInUse {
			// Проверяем, свободен ли порт в системе
			listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
			if err == nil {
				listener.Close()
				logrus.Infof("✅ Найден свободный порт: %d", port)
				return port, nil
			} else {
				logrus.Debugf("🔍 Порт %d занят в системе: %v", port, err)
			}
		}
	}

	logrus.Errorf("❌ Не удалось найти свободный порт в диапазоне %d-%d", basePort, basePort+maxAttempts-1)
	return 0, fmt.Errorf("не удалось найти свободный порт в диапазоне %d-%d", basePort, basePort+maxAttempts-1)
}

// resolveNBDBindHost возвращает адрес, на котором должен слушать NBD сервер.
// QUIC/FRP: FRP подключается локально → 127.0.0.1.
// Tailscale: только интерфейс Tailscale (100.x.x.x), иначе 127.0.0.1.
func (dw *DiskWidget) resolveNBDBindHost() string {
	if dw.frpService != nil {
		return "127.0.0.1"
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		name := strings.ToLower(iface.Name)
		if !strings.Contains(name, "tailscale") && !strings.Contains(name, "wg") && !strings.Contains(name, "tun") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok {
				if ip := ipnet.IP.To4(); ip != nil && ip[0] == 100 {
					return ip.String()
				}
			}
		}
	}
	return "127.0.0.1"
}

// startNBDServer запускает NBD для файла. Для VMDK/QCOW2/VDI на десктопе используется qemu-nbd
// (экспортируется виртуальный диск — MBR/GPT), иначе — наш go-nbd (файл как есть).
func (dw *DiskWidget) startNBDServer(diskInfo *models.DiskInfo, port int, exportName string, readOnly bool) (service.NBDRunner, error) {
	logrus.Infof("🔧 [START-NBD-1] Создание NBD: файл='%s', порт=%d, export_name=%s, readOnly=%v", diskInfo.Name, port, exportName, readOnly)

	// На десктопе с реальным путём для VMDK/QCOW2/VDI экспортируем виртуальный диск через qemu-nbd
	useQemuNbd := runtime.GOOS != "android" &&
		!strings.HasPrefix(diskInfo.Path, "content://") &&
		service.IsQemuNbdFormatForPath(diskInfo.Path)

	if useQemuNbd {
		logrus.Infof("📍 [START-NBD-QEMU] Формат образа требует qemu-nbd (экспорт виртуального диска)")
		bindHost := dw.resolveNBDBindHost()
		runner := service.NewQemuNBDRunner(diskInfo.Path, readOnly, bindHost)
		if err := runner.EnsureQemuNbdForExport(); err != nil {
			return nil, fmt.Errorf("для образов VMDK/QCOW2/VDI нужен qemu-nbd: %w", err)
		}
		if err := runner.Start(port); err != nil {
			return nil, fmt.Errorf("для образов VMDK/QCOW2/VDI нужен qemu-nbd (установите QEMU): %w", err)
		}
		logrus.Infof("✅ [START-NBD-QEMU] qemu-nbd запущен на %s:%d для %s", bindHost, port, diskInfo.Name)
		logrus.Infof("   📡 NBD export: %s:%d", bindHost, port)
		return runner, nil
	}

	// go-nbd: ISO, raw, img (файл как есть)
	bindHost := dw.resolveNBDBindHost()
	nbdServer := service.NewNBDServerWithApp(bindHost, dw.app)
	if err := nbdServer.Start(port); err != nil {
		return nil, fmt.Errorf("ошибка запуска NBD сервера: %v", err)
	}

	export := &models.DiskExport{
		Name:        diskInfo.Name,
		FilePath:    diskInfo.Path,
		Size:        diskInfo.Size,
		ReadOnly:    readOnly,
		Description: diskInfo.Description,
		IsActive:    true,
		ExportName:  exportName,
	}
	if err := nbdServer.AddExport(export); err != nil {
		nbdServer.Stop()
		return nil, fmt.Errorf("ошибка добавления экспорта: %v", err)
	}

	logrus.Infof("✅ [START-NBD-7-SUCCESS] NBD сервер для '%s' на %s:%d", diskInfo.Name, bindHost, port)
	logrus.Infof("   📡 NBD export: %s:%d", bindHost, port)
	return nbdServer, nil
}

// UpdateClient обновляет USB клиент
// SetFRPService устанавливает FRP сервис для проверки перед монтированием NBD
func (dw *DiskWidget) SetFRPService(frp *service.FRPService) {
	dw.frpService = frp
}

func (dw *DiskWidget) UpdateClient(usbClient *api.USBClient) {
	dw.usbClient = usbClient
	dw.agentOS = "" // Сбрасываем ОС при смене клиента
	if usbClient == nil {
		dw.sdSpaceInfo = nil
		dw.updateSDStorageInfo()
		dw.stopAllGamepadCaptures()
	}
	// Обновляем данные при смене клиента
	dw.loadLocalDrives()
	dw.loadMountedDevices()
	dw.combineDrives()
	dw.Refresh()
}

// TODO: Функционал добавления папок как MTP устройств
// Планируется реализовать:
// 1. Кнопка выбора папки в UI (рядом с кнопкой добавления образа)
// 2. Dialog выбора папки (desktop + Android SAF)
// 3. Создание NBD сервера для папки (виртуальный образ файловой системы)
// 4. Отправка NBD источника на USBridge 2 в формате:
//    {
//      "device": "drive",
//      "server": "127.0.0.1",
//      "port": 10809,
//      "export_name": "FolderName"
//    }
// 5. USBridge 2 подключает NBD и монтирует как MTP устройство
//
// Требования:
// - NBD backend должен уметь создавать виртуальный образ из папки
// - Или NBD клиент на стороне USBridge 2 должен поддерживать прямую отдачу папки
//
// См. закомментированный код ниже для референса:

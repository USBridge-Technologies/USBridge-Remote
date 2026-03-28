package controller

import (
	"fmt"
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usbridge-client/internal/api"
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
	devicesList *widget.List
	ui          *view.DiskWidgetUI
	mountBtn    *widget.Button
	unmountBtn  *widget.Button
	window      fyne.Window
	app         fyne.App

	// Компактные кнопки для статус бара
	compactMountBtn   *widget.Button
	compactUnmountBtn *widget.Button

	// Данные
	localDrives  []*models.LocalDrive // Устройства из API
	localFiles   []*models.DiskInfo   // Локальные файлы из папки isos
	videoDevices []models.SystemDevice
	sdSpaceInfo  *models.ISOSpaceInfo // Информация о месте на SD-карте (при монтировании)

	// Callback для обновления общего прогрессбара в main window (место на флешке)
	onStorageInfoUpdate func(usedPct float64, available, total int64)
	userImages          []*models.DiskInfo // Образы, добавленные пользователем
	allDrives           []DriveItem        // Объединенный список
	mountedDevices      []*models.DeviceInfo
	selectedDrive       *DriveItem
	selectedItems       map[int]bool // Множественный выбор элементов

	// Сервисы
	nbdServers   map[string]service.NBDRunner // Карта NBD (go-nbd или qemu-nbd) по именам экспортов
	usbClient    *api.USBClient
	updateStatus func()
	frpService   *service.FRPService // Для проверки FRP перед монтированием NBD

	// Конфигурация
	config         *models.AppConfig
	scanPaths      []string
	supportedTypes []string

	// Callback: тип манипулятора при запуске мыши (mouse/touchscreen) — для синхронизации с VideoWidget
	onMouseTypeChanged     func(mouseType string)
	onVideoConfigRequested func(devicePath string)
	onVideoConnect         func(devicePath string)
	onVideoDisconnect      func()

	// SAF helper для Android
	safHelper *platform.SAFHelper
}

// MaxDevicesToMount максимальное количество устройств для одновременного выбора
const MaxDevicesToMount = 5

var rndisModeOptions = []string{"auto", "wifirouter", "etherouter", "etherbridge"}

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
	Source         string // "api", "local", "user", "keyboard", "mouse" или "rndis"
	IsMounted      bool
	LocalDrive     *models.LocalDrive // Для устройств из API
	DiskInfo       *models.DiskInfo   // Для локальных файлов
	IsKeyboard     bool               // Для клавиатуры
	IsMouse        bool               // Для мыши
	MouseType      string             // "mouse" (touchpad), "double", "touchscreen" или "absolute", только для мыши
	IsRNDIS        bool               // Для сетевой карты
	RNDISMode      string             // "auto", "wifirouter", "etherouter" или "etherbridge", только для RNDIS
	IsVideo        bool               // Для видеоустройства /dev/video*
	VideoDevice    *models.SystemDevice
	ReadOnly       bool    // Для образов vdi/vmdk/qcow2: true=RO, false=RW через overlay (только чтение не портит базовый образ)
	UploadProgress float64 // Прогресс загрузки 0-100
	UploadSpeed    float64 // Скорость загрузки МБ/с
	IsUploading    bool    // Идет ли загрузка
	IsMounting     bool    // Идёт монтирование (202 Accepted)
}

func defaultMouseMode() string {
	if fyne.CurrentDevice().IsMobile() {
		return "mouse"
	}
	return "double"
}

func normalizeMouseMode(mode string) string {
	switch mode {
	case "mouse", "double", "touchscreen", "absolute":
		return mode
	default:
		return "mouse"
	}
}

// Double currently reuses the same absolute transport as Absolute because that
// path is the stable one for host-side pointer positioning.
func mouseTransportType(mode string) string {
	mode = normalizeMouseMode(mode)
	if mode == "double" {
		return "absolute"
	}
	return mode
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
		nbdServers:     make(map[string]service.NBDRunner),
		usbClient:      usbClient,
		updateStatus:   updateStatus,
		app:            app,
		config:         config,
		localDrives:    make([]*models.LocalDrive, 0),
		localFiles:     make([]*models.DiskInfo, 0),
		videoDevices:   make([]models.SystemDevice, 0),
		userImages:     make([]*models.DiskInfo, 0),
		allDrives:      make([]DriveItem, 0),
		mountedDevices: make([]*models.DeviceInfo, 0),
		selectedItems:  make(map[int]bool),
		scanPaths:      scanPaths,
		supportedTypes: supportedTypes,
		safHelper:      platform.GetSAFHelper(app),
	}

	dw.createInterface()
	dw.loadUserImagesFromPreferences() // Загружаем сохраненные образы
	dw.loadLocalDrives()
	dw.loadLocalFiles()
	dw.loadVideoDevices()
	dw.combineDrives()
	dw.loadMountedDevices()

	// Запускаем периодическое обновление состояния
	dw.startPeriodicRefresh()

	return dw
}

// SetWindow устанавливает окно для диалогов
func (dw *DiskWidget) SetWindow(window fyne.Window) {
	dw.window = window
}

// createInterface создает интерфейс виджета
func (dw *DiskWidget) createInterface() {
	dw.ui = view.NewDiskWidgetUI(
		func() int { return len(dw.allDrives) },
		view.NewDiskRowTemplate,
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(dw.allDrives) {
				drive := dw.allDrives[id]
				row := view.ResolveDiskRowWidgets(obj)
				if row == nil {
					logrus.Warn("disk row widgets could not be resolved")
					return
				}
				checkbox := row.Checkbox
				nameLabel := row.NameLabel
				statusLabel := row.StatusLabel
				roRwBtn := row.RORWButton
				modeSelect := row.ModeSelect
				uploadBtn := row.UploadButton
				deleteBtn := row.DeleteButton
				settingsBtn := row.SettingsButton
				modeRowIconText := row.ModeIcon
				modeTitleLabel := row.ModeTitleLabel
				if checkbox == nil || nameLabel == nil || statusLabel == nil {
					logrus.Warnf("disk row widgets missing essentials for row %d", id)
					return
				}
				videoUnavailable := drive.IsVideo && drive.VideoDevice != nil && !drive.VideoDevice.Connected && !drive.IsMounted
				videoSelected := drive.IsVideo && drive.VideoDevice != nil && drive.VideoDevice.Path == selectedVideoDevicePath()

				// Для строк mouse/rndis: показываем иконку + название + выпадающий список режима, иначе — подпись устройства
				if nameLabel != nil && modeSelect != nil {
					if (drive.Source == "mouse" || drive.Source == "rndis") && !drive.IsMounting {
						nameLabel.Hide()
						if modeRowIconText != nil {
							if drive.Source == "rndis" {
								modeRowIconText.Text = "🌐"
							} else {
								switch drive.MouseType {
								case "double":
									modeRowIconText.Text = "🖱️"
								case "touchscreen":
									modeRowIconText.Text = "🖥️" // Экран/доска — тачскрин
								case "absolute":
									modeRowIconText.Text = "🖱️"
								default:
									modeRowIconText.Text = "🖱️"
								}
							}
							modeRowIconText.Show()
							obj.Refresh()
						}
						if modeTitleLabel != nil {
							if drive.Source == "rndis" {
								modeTitleLabel.SetText(i18n.Current.DeviceNetworkCard)
							} else {
								modeTitleLabel.SetText(i18n.Current.DeviceMouse)
							}
							modeTitleLabel.Show()
						}
						modeSelect.Show()
						if drive.Source == "rndis" {
							modeSelect.SetOptions(rndisModeOptions)
							modeSelect.SetSelected(normalizeRNDISMode(drive.RNDISMode))
						} else {
							modeSelect.SetOptions([]string{i18n.Current.DeviceTouchPad, i18n.Current.DeviceMouse, i18n.Current.DeviceTouch, i18n.Current.DeviceAbsolute})
							switch drive.MouseType {
							case "double":
								modeSelect.SetSelected(i18n.Current.DeviceMouse)
							case "touchscreen":
								modeSelect.SetSelected(i18n.Current.DeviceTouch)
							case "absolute":
								modeSelect.SetSelected(i18n.Current.DeviceAbsolute)
							default:
								modeSelect.SetSelected(i18n.Current.DeviceTouchPad)
							}
						}
						rowID := id
						modeSelect.OnChanged = func(s string) {
							if rowID < len(dw.allDrives) {
								if dw.allDrives[rowID].Source == "rndis" {
									dw.allDrives[rowID].RNDISMode = normalizeRNDISMode(s)
								} else {
									if s == i18n.Current.DeviceTouchPad {
										dw.allDrives[rowID].MouseType = "mouse"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖱️"
											obj.Refresh()
										}
									} else if s == i18n.Current.DeviceMouse {
										dw.allDrives[rowID].MouseType = "double"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖱️"
											obj.Refresh()
										}
									} else if s == i18n.Current.DeviceTouch {
										dw.allDrives[rowID].MouseType = "touchscreen"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖥️"
											obj.Refresh()
										}
									} else if s == i18n.Current.DeviceAbsolute {
										dw.allDrives[rowID].MouseType = "absolute"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖱️"
											obj.Refresh()
										}
									} else {
										dw.allDrives[rowID].MouseType = "mouse"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖱️"
											obj.Refresh()
										}
									}
								}
							}
						}
					} else {
						nameLabel.Show()
						if modeRowIconText != nil {
							modeRowIconText.Hide()
						}
						if modeTitleLabel != nil {
							modeTitleLabel.Hide()
						}
						modeSelect.Hide()
					}
				}

				// Устанавливаем состояние чекбокса
				checkbox.SetChecked(dw.selectedItems[id])
				checkbox.Show()
				if drive.IsMounting || videoUnavailable {
					checkbox.Disable()
				} else {
					checkbox.Enable()
				}
				checkbox.OnChanged = func(checked bool) {
					if drive.IsMounting || videoUnavailable {
						return
					}
					if checked && drive.IsVideo && drive.VideoDevice != nil {
						dw.setPreferredVideoDevice(*drive.VideoDevice)
					}
					if checked {
						// Проверяем лимит: не более 5 устройств
						selectedCount := dw.countSelectedGadgetItems()
						if !drive.IsVideo && selectedCount >= MaxDevicesToMount {
							checkbox.SetChecked(false)
							if dw.window != nil {
								dialog.ShowInformation(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
							}
							return
						}
					}
					dw.selectedItems[id] = checked
					dw.updateButtons()
				}

				// Показываем кнопку загрузки (Upload) для пользовательских образов
				if uploadBtn != nil {
					if drive.Source == "user" && drive.DiskInfo != nil && !drive.IsMounting {
						uploadBtn.Show()
						if drive.IsUploading {
							uploadBtn.SetText(fmt.Sprintf("%.0f%%", drive.UploadProgress))
							uploadBtn.Disable()
						} else {
							uploadBtn.SetText("⬆️")
							uploadBtn.Enable()
							uploadBtn.OnTapped = func() {
								dw.handleUploadImage(id)
							}
						}
					} else if drive.IsMounting {
						uploadBtn.Hide()
					} else {
						uploadBtn.Hide()
					}
				}

				// Показываем кнопку удаления для пользовательских образов и образов из API (local/api)
				if deleteBtn != nil {
					if drive.IsMounting {
						deleteBtn.Hide()
					} else if drive.Source == "user" {
						// Для пользовательских образов - удаляем из списка локально
						deleteBtn.Show()
						deleteBtn.OnTapped = func() {
							dw.removeUserImage(id)
						}
					} else if drive.Source == "api" || drive.Source == "local" {
						// Проверяем, это НЕ Backup Flash
						isBackupFlash := drive.LocalDrive != nil &&
							drive.LocalDrive.Name == "data" &&
							drive.LocalDrive.SourceType == "mtp"

						if !isBackupFlash {
							// Для образов из API - удаляем с устройства (кроме Backup Flash)
							deleteBtn.Show()
							deleteBtn.OnTapped = func() {
								dw.handleDeleteImageFromDevice(id)
							}
						} else {
							// Скрываем кнопку удаления для Backup Flash
							deleteBtn.Hide()
						}
					} else {
						deleteBtn.Hide()
					}
				}

				if settingsBtn != nil {
					if drive.IsVideo && drive.VideoDevice != nil {
						settingsBtn.Show()
						devicePath := drive.VideoDevice.Path
						if videoUnavailable {
							settingsBtn.Disable()
						} else {
							settingsBtn.Enable()
						}
						settingsBtn.OnTapped = func() {
							dw.setPreferredVideoDevice(*drive.VideoDevice)
							if dw.onVideoConfigRequested != nil {
								dw.onVideoConfigRequested(devicePath)
							}
						}
					} else {
						settingsBtn.Hide()
					}
				}

				// Переключатель RO/RW для образов vdi, vmdk, qcow2 и т.д. (запись идёт в overlay, базовый образ не портится)
				if roRwBtn != nil {
					overlayCapable := false
					if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil {
						overlayCapable = service.IsOverlayCapableExtension(strings.ToLower(filepath.Ext(drive.DiskInfo.Path)))
					} else if drive.Source == "api" && drive.LocalDrive != nil && drive.LocalDrive.SourceType != "mtp" {
						overlayCapable = service.IsOverlayCapableExtension(strings.ToLower(filepath.Ext(drive.LocalDrive.Name)))
					}
					if overlayCapable && !drive.IsMounting {
						roRwBtn.Show()
						if drive.ReadOnly {
							roRwBtn.SetText("RO")
						} else {
							roRwBtn.SetText("RW")
						}
						rowID := id
						roRwBtn.OnTapped = func() {
							if rowID < len(dw.allDrives) {
								dw.allDrives[rowID].ReadOnly = !dw.allDrives[rowID].ReadOnly
								dw.devicesList.Refresh()
							}
						}
					} else {
						roRwBtn.Hide()
					}
				}

				// Добавляем префикс для различения источников
				var sourcePrefix string

				if drive.Source == "api" {
					// Проверяем тип источника для API устройств
					if drive.LocalDrive != nil {
						// Проверяем сначала по имени для Backup Flash
						if drive.LocalDrive.Name == "data" && drive.LocalDrive.SourceType == "mtp" {
							sourcePrefix = "🛡️ " // Backup Flash (версионная флешка)
						} else {
							switch drive.LocalDrive.SourceType {
							case "mtp":
								sourcePrefix = "📱 " // MTP устройство
							default:
								sourcePrefix = "📀 " // DVD для ISO образов
							}
						}
					} else {
						sourcePrefix = "📀 " // DVD для ISO образов
					}
				} else if drive.Source == "local" {
					sourcePrefix = "📁 "
				} else if drive.Source == "user" {
					// Для пользовательских образов проверяем тип пути
					if drive.DiskInfo != nil && strings.HasPrefix(drive.DiskInfo.Path, "content://") {
						sourcePrefix = "🤖 " // Android для content:// URI
					} else {
						sourcePrefix = "📂 " // Папка для обычных путей
					}
				} else if drive.Source == "keyboard" {
					sourcePrefix = "⌨️ "
				} else if drive.Source == "mouse" {
					sourcePrefix = "🖱️ "
				} else if drive.Source == "rndis" {
					sourcePrefix = "🌐 "
				} else if drive.Source == "video" {
					sourcePrefix = "📺 "
				}

				// Устанавливаем иконку статуса подключения
				if drive.IsMounting {
					statusLabel.SetText("⏳")
					statusLabel.Importance = widget.MediumImportance
					statusLabel.TextStyle.Bold = false
					nameLabel.TextStyle.Bold = false
				} else if drive.IsMounted {
					statusLabel.SetText("✅")
					statusLabel.Importance = widget.HighImportance
					statusLabel.TextStyle.Bold = true
					nameLabel.TextStyle.Bold = true
				} else if videoUnavailable {
					statusLabel.SetText("◌")
					statusLabel.Importance = widget.MediumImportance
					statusLabel.TextStyle.Bold = false
					nameLabel.TextStyle.Bold = false
				} else {
					statusLabel.SetText("⭕")
					statusLabel.Importance = widget.MediumImportance
					statusLabel.TextStyle.Bold = false
					nameLabel.TextStyle.Bold = videoSelected
				}

				// Создаем текст без дублирования иконки статуса
				deviceName := drive.Name
				if drive.IsVideo {
					var tags []string
					if videoSelected {
						tags = append(tags, i18n.Current.VideoDeviceSelected)
					}
					if drive.IsMounted {
						tags = append(tags, i18n.Current.VideoDeviceCurrent)
					}
					if videoUnavailable {
						tags = append(tags, i18n.Current.VideoDeviceUnavailable)
					}
					if len(tags) > 0 {
						deviceName = fmt.Sprintf("%s [%s]", deviceName, strings.Join(tags, ", "))
					}
				}
				deviceText := fmt.Sprintf("%s%s", sourcePrefix, deviceName)
				nameLabel.SetText(deviceText)
			}
		},
	)
	dw.devicesList = dw.ui.DevicesList

	// Обработчик клика по элементу списка
	dw.devicesList.OnSelected = func(id widget.ListItemID) {
		if id < len(dw.allDrives) {
			dw.selectedDrive = &dw.allDrives[id]
			if dw.selectedDrive.IsVideo && dw.selectedDrive.VideoDevice != nil {
				dw.setPreferredVideoDevice(*dw.selectedDrive.VideoDevice)
				dw.devicesList.Refresh()
			}
			dw.updateButtons()
		}
	}

	// Кнопки
	dw.mountBtn = widget.NewButton(i18n.Current.MountButton, dw.handleMount)
	dw.unmountBtn = widget.NewButton(i18n.Current.UnmountButton, dw.handleUnmount)

	// Создаем одноколоночный интерфейс
	dw.container = dw.ui.Container

	// НЕ вызываем updateButtons() здесь - кнопки будут обновлены после загрузки данных
	// updateButtons() будет вызван из checkbox.OnChanged и других обработчиков
}

// SetOnStorageInfoUpdate устанавливает callback для обновления прогрессбара в main window
func (dw *DiskWidget) SetOnStorageInfoUpdate(fn func(usedPct float64, available, total int64)) {
	dw.onStorageInfoUpdate = fn
}

// SetOnMouseTypeChanged устанавливает callback при смене типа манипулятора (мышь/тачскрин) после запуска устройства.
func (dw *DiskWidget) SetOnMouseTypeChanged(fn func(mouseType string)) {
	dw.onMouseTypeChanged = fn
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

func (dw *DiskWidget) setPreferredVideoDevice(device models.SystemDevice) {
	if strings.TrimSpace(device.Path) == "" {
		return
	}
	cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
	cfg.DevicePath = device.Path
	cfg.DeviceName = device.Name
	saveVideoDeviceConfig(cfg)
}

// createButtonBar создает панель кнопок

// Refresh обновляет виджет
func (dw *DiskWidget) Refresh() {
	dw.loadLocalDrives()
	dw.loadLocalFiles()
	dw.loadVideoDevices()
	dw.combineDrives()
	dw.loadMountedDevices()
	dw.devicesList.Refresh()
}

// GetContainer возвращает контейнер виджета
func (dw *DiskWidget) GetContainer() *fyne.Container {
	return dw.container
}

// GetButtons возвращает компактные кнопки управления для размещения в statusBar
func (dw *DiskWidget) GetButtons() (mount, unmount, addImage *widget.Button) {
	// Создаем компактные кнопки для статус бара (только если еще не созданы)
	if dw.compactMountBtn == nil {
		dw.compactMountBtn = widget.NewButton(i18n.Current.MountButtonCompact, dw.handleMount)
		dw.compactUnmountBtn = widget.NewButton(i18n.Current.UnmountButtonCompact, dw.handleUnmount)
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

// pollMountStatus опрашивает /api/device/info пока mount_in_progress, потом обновляет UI
func (dw *DiskWidget) pollMountStatus(mountingExportNames map[string]bool) {
	const pollInterval = 1500 * time.Millisecond
	const maxPolls = 60 // ~90 секунд

	for i := 0; i < maxPolls; i++ {
		time.Sleep(pollInterval)

		if dw.usbClient == nil {
			break
		}

		info, err := dw.usbClient.GetDeviceInfo()
		if err != nil {
			logrus.Debugf("pollMountStatus: GetDeviceInfo: %v", err)
			continue
		}

		if !info.MountInProgress {
			// Монтирование завершено
			if info.LastMountError != "" {
				logrus.Warnf("pollMountStatus: Ошибка монтирования: %s", info.LastMountError)
				dw.showErrorAsync(fmt.Errorf("ошибка монтирования: %s", info.LastMountError))
			}
			break
		}
		logrus.Debugf("pollMountStatus: монтирование в процессе (%d/%d)", i+1, maxPolls)
	}

	dw.updateUIAsync(func() {
		dw.setMountingStateByExportNames(mountingExportNames, false)
		dw.loadMountedDevices()
		dw.loadLocalDrives()
		dw.updateStatus()
		dw.devicesList.Refresh()
		dw.setButtonsEnabled(true) // разблокируем Mount после завершения монтирования
	})
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
			dialog.ShowError(err, dw.window)
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
			// Обновляем данные через API и локальные файлы
			dw.updateUIAsync(func() {
				dw.loadLocalDrives()
				dw.loadLocalFiles()
				dw.loadVideoDevices()
				dw.combineDrives()
				dw.loadMountedDevices()
				dw.devicesList.Refresh()
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
		bindHost := strings.TrimSpace(dw.config.NBDBindHost)
		if bindHost == "" {
			bindHost = "127.0.0.1"
		}
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
	bindHost := strings.TrimSpace(dw.config.NBDBindHost)
	if bindHost == "" {
		bindHost = "127.0.0.1"
	}
	config := &models.AppConfig{NBDPort: port, NBDBindHost: bindHost}
	nbdServer := service.NewNBDServerWithApp(config, dw.app)
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
	if usbClient == nil {
		dw.sdSpaceInfo = nil
		dw.updateSDStorageInfo()
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

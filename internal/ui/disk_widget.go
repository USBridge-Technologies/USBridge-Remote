package ui

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// DiskWidget виджет управления устройствами и ISO
type DiskWidget struct {
	container   *fyne.Container
	devicesList *widget.List
	mountBtn    *widget.Button
	unmountBtn  *widget.Button
	window      fyne.Window
	app         fyne.App

	// Компактные кнопки для статус бара
	compactMountBtn   *widget.Button
	compactUnmountBtn *widget.Button

	// Данные
	localDrives []*models.LocalDrive // Устройства из API
	localFiles  []*models.DiskInfo   // Локальные файлы из папки isos
	sdSpaceInfo *models.ISOSpaceInfo // Информация о месте на SD-карте (при монтировании)

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
	onMouseTypeChanged func(mouseType string)

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
	MouseType      string             // "mouse" (мышь/тачпад), "touchscreen" (тачскрин) или "absolute" (абсолютный), только для мыши
	IsRNDIS        bool               // Для сетевой карты
	RNDISMode      string             // "auto", "wifirouter", "etherouter" или "etherbridge", только для RNDIS
	ReadOnly       bool               // Для образов vdi/vmdk/qcow2: true=RO, false=RW через overlay (только чтение не портит базовый образ)
	UploadProgress float64            // Прогресс загрузки 0-100
	UploadSpeed    float64            // Скорость загрузки МБ/с
	IsUploading    bool               // Идет ли загрузка
	IsMounting     bool               // Идёт монтирование (202 Accepted)
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
	// Единый список устройств с чекбоксами для мультивыделения
	dw.devicesList = widget.NewList(
		func() int { return len(dw.allDrives) },
		func() fyne.CanvasObject {
			// Создаем чекбокс, лейблы и кнопки (загрузка + удаление)
			checkbox := widget.NewCheck("", nil)
			nameLabel := widget.NewLabel(i18n.Current.DeviceRowTemplateName)
			nameLabel.Wrapping = fyne.TextWrapOff

			statusLabel := widget.NewLabel(i18n.Current.DeviceRowTemplateStatus)
			statusLabel.Alignment = fyne.TextAlignTrailing

			roRwBtn := widget.NewButton("RO", nil)
			roRwBtn.Hide() // Показываем только для образов vdi/vmdk/qcow2 и т.д.

			uploadBtn := widget.NewButton("⬆️", nil)
			uploadBtn.Hide() // По умолчанию скрыта

			deleteBtn := widget.NewButton("🗑️", nil)
			deleteBtn.Hide() // По умолчанию скрыта

			// Для строки mouse/rndis: иконка + название + выпадающий список режима
			modeRowIconText := canvas.NewText("🖱️", theme.Color(theme.ColorNameForeground))
			modeRowIconText.TextSize = theme.TextSize() // Размер как у остальных иконок в списке
			modeRowIconText.Hide()
			modeTitleLabel := widget.NewLabel("Manipulator")
			modeTitleLabel.Hide()
			modeSelect := widget.NewSelect([]string{i18n.Current.DeviceMouse, i18n.Current.DeviceTouch, i18n.Current.DeviceAbsolute}, nil)
			modeSelect.Hide()

			// Оборачиваем чекбокс в контейнер фиксированного размера для лучшего touch-взаимодействия
			checkboxContainer := container.NewPadded(checkbox)
			checkboxContainer.Resize(fyne.NewSize(50, 50)) // Увеличенная область для touch

			// Центр: либо подпись (nameLabel), либо иконка + название устройства
			modeLabelWrap := container.NewHBox(modeRowIconText, modeTitleLabel)
			centerContainer := container.NewStack(nameLabel, modeLabelWrap)

			// Используем Border layout: чекбокс слева, название по центру, кнопки/селекты справа
			rightContainer := container.NewHBox(roRwBtn, modeSelect, uploadBtn, deleteBtn, statusLabel)
			return container.NewBorder(
				nil, nil, // top, bottom
				checkboxContainer, rightContainer, // left, right
				centerContainer, // center
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(dw.allDrives) {
				drive := dw.allDrives[id]
				borderContainer := obj.(*fyne.Container)

				// Находим элементы по типу, а не по индексу
				var checkbox *widget.Check
				var nameLabel *widget.Label
				var statusLabel *widget.Label
				var roRwBtn *widget.Button
				var modeSelect *widget.Select
				var uploadBtn *widget.Button
				var deleteBtn *widget.Button
				var rightContainer *fyne.Container
				var checkboxContainer *fyne.Container
				var centerContainer *fyne.Container

				for _, child := range borderContainer.Objects {
					switch v := child.(type) {
					case *fyne.Container:
						if len(v.Objects) == 2 {
							var hasLabel bool
							var hasModeWrap bool
							for _, o := range v.Objects {
								if _, ok := o.(*widget.Label); ok {
									hasLabel = true
								}
								if c, ok := o.(*fyne.Container); ok {
									for _, sub := range c.Objects {
										if _, ok := sub.(*canvas.Text); ok {
											hasModeWrap = true
											break
										}
									}
								}
							}
							if hasLabel && hasModeWrap {
								centerContainer = v
								continue
							}
						}
						if len(v.Objects) > 0 {
							if _, ok := v.Objects[0].(*widget.Check); ok {
								checkboxContainer = v
							} else {
								rightContainer = v
							}
						}
					}
				}

				// Из центрального контейнера достаём подпись, иконку и заголовок режима
				var modeRowIconText *canvas.Text
				var modeTitleLabel *widget.Label
				if centerContainer != nil {
					for _, o := range centerContainer.Objects {
						if l, ok := o.(*widget.Label); ok {
							nameLabel = l
						}
						if c, ok := o.(*fyne.Container); ok {
							// HBox с иконкой + заголовком: первый элемент — иконка (canvas.Text), второй — Label
							for _, sub := range c.Objects {
								if txt, ok := sub.(*canvas.Text); ok && modeRowIconText == nil {
									modeRowIconText = txt
								}
								if l, ok := sub.(*widget.Label); ok && modeTitleLabel == nil {
									modeTitleLabel = l
								}
							}
						}
					}
				}

				// Находим чекбокс в контейнере
				if checkboxContainer != nil {
					for _, child := range checkboxContainer.Objects {
						if c, ok := child.(*widget.Check); ok {
							checkbox = c
							break
						}
					}
				}

				// Находим roRwBtn, modeSelect, uploadBtn, deleteBtn и statusLabel в правом контейнере
				if rightContainer != nil {
					buttonIndex := 0
					for _, child := range rightContainer.Objects {
						switch v := child.(type) {
						case *widget.Button:
							if buttonIndex == 0 {
								roRwBtn = v
							} else if buttonIndex == 1 {
								uploadBtn = v
							} else if buttonIndex == 2 {
								deleteBtn = v
							}
							buttonIndex++
						case *widget.Select:
							modeSelect = v
						case *widget.Label:
							statusLabel = v
						}
					}
				}

				// Для строк mouse/rndis: показываем иконку + название + выпадающий список режима, иначе — подпись устройства
				if nameLabel != nil && modeSelect != nil {
					if (drive.Source == "mouse" || drive.Source == "rndis") && !drive.IsMounting {
						nameLabel.Hide()
						if modeRowIconText != nil {
							if drive.Source == "rndis" {
								modeRowIconText.Text = "🌐"
							} else {
								switch drive.MouseType {
								case "touchscreen":
									modeRowIconText.Text = "🖥️" // Экран/доска — тачскрин
								case "absolute":
									modeRowIconText.Text = "📍"
								default:
									modeRowIconText.Text = "🖱️"
								}
							}
							modeRowIconText.Show()
							borderContainer.Refresh()
						}
						if modeTitleLabel != nil {
							if drive.Source == "rndis" {
								modeTitleLabel.SetText(i18n.Current.DeviceNetworkCard)
							} else {
								modeTitleLabel.SetText("Manipulator")
							}
							modeTitleLabel.Show()
						}
						modeSelect.Show()
						if drive.Source == "rndis" {
							modeSelect.SetOptions(rndisModeOptions)
							modeSelect.SetSelected(normalizeRNDISMode(drive.RNDISMode))
						} else {
							modeSelect.SetOptions([]string{i18n.Current.DeviceMouse, i18n.Current.DeviceTouch, i18n.Current.DeviceAbsolute})
							switch drive.MouseType {
							case "touchscreen":
								modeSelect.SetSelected(i18n.Current.DeviceTouch)
							case "absolute":
								modeSelect.SetSelected(i18n.Current.DeviceAbsolute)
							default:
								modeSelect.SetSelected(i18n.Current.DeviceMouse)
							}
						}
						rowID := id
						modeSelect.OnChanged = func(s string) {
							if rowID < len(dw.allDrives) {
								if dw.allDrives[rowID].Source == "rndis" {
									dw.allDrives[rowID].RNDISMode = normalizeRNDISMode(s)
								} else {
									if s == i18n.Current.DeviceTouch {
										dw.allDrives[rowID].MouseType = "touchscreen"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖥️"
											borderContainer.Refresh()
										}
									} else if s == i18n.Current.DeviceAbsolute {
										dw.allDrives[rowID].MouseType = "absolute"
										if modeRowIconText != nil {
											modeRowIconText.Text = "📍"
											borderContainer.Refresh()
										}
									} else {
										dw.allDrives[rowID].MouseType = "mouse"
										if modeRowIconText != nil {
											modeRowIconText.Text = "🖱️"
											borderContainer.Refresh()
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
				if drive.IsMounting {
					checkbox.Disable()
				} else {
					checkbox.Enable()
				}
				checkbox.OnChanged = func(checked bool) {
					if drive.IsMounting {
						return
					}
					if checked {
						// Проверяем лимит: не более 5 устройств
						selectedCount := dw.countSelectedItems()
						if selectedCount >= MaxDevicesToMount {
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
				} else {
					statusLabel.SetText("⭕")
					statusLabel.Importance = widget.MediumImportance
					statusLabel.TextStyle.Bold = false
					nameLabel.TextStyle.Bold = false
				}

				// Создаем текст без дублирования иконки статуса
				deviceText := fmt.Sprintf("%s%s", sourcePrefix, drive.Name)
				nameLabel.SetText(deviceText)
			}
		},
	)

	// Обработчик клика по элементу списка
	dw.devicesList.OnSelected = func(id widget.ListItemID) {
		if id < len(dw.allDrives) {
			dw.selectedDrive = &dw.allDrives[id]
			dw.updateButtons()
		}
	}

	// Кнопки
	dw.mountBtn = widget.NewButton(i18n.Current.MountButton, dw.handleMount)
	dw.unmountBtn = widget.NewButton(i18n.Current.UnmountButton, dw.handleUnmount)

	// Создаем одноколоночный интерфейс
	mainContent := dw.createDevicesPanel()

	// Контейнер занимает всё место - кнопки будут добавлены снаружи
	dw.container = mainContent

	// НЕ вызываем updateButtons() здесь - кнопки будут обновлены после загрузки данных
	// updateButtons() будет вызван из checkbox.OnChanged и других обработчиков
}

// createDevicesPanel создает панель устройств (одноколоночный интерфейс)
// Прогрессбар места на флешке перенесён в main window (верхняя панель на всех экранах)
func (dw *DiskWidget) createDevicesPanel() *fyne.Container {
	// Заголовок для списка
	headerLabel := widget.NewRichTextFromMarkdown("## " + i18n.Current.Devices)
	subtitleLabel := widget.NewLabel(i18n.Current.AllAvailableDevices)
	subtitleLabel.TextStyle.Italic = true

	headerContainer := container.NewVBox(headerLabel, subtitleLabel)

	return container.NewBorder(
		headerContainer, // top
		nil,             // bottom - кнопки перенесены в главный контейнер
		nil,             // left
		nil,             // right
		dw.devicesList,  // center - список занимает всё доступное место
	)
}

// SetOnStorageInfoUpdate устанавливает callback для обновления прогрессбара в main window
func (dw *DiskWidget) SetOnStorageInfoUpdate(fn func(usedPct float64, available, total int64)) {
	dw.onStorageInfoUpdate = fn
}

// SetOnMouseTypeChanged устанавливает callback при смене типа манипулятора (мышь/тачскрин) после запуска устройства.
func (dw *DiskWidget) SetOnMouseTypeChanged(fn func(mouseType string)) {
	dw.onMouseTypeChanged = fn
}

// createButtonBar создает панель кнопок

// loadLocalDrives загружает локальные устройства через API
func (dw *DiskWidget) loadLocalDrives() {
	go func() {
		if dw.usbClient == nil {
			logrus.Debug("USB клиент не инициализирован, пропускаем загрузку локальных устройств")
			return
		}

		localDrives, err := dw.usbClient.GetLocalDrives()
		if err != nil {
			logrus.Errorf("Ошибка загрузки локальных устройств: %v", err)
			return
		}

		// Конвертируем в указатели
		dw.localDrives = make([]*models.LocalDrive, len(localDrives.Drives))
		for i := range localDrives.Drives {
			dw.localDrives[i] = &localDrives.Drives[i]
		}

		logrus.Infof("Загружено %d устройств из API", len(dw.localDrives))
		dw.updateUIAsync(func() {
			dw.combineDrives()
			dw.devicesList.Refresh()
		})

		// Загружаем информацию о месте на SD-карте (раздел iso/data/backup)
		dw.loadISOSpace()
	}()
}

// loadISOSpace загружает информацию о месте на SD-карте
func (dw *DiskWidget) loadISOSpace() {
	if dw.usbClient == nil {
		return
	}
	spaceInfo, err := dw.usbClient.GetISOSpace()
	if err != nil {
		logrus.Debugf("Информация о месте на SD-карте недоступна: %v", err)
		dw.updateUIAsync(func() {
			dw.sdSpaceInfo = nil
			dw.updateSDStorageInfo()
		})
		return
	}
	dw.updateUIAsync(func() {
		dw.sdSpaceInfo = spaceInfo
		dw.updateSDStorageInfo()
	})
}

// updateSDStorageInfo обновляет прогрессбар в main window через callback
func (dw *DiskWidget) updateSDStorageInfo() {
	if dw.sdSpaceInfo == nil || dw.sdSpaceInfo.TotalSpace <= 0 {
		if dw.onStorageInfoUpdate != nil {
			dw.onStorageInfoUpdate(0, 0, 0)
		}
		return
	}
	usedPct := dw.sdSpaceInfo.UsedPercent
	total := dw.sdSpaceInfo.TotalSpace
	available := dw.sdSpaceInfo.AvailableSpace
	if dw.onStorageInfoUpdate != nil {
		dw.onStorageInfoUpdate(usedPct/100, available, total)
	}
}

// loadLocalFiles загружает локальные файлы из папки isos
func (dw *DiskWidget) loadLocalFiles() {
	var foundFiles []*models.DiskInfo

	for _, scanPath := range dw.scanPaths {
		if _, err := os.Stat(scanPath); os.IsNotExist(err) {
			continue
		}

		err := filepath.Walk(scanPath, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}

			if info.IsDir() {
				return nil
			}

			if dw.isSupportedFile(path) {
				diskInfo, err := models.NewDiskInfo(path)
				if err != nil {
					logrus.Errorf("Ошибка создания DiskInfo для %s: %v", path, err)
					return nil
				}

				foundFiles = append(foundFiles, diskInfo)
			}

			return nil
		})

		if err != nil {
			logrus.Errorf("Ошибка сканирования %s: %v", scanPath, err)
		}
	}

	dw.localFiles = foundFiles
	logrus.Infof("Найдено %d локальных файлов", len(foundFiles))
}

// isSupportedFile проверяет, поддерживается ли файл
func (dw *DiskWidget) isSupportedFile(filePath string) bool {
	diskInfo := &models.DiskInfo{Path: filePath}
	return diskInfo.IsSupported(dw.supportedTypes)
}

// uploadState сохраняет состояние загрузки для восстановления после combineDrives
type uploadState struct {
	progress float64
	speed    float64
}

// combineDrives объединяет устройства из API, локальные файлы и клавиатуру
func (dw *DiskWidget) combineDrives() {
	// Сохраняем состояние загрузки и монтирования перед перестроением
	uploadingByPath := make(map[string]uploadState)
	mountingByExportName := make(map[string]bool)
	oldReadOnly := make(map[string]bool)
	oldMouseType := "mouse" // сохраняем выбор пользователя (мышь/тачскрин)
	oldRNDISMode := "auto"
	for _, d := range dw.allDrives {
		if d.IsMouse && d.MouseType != "" {
			oldMouseType = d.MouseType
		}
		if d.IsRNDIS && d.RNDISMode != "" {
			oldRNDISMode = d.RNDISMode
		}
		if d.IsUploading && d.DiskInfo != nil && d.DiskInfo.Path != "" {
			uploadingByPath[d.DiskInfo.Path] = uploadState{progress: d.UploadProgress, speed: d.UploadSpeed}
		}
		if d.IsMounting {
			if d.DiskInfo != nil {
				mountingByExportName[d.DiskInfo.Name] = true
			} else if d.LocalDrive != nil {
				mountingByExportName[d.LocalDrive.Name] = true
			} else {
				mountingByExportName[d.Name] = true
			}
		}
		// Сохраняем RO/RW для overlay-образов (vdi/vmdk/qcow2)
		key := ""
		if d.DiskInfo != nil {
			key = d.DiskInfo.Path
		} else if d.LocalDrive != nil && d.LocalDrive.SourceType != "mtp" {
			key = "api:" + d.LocalDrive.Name
		}
		if key != "" {
			ext := ""
			if d.DiskInfo != nil {
				ext = strings.ToLower(filepath.Ext(d.DiskInfo.Path))
			} else if d.LocalDrive != nil {
				ext = strings.ToLower(filepath.Ext(d.LocalDrive.Name))
			}
			if service.IsOverlayCapableExtension(ext) {
				oldReadOnly[key] = d.ReadOnly
			}
		}
	}

	dw.allDrives = make([]DriveItem, 0)

	// Добавляем устройства из API
	for _, drive := range dw.localDrives {
		displayName := drive.Name
		// Для версионной backup флешки меняем название
		if drive.Name == "data" && drive.SourceType == "mtp" {
			displayName = i18n.Current.BackupFlashName
		}
		// По умолчанию: overlay-образы (vdi/vmdk/qcow2) — RW, иначе RO (чтобы гаджет не вызывал nbd-client -R и не было I/O error при записи в loop)
		readOnly := true
		if drive.SourceType != "mtp" {
			ext := strings.ToLower(filepath.Ext(drive.Name))
			if service.IsOverlayCapableExtension(ext) && ext != ".iso" {
				readOnly = false
			}
			if ro, ok := oldReadOnly["api:"+drive.Name]; ok {
				readOnly = ro
			}
		}
		item := DriveItem{
			Name:       displayName,
			Size:       drive.FormatSize(),
			Source:     "api",
			IsMounted:  drive.IsMounted,
			LocalDrive: drive,
			ReadOnly:   readOnly,
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем локальные файлы
	for _, file := range dw.localFiles {
		ext := strings.ToLower(filepath.Ext(file.Path))
		readOnly := !service.IsOverlayCapableExtension(ext) || ext == ".iso" // overlay-образы по умолчанию RW
		if ro, ok := oldReadOnly[file.Path]; ok {
			readOnly = ro
		}
		item := DriveItem{
			Name:      file.Name,
			Size:      file.FormatSize(),
			Source:    "local",
			IsMounted: false, // Локальные файлы показываем как не смонтированные
			DiskInfo:  file,
			ReadOnly:  readOnly,
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем пользовательские образы (показываем красиво отформатированный путь)
	for _, file := range dw.userImages {
		logrus.Debugf("🔍 [combineDrives] Обработка пользовательского образа: Name='%s', Path='%s', Type='%s'", file.Name, file.Path, file.Type)
		displayName := dw.formatPathForDisplay(file.Path, file.Name)
		logrus.Debugf("🔍 [combineDrives] После formatPathForDisplay: displayName='%s'", displayName)

		ext := strings.ToLower(filepath.Ext(file.Path))
		readOnly := !service.IsOverlayCapableExtension(ext) || ext == ".iso" // overlay-образы по умолчанию RW
		if ro, ok := oldReadOnly[file.Path]; ok {
			readOnly = ro
		}
		item := DriveItem{
			Name:      displayName,
			Size:      file.FormatSize(),
			Source:    "user",
			IsMounted: false,
			DiskInfo:  file,
			ReadOnly:  readOnly,
		}
		dw.allDrives = append(dw.allDrives, item)
	}

	// Добавляем клавиатуру
	keyboardItem := DriveItem{
		Name:       i18n.Current.DeviceKeyboard,
		Size:       "N/A",
		Source:     "keyboard",
		IsMounted:  false, // Будем проверять статус через API
		IsKeyboard: true,
	}
	dw.allDrives = append(dw.allDrives, keyboardItem)

	// Добавляем мышь (тип мышь/тачскрин сохраняем из предыдущего состояния списка)
	if oldMouseType != "mouse" && oldMouseType != "touchscreen" && oldMouseType != "absolute" {
		oldMouseType = "mouse"
	}
	oldRNDISMode = normalizeRNDISMode(oldRNDISMode)
	mouseItem := DriveItem{
		Name:      i18n.Current.DeviceMouse,
		Size:      "N/A",
		Source:    "mouse",
		IsMounted: false,
		IsMouse:   true,
		MouseType: oldMouseType,
	}
	dw.allDrives = append(dw.allDrives, mouseItem)

	// Добавляем сетевую карту (RNDIS)
	rndisItem := DriveItem{
		Name:      i18n.Current.DeviceNetworkCard,
		Size:      "N/A",
		Source:    "rndis",
		IsMounted: false, // Будем проверять статус через API
		IsRNDIS:   true,
		RNDISMode: oldRNDISMode,
	}
	dw.allDrives = append(dw.allDrives, rndisItem)

	// Восстанавливаем состояние загрузки и монтирования
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil {
			if state, ok := uploadingByPath[dw.allDrives[i].DiskInfo.Path]; ok {
				dw.allDrives[i].IsUploading = true
				dw.allDrives[i].UploadProgress = state.progress
				dw.allDrives[i].UploadSpeed = state.speed
			}
			if mountingByExportName[dw.allDrives[i].DiskInfo.Name] {
				dw.allDrives[i].IsMounting = true
			}
		}
		if dw.allDrives[i].LocalDrive != nil && mountingByExportName[dw.allDrives[i].LocalDrive.Name] {
			dw.allDrives[i].IsMounting = true
		}
	}

	// Обновляем статус устройств после объединения
	dw.updateDevicesStatus()

	logrus.Infof("Объединено %d элементов (API: %d, локальные: %d, пользовательские: %d, клавиатура: 1, мышь: 1, RNDIS: 1)", len(dw.allDrives), len(dw.localDrives), len(dw.localFiles), len(dw.userImages))
}

// loadMountedDevices загружает смонтированные устройства через API
func (dw *DiskWidget) loadMountedDevices() {
	go func() {
		if dw.usbClient == nil {
			logrus.Debug("USB клиент не инициализирован, пропускаем загрузку устройств")
			return
		}

		deviceInfo, err := dw.usbClient.GetDeviceInfo()
		if err != nil {
			logrus.Errorf("Ошибка загрузки информации об устройствах: %v", err)
			return
		}

		// Конвертируем в указатели
		dw.mountedDevices = make([]*models.DeviceInfo, len(deviceInfo.Devices))
		for i := range deviceInfo.Devices {
			dw.mountedDevices[i] = &deviceInfo.Devices[i]
		}

		logrus.Debugf("Загружено %d смонтированных устройств", len(dw.mountedDevices))
		dw.updateUIAsync(func() {
			dw.updateDevicesStatus()
			dw.devicesList.Refresh()
		})
	}()
}

// updateDevicesStatus обновляет статус всех устройств в списке
func (dw *DiskWidget) updateDevicesStatus() {
	logrus.Debugf("🔄 Обновление статуса устройств. Всего устройств: %d, смонтированных: %d", len(dw.allDrives), len(dw.mountedDevices))

	// Логируем смонтированные устройства для отладки
	for _, device := range dw.mountedDevices {
		logrus.Debugf("📡 Смонтированное устройство: name='%s', device='%s', type='%s', status='%s'",
			device.Name, device.Device, device.Type, device.Status)
	}

	// Имя экспорта в API (nbd_10809) -> имя диска в nbdServers (ключ), чтобы NBD без метки отображались как подключённые
	nbdExportToDriveName := make(map[string]string)
	for name, srv := range dw.nbdServers {
		if srv != nil && srv.IsRunning() {
			apiName := srv.NBDExportNameForAPI()
			if apiName != "" {
				nbdExportToDriveName[apiName] = name
			}
		}
	}

	// Обновляем статус всех устройств
	for i := range dw.allDrives {
		drive := &dw.allDrives[i]
		oldStatus := drive.IsMounted
		isMounted := false

		// Проверяем, есть ли устройство в API ответе со статусом "connected"
		for _, device := range dw.mountedDevices {
			if device.Status == "connected" {
				// Для клавиатуры проверяем тип
				if drive.IsKeyboard && (device.Type == "keyboard" || strings.HasPrefix(device.Type, "keyboard:")) {
					isMounted = true
					logrus.Debugf("⌨️ Найдена подключенная клавиатура: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
					break
				}
				// Для мыши проверяем тип (мышь или тачскрин)
				if drive.IsMouse && (device.Type == "mouse" || device.Type == "touchscreen" || strings.HasPrefix(device.Type, "mouse:")) {
					isMounted = true
					if device.Type == "touchscreen" {
						drive.MouseType = "touchscreen"
					} else {
						drive.MouseType = "mouse"
					}
					if dw.onMouseTypeChanged != nil {
						dw.onMouseTypeChanged(drive.MouseType)
					}
					logrus.Debugf("🖱️ Найдена подключенная мышь: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
					break
				}
				// Для RNDIS проверяем тип
				if drive.IsRNDIS && (device.Type == "rndis" || strings.HasPrefix(device.Type, "rndis:")) {
					isMounted = true
					if strings.HasPrefix(device.Type, "rndis:") {
						rndisMode := strings.TrimPrefix(device.Type, "rndis:")
						drive.RNDISMode = normalizeRNDISMode(rndisMode)
					}
					logrus.Debugf("🌐 Найдена подключенная сетевая карта: %s (type: %s, device: %s)", device.Name, device.Type, device.Device)
					break
				}
				// Для дисков проверяем имя файла в названии устройства
				if !drive.IsKeyboard && !drive.IsMouse && !drive.IsRNDIS && (device.Type == "local" || device.Type == "mtp" || device.Type == "nbd") {
					// Для пользовательских образов сравниваем по полному пути
					var driveName string
					if drive.Source == "user" && drive.DiskInfo != nil {
						driveName = filepath.Base(drive.DiskInfo.Path)
					} else if drive.Source == "api" && drive.LocalDrive != nil {
						// Для устройств из API используем оригинальное имя из LocalDrive
						driveName = drive.LocalDrive.Name
					} else {
						driveName = drive.Name
					}

					// NBD с именем экспорта API (nbd_10809): сопоставление по нашему nbdServers — VDI/диски без метки отображаются как подключённые
					if device.Type == "nbd" && device.Name != "" {
						expectedName := nbdExportToDriveName[device.Name]
						matchesDrive := expectedName != "" && (driveName == expectedName || drive.Name == expectedName ||
							(drive.DiskInfo != nil && drive.DiskInfo.Name == expectedName))
						if matchesDrive {
							isMounted = true
							logrus.Debugf("🌐 Найден подключенный NBD диск (по export_name): %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
							break
						}
					}

					// Сравниваем имя файла с именем устройства в API
					if strings.Contains(device.Name, driveName) || strings.Contains(driveName, device.Name) {
						isMounted = true
						if device.Type == "mtp" {
							logrus.Debugf("📱 Найден подключенный MTP диск: %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
						} else if device.Type == "nbd" {
							logrus.Debugf("🌐 Найден подключенный NBD диск: %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
						} else {
							logrus.Debugf("💿 Найден подключенный диск: %s (device: %s, API name: %s)", drive.Name, device.Device, device.Name)
						}
						break
					}
				}
			}
		}

		drive.IsMounted = isMounted
		logrus.Debugf("📊 %s (%s): %v -> %v", drive.Name, drive.Source, oldStatus, drive.IsMounted)
	}
	dw.updateButtons()
}

// startDevicesWithRetry выполняет StartDevicesBatch с 3 попытками и паузой 3 с между ними (FRP/туннель может не успеть подняться)
func (dw *DiskWidget) startDevicesWithRetry(batchRequest models.DeviceStartBatchRequest) (*models.APIResponse, error) {
	const maxAttempts = 3
	const retryDelay = 3 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		resp, err := dw.usbClient.StartDevicesBatch(batchRequest)
		if err == nil {
			if attempt > 1 {
				logrus.Infof("✅ [MOUNT-API-RETRY] Попытка %d/%d успешна", attempt, maxAttempts)
			}
			return resp, nil
		}
		lastErr = err
		errStr := err.Error()
		isRetryable := strings.Contains(errStr, "EOF") || strings.Contains(errStr, "NBD") ||
			strings.Contains(errStr, "Failed to connect NBD") || strings.Contains(errStr, "connection reset") ||
			strings.Contains(errStr, "connection refused")
		if !isRetryable || attempt == maxAttempts {
			return nil, err
		}
		logrus.Warnf("⚠️ [MOUNT-API-RETRY] Попытка %d/%d: %v, повтор через 3 с...", attempt, maxAttempts, err)
		dw.updateStatusAsync(fmt.Sprintf("Повтор подключения %d/%d через 3 с...", attempt, maxAttempts))
		time.Sleep(retryDelay)
	}
	return nil, lastErr
}

// handleMount обрабатывает монтирование: добавляет выбранные к уже подключённым (пересобирает batch).
func (dw *DiskWidget) handleMount() {
	dw.setButtonsEnabled(false) // блокируем сразу после клика (исключить даблклик)
	logrus.Infof("📍 [MOUNT-1] handleMount вызван, GOOS: %s", runtime.GOOS)

	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		dw.setButtonsEnabled(true)
		return
	}

	// Уже подключённые устройства
	var mountedDrives []DriveItem
	for _, drive := range dw.allDrives {
		if drive.IsMounted {
			mountedDrives = append(mountedDrives, drive)
		}
	}

	// Выбранные и не подключённые
	var selectedDrives []DriveItem
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			drive := dw.allDrives[id]
			if !drive.IsMounted {
				selectedDrives = append(selectedDrives, drive)
			}
		}
	}

	if len(selectedDrives) == 0 {
		logrus.Warnf("⚠️ Нет выбранных устройств для подключения")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.SelectDevicesToMount), dw.window)
		}
		dw.setButtonsEnabled(true)
		return
	}

	totalCount := len(mountedDrives) + len(selectedDrives)
	if totalCount > MaxDevicesToMount {
		logrus.Warnf("⚠️ Слишком много устройств: %d (максимум %d)", totalCount, MaxDevicesToMount)
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, i18n.Current.MaxDevicesReached, dw.window)
		}
		dw.setButtonsEnabled(true)
		return
	}

	logrus.Infof("📁 Подключено: %d, добавляем: %d, итого: %d", len(mountedDrives), len(selectedDrives), totalCount)

	// Проверяем, есть ли среди выбранных файлы из Google Drive
	hasGoogleDriveFiles := false
	for _, drive := range selectedDrives {
		if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil {
			if strings.Contains(drive.DiskInfo.Path, "com.google.android.apps.docs.storage") {
				hasGoogleDriveFiles = true
				break
			}
		}
	}

	// Если есть файлы из Google Drive - показываем предупреждение С прогресс-индикатором
	var progressDialog dialog.Dialog
	if hasGoogleDriveFiles {
		logrus.Warnf("⚠️  Обнаружены файлы из Google Drive! Показываем предупреждение с прогрессом")

		// Создаем прогресс-бар (бесконечный, т.к. не знаем точное время)
		progressBar := widget.NewProgressBarInfinite()

		// Создаем кастомный диалог с прогресс-баром
		progressContent := container.NewVBox(
			widget.NewLabel(i18n.Current.LoadingFromCloud),
			widget.NewLabel(""),
			widget.NewLabel(i18n.Current.CloudFilesDetected),
			widget.NewLabel(i18n.Current.AndroidBuffering),
			widget.NewLabel(i18n.Current.MayTake30Seconds),
			widget.NewLabel(""),
			progressBar,
			widget.NewLabel(""),
			widget.NewLabel(i18n.Current.PleaseWait),
		)

		progressDialog = dialog.NewCustomWithoutButtons(i18n.Current.PreparingToMount, progressContent, dw.window)
		progressDialog.Show()
	}

	logrus.Infof("Монтирование %d устройств...", len(selectedDrives))

	// Выполняем монтирование в горутине
	go func() {
		reEnableButtons := true // разблокировать при ошибке/return; при успехе — pollMountStatus разблокирует
		defer func() {
			if progressDialog != nil {
				fyne.Do(func() {
					progressDialog.Hide()
				})
			}
			if reEnableButtons {
				dw.setButtonsEnabled(true)
			}
		}()

		var deviceRequests []models.DeviceStartRequest

		// Сначала добавляем уже подключённые устройства (пересборка batch)
		for _, mountedDrive := range mountedDrives {
			req, err := dw.buildDeviceRequestForDrive(mountedDrive, true)
			if err != nil {
				logrus.Warnf("⚠️ Не удалось построить запрос для подключённого %s: %v", mountedDrive.Name, err)
				continue
			}
			deviceRequests = append(deviceRequests, *req)
		}

		// Обрабатываем каждое новое выбранное устройство
		for _, selectedDrive := range selectedDrives {
			var deviceRequest *models.DeviceStartRequest

			if selectedDrive.Source == "keyboard" {
				// Клавиатура - используем параметр -k
				deviceRequest = &models.DeviceStartRequest{
					Device:       "keyboard",
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge Keyboard",
					Manufacturer: "USBridge",
					// Добавляем параметр -k для клавиатуры
					KeyboardMode: true,
				}
				logrus.Infof("⌨️ Подготовка клавиатуры для монтирования")
			} else if selectedDrive.Source == "mouse" {
				// Мышь или тачскрин (тип выбран в строке устройства)
				mouseType := selectedDrive.MouseType
				if mouseType != "mouse" && mouseType != "touchscreen" && mouseType != "absolute" {
					mouseType = "mouse"
				}
				deviceRequest = &models.DeviceStartRequest{
					Device:       "mouse",
					Type:         mouseType,
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge Mouse",
					Manufacturer: "USBridge",
				}
				logrus.Infof("🖱️ Подготовка манипулятора для монтирования: %s", mouseType)
			} else if selectedDrive.Source == "rndis" {
				// Сетевая карта RNDIS
				rndisMode := normalizeRNDISMode(selectedDrive.RNDISMode)
				deviceRequest = &models.DeviceStartRequest{
					Device:       "rndis",
					RNDISMode:    rndisMode,
					VendorID:     "0x1d6b",
					ProductID:    "0x0104",
					ProductName:  "USBridge RNDIS",
					Manufacturer: "USBridge",
				}
				logrus.Infof("🌐 Подготовка сетевой карты RNDIS для монтирования (mode=%s)", rndisMode)
			} else if selectedDrive.Source == "api" && selectedDrive.LocalDrive != nil {
				// Устройство из API - определяем тип монтирования
				if selectedDrive.LocalDrive.SourceType == "mtp" {
					// MTP устройство - монтируем как MTP
					deviceRequest = &models.DeviceStartRequest{
						Device:       "mtp",
						Server:       selectedDrive.LocalDrive.Name,
						VendorID:     "0x1d6b",
						ProductID:    "0x0104",
						ProductName:  "BackupDrive",
						Manufacturer: "USBridge",
					}
					logrus.Infof("📱 Подготовка MTP устройства из API: %s", selectedDrive.Name)
				} else {
					// Обычное устройство - локальное монтирование
					deviceRequest = &models.DeviceStartRequest{
						Device:       "drive",
						Server:       selectedDrive.LocalDrive.Name,
						VendorID:     "0x1d6b",
						ProductID:    "0x0104",
						ProductName:  "USBridge Local CD-ROM",
						Manufacturer: "USBridge",
					}
					logrus.Infof("📱 Подготовка устройства из API: %s", selectedDrive.Name)
				}
			} else if (selectedDrive.Source == "local" || selectedDrive.Source == "user") && selectedDrive.DiskInfo != nil {
				// Для любого источника (файл или папка) используем NBD
				// NBD сервер будет создавать виртуальный образ для папок

				// Локальный файл или папка - NBD монтирование
				localIP, err := dw.getLocalIP()
				if err != nil {
					dw.showErrorAsync(fmt.Errorf("ошибка получения локального IP: %v", err))
					return
				}

				nbdPort, err := dw.getAvailablePort()
				if err != nil {
					dw.showErrorAsync(fmt.Errorf("ошибка получения свободного порта: %v", err))
					return
				}

				exportName := selectedDrive.DiskInfo.Name

				// Проверяем, есть ли уже NBD сервер для этого экспорта
				if existingServer, exists := dw.nbdServers[exportName]; exists {
					logrus.Infof("⚠️ NBD сервер для экспорта '%s' уже существует, останавливаем его перед созданием нового", exportName)
					if existingServer.IsRunning() {
						if err := existingServer.Stop(); err != nil {
							logrus.Warnf("⚠️ Ошибка остановки существующего NBD сервера: %v", err)
						}
					}
					// Удаляем из карты
					delete(dw.nbdServers, exportName)
				}

				nbdServer, err := dw.startNBDServer(selectedDrive.DiskInfo, nbdPort, exportName, selectedDrive.ReadOnly)
				if err != nil {
					dw.showErrorAsync(fmt.Errorf("ошибка запуска NBD сервера: %v", err))
					return
				}

				dw.nbdServers[exportName] = nbdServer

				// Для qemu-nbd — метка тома из образа (как диск называется внутри), не имя файла.
				exportNameForAPI := nbdServer.NBDExportNameForAPI()
				logrus.Infof("📁 Подготовка локального файла: %s, export_name для API: %s", selectedDrive.Name, exportNameForAPI)

				deviceRequest = &models.DeviceStartRequest{
					Device:                  "drive",
					Server:                  localIP,
					Port:                    nbdPort,
					ExportName:              exportNameForAPI,
					NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
					ReadOnly:                selectedDrive.ReadOnly,
					VendorID:                "0x1d6b",
					ProductID:               "0x0104",
					ProductName:             "USBridge NBD CD-ROM",
					Manufacturer:            "USBridge",
				}
			} else {
				logrus.Warnf("⚠️ Неизвестный тип устройства: %s", selectedDrive.Name)
				continue
			}

			if deviceRequest != nil {
				deviceRequests = append(deviceRequests, *deviceRequest)
			}
		}

		if len(deviceRequests) == 0 {
			dw.showErrorAsync(fmt.Errorf("не удалось подготовить устройства для монтирования"))
			return
		}

		// Имена NBD-экспортов для UI «монтируется» (по выбранным дискам; для qemu-nbd в запросе ExportName пустой)
		nbdExportNamesForUI := make(map[string]bool)
		for _, req := range deviceRequests {
			if req.Device != "drive" {
				continue
			}
			// Найти имя по порту (у нас один NBD на порт)
			for name, srv := range dw.nbdServers {
				if !srv.IsRunning() {
					continue
				}
				st := srv.GetServerStatus()
				if p, ok := st["server_port"]; ok {
					var port int
					switch v := p.(type) {
					case int:
						port = v
					case int64:
						port = int(v)
					case float64:
						port = int(v)
					default:
						continue
					}
					if port == req.Port {
						nbdExportNamesForUI[name] = true
						break
					}
				}
			}
		}

		// Если есть NBD серверы, запускаем проверку готовности для каждого и ожидаем
		if len(dw.nbdServers) > 0 {
			logrus.Infof("📡 [MOUNT-NBD-1] Запуск проверки готовности для %d NBD серверов...", len(dw.nbdServers))

			// Запускаем SignalReady() для всех серверов - они запустят проверку асинхронно
			for exportName, nbdServer := range dw.nbdServers {
				logrus.Infof("  📡 [MOUNT-NBD-2] Запуск проверки готовности для: %s", exportName)
				nbdServer.SignalReady()
			}

			logrus.Infof("⏱️ [MOUNT-NBD-3] Ожидание готовности %d NBD серверов (таймаут: 30 секунд)...", len(dw.nbdServers))

			// Ждем готовности всех NBD серверов
			// Каждый сервер делает до 20 попыток по 100ms = ~2 секунды на сервер
			// Даем 30 секунд общего таймаута для всех серверов
			readyCount := 0
			timeoutChan := time.After(30 * time.Second)
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()

			serversToWait := make(map[string]service.NBDRunner)
			for exportName, srv := range dw.nbdServers {
				serversToWait[exportName] = srv
			}

		waitLoop:
			for {
				select {
				case <-timeoutChan:
					// Таймаут - проверяем какие серверы не готовы
					notReadyServers := []string{}
					for exportName := range serversToWait {
						notReadyServers = append(notReadyServers, exportName)
					}
					logrus.Errorf("❌ [MOUNT-NBD-TIMEOUT] Таймаут ожидания готовности NBD серверов. Не готовы: %v", notReadyServers)
					dw.showErrorAsync(fmt.Errorf("таймаут ожидания готовности NBD серверов: %v", notReadyServers))
					return

				case <-ticker.C:
					// Проверяем готовность серверов
					for exportName, nbdServer := range serversToWait {
						select {
						case <-nbdServer.WaitReady():
							logrus.Infof("✅ [MOUNT-NBD-READY] NBD сервер %s готов к приему соединений", exportName)
							delete(serversToWait, exportName)
							readyCount++
						default:
							// Сервер еще не готов, продолжаем ждать
						}
					}

					// Если все серверы готовы, выходим
					if len(serversToWait) == 0 {
						logrus.Infof("✅ [MOUNT-NBD-4] Все %d NBD серверов готовы к приему соединений", readyCount)
						break waitLoop
					}
				}
			}
		}

		// Проверка FRP перед монтированием NBD: proxies должны быть зарегистрированы в frps
		if len(dw.nbdServers) > 0 && dw.frpService != nil && !dw.frpService.IsRunning() {
			logrus.Errorf("❌ [MOUNT-NBD-FRP] FRP туннель не активен, NBD proxies не зарегистрированы в frps")
			dw.showErrorAsync(fmt.Errorf("FRP туннель не активен — переподключитесь перед монтированием NBD"))
			return
		}
		if len(dw.nbdServers) > 0 && dw.frpService != nil {
			logrus.Infof("✅ [MOUNT-NBD-FRP] FRP туннель активен, NBD proxies (nbd_srv1-16) зарегистрированы")
		}

		// Задержка после готовности NBD: Bridge делает StopAll, cleanup, NBD-WAIT (~3 с) до первого подключения.
		// 1 с — запас на: очистку после verifyReadiness (conn.Close), стабилизацию FRP туннеля.
		if len(dw.nbdServers) > 0 {
			logrus.Infof("⏱️ [MOUNT-NBD-DELAY] Ожидание 1 с перед отправкой API (NBD готов, даём FRP/туннелю стабилизироваться)")
			time.Sleep(1 * time.Second)
		}

		// Создаем batch запрос для всех устройств
		batchRequest := models.DeviceStartBatchRequest(deviceRequests)

		dw.updateStatusAsync("Запуск устройств...")
		logrus.Infof("🚀 [MOUNT-API-1] Запуск %d устройств, отправляем запрос /api/device/start", len(deviceRequests))
		for i, req := range deviceRequests {
			logrus.Infof("   📤 [MOUNT-API-1] Устройство %d: device=%s, server=%s, port=%d, export_name=%s, read_only=%v", i+1, req.Device, req.Server, req.Port, req.ExportName, req.ReadOnly)
		}

		deviceResp, err := dw.startDevicesWithRetry(batchRequest)
		if err != nil {
			logrus.Errorf("❌ [MOUNT-API-ERROR] Ошибка запуска устройств: %v", err)
			dw.showErrorAsync(fmt.Errorf("ошибка запуска устройств: %v", err))
			return
		}

		// Логируем успешный ответ API
		logrus.Infof("✅ [MOUNT-API-2] API ответ от USB Bridge 2:")
		logrus.Infof("  - Success: %v", deviceResp.Success)
		logrus.Infof("  - Message: %s", deviceResp.Message)
		if deviceResp.Data != nil {
			logrus.Infof("  - Data: %+v", deviceResp.Data)
		}

		// Уведомляем о типе манипулятора (мышь/тачскрин) для синхронизации с экраном управления
		for _, req := range deviceRequests {
			if req.Device == "mouse" && dw.onMouseTypeChanged != nil {
				t := req.Type
				if t != "mouse" && t != "touchscreen" && t != "absolute" {
					t = "mouse"
				}
				dw.onMouseTypeChanged(t)
				break
			}
		}

		// Отмечаем монтируемые устройства для UI (по именам экспортов из nbdServers, не по req.ExportName — у qemu-nbd он пустой)
		mountingExportNames := nbdExportNamesForUI
		dw.updateUIAsync(func() {
			dw.setMountingStateByExportNames(mountingExportNames, true)
			dw.devicesList.Refresh()
		})

		// Очищаем выбор
		dw.updateUIAsync(func() {
			dw.selectedItems = make(map[int]bool)
		})

		// Опрашиваем device/info пока mount_in_progress (202 Accepted); кнопки разблокируются по завершении
		reEnableButtons = false
		go dw.pollMountStatus(mountingExportNames)

		logrus.Infof("✅ Запрос на монтирование %d устройств отправлен", len(deviceRequests))
	}()
}

// handleUnmount обрабатывает размонтирование.
// Если ничего не выбрано — через подтверждение отключает все устройства.
// Если выбраны устройства — отключает только их (запрос подключения оставшихся).
func (dw *DiskWidget) handleUnmount() {
	dw.setButtonsEnabled(false) // блокируем сразу после клика (исключить даблклик)
	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		dw.setButtonsEnabled(true)
		return
	}

	// Находим подключенные устройства
	var mountedDrives []DriveItem
	var mountedIndices []int
	for i, drive := range dw.allDrives {
		if drive.IsMounted {
			mountedDrives = append(mountedDrives, drive)
			mountedIndices = append(mountedIndices, i)
		}
	}

	if len(mountedDrives) == 0 {
		logrus.Warnf("⚠️ Нет подключенных устройств для размонтирования")
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Information, i18n.Current.NoMountedDevices, dw.window)
		}
		dw.setButtonsEnabled(true)
		return
	}

	// Проверяем, есть ли выбранные устройства
	selectedAndMountedIndices := make(map[int]bool)
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) && dw.allDrives[id].IsMounted {
			selectedAndMountedIndices[id] = true
		}
	}

	confirmMsg := i18n.Current.UnmountAllConfirm
	unmountAll := len(selectedAndMountedIndices) == 0

	if !unmountAll {
		confirmMsg = i18n.Current.UnmountSelectedConfirm
	}

	finalUnmountAll := unmountAll
	finalSelectedIndices := make(map[int]bool)
	for k, v := range selectedAndMountedIndices {
		finalSelectedIndices[k] = v
	}
	finalMountedDrives := make([]DriveItem, len(mountedDrives))
	copy(finalMountedDrives, mountedDrives)
	finalMountedIndices := make([]int, len(mountedIndices))
	copy(finalMountedIndices, mountedIndices)

	ShowConfirmYesLeft(i18n.Current.Confirmation, confirmMsg, func(ok bool) {
		if !ok {
			dw.setButtonsEnabled(true) // разблокируем при отмене
			return
		}
		go dw.doUnmount(finalUnmountAll, finalSelectedIndices, finalMountedDrives, finalMountedIndices)
	}, dw.window)
}

// doUnmount выполняет размонтирование. unmountAll: отключить все; иначе — только selectedIndices.
func (dw *DiskWidget) doUnmount(unmountAll bool, selectedIndices map[int]bool, mountedDrives []DriveItem, mountedIndices []int) {
	defer func() {
		dw.updateUIAsync(func() {
			dw.setButtonsEnabled(true)
		})
	}()

	if unmountAll {
		dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
		if err := dw.usbClient.StopAllDevices(); err != nil {
			logrus.Warnf("⚠️ Ошибка остановки устройств: %v", err)
		} else {
			logrus.Infof("✅ Все устройства остановлены")
		}
		dw.stopNBDAndCleanup(mountedDrives, true)
	} else {
		// Отключаем только выбранные: переподключаем оставшиеся (keep)
		keepIndices := make(map[int]bool)
		for _, idx := range mountedIndices {
			if !selectedIndices[idx] {
				keepIndices[idx] = true
			}
		}

		drivesToUnmount := make([]DriveItem, 0, len(selectedIndices))
		for idx := range selectedIndices {
			if idx < len(dw.allDrives) {
				drivesToUnmount = append(drivesToUnmount, dw.allDrives[idx])
			}
		}

		if len(keepIndices) == 0 {
			dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
			if err := dw.usbClient.StopAllDevices(); err != nil {
				logrus.Warnf("⚠️ Ошибка остановки устройств: %v", err)
			}
			dw.stopNBDAndCleanup(drivesToUnmount, true)
		} else {
			// Строим batch из устройств, которые остаём подключёнными
			var deviceRequests []models.DeviceStartRequest
			for idx := range keepIndices {
				if idx >= len(dw.allDrives) {
					continue
				}
				req, err := dw.buildDeviceRequestForDrive(dw.allDrives[idx], true)
				if err != nil {
					logrus.Warnf("⚠️ Не удалось построить запрос для %s: %v", dw.allDrives[idx].Name, err)
					continue
				}
				deviceRequests = append(deviceRequests, *req)
			}
			if len(deviceRequests) > 0 {
				batchRequest := models.DeviceStartBatchRequest(deviceRequests)
				dw.updateStatusAsync(i18n.Current.StoppingAllDevices)
				if _, err := dw.startDevicesWithRetry(batchRequest); err != nil {
					logrus.Warnf("⚠️ Ошибка переподключения устройств: %v", err)
				}
			}
			dw.stopNBDAndCleanup(drivesToUnmount, false)
		}
	}

	time.Sleep(2 * time.Second)
	dw.updateUIAsync(func() {
		dw.loadMountedDevices()
		dw.loadLocalDrives()
	})
	if dw.updateStatus != nil {
		dw.updateStatus()
	}
	dw.updateStatusAsync(i18n.Current.AllDevicesUnmounted)
	logrus.Infof("✅ Размонтирование завершено")
}

// stopNBDAndCleanup останавливает NBD серверы для drives и при stopAll очищает карту; иначе — только для переданных drives.
func (dw *DiskWidget) stopNBDAndCleanup(drives []DriveItem, stopAll bool) {
	dw.updateStatusAsync(i18n.Current.StoppingNBDServers)
	toStop := make(map[string]bool)
	if stopAll {
		for exportName := range dw.nbdServers {
			toStop[exportName] = true
		}
	} else {
		for _, drive := range drives {
			if drive.DiskInfo != nil {
				exportName := drive.DiskInfo.Name
				if _, exists := dw.nbdServers[exportName]; exists {
					toStop[exportName] = true
				}
			}
		}
	}
	for exportName := range toStop {
		if nbdServer, exists := dw.nbdServers[exportName]; exists {
			if nbdServer.IsRunning() {
				if err := nbdServer.Stop(); err != nil {
					logrus.Warnf("⚠️ Ошибка остановки NBD сервера %s: %v", exportName, err)
				}
			}
			delete(dw.nbdServers, exportName)
		}
	}
	if stopAll {
		dw.nbdServers = make(map[string]service.NBDRunner)
	}
	if runtime.GOOS == "android" && dw.safHelper != nil {
		for _, drive := range drives {
			if drive.DiskInfo != nil && strings.HasPrefix(drive.DiskInfo.URI, "content://") {
				_ = dw.safHelper.CloseFD(drive.DiskInfo.URI)
			}
		}
	}
}

// buildDeviceRequestForDrive строит DeviceStartRequest для drive. useExistingNBD: для NBD использовать уже запущенный сервер.
func (dw *DiskWidget) buildDeviceRequestForDrive(drive DriveItem, useExistingNBD bool) (*models.DeviceStartRequest, error) {
	if drive.Source == "keyboard" {
		return &models.DeviceStartRequest{
			Device: "keyboard", VendorID: "0x1d6b", ProductID: "0x0104",
			ProductName: "USBridge Keyboard", Manufacturer: "USBridge", KeyboardMode: true,
		}, nil
	}
	if drive.Source == "mouse" {
		mouseType := drive.MouseType
		if mouseType != "mouse" && mouseType != "touchscreen" && mouseType != "absolute" {
			mouseType = "mouse"
		}
		return &models.DeviceStartRequest{
			Device: "mouse", Type: mouseType,
			VendorID: "0x1d6b", ProductID: "0x0104",
			ProductName: "USBridge Mouse", Manufacturer: "USBridge",
		}, nil
	}
	if drive.Source == "rndis" {
		rndisMode := normalizeRNDISMode(drive.RNDISMode)
		return &models.DeviceStartRequest{
			Device: "rndis", VendorID: "0x1d6b", ProductID: "0x0104",
			ProductName: "USBridge RNDIS", Manufacturer: "USBridge", RNDISMode: rndisMode,
		}, nil
	}
	if drive.Source == "api" && drive.LocalDrive != nil {
		if drive.LocalDrive.SourceType == "mtp" {
			return &models.DeviceStartRequest{
				Device: "mtp", Server: drive.LocalDrive.Name,
				VendorID: "0x1d6b", ProductID: "0x0104", ProductName: "BackupDrive", Manufacturer: "USBridge",
			}, nil
		}
		return &models.DeviceStartRequest{
			Device: "drive", Server: drive.LocalDrive.Name,
			VendorID: "0x1d6b", ProductID: "0x0104", ProductName: "USBridge Local CD-ROM", Manufacturer: "USBridge",
		}, nil
	}
	if (drive.Source == "local" || drive.Source == "user") && drive.DiskInfo != nil && useExistingNBD {
		exportName := drive.DiskInfo.Name
		nbdServer, exists := dw.nbdServers[exportName]
		if !exists || !nbdServer.IsRunning() {
			return nil, fmt.Errorf("NBD сервер для %s не найден или не запущен", exportName)
		}
		status := nbdServer.GetServerStatus()
		portVal, ok := status["server_port"]
		if !ok {
			return nil, fmt.Errorf("порт NBD сервера %s не найден", exportName)
		}
		var port int
		switch p := portVal.(type) {
		case int:
			port = p
		case int64:
			port = int(p)
		case float64:
			port = int(p)
		default:
			return nil, fmt.Errorf("неверный тип порта для %s", exportName)
		}
		localIP, err := dw.getLocalIP()
		if err != nil {
			return nil, err
		}
		// Для qemu-nbd — метка тома из образа; nbd_handshake_empty_export=true чтобы Bridge использовал пустое имя в NBD handshake.
		exportNameForAPI := nbdServer.NBDExportNameForAPI()
		return &models.DeviceStartRequest{
			Device:                  "drive",
			Server:                  localIP,
			Port:                    port,
			ExportName:              exportNameForAPI,
			NBDHandshakeEmptyExport: nbdServer.NBDHandshakeEmptyExport(),
			ReadOnly:                drive.ReadOnly,
			VendorID:                "0x1d6b",
			ProductID:               "0x0104",
			ProductName:             "USBridge NBD CD-ROM",
			Manufacturer:            "USBridge",
		}, nil
	}
	return nil, fmt.Errorf("неизвестный тип устройства: %s", drive.Name)
}

// handleScan обрабатывает обновление данных

// countSelectedItems возвращает количество выбранных элементов
func (dw *DiskWidget) countSelectedItems() int {
	count := 0
	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			count++
		}
	}
	return count
}

// updateButtons обновляет состояние кнопок
// ВАЖНО: Эта функция безопасна для вызова как из UI потока, так и из горутин
func (dw *DiskWidget) updateButtons() {
	selectedCount := 0
	selectedNotMountedCount := 0
	mountedCount := 0

	for id, selected := range dw.selectedItems {
		if selected && id < len(dw.allDrives) {
			selectedCount++
			if !dw.allDrives[id].IsMounted {
				selectedNotMountedCount++
			}
		}
	}
	for _, drive := range dw.allDrives {
		if drive.IsMounted {
			mountedCount++
		}
	}

	hasMountedDevices := mountedCount > 0
	canAdd := selectedNotMountedCount > 0 && (mountedCount+selectedNotMountedCount) <= MaxDevicesToMount

	fyne.Do(func() {
		// Mount: скрыта когда ничего не выделено
		if selectedCount == 0 {
			dw.mountBtn.Hide()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Hide()
			}
		} else {
			dw.mountBtn.Show()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Show()
			}
		}

		// Unmount: всегда видна, когда есть подключённые устройства (ничего выделено → отключить все; что-то выделено → отключить только выделенное)
		if hasMountedDevices {
			dw.unmountBtn.Show()
			dw.unmountBtn.Enable()
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Show()
				dw.compactUnmountBtn.Enable()
			}
		} else {
			dw.unmountBtn.Show()
			dw.unmountBtn.Disable()
			if dw.compactUnmountBtn != nil {
				dw.compactUnmountBtn.Show()
				dw.compactUnmountBtn.Disable()
			}
		}

		if selectedCount == 0 {
			return
		}

		if canAdd {
			dw.mountBtn.Enable()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Enable()
			}
		} else {
			dw.mountBtn.Disable()
			if dw.compactMountBtn != nil {
				dw.compactMountBtn.Disable()
			}
		}
	})
}

// Refresh обновляет виджет
func (dw *DiskWidget) Refresh() {
	dw.loadLocalDrives()
	dw.loadLocalFiles()
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
	addImageBtn := widget.NewButton("➕", dw.handleAddImage)

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

// setUploadStateByPath устанавливает состояние загрузки по пути файла (индекс может меняться при refresh)
func (dw *DiskWidget) setUploadStateByPath(path string, uploading bool, progress, speed float64) {
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil && dw.allDrives[i].DiskInfo.Path == path {
			dw.allDrives[i].IsUploading = uploading
			dw.allDrives[i].UploadProgress = progress
			dw.allDrives[i].UploadSpeed = speed
			return
		}
	}
}

// refreshDriveItemByPath обновляет отображение элемента в списке по пути
func (dw *DiskWidget) refreshDriveItemByPath(path string) {
	for i := range dw.allDrives {
		if dw.allDrives[i].DiskInfo != nil && dw.allDrives[i].DiskInfo.Path == path {
			dw.devicesList.RefreshItem(i)
			return
		}
	}
	dw.devicesList.Refresh() // если не нашли — обновляем весь список
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

	// Пытаемся подключиться к USB Bridge 2 чтобы определить локальный IP
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
		runner := service.NewQemuNBDRunner(diskInfo.Path, readOnly)
		if err := runner.EnsureQemuNbdForExport(); err != nil {
			return nil, fmt.Errorf("для образов VMDK/QCOW2/VDI нужен qemu-nbd: %w", err)
		}
		if err := runner.Start(port); err != nil {
			return nil, fmt.Errorf("для образов VMDK/QCOW2/VDI нужен qemu-nbd (установите QEMU): %w", err)
		}
		logrus.Infof("✅ [START-NBD-QEMU] qemu-nbd запущен на 127.0.0.1:%d для %s", port, diskInfo.Name)
		logrus.Infof("   📡 FRP туннель: nbd_srv%d -> localhost:%d", port-10808, port)
		return runner, nil
	}

	// go-nbd: ISO, raw, img (файл как есть)
	config := &models.AppConfig{NBDPort: port}
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

	logrus.Infof("✅ [START-NBD-7-SUCCESS] NBD сервер для '%s' на 127.0.0.1:%d", diskInfo.Name, port)
	logrus.Infof("   📡 FRP туннель: nbd_srv%d -> localhost:%d", port-10808, port)
	return nbdServer, nil
}

// handleAddImage обрабатывает добавление образа диска из файловой системы
func (dw *DiskWidget) handleAddImage() {
	if dw.window == nil {
		logrus.Warn("⚠️ Окно не установлено")
		return
	}

	// Проверяем доступ к хранилищу на Android
	if !dw.checkStoragePermission() {
		fyne.Do(func() {
			dialog.ShowInformation(
				i18n.Current.StoragePermissionRequired,
				i18n.Current.StoragePermissionMessage+"\n\n"+i18n.Current.StoragePermissionSteps,
				dw.window,
			)
		})
		return
	}

	// Создаем диалог выбора файла
	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			logrus.Errorf("❌ [ADD-IMAGE-ERROR] Ошибка при выборе файла: %v", err)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf(i18n.Current.ErrorSelectingFile, err), dw.window)
			})
			return
		}

		if reader == nil {
			// Пользователь отменил выбор
			logrus.Info("ℹ️ [ADD-IMAGE] Выбор файла отменен")
			return
		}

		uri := reader.URI()
		reader.Close()

		// Получаем путь к файлу
		fileName := uri.Name()
		uriString := uri.String()

		logrus.Infof("📂 [ADD-IMAGE-1] Выбран файл: %s (URI: %s)", fileName, uriString)
		logrus.Infof("📂 [ADD-IMAGE-1] Runtime GOOS: %s", runtime.GOOS)

		var filePath string
		var fileSize int64

		// На Android сохраняем content:// URI напрямую
		if runtime.GOOS == "android" && strings.HasPrefix(uriString, "content://") {
			logrus.Infof("📍 [ADD-IMAGE-ANDROID-2] Обнаружен Android content:// URI")

			// Сохраняем постоянный доступ к URI через SAF
			logrus.Infof("📍 [ADD-IMAGE-ANDROID-3] Вызов TakePersistableUriPermission для URI: %s", uriString)
			if err := dw.safHelper.TakePersistableUriPermission(uriString); err != nil {
				logrus.Errorf("❌ [ADD-IMAGE-ANDROID-3-ERROR] Не удалось сохранить разрешение для URI: %v", err)
				// Продолжаем, возможно разрешение уже есть
			} else {
				logrus.Infof("✅ [ADD-IMAGE-ANDROID-3-SUCCESS] Разрешение для URI сохранено")
			}

			// Используем URI как путь (для Android)
			filePath = uriString
			logrus.Infof("📍 [ADD-IMAGE-ANDROID-4] Используем content:// URI напрямую: %s", filePath)

			// Получаем размер через SAF
			// Проверяем, является ли файл из Google Drive
			isGoogleDrive := strings.Contains(uriString, "com.google.android.apps.docs.storage")
			if isGoogleDrive {
				logrus.Warnf("⚠️  [ADD-IMAGE-ANDROID-5-GDRIVE] Файл из Google Drive! Размер будет получен асинхронно при первом монтировании")
				// Для Google Drive файлов используем примерный размер
				// Реальный размер будет получен при монтировании
				fileSize = 0 // Размер неизвестен, получим позже
			} else {
				// Для обычных файлов получаем размер синхронно
				logrus.Infof("📍 [ADD-IMAGE-ANDROID-5] Попытка получить размер файла через SAF")
				file, err := dw.safHelper.OpenFileDescriptor(uriString, "rw")
				if err == nil && file != nil {
					stat, err := file.Stat()
					if err == nil {
						fileSize = stat.Size()
						logrus.Infof("✅ [ADD-IMAGE-ANDROID-5-SUCCESS] Размер файла через SAF FD: %d байт", fileSize)
					} else {
						logrus.Warnf("⚠️ [ADD-IMAGE-ANDROID-5-ERROR] Не удалось получить размер: %v", err)
					}
					// НЕ закрываем файл! Он остается в кэше для последующего использования в NBD
					logrus.Infof("📍 [ADD-IMAGE-ANDROID-5] Файл остается открытым в кэше SAFHelper")
				} else {
					logrus.Warnf("⚠️ [ADD-IMAGE-ANDROID-5-ERROR] Не удалось открыть файл через SAF: %v", err)
				}
			}
		} else {
			// Десктоп или file:// URI
			logrus.Infof("📍 [ADD-IMAGE-DESKTOP-2] Десктоп режим или file:// URI")
			filePath = dw.convertAndroidURIToPath(uriString, fileName)
			logrus.Infof("📁 [ADD-IMAGE-DESKTOP-3] Путь к файлу: %s", filePath)

			// Получаем размер файла
			if info, err := os.Stat(filePath); err == nil {
				fileSize = info.Size()
				logrus.Infof("✅ [ADD-IMAGE-DESKTOP-4] Размер файла: %d байт", fileSize)
			} else {
				logrus.Warnf("⚠️ [ADD-IMAGE-DESKTOP-4-ERROR] Не удалось получить размер файла: %v", err)
			}
		}

		// Проверяем расширение файла
		ext := strings.ToLower(filepath.Ext(fileName))
		supported := false
		for _, supportedType := range dw.supportedTypes {
			if strings.ToLower(supportedType) == ext {
				supported = true
				break
			}
		}

		if !supported {
			logrus.Warnf("⚠️ Неподдерживаемый формат файла: %s", fileName)
			fyne.Do(func() {
				dialog.ShowError(fmt.Errorf(i18n.Current.UnsupportedFileFormat, strings.Join(dw.supportedTypes, ", ")), dw.window)
			})
			return
		}

		// Создаем DiskInfo для файла
		diskInfo := &models.DiskInfo{
			Name:        fileName,
			Path:        filePath, // На Android это content:// URI
			URI:         uriString,
			Size:        fileSize,
			Type:        strings.TrimPrefix(ext, "."),
			Description: fmt.Sprintf("Пользовательский образ: %s", fileName),
			IsActive:    false,
		}

		logrus.Infof("📍 [ADD-IMAGE-6] Создан DiskInfo: Name=%s, Path=%s, URI=%s, Size=%d",
			diskInfo.Name, diskInfo.Path, diskInfo.URI, diskInfo.Size)

		// Проверяем, не добавлен ли уже этот файл (проверяем по URI для Android)
		for _, existingImage := range dw.userImages {
			if existingImage.URI == uriString || existingImage.Path == filePath {
				logrus.Warnf("⚠️ [ADD-IMAGE-6-WARN] Файл уже добавлен: %s (URI: %s)", filePath, uriString)
				fyne.Do(func() {
					dialog.ShowInformation(i18n.Current.Information, i18n.Current.FileAlreadyAdded, dw.window)
				})
				return
			}
		}

		// Добавляем образ в список пользовательских образов
		dw.userImages = append(dw.userImages, diskInfo)
		logrus.Infof("✅ [ADD-IMAGE-7] Образ добавлен в userImages: %s (всего: %d)", diskInfo.Name, len(dw.userImages))

		// Сохраняем в preferences
		logrus.Infof("📍 [ADD-IMAGE-8] Сохранение в preferences...")
		dw.saveUserImagesToPreferences()
		logrus.Infof("✅ [ADD-IMAGE-8-SUCCESS] Preferences сохранены")

		// Обновляем UI
		logrus.Infof("📍 [ADD-IMAGE-9] Обновление UI...")
		dw.updateUIAsync(func() {
			dw.combineDrives()
			dw.devicesList.Refresh()
			logrus.Infof("✅ [ADD-IMAGE-9-SUCCESS] UI обновлен, образ отображен в списке")
		})
	}, dw.window)

	// На десктопе: настраиваем удобный вид (список вместо сетки с огромными иконками),
	// размер окна и фильтр по типам образов
	if runtime.GOOS != "android" {
		fileDialog.SetView(dialog.ListView) // Компактный список — иконки не занимают пол-экрана
		fileDialog.SetTitleText(i18n.Current.SelectDiskImage)
		fileDialog.SetFilter(storage.NewExtensionFileFilter(dw.supportedTypes))
	}

	fileDialog.Show()

	// Resize нужно вызывать ПОСЛЕ Show() — иначе не применяется
	if runtime.GOOS != "android" {
		go func() {
			time.Sleep(50 * time.Millisecond)
			fyne.Do(func() {
				fileDialog.Resize(fyne.NewSize(900, 650))
			})
		}()
	}
}

// checkStoragePermission проверяет доступ к хранилищу (только для Android)
func (dw *DiskWidget) checkStoragePermission() bool {
	// Проверяем только на Android по наличию специфичных путей
	testPaths := []string{
		"/storage/emulated/0",
		"/sdcard",
	}

	hasAndroidPaths := false
	for _, path := range testPaths {
		if _, err := os.Stat(path); err == nil {
			hasAndroidPaths = true
			// Пробуем прочитать директорию
			if _, err := os.ReadDir(path); err == nil {
				logrus.Infof("✅ Доступ к хранилищу есть: %s", path)
				return true
			}
		}
	}

	// Если нет Android путей - это десктоп, возвращаем true
	if !hasAndroidPaths {
		return true
	}

	logrus.Warn("⚠️ Нет доступа к хранилищу на Android")
	return false
}

// convertAndroidURIToPath конвертирует Android document URI в реальный путь файла
func (dw *DiskWidget) convertAndroidURIToPath(uriString string, fileName string) string {
	// Проверяем различные типы Android URI

	// 1. content://com.android.externalstorage.documents/document/primary%3A...
	if strings.HasPrefix(uriString, "content://com.android.externalstorage.documents/document/primary") {
		// Извлекаем путь после "primary:"
		parts := strings.Split(uriString, "primary")
		if len(parts) >= 2 {
			// Убираем %3A (это URL-encoded ":")
			relativePath := strings.TrimPrefix(parts[1], "%3A")
			relativePath = strings.TrimPrefix(relativePath, ":")

			// Декодируем URL-encoded символы
			relativePath = strings.ReplaceAll(relativePath, "%20", " ")
			relativePath = strings.ReplaceAll(relativePath, "%2F", "/")

			// Пробуем различные пути к внешнему хранилищу
			possiblePaths := []string{
				filepath.Join("/storage/emulated/0", relativePath),
				filepath.Join("/sdcard", relativePath),
				filepath.Join("/mnt/sdcard", relativePath),
				filepath.Join(os.Getenv("EXTERNAL_STORAGE"), relativePath),
			}

			for _, path := range possiblePaths {
				if path == "" {
					continue
				}
				if _, err := os.Stat(path); err == nil {
					logrus.Infof("✅ Найден файл по пути: %s", path)
					return path
				}
			}

			// Если не нашли, возвращаем первый вариант
			return possiblePaths[0]
		}
	}

	// 2. content://com.android.externalstorage.documents/document/...
	if strings.HasPrefix(uriString, "content://") && strings.Contains(uriString, "/document/") {
		// Пытаемся извлечь имя файла и найти его в стандартных местах
		searchPaths := []string{
			"/storage/emulated/0",
			"/sdcard",
			"/mnt/sdcard",
			os.Getenv("EXTERNAL_STORAGE"),
		}

		for _, basePath := range searchPaths {
			if basePath == "" {
				continue
			}

			// Проверяем в корне
			fullPath := filepath.Join(basePath, fileName)
			if _, err := os.Stat(fullPath); err == nil {
				logrus.Infof("✅ Найден файл в корне: %s", fullPath)
				return fullPath
			}

			// Проверяем в Download
			fullPath = filepath.Join(basePath, "Download", fileName)
			if _, err := os.Stat(fullPath); err == nil {
				logrus.Infof("✅ Найден файл в Download: %s", fullPath)
				return fullPath
			}
		}
	}

	// 3. file:// URI - просто убираем префикс
	if strings.HasPrefix(uriString, "file://") {
		return strings.TrimPrefix(uriString, "file://")
	}

	// 4. Если это уже нормальный путь
	if strings.HasPrefix(uriString, "/") {
		return uriString
	}

	// По умолчанию пытаемся использовать как путь напрямую
	logrus.Warnf("⚠️ Не удалось конвертировать URI, используем как есть: %s", uriString)
	return uriString
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

// saveUserImagesToPreferences сохраняет список пользовательских образов в preferences
func (dw *DiskWidget) saveUserImagesToPreferences() {
	if dw.app == nil {
		logrus.Warn("⚠️ App не установлен, не можем сохранить образы")
		return
	}

	prefs := dw.app.Preferences()

	// Сохраняем количество образов
	prefs.SetInt("user_images_count", len(dw.userImages))

	// Сохраняем каждый образ
	for i, img := range dw.userImages {
		prefix := fmt.Sprintf("user_image_%d_", i)
		prefs.SetString(prefix+"name", img.Name)
		prefs.SetString(prefix+"path", img.Path)
		prefs.SetString(prefix+"uri", img.URI)
		prefs.SetString(prefix+"type", img.Type)
		prefs.SetString(prefix+"description", img.Description)
		// Size как строку, т.к. SetInt64 может не работать на всех платформах
		prefs.SetString(prefix+"size", fmt.Sprintf("%d", img.Size))
	}

	logrus.Infof("💾 Сохранено %d пользовательских образов в preferences", len(dw.userImages))
}

// loadUserImagesFromPreferences загружает список пользовательских образов из preferences
func (dw *DiskWidget) loadUserImagesFromPreferences() {
	if dw.app == nil {
		logrus.Warn("⚠️ App не установлен, не можем загрузить образы")
		return
	}

	prefs := dw.app.Preferences()
	count := prefs.IntWithFallback("user_images_count", 0)

	if count == 0 {
		logrus.Info("ℹ️ Нет сохраненных пользовательских образов")
		return
	}

	dw.userImages = make([]*models.DiskInfo, 0, count)

	// Загружаем каждый образ
	for i := 0; i < count; i++ {
		prefix := fmt.Sprintf("user_image_%d_", i)

		name := prefs.StringWithFallback(prefix+"name", "")
		path := prefs.StringWithFallback(prefix+"path", "")

		if name == "" || path == "" {
			logrus.Warnf("⚠️ Пропускаем образ %d: пустое имя или путь", i)
			continue
		}

		uri := prefs.StringWithFallback(prefix+"uri", "")

		// Проверяем, существует ли файл
		// Для Android content:// URI используем SAFHelper, для обычных путей - os.Stat
		fileExists := false
		if runtime.GOOS == "android" && strings.HasPrefix(path, "content://") {
			// Проверяем, является ли файл из Google Drive
			isGoogleDrive := strings.Contains(path, "com.google.android.apps.docs.storage")

			if isGoogleDrive {
				// Для файлов из Google Drive пропускаем проверку при загрузке, чтобы не тормозить старт
				// Пользователь сможет использовать их, но загрузка будет при первом доступе
				logrus.Warnf("⚠️ Файл из Google Drive при загрузке из preferences: %s - добавляем без проверки", path)
				fileExists = true // Считаем что файл существует, проверим при первом обращении
			} else {
				// На Android проверяем через SAF только для НЕ-Google Drive файлов
				if dw.safHelper != nil {
					file, err := dw.safHelper.OpenFileDescriptor(path, "r")
					if err == nil && file != nil {
						file.Close() // Закрываем сразу, нужна только проверка существования
						fileExists = true
						logrus.Debugf("✅ Файл найден через SAF: %s", path)
					} else {
						logrus.Debugf("⚠️ Файл не найден через SAF: %s (err: %v)", path, err)
					}
				}
			}
		} else {
			// На десктопе используем стандартную проверку
			if _, err := os.Stat(path); err == nil {
				fileExists = true
				logrus.Debugf("✅ Файл найден через os.Stat: %s", path)
			} else {
				logrus.Debugf("⚠️ Файл не найден через os.Stat: %s (err: %v)", path, err)
			}
		}

		if !fileExists {
			logrus.Warnf("⚠️ Пропускаем образ %s: файл не существует по пути %s", name, path)
			continue
		}

		imgType := prefs.StringWithFallback(prefix+"type", "")
		description := prefs.StringWithFallback(prefix+"description", "")
		sizeStr := prefs.StringWithFallback(prefix+"size", "0")

		var size int64
		fmt.Sscanf(sizeStr, "%d", &size)

		diskInfo := &models.DiskInfo{
			Name:        name,
			Path:        path,
			URI:         uri,
			Size:        size,
			Type:        imgType,
			Description: description,
			IsActive:    false,
		}

		dw.userImages = append(dw.userImages, diskInfo)
	}

	logrus.Infof("📂 Загружено %d пользовательских образов из preferences", len(dw.userImages))
}

// removeUserImage удаляет пользовательский образ из списка
func (dw *DiskWidget) removeUserImage(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		logrus.Warnf("⚠️ Неверный индекс образа: %d", driveIndex)
		return
	}

	drive := dw.allDrives[driveIndex]

	// Проверяем, что это пользовательский образ
	if drive.Source != "user" || drive.DiskInfo == nil {
		logrus.Warnf("⚠️ Попытка удалить не пользовательский образ: %s", drive.Name)
		return
	}

	// Находим индекс в массиве userImages
	userImageIndex := -1
	for i, img := range dw.userImages {
		if img.Path == drive.DiskInfo.Path {
			userImageIndex = i
			break
		}
	}

	if userImageIndex == -1 {
		logrus.Warnf("⚠️ Образ не найден в списке пользовательских образов: %s", drive.Name)
		return
	}

	// Показываем диалог подтверждения
	if dw.window != nil {
		fyne.Do(func() {
			ShowConfirmYesLeft(
				i18n.Current.DeleteImageTitle,
				fmt.Sprintf(i18n.Current.DeleteImageConfirm, drive.Name),
				func(confirmed bool) {
					if confirmed {
						// Удаляем из массива
						dw.userImages = append(dw.userImages[:userImageIndex], dw.userImages[userImageIndex+1:]...)
						logrus.Infof("🗑️ Образ удален: %s", drive.Name)

						// Сохраняем в preferences
						dw.saveUserImagesToPreferences()

						// Обновляем UI
						dw.updateUIAsync(func() {
							dw.combineDrives()
							dw.devicesList.Refresh()
						})
					}
				},
				dw.window,
			)
		})
	}
}

// formatPathForDisplay форматирует путь для красивого отображения
// Заменяет content:// на иконку Android и декодирует URL-encoded части пути
func (dw *DiskWidget) formatPathForDisplay(path, fileName string) string {
	// Если это content:// URI
	if strings.HasPrefix(path, "content://") {
		logrus.Debugf("🔍 formatPathForDisplay: исходный path=%s, fileName=%s", path, fileName)

		// Для Downloads Manager (msf:, raw:, etc.) - просто возвращаем имя файла
		if strings.Contains(path, "downloads.documents/document") {
			// Проверяем, есть ли путь в URI после декодирования
			displayPath := strings.TrimPrefix(path, "content://")
			if idx := strings.Index(displayPath, "/document/"); idx != -1 {
				displayPath = displayPath[idx+len("/document/"):]
			}
			// URL-декодирование
			displayPath = strings.ReplaceAll(displayPath, "%3A", ":")
			displayPath = strings.ReplaceAll(displayPath, "%2F", "/")

			// Если после двоеточия есть слэш, значит есть путь
			if strings.Contains(displayPath, ":") && strings.Contains(displayPath, "/") {
				// Есть путь, обрабатываем
				if idx := strings.Index(displayPath, ":"); idx != -1 {
					displayPath = displayPath[idx+1:]
				}
				logrus.Debugf("🔍 formatPathForDisplay (downloads с путем): результат=%s", displayPath)
				return displayPath
			}

			// Нет пути, только ID - возвращаем имя файла
			logrus.Debugf("🔍 formatPathForDisplay (downloads без пути): результат=%s", fileName)
			return fileName
		}

		// Для externalstorage (primary:, home:, etc.)
		displayPath := strings.TrimPrefix(path, "content://")

		// Ищем /document/ и берем все после него
		if idx := strings.Index(displayPath, "/document/"); idx != -1 {
			displayPath = displayPath[idx+len("/document/"):]
		}

		logrus.Debugf("🔍 formatPathForDisplay: после удаления document=%s", displayPath)

		// URL-декодирование основных символов
		displayPath = strings.ReplaceAll(displayPath, "%3A", ":")
		displayPath = strings.ReplaceAll(displayPath, "%2F", "/")
		displayPath = strings.ReplaceAll(displayPath, "%20", " ")
		displayPath = strings.ReplaceAll(displayPath, "%2B", "+")
		displayPath = strings.ReplaceAll(displayPath, "%40", "@")

		logrus.Debugf("🔍 formatPathForDisplay: после декодирования=%s", displayPath)

		// Убираем storage prefix (primary:, home:, etc.) оставляя только путь
		// Например: "primary:Download/file.iso" → "Download/file.iso"
		if idx := strings.Index(displayPath, ":"); idx != -1 {
			displayPath = displayPath[idx+1:]
		}

		// Если после обработки осталась пустая строка или только имя файла без пути
		// возвращаем имя файла с указанием что он в корне
		if displayPath == "" || !strings.Contains(displayPath, "/") {
			logrus.Debugf("🔍 formatPathForDisplay: файл в корне, результат=%s", fileName)
			return fileName
		}

		logrus.Debugf("🔍 formatPathForDisplay: результат=%s", displayPath)
		return displayPath
	}

	// Для обычных путей просто возвращаем как есть
	return path
}

// handleUploadImage обрабатывает загрузку образа на устройство
func (dw *DiskWidget) handleUploadImage(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		logrus.Warnf("⚠️ Неверный индекс образа: %d", driveIndex)
		return
	}

	drive := dw.allDrives[driveIndex]

	// Проверяем, что это пользовательский образ
	if drive.Source != "user" || drive.DiskInfo == nil {
		logrus.Warnf("⚠️ Попытка загрузить не пользовательский образ: %s", drive.Name)
		return
	}

	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	// Показываем диалог подтверждения
	if dw.window != nil {
		fyne.Do(func() {
			ShowConfirmYesLeft(
				i18n.Current.UploadImageTitle,
				fmt.Sprintf(i18n.Current.UploadImageConfirm, drive.Name),
				func(confirmed bool) {
					if confirmed {
						go dw.uploadImageToDevice(drive)
					}
				},
				dw.window,
			)
		})
	}
}

// uploadImageToDevice выполняет загрузку образа на устройство
func (dw *DiskWidget) uploadImageToDevice(drive DriveItem) {
	logrus.Infof("📤 Начало загрузки образа на устройство: %s", drive.Name)

	// Открываем файл для чтения
	var fileReader *os.File
	var err error

	if runtime.GOOS == "android" && strings.HasPrefix(drive.DiskInfo.Path, "content://") {
		// На Android используем SAFHelper
		fileReader, err = dw.safHelper.OpenFileDescriptor(drive.DiskInfo.Path, "r")
		if err != nil {
			dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorOpeningFileForUpload, err))
			return
		}
	} else {
		// На десктопе открываем файл напрямую
		fileReader, err = os.Open(drive.DiskInfo.Path)
		if err != nil {
			dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorOpeningFileForUpload, err))
			return
		}
	}
	defer fileReader.Close()

	// Используем путь для поиска — индекс может измениться после combineDrives (периодический refresh)
	uploadPath := drive.DiskInfo.Path

	// Устанавливаем флаг загрузки
	dw.updateUIAsync(func() {
		dw.setUploadStateByPath(uploadPath, true, 0, 0)
		dw.refreshDriveItemByPath(uploadPath)
	})

	// Callback для обновления прогресса — ищем элемент по пути, т.к. индекс может измениться
	progressCallback := func(percent float64, current, total int64, speed float64, eta time.Duration) {
		logrus.Debugf("🔄 UI callback: %.1f%%, скорость: %.2f МБ/с", percent, speed)
		dw.updateUIAsync(func() {
			dw.setUploadStateByPath(uploadPath, true, percent, speed)
			dw.refreshDriveItemByPath(uploadPath)
		})
	}

	// Загружаем файл на устройство с прогрессом (потоковая передача, UI не зависает)
	err = dw.usbClient.UploadISOWithProgress(drive.DiskInfo.Name, fileReader, progressCallback)

	// Сбрасываем флаг загрузки
	dw.updateUIAsync(func() {
		dw.setUploadStateByPath(uploadPath, false, 0, 0)
		dw.refreshDriveItemByPath(uploadPath)
	})

	if err != nil {
		dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorUploadingImage, err))
		return
	}

	// Показываем сообщение об успехе
	dw.updateUIAsync(func() {
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageUploadedSuccess, drive.Name), dw.window)
		}
	})

	// Обновляем список устройств
	dw.loadLocalDrives()
	logrus.Infof("✅ Образ успешно загружен на устройство: %s", drive.Name)
}

// handleDeleteImageFromDevice обрабатывает удаление образа с устройства
func (dw *DiskWidget) handleDeleteImageFromDevice(driveIndex int) {
	if driveIndex < 0 || driveIndex >= len(dw.allDrives) {
		logrus.Warnf("⚠️ Неверный индекс образа: %d", driveIndex)
		return
	}

	drive := dw.allDrives[driveIndex]

	// Проверяем, что это образ из API или локальный
	if drive.Source != "api" && drive.Source != "local" {
		logrus.Warnf("⚠️ Попытка удалить образ неподдерживаемого типа: %s", drive.Name)
		return
	}

	if dw.usbClient == nil {
		logrus.Warn("⚠️ USB клиент не инициализирован")
		if dw.window != nil {
			dialog.ShowError(fmt.Errorf("%s", i18n.Current.ErrorNotConnected), dw.window)
		}
		return
	}

	// Получаем имя файла для удаления
	var filename string
	if drive.LocalDrive != nil {
		filename = drive.LocalDrive.Name
	} else if drive.DiskInfo != nil {
		filename = drive.DiskInfo.Name
	} else {
		filename = drive.Name
	}

	// Показываем диалог подтверждения
	if dw.window != nil {
		fyne.Do(func() {
			ShowConfirmYesLeft(
				i18n.Current.DeleteImageTitle,
				fmt.Sprintf(i18n.Current.DeleteImageFromDeviceConfirm, drive.Name),
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

// deleteImageFromDevice выполняет удаление образа с устройства
func (dw *DiskWidget) deleteImageFromDevice(filename string, displayName string) {
	logrus.Infof("🗑️ Начало удаления образа с устройства: %s", filename)

	// Удаляем образ с устройства
	err := dw.usbClient.DeleteISO(filename)
	if err != nil {
		dw.showErrorAsync(fmt.Errorf(i18n.Current.ErrorDeletingImage, err))
		return
	}

	// Показываем сообщение об успехе
	dw.updateUIAsync(func() {
		if dw.window != nil {
			dialog.ShowInformation(i18n.Current.Success, fmt.Sprintf(i18n.Current.ImageDeletedSuccess, displayName), dw.window)
		}
	})

	// Обновляем список устройств
	dw.loadLocalDrives()
	logrus.Infof("✅ Образ успешно удален с устройства: %s", filename)
}

// TODO: Функционал добавления папок как MTP устройств
// Планируется реализовать:
// 1. Кнопка выбора папки в UI (рядом с кнопкой добавления образа)
// 2. Dialog выбора папки (desktop + Android SAF)
// 3. Создание NBD сервера для папки (виртуальный образ файловой системы)
// 4. Отправка NBD источника на USB Bridge 2 в формате:
//    {
//      "device": "drive",
//      "server": "127.0.0.1",
//      "port": 10809,
//      "export_name": "FolderName"
//    }
// 5. USB Bridge 2 подключает NBD и монтирует как MTP устройство
//
// Требования:
// - NBD backend должен уметь создавать виртуальный образ из папки
// - Или NBD клиент на стороне USB Bridge 2 должен поддерживать прямую отдачу папки
//
// См. закомментированный код ниже для референса:

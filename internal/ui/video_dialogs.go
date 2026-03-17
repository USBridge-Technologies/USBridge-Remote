package ui

import (
	"fmt"
	"image"
	"strconv"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/service"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// VideoQualityDialog диалог настроек качества видео
type VideoQualityDialog struct {
	parent fyne.Window

	// Настройки качества
	widthEntry   *widget.Entry
	heightEntry  *widget.Entry
	fpsEntry     *widget.Entry
	qualityEntry *widget.Entry
	bitrateEntry *widget.Entry

	// Callbacks
	onApply func(width, height, fps, quality, bitrate int)
}

// NewVideoQualityDialog создает новый диалог настроек качества
func NewVideoQualityDialog(parent fyne.Window) *VideoQualityDialog {
	return &VideoQualityDialog{
		parent: parent,
	}
}

// Show показывает диалог настроек качества
func (vqd *VideoQualityDialog) Show(currentWidth, currentHeight, currentFPS, currentQuality, currentBitrate int, onApply func(int, int, int, int, int)) {
	vqd.onApply = onApply

	// Создаем поля ввода
	vqd.widthEntry = widget.NewEntry()
	vqd.widthEntry.SetText(fmt.Sprintf("%d", currentWidth))
	vqd.widthEntry.SetPlaceHolder("640")

	vqd.heightEntry = widget.NewEntry()
	vqd.heightEntry.SetText(fmt.Sprintf("%d", currentHeight))
	vqd.heightEntry.SetPlaceHolder("480")

	vqd.fpsEntry = widget.NewEntry()
	vqd.fpsEntry.SetText(fmt.Sprintf("%d", currentFPS))
	vqd.fpsEntry.SetPlaceHolder("30")

	vqd.qualityEntry = widget.NewEntry()
	vqd.qualityEntry.SetText(fmt.Sprintf("%d", currentQuality))
	vqd.qualityEntry.SetPlaceHolder("80")

	vqd.bitrateEntry = widget.NewEntry()
	vqd.bitrateEntry.SetText(fmt.Sprintf("%d", currentBitrate))
	vqd.bitrateEntry.SetPlaceHolder("2000")

	// Создаем контейнер с настройками
	settingsContainer := container.NewVBox(
		widget.NewLabel(i18n.Current.VideoQualitySettings+":"),
		widget.NewSeparator(),

		container.NewHBox(
			widget.NewLabel(i18n.Current.Width),
			vqd.widthEntry,
			widget.NewLabel(i18n.Current.UnitPx),
		),

		container.NewHBox(
			widget.NewLabel(i18n.Current.Height),
			vqd.heightEntry,
			widget.NewLabel(i18n.Current.UnitPx),
		),

		container.NewHBox(
			widget.NewLabel(i18n.Current.FPS),
			vqd.fpsEntry,
			widget.NewLabel(i18n.Current.FramesPerSecond),
		),

		container.NewHBox(
			widget.NewLabel(i18n.Current.Quality),
			vqd.qualityEntry,
			widget.NewLabel(i18n.Current.UnitPercent),
		),

		container.NewHBox(
			widget.NewLabel(i18n.Current.Bitrate),
			vqd.bitrateEntry,
			widget.NewLabel(i18n.Current.UnitKbps),
		),

		widget.NewSeparator(),

		container.NewHBox(
			widget.NewButton(i18n.Current.Apply, vqd.handleApply),
			widget.NewButton(i18n.Current.Cancel, func() {
				// Close dialog without changes
			}),
		),
	)

	// Создаем диалог
	dialog.ShowCustom(i18n.Current.VideoQualitySettings, i18n.Current.Close, settingsContainer, vqd.parent)
}

// handleApply обрабатывает применение настроек
func (vqd *VideoQualityDialog) handleApply() {
	// Parse values
	width, err := strconv.Atoi(vqd.widthEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidWidth, err), vqd.parent)
		return
	}

	height, err := strconv.Atoi(vqd.heightEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidHeight, err), vqd.parent)
		return
	}

	fps, err := strconv.Atoi(vqd.fpsEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidFPS, err), vqd.parent)
		return
	}

	quality, err := strconv.Atoi(vqd.qualityEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidQuality, err), vqd.parent)
		return
	}

	bitrate, err := strconv.Atoi(vqd.bitrateEntry.Text)
	if err != nil {
		dialog.ShowError(fmt.Errorf(i18n.Current.InvalidBitrate, err), vqd.parent)
		return
	}

	// Validate values
	if width < 320 || width > 1920 {
		dialog.ShowError(fmt.Errorf(i18n.Current.WidthRange), vqd.parent)
		return
	}

	if height < 240 || height > 1080 {
		dialog.ShowError(fmt.Errorf(i18n.Current.HeightRange), vqd.parent)
		return
	}

	if fps < 1 || fps > 60 {
		dialog.ShowError(fmt.Errorf(i18n.Current.FPSRange), vqd.parent)
		return
	}

	if quality < 1 || quality > 100 {
		dialog.ShowError(fmt.Errorf(i18n.Current.QualityRange), vqd.parent)
		return
	}

	if bitrate < 100 || bitrate > 10000 {
		dialog.ShowError(fmt.Errorf(i18n.Current.BitrateRange), vqd.parent)
		return
	}

	// Применяем настройки
	if vqd.onApply != nil {
		vqd.onApply(width, height, fps, quality, bitrate)
	}

	logrus.Infof("✅ Настройки качества видео применены: %dx%d, %d FPS, качество %d%%, битрейт %d kbps",
		width, height, fps, quality, bitrate)

	// Закрываем диалог
	dialog.ShowInformation(i18n.Current.Success, i18n.Current.VideoSettingsApplied, vqd.parent)
}

// FullscreenDialog диалог полноэкранного режима
type FullscreenDialog struct {
	parent           fyne.Window
	videoWidget      *VideoWidget
	isFullscreen     bool
	gstreamerService *service.GStreamerService
	usbClient        *api.USBClient
	keyboardEnabled  bool
	fullscreenWindow fyne.Window
	virtualKeyboard  *VirtualKeyboard
	videoImage       *canvas.Image
	touchpadWrapper  *TouchpadWrapper // Wrapper для обработки мыши
	lastFrame        image.Image
	frameMutex       sync.RWMutex
	originalContent  *fyne.Container // Оригинальное содержимое окна
	originalTitle    string          // Оригинальный заголовок окна
}

// NewFullscreenDialog создает новый диалог полноэкранного режима
func NewFullscreenDialog(parent fyne.Window) *FullscreenDialog {
	return &FullscreenDialog{
		parent:       parent,
		isFullscreen: false,
	}
}

// SetVideoWidget устанавливает ссылку на видео виджет
func (fd *FullscreenDialog) SetVideoWidget(videoWidget *VideoWidget) {
	fd.videoWidget = videoWidget
}

// SetGStreamerService устанавливает ссылку на GStreamer сервис
func (fd *FullscreenDialog) SetGStreamerService(gstreamerService *service.GStreamerService) {
	fd.gstreamerService = gstreamerService
}

// SetUSBClient устанавливает ссылку на USB клиент
func (fd *FullscreenDialog) SetUSBClient(usbClient *api.USBClient) {
	fd.usbClient = usbClient
	fd.checkKeyboardStatus()
}

// checkKeyboardStatus проверяет, подключена ли клавиатура
func (fd *FullscreenDialog) checkKeyboardStatus() {
	logrus.Infof("⌨️ checkKeyboardStatus: usbClient=%v", fd.usbClient != nil)

	if fd.usbClient == nil {
		fd.keyboardEnabled = false
		logrus.Warn("⌨️ USB клиент не установлен")
		return
	}

	// Получаем информацию об устройствах
	deviceInfo, err := fd.usbClient.GetDeviceInfo()
	if err != nil {
		logrus.Warnf("⚠️ Ошибка получения информации об устройствах: %v", err)
		fd.keyboardEnabled = false
		return
	}

	logrus.Infof("⌨️ Получена информация об устройствах: %d устройств", len(deviceInfo.Devices))

	// Проверяем, есть ли подключенная клавиатура
	fd.keyboardEnabled = false
	for i, device := range deviceInfo.Devices {
		logrus.Infof("⌨️ Устройство %d: type=%s, status=%s, name=%s, vendor=%s, product=%s",
			i, device.Device, device.Status, device.ProductName, device.VendorID, device.ProductID)
		if device.Device == "keyboard" && device.Status == "connected" {
			fd.keyboardEnabled = true
			logrus.Info("⌨️ Клавиатура HID подключена и готова к использованию")
			break
		}
	}

	// Дополнительная проверка - возможно клавиатура называется по-другому
	if !fd.keyboardEnabled {
		logrus.Warn("⌨️ Клавиатура не найдена в списке устройств, попробуем принудительно включить")
		// Временно включаем клавиатуру для тестирования
		fd.keyboardEnabled = true
		logrus.Info("⌨️ Клавиатура принудительно включена для тестирования")
	}

	if !fd.keyboardEnabled {
		logrus.Warn("⌨️ Клавиатура HID не подключена")
	}
}

// Show показывает полноэкранный режим сразу без диалога
func (fd *FullscreenDialog) Show() {
	if fd.isFullscreen {
		fd.exitFullscreen()
		return
	}

	// Проверяем, что видео виджет установлен
	if fd.videoWidget == nil {
		logrus.Warn("⚠️ Видео виджет не установлен")
		return
	}

	// Проверяем, что видео запущено
	if !fd.videoWidget.IsStreaming() {
		logrus.Warn("⚠️ Видео не запущено")
		return
	}

	// Входим в полноэкранный режим сразу без диалога
	fd.enterFullscreen()
}

// enterFullscreen входит в полноэкранный режим с ffplay
func (fd *FullscreenDialog) enterFullscreen() {
	logrus.Info("🔍 Вход в полноэкранный режим с GStreamer")

	fd.isFullscreen = true

	// Создаем полноэкранное окно для захвата клавиш
	fd.createFullscreenWindow()

	// Подписываемся на кадры от GStreamer
	// ВАЖНО: вызываем callback для ОБОИХ окон - и основного, и полноэкранного
	if fd.gstreamerService != nil && fd.videoWidget != nil {
		fd.gstreamerService.SetOnFrameReceived(func(frame image.Image) {
			// Обновляем основное окно (чтобы видео не замерло)
			fd.videoWidget.handleVideoFrame(frame)
			// Обновляем полноэкранное окно
			fd.updateVideoFrame(frame)
		})
		logrus.Info("✅ Подписка на кадры GStreamer для полноэкранного режима активирована")
	} else {
		logrus.Warn("⚠️ GStreamer сервис не установлен")
	}

	logrus.Info("✅ Полноэкранный режим активирован")
}

// updateVideoFrame обновляет кадр видео в полноэкранном режиме
func (fd *FullscreenDialog) updateVideoFrame(frame image.Image) {
	if !fd.isFullscreen {
		return
	}

	fd.frameMutex.Lock()
	fd.lastFrame = frame
	videoImg := fd.videoImage // Копируем ссылку под мьютексом
	touchpad := fd.touchpadWrapper
	frameCount := fd.videoWidget.frameCount
	fd.frameMutex.Unlock()

	if videoImg == nil {
		logrus.Warn("⚠️ updateVideoFrame: videoImg is nil")
		return
	}

	// Логируем каждый 30-й кадр
	if frameCount%30 == 0 {
		logrus.Infof("🖼️ Полноэкранный режим: обновление кадра %d", frameCount)
	}

	fyne.Do(func() {
		// Обновляем изображение
		videoImg.Image = frame
		videoImg.Refresh()

		// Обновляем touchpad wrapper
		if touchpad != nil {
			touchpad.Refresh()
		}
	})
}

// createFullscreenWindow создает полноэкранное окно с видео
func (fd *FullscreenDialog) createFullscreenWindow() {
	logrus.Info("🔍 Создание полноэкранного окна с видео")

	// Определяем платформу
	isAndroid := fyne.CurrentDevice().IsMobile()

	// На Android используем основное окно, на desktop создаем новое
	if isAndroid {
		logrus.Info("🔍 Android: используем основное окно для полноэкранного режима")
		fd.fullscreenWindow = fd.parent
		// Сохраняем оригинальное содержимое
		fd.originalContent = fd.parent.Content().(*fyne.Container)
		fd.originalTitle = fd.parent.Title()
	} else {
		logrus.Info("🔍 Desktop: создаем новое окно для полноэкранного режима")
		fd.fullscreenWindow = fyne.CurrentApp().NewWindow("")
	}

	// Создаем canvas.Image для отображения видео
	// Получаем текущий кадр из основного окна
	currentFrame := fd.videoWidget.GetCurrentFrame()
	fd.videoImage = canvas.NewImageFromImage(currentFrame)
	// Используем ImageFillContain для сохранения пропорций
	fd.videoImage.FillMode = canvas.ImageFillContain
	fd.videoImage.ScaleMode = canvas.ImageScaleSmooth

	if currentFrame != nil {
		bounds := currentFrame.Bounds()
		logrus.Infof("✅ Установлен начальный кадр в полноэкранное окно: %dx%d", bounds.Dx(), bounds.Dy())
	} else {
		logrus.Warn("⚠️ Нет текущего кадра для полноэкранного окна - будет черный экран до первого кадра")
	}

	// Создаем TouchpadWrapper для обработки мыши в полноэкранном режиме
	// ВАЖНО: передаем fd.videoImage, чтобы TouchpadWrapper использовал то же изображение
	fd.touchpadWrapper = NewTouchpadWrapperWithImage(fd.videoWidget, fd.videoImage)
	// На macOS Canvas.SetOnTypedKey ненадёжен — пересылаем клавиши через виджет с фокусом
	fd.touchpadWrapper.SetKeyHandlers(fd.handleKeyPress, fd.handleRunePress)
	fd.touchpadWrapper.SetWindowForFocus(fd.fullscreenWindow)
	logrus.Info("✅ TouchpadWrapper создан для полноэкранного режима")

	// Создаем виртуальную клавиатуру
	logrus.Info("⌨️ [DEBUG] Создание виртуальной клавиатуры для полноэкранного режима")
	fd.virtualKeyboard = NewVirtualKeyboard(fd.fullscreenWindow, fd.handleVirtualKeyPress, fd.handleRunePress)
	keyboardLayout := fd.virtualKeyboard.GetKeyboardLayout()

	logrus.Infof("⌨️ [DEBUG] keyboardLayout получен: %v, MinSize: %v", keyboardLayout != nil, keyboardLayout.MinSize())
	keyboardLayout.Hide() // Скрываем по умолчанию
	logrus.Infof("⌨️ [DEBUG] keyboardLayout.Hide() вызван, Visible: %v", keyboardLayout.Visible())

	logrus.Info("⌨️ [DEBUG] Создание overlay layout для размещения клавиатуры поверх видео")

	// Используем Border layout для видео и клавиатуры
	// Видео в центре (занимает все пространство)
	// Клавиатура внизу (когда видима)
	videoContainer := container.NewMax(fd.touchpadWrapper)

	// Размещаем видео и клавиатуру через Border
	videoWithKeyboardContainer := container.NewBorder(
		nil,            // Top
		keyboardLayout, // Bottom - клавиатура
		nil,            // Left
		nil,            // Right
		videoContainer, // Center - видео (получает touch события)
	)

	// Создаем кнопку для переключения клавиатуры (после создания контейнера для доступа к нему)
	keyboardBtn := widget.NewButton("⌨️", func() {
		logrus.Info("⌨️ ========== НАЖАТА КНОПКА КЛАВИАТУРЫ В ПОЛНОЭКРАННОМ РЕЖИМЕ ==========")

		if keyboardLayout.Visible() {
			logrus.Info("⌨️ Клавиатура видима - скрываем")
			keyboardLayout.Hide()
		} else {
			logrus.Info("⌨️ Клавиатура скрыта - показываем")
			keyboardLayout.Show()
		}

		// Принудительно обновляем layout контейнера чтобы видео расширилось/сжалось
		videoWithKeyboardContainer.Refresh()

		// Принудительно обновляем весь canvas
		fd.fullscreenWindow.Canvas().Refresh(fd.fullscreenWindow.Content())

		logrus.Infof("⌨️ После переключения - Visible: %v, Size: %v, Position: %v",
			keyboardLayout.Visible(), keyboardLayout.Size(), keyboardLayout.Position())
	})
	keyboardBtn.Importance = widget.HighImportance // Синяя заливка

	// Создаем контейнер для кнопки как overlay
	// Используем WithoutLayout для ручного позиционирования
	overlayContainer := container.NewWithoutLayout(keyboardBtn)

	// Используем Stack для наложения кнопки поверх видео
	// Сначала идет видео с клавиатурой, затем кнопка сверху
	mainContainer := container.NewStack(
		videoWithKeyboardContainer, // Нижний слой - видео и клавиатура
		overlayContainer,           // Верхний слой - кнопка overlay
	)

	logrus.Info("⌨️ [DEBUG] Stack контейнер создан с overlay элементами")

	// Устанавливаем контент окна
	fd.fullscreenWindow.SetContent(mainContainer)

	// Настраиваем позиции и размеры элементов
	updatePositions := func() {
		canvasSize := fd.fullscreenWindow.Canvas().Size()
		logrus.Infof("🔍 [DEBUG] updatePositions - Canvas Size: %v", canvasSize)

		// Устанавливаем размер и позицию кнопки в левом верхнем углу как overlay
		buttonSize := fyne.NewSize(50, 40) // Обычный размер кнопки
		keyboardBtn.Resize(buttonSize)
		keyboardBtn.Move(fyne.NewPos(10, 10)) // 10 пикселей отступа от левого верхнего угла

		// Устанавливаем минимальный размер клавиатуры (высоту)
		keyboardHeight := float32(300)
		keyboardLayout.Resize(fyne.NewSize(canvasSize.Width, keyboardHeight))

		logrus.Infof("⌨️ [DEBUG] Позиции установлены:")
		logrus.Infof("⌨️ [DEBUG]   Canvas Size: %v", canvasSize)
		logrus.Infof("⌨️ [DEBUG]   Button Position: %v, Size: %v", keyboardBtn.Position(), keyboardBtn.Size())
		logrus.Infof("⌨️ [DEBUG]   Keyboard Height: %v", keyboardHeight)
		logrus.Infof("⌨️ [DEBUG]   Keyboard Size: %v", keyboardLayout.Size())
	}

	// Настраиваем окно в зависимости от платформы
	if !isAndroid {
		// На desktop показываем заголовок и создаем обработчики для нового окна
		fd.fullscreenWindow.SetTitle(i18n.Current.FullscreenWindowTitle)

		// Перехватываем попытку закрытия окна (крестик на desktop)
		fd.fullscreenWindow.SetCloseIntercept(func() {
			logrus.Info("🔍 Перехвачена попытка закрытия окна - выход из полноэкранного режима")
			fd.exitFullscreen()
		})

		// Обработчик закрытия окна (для выхода через exitFullscreen)
		fd.fullscreenWindow.SetOnClosed(func() {
			logrus.Info("🔍 Окно полноэкранного режима закрыто")
		})
	} else {
		// На Android используем основное окно - убираем только заголовок
		logrus.Info("🔍 Android режим: используем основное окно, убираем заголовок")
		fd.fullscreenWindow.SetTitle("")
		// НЕ используем SetFullScreen на Android, т.к. это сдвигает позиционирование клавиатуры
		// На Android SetCloseIntercept уже установлен в MainWindow.handleClose
		// который проверяет IsFullscreen() и вызывает exitFullscreen()
	}

	// На desktop включаем полноэкранный режим
	if !isAndroid {
		fd.fullscreenWindow.SetFullScreen(true)
	}

	// Обработчик нажатия клавиш
	fd.fullscreenWindow.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		// ESC на PC или Back на Android
		if event.Name == fyne.KeyEscape || string(event.Name) == "Back" {
			logrus.Infof("🔍 Нажата клавиша выхода (%s) - выход из полноэкранного режима", event.Name)
			fd.exitFullscreen()
			return
		}
		// Остальные клавиши обрабатываем как обычно
		fd.handleKeyPress(event)
	})

	// Обработчик символов
	fd.fullscreenWindow.Canvas().SetOnTypedRune(func(r rune) {
		fd.handleRunePress(r)
	})

	// Вызываем updatePositions после небольшой задержки для инициализации
	go func() {
		time.Sleep(150 * time.Millisecond)
		fyne.Do(updatePositions)

		// Периодически обновляем позиции (для обработки поворота экрана)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		lastSize := fyne.NewSize(0, 0)
		for range ticker.C {
			if !fd.isFullscreen {
				return
			}

			fyne.Do(func() {
				// Проверяем что fullscreenWindow не nil перед использованием
				if !fd.isFullscreen || fd.fullscreenWindow == nil {
					return
				}
				currentSize := fd.fullscreenWindow.Canvas().Size()
				// Обновляем позиции только если размер изменился (поворот экрана)
				if currentSize != lastSize {
					logrus.Infof("🔄 Обнаружено изменение размера экрана: %v -> %v", lastSize, currentSize)
					updatePositions()
					lastSize = currentSize
				}
			})
		}
	}()

	logrus.Infof("⌨️ [DEBUG] Overlay контейнер создан")

	logrus.Info("🔍 Полноэкранное окно создано")

	// Показываем окно
	fd.fullscreenWindow.Show()
	logrus.Info("🔍 Полноэкранное окно показано")

	// На macOS полноэкранное окно должно явно запросить фокус (key window)
	if !isAndroid {
		fd.fullscreenWindow.RequestFocus()
	}

	// Принудительно обновляем изображение после показа окна
	if fd.videoImage != nil && fd.videoImage.Image != nil {
		fd.videoImage.Refresh()
		logrus.Info("🔍 Изображение обновлено после показа окна")
	}

	// Автофокус для клавиатуры (важно для macOS — после анимации fullscreen)
	go func() {
		// Даём время на завершение fullscreen-перехода (macOS анимирует)
		time.Sleep(500 * time.Millisecond)
		if !fd.isFullscreen || fd.fullscreenWindow == nil || fd.touchpadWrapper == nil {
			return
		}
		if !fyne.CurrentDevice().IsMobile() {
			fyne.Do(func() {
				fd.fullscreenWindow.RequestFocus()
				fd.fullscreenWindow.Canvas().Focus(fd.touchpadWrapper)
			})
		}
	}()
}

// updateLayout обновляет позиции элементов в полноэкранном окне
func (fd *FullscreenDialog) updateLayout() {
	// Layout управляется через container.NewBorder, ничего не делаем
}

// handleVirtualKeyPress обрабатывает нажатие клавиш виртуальной клавиатуры
func (fd *FullscreenDialog) handleVirtualKeyPress(keyCode int, modifiers int) {
	logrus.Infof("⌨️ Виртуальная клавиатура: получено нажатие клавиши %d с модификаторами %d", keyCode, modifiers)

	// Всегда пытаемся отправить клавишу на хост, если USB клиент доступен
	if fd.usbClient == nil {
		logrus.Warnf("⌨️ USB клиент не подключен, игнорируем клавишу: %d", keyCode)
		return
	}

	logrus.Infof("⌨️ Отправляем клавишу на удаленную машину: код=%d, модификаторы=%d", keyCode, modifiers)
	// Отправляем клавишу на удаленную машину
	go fd.sendKeyToRemoteVirtual(keyCode, modifiers)
}

// sendKeyToRemoteVirtual отправляет клавишу на удаленную машину через HID (из виртуальной клавиатуры)
func (fd *FullscreenDialog) sendKeyToRemoteVirtual(keyCode int, modifiers int) {
	logrus.Infof("⌨️ sendKeyToRemoteVirtual: отправка клавиши %d с модификаторами %d", keyCode, modifiers)

	// Отправляем клавишу
	var err error
	if modifiers > 0 {
		// Отправляем комбинацию клавиш
		err = fd.usbClient.SendCombo(modifiers, keyCode)
		logrus.Infof("⌨️ Отправлена комбинация: модификаторы=%d, клавиша=%d", modifiers, keyCode)
	} else {
		// Отправляем одиночную клавишу
		logrus.Infof("⌨️ Отправляем одиночную клавишу: %d", keyCode)
		err = fd.usbClient.SendKey(keyCode)
		logrus.Infof("⌨️ Отправлена клавиша: %d - результат: %v", keyCode, err)
	}

	if err != nil {
		logrus.Errorf("⚠️ Ошибка отправки клавиши: %v", err)
	} else {
		logrus.Infof("✅ Клавиша успешно отправлена: код=%d, модификаторы=%d", keyCode, modifiers)
	}
}

// handleKeyPress обрабатывает нажатие клавиш в полноэкранном режиме (DEPRECATED - используется виртуальная клавиатура)
func (fd *FullscreenDialog) handleKeyPress(event *fyne.KeyEvent) {
	logrus.Infof("⌨️ Получено нажатие клавиши: %s (физическая: %v)", event.Name, event.Physical)

	// Специальные клавиши для управления полноэкранным режимом
	switch event.Name {
	case fyne.KeyEscape, fyne.KeyF11:
		logrus.Info("🔍 Нажата клавиша выхода из полноэкранного режима")
		fd.exitFullscreen()
		return
	}
	// Печатаемые клавиши обрабатываем только в handleRunePress (с модификаторами)
	if IsPrintableKey(event.Name) {
		return
	}

	if !fd.keyboardEnabled || fd.usbClient == nil {
		logrus.Warnf("⌨️ Клавиатура не подключена, игнорируем клавишу: %s", event.Name)
		return
	}

	go fd.sendKeyToRemote(event)
}

// sendKeyToRemote отправляет клавишу на удаленную машину через HID (только непечатаемые: F-клавиши, стрелки и т.д.).
func (fd *FullscreenDialog) sendKeyToRemote(event *fyne.KeyEvent) {
	keyCode := GetKeyCodeFromPhysical(event.Physical)
	if keyCode == 0 {
		keyCode = GetKeyCode(event.Name)
	}
	if keyCode == 0 {
		logrus.Warnf("⌨️ Неизвестная клавиша: %s", event.Name)
		return
	}
	if err := fd.usbClient.SendKey(keyCode); err != nil {
		logrus.Errorf("⚠️ Ошибка отправки клавиши: %v", err)
	}
}

// handleRunePress обрабатывает нажатие символов (буквы, цифры, знаки препинания)
func (fd *FullscreenDialog) handleRunePress(r rune) {
	logrus.Infof("⌨️ Получен символ: %c (код: %d)", r, r)

	// Если клавиатура не подключена, игнорируем символы
	if !fd.keyboardEnabled || fd.usbClient == nil {
		logrus.Warnf("⌨️ Клавиатура не подключена, игнорируем символ: %c", r)
		return
	}

	logrus.Infof("⌨️ Отправляем символ на удаленную машину: %c", r)
	// Отправляем символ на удаленную машину
	go fd.sendRuneToRemote(r)
}

// sendRuneToRemote отправляет символ на удаленную машину через HID
func (fd *FullscreenDialog) sendRuneToRemote(r rune) {
	if r == '\n' || r == '\r' {
		return
	}
	logrus.Infof("⌨️ sendRuneToRemote: обработка символа %c (код: %d)", r, r)

	keyCode, modifiers := GetRuneKeyCodeWithModifiers(r)
	if keyCode == 0 {
		logrus.Warnf("⌨️ Неизвестный символ: %c (код: %d)", r, r)
		return
	}

	logrus.Infof("⌨️ Найден код клавиши: %d, модификаторы: %d для символа %c", keyCode, modifiers, r)

	// Отправляем клавишу с модификаторами
	var err error
	if modifiers > 0 {
		// Отправляем комбинацию клавиш
		logrus.Infof("⌨️ Отправляем символ с модификаторами: %d, модификаторы: %d (%c)", keyCode, modifiers, r)
		err = fd.usbClient.SendCombo(modifiers, keyCode)
		logrus.Infof("⌨️ Отправлен символ с модификаторами: %d, модификаторы: %d (%c) - результат: %v", keyCode, modifiers, r, err)
	} else {
		// Отправляем одиночную клавишу
		logrus.Infof("⌨️ Отправляем символ: %d (%c)", keyCode, r)
		err = fd.usbClient.SendKey(keyCode)
		logrus.Infof("⌨️ Отправлен символ: %d (%c) - результат: %v", keyCode, r, err)
	}

	if err != nil {
		logrus.Errorf("⚠️ Ошибка отправки символа: %v", err)
	} else {
		logrus.Infof("✅ Символ успешно отправлен: %c (код: %d, модификаторы: %d)", r, keyCode, modifiers)
	}
}

// exitFullscreen выходит из полноэкранного режима
func (fd *FullscreenDialog) exitFullscreen() {
	if !fd.isFullscreen {
		return
	}

	logrus.Info("🔍 Выход из полноэкранного режима")

	// ВАЖНО: сначала сбрасываем флаг и очищаем videoImage и touchpadWrapper
	fd.isFullscreen = false
	fd.frameMutex.Lock()
	fd.videoImage = nil
	fd.touchpadWrapper = nil
	fd.frameMutex.Unlock()

	// Скрываем виртуальную клавиатуру
	if fd.virtualKeyboard != nil {
		fd.virtualKeyboard.Hide()
		fd.virtualKeyboard = nil
	}

	// Определяем платформу
	isAndroid := fyne.CurrentDevice().IsMobile()

	if isAndroid {
		// На Android восстанавливаем оригинальное содержимое основного окна
		logrus.Info("🔍 Android: восстанавливаем оригинальное содержимое основного окна")
		if fd.originalContent != nil {
			fd.parent.SetContent(fd.originalContent)
			fd.parent.SetTitle(fd.originalTitle)
			logrus.Info("✅ Оригинальное содержимое восстановлено")
		}
		// НЕ вызываем SetFullScreen(false) на Android, т.к. мы не вызывали SetFullScreen(true)
		// SetCloseIntercept остается в MainWindow.handleClose
	} else {
		// На desktop закрываем полноэкранное окно
		logrus.Info("🔍 Desktop: закрываем полноэкранное окно")
		if fd.fullscreenWindow != nil {
			fd.fullscreenWindow.Close()
			fd.fullscreenWindow = nil
		}
	}

	// ПОТОМ восстанавливаем callback для основного окна
	// (чтобы updateVideoFrame не вызывался с nil videoImage)
	if fd.gstreamerService != nil && fd.videoWidget != nil {
		fd.gstreamerService.SetOnFrameReceived(func(frame image.Image) {
			fd.videoWidget.handleVideoFrame(frame)
		})
		logrus.Info("✅ Восстановлена подписка на кадры для основного окна")
	}
	fd.lastFrame = nil

	logrus.Info("✅ Полноэкранный режим деактивирован")
}

// IsFullscreen возвращает состояние полноэкранного режима
func (fd *FullscreenDialog) IsFullscreen() bool {
	return fd.isFullscreen
}

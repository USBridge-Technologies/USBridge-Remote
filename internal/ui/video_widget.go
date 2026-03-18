package ui

import (
	"fmt"
	"image"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
	"usbridge-client/internal/ui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

// VideoWidget виджет управления видео захватом
type VideoWidget struct {
	container        *fyne.Container
	videoCanvas      *canvas.Image
	touchpadWrapper  *TouchpadWrapper
	controls         *fyne.Container
	startBtn         *widget.Button
	stopBtn          *widget.Button
	statusLabel      *widget.Label
	infoLabel        *widget.Label
	statsLabel       *widget.Label
	contentContainer *fyne.Container // Контейнер для видео и клавиатуры

	// Состояние
	isStreaming          bool
	streamURL            string
	isGStreamerConnected bool
	isMouseConnected     bool // Флаг подключенной мыши

	// Сервисы
	usbClient        *api.USBClient
	gstreamerService *service.GStreamerService
	frpService       *service.FRPService // для проверки режима FRP
	updateStatus     func()

	// Видео поток
	currentFrame  image.Image
	frameMutex    sync.RWMutex
	lastFrameTime time.Time
	frameCount    int64
	frameDecoder  *VideoFrameDecoder

	// Диалоги
	fullscreenDialog *FullscreenDialog
	startDialog      *VideoStartDialog
	parentWindow     fyne.Window
	virtualKeyboard  *VirtualKeyboard

	// Мышь/тачпад
	lastMouseX       float32
	lastMouseY       float32
	currentMouseX    float32 // Текущая позиция мыши (для polling)
	currentMouseY    float32 // Текущая позиция мыши (для polling)
	isDragging       bool
	dragButton       int
	touchStartX      float32
	touchStartY      float32
	touchStartTime   time.Time
	mousePollingQuit chan bool // Канал для остановки polling горутины
	mouseInputMode   string    // "mouse" (по умолчанию), "touchscreen" или "absolute"
	touchpadSizeW    float32   // Ширина области ввода (для перевода в абсолютные координаты)
	touchpadSizeH    float32   // Высота области ввода
	// Прямоугольник видео внутри области ввода (ImageFillContain): для корректного перевода координат в 0..4095
	contentRectX      float32
	contentRectY      float32
	contentRectW      float32
	contentRectH      float32
	lastTouchX        int // последние отправленные координаты touch (чтобы не дублировать в MouseMoved)
	lastTouchY        int
	lastAbsX          int // последние отправленные координаты absolute (touch_position) чтобы не спамить
	lastAbsY          int
	lastAbsSentTime   time.Time // время последней отправки absolute (для дебаунса)
	absButtons        uint8     // битмаска кнопок для absolute режима
	lastTouchDownTime time.Time // время последнего SendTouch(_, _, true) — для дедупликации
	touchDedupMu      sync.Mutex
	// Задержка touch(down) при MouseDown: если за ~120ms не пришёл Tapped — считаем драг, шлём touch(true).
	// Tapped приходит при полном клике на виджет; MouseUp в Fyne приходит только виджету под курсором при отпускании.
	touchDownDelayTimer *time.Timer
	touchDownDelayMu    sync.Mutex
	touchActive         bool // touch(true) уже отправлен и ещё не отправлен touch(false); MouseMoved шлёт только при true
}

// createInterface создает интерфейс виджета
func (vw *VideoWidget) createInterface() {
	// Создаем canvas для видео
	vw.videoCanvas = canvas.NewImageFromResource(nil)
	vw.videoCanvas.FillMode = canvas.ImageFillContain

	// Создаем touchpad wrapper для обработки мыши и тачскрина
	vw.touchpadWrapper = NewTouchpadWrapper(vw)

	// Используем NewMax для растягивания видео на всю доступную область
	videoContainer := container.NewMax(vw.touchpadWrapper)

	// Создаем лейблы статуса
	vw.statusLabel = widget.NewLabel(i18n.Current.VideoNotStarted)
	vw.infoLabel = widget.NewLabel("")
	vw.statsLabel = widget.NewLabel("")

	// Создаем пустой контейнер для клавиатуры (будет заполнен позже)
	vw.contentContainer = container.NewWithoutLayout()
	vw.contentContainer.Hide() // Скрываем по умолчанию

	// Создаем главный контейнер
	// Используем Border: видео в центре, клавиатура внизу (когда показана)
	vw.container = container.NewBorder(
		vw.createInfoPanel(), // Верх
		vw.contentContainer,  // Низ - контейнер для клавиатуры
		nil,                  // Лево
		nil,                  // Право
		videoContainer,       // Центр - видео занимает все оставшееся место
	)

	vw.updateButtons()
}

// createInfoPanel создает панель информации
func (vw *VideoWidget) createInfoPanel() *fyne.Container {
	// Создаем кнопки управления с явным указанием, что они должны быть компактными
	vw.startBtn = widget.NewButton(i18n.Current.StartVideoButton, vw.handleStartVideo)
	vw.stopBtn = widget.NewButton(i18n.Current.StopVideoButton, vw.handleStopVideo)
	fullscreenBtn := widget.NewButton(i18n.Current.FullscreenButton, vw.handleFullscreen)

	// Компактная панель управления - кнопки рядом
	vw.controls = container.NewHBox(
		vw.startBtn,
		vw.stopBtn,
		fullscreenBtn,
	)

	// Одна строка: только FPS и время слева, кнопки справа
	allStatusRow := container.NewHBox(
		vw.statsLabel,      // Лево - только FPS и время
		layout.NewSpacer(), // Заполнитель между информацией и кнопками
		vw.controls,        // Право - кнопки управления
	)

	return allStatusRow
}

// handleStartVideo обрабатывает запуск видео
func (vw *VideoWidget) handleStartVideo() {
	if vw.usbClient == nil {
		logrus.Warn("⚠️ USB client is not initialized")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
		})
		return
	}

	// Ленивая инициализация диалога при первом использовании
	if vw.startDialog == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window not set")
			fyne.Do(func() {
				vw.statusLabel.SetText(i18n.Current.ErrorWindowNotInit)
			})
			return
		}
		vw.startDialog = NewVideoStartDialog(vw.parentWindow)
	}

	// Устанавливаем значения по умолчанию (800×600)
	vw.startDialog.SetDefaults(800, 600, 30, 80, "2M")
	vw.startDialog.Show(vw.handleVideoStartWithParams)
}

// handleVideoStartWithParams обрабатывает запуск видео с параметрами из диалога
func (vw *VideoWidget) handleVideoStartWithParams(request *models.VideoStartRequest) {
	if vw.gstreamerService == nil {
		logrus.Warn("⚠️ GStreamer service is not initialized")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.VideoLaunchFailed)
		})
		return
	}

	vw.handleVideoStartWithParamsGStreamer(request)
}

// handleStopVideo обрабатывает остановку видео
func (vw *VideoWidget) handleStopVideo() {
	if vw.usbClient == nil {
		logrus.Warn("⚠️ USB client not initialized")
		fyne.Do(func() {
			vw.statusLabel.SetText(i18n.Current.ErrorNoConnection)
		})
		return
	}

	fyne.Do(func() {
		vw.statusLabel.SetText(i18n.Current.StoppingVideoCapture)
		vw.stopBtn.Disable()
	})

	// Отключаемся от WebSocket для мыши
	vw.usbClient.DisconnectMouseWebSocket()

	// Отключаемся от GStreamer потока
	if vw.gstreamerService != nil {
		if err := vw.gstreamerService.Disconnect(); err != nil {
			logrus.Errorf("Failed to disconnect GStreamer: %v", err)
		}
	}

	// Останавливаем видео захват на USBridge 2
	if err := vw.usbClient.StopVideo(); err != nil {
		logrus.Warnf("⚠️ Failed to stop video on the server: %v (ignoring because it may already be stopped)", err)
		// Не возвращаем ошибку - видео уже может быть остановлено или сервер недоступен
		// Продолжаем локальную очистку
	}

	vw.isStreaming = false
	vw.isGStreamerConnected = false
	vw.isMouseConnected = false

	// Очищаем видео
	vw.clearVideo()

	// Обновляем UI в главном потоке
	fyne.Do(func() {
		vw.updateButtons()
		vw.statusLabel.SetText(i18n.Current.VideoStopped)
	})

	vw.updateStatus()
	logrus.Info("🛑 Video capture stopped")
}

// updateButtons обновляет состояние кнопок
func (vw *VideoWidget) updateButtons() {
	if vw.isStreaming {
		vw.startBtn.Disable()
		vw.stopBtn.Enable()
	} else {
		vw.startBtn.Enable()
		vw.stopBtn.Enable() // Кнопка остановки всегда активна
	}
}

// Refresh обновляет виджет
func (vw *VideoWidget) Refresh() {
	if vw.usbClient == nil {
		logrus.Debug("USB client is not initialized, skipping video refresh")
		fyne.Do(func() {
			vw.infoLabel.SetText(i18n.Current.VideoWaitingConnection)
		})
		return
	}

	// Проверяем статус подключенной мыши
	vw.checkMouseConnected()

	// Получаем информацию о видео
	videoInfo, err := vw.usbClient.GetVideoInfo()
	if err != nil {
		logrus.Errorf("Failed to get video information: %v", err)
		fyne.Do(func() {
			vw.infoLabel.SetText(i18n.Current.ErrorVideoInfo)
		})
		return
	}

	fyne.Do(func() {
		if videoInfo.Success && videoInfo.Data != nil {
			vw.infoLabel.SetText(i18n.Current.VideoInfoReceived)
		} else {
			vw.infoLabel.SetText(i18n.Current.VideoInfoUnavailable)
		}
	})

	// Обновляем статистику GStreamer
	if vw.gstreamerService != nil {
		vw.updateGStreamerStats()
	}
}

// checkMouseConnected проверяет, подключена ли мышь
func (vw *VideoWidget) checkMouseConnected() {
	if vw.usbClient == nil {
		logrus.Debug("🖱️ checkMouseConnected: USB client is not initialized")
		vw.isMouseConnected = false
		return
	}

	// Получаем информацию о подключенных устройствах
	deviceInfo, err := vw.usbClient.GetDeviceInfo()
	if err != nil {
		logrus.Infof("🖱️ Failed to get device information: %v", err)
		vw.isMouseConnected = false
		return
	}

	logrus.Debugf("🖱️ checkMouseConnected: received %d devices", len(deviceInfo.Devices))

	// Проверяем, есть ли подключенный манипулятор (мышь, тачскрин или absolute)
	mouseConnected := false
	for _, device := range deviceInfo.Devices {
		logrus.Debugf("🖱️ Inspecting device: type=%s, status=%s, name=%s", device.Type, device.Status, device.Name)

		// Мышь или тачскрин — оба дают возможность ввода на экране управления
		if device.Status == "connected" &&
			(device.Type == "mouse" || device.Type == "touchscreen" || device.Type == "absolute" || strings.HasPrefix(device.Type, "mouse:")) {
			mouseConnected = true
			// Синхронизируем режим ввода с типом устройства на сервере
			if device.Type == "touchscreen" {
				vw.SetMouseInputMode("touchscreen")
			} else if device.Type == "absolute" {
				vw.SetMouseInputMode("absolute")
			}
			logrus.Infof("🖱️ ✅ Pointer device connected: %s (type: %s)", device.Name, device.Type)
			break
		}
	}

	logrus.Debugf("🖱️ checkMouseConnected: mouseConnected=%v (previously %v)", mouseConnected, vw.isMouseConnected)

	if vw.isMouseConnected != mouseConnected {
		vw.isMouseConnected = mouseConnected
		if mouseConnected {
			logrus.Info("🖱️ Touchpad activated: pointer device connected")

			// Подключаемся к WebSocket для управления мышью
			go func() {
				if err := vw.usbClient.ConnectMouseWebSocket(); err != nil {
					logrus.Warnf("⚠️ Failed to connect mouse WebSocket: %v (HTTP fallback will be used)", err)
				} else {
					logrus.Info("✅ Mouse WebSocket connected successfully")
				}
			}()

			// Запускаем polling горутину для плавного управления мышью на desktop
			vw.startDesktopMousePolling()

			// Подсказка теперь только в логах
			logrus.Info("🖱️ Pointer device connected (WebSocket)")
		} else {
			logrus.Info("🖱️ Touchpad deactivated: pointer device disconnected")

			// Останавливаем polling горутину
			vw.stopDesktopMousePolling()

			// Отключаемся от WebSocket
			vw.usbClient.DisconnectMouseWebSocket()

			fyne.Do(func() {
				if vw.statusLabel != nil {
					vw.statusLabel.SetText("")
				}
			})
		}
	}
}

// handleVideoFrame обрабатывает полученный видео кадр
func (vw *VideoWidget) handleVideoFrame(frame image.Image) {
	if frame == nil {
		return
	}

	// Обновляем статистику
	vw.frameMutex.Lock()
	vw.currentFrame = frame // Сохраняем текущий кадр для полноэкранного режима
	vw.frameCount++
	frameNum := vw.frameCount
	vw.lastFrameTime = time.Now()
	vw.frameMutex.Unlock()

	// Обновляем счетчик кадров в декодере
	vw.frameDecoder.IncrementFrameCount()

	if frameNum == 1 {
		logrus.Info("✅ [VIDEO] Step 7: frame rendered in UI")
	}
	if frameNum%300 == 0 {
		logrus.Infof("🖼️ [VIDEO] UI: processed %d frames", frameNum)
	}

	go fyne.Do(func() {
		if vw.videoCanvas != nil {
			vw.videoCanvas.Image = frame
			vw.videoCanvas.Refresh()
		}
		if vw.touchpadWrapper != nil {
			vw.touchpadWrapper.Refresh()
		}
		// Первый кадр: принудительный refresh контейнера (Android может не перерисовать иначе)
		if frameNum == 1 && vw.container != nil {
			vw.container.Refresh()
		}
		if frameNum%30 == 0 {
			vw.updateStats()
		}
	})
}

// handleFullscreen обрабатывает переключение в полноэкранный режим
func (vw *VideoWidget) handleFullscreen() {
	// Ленивая инициализация диалога при первом использовании
	if vw.fullscreenDialog == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.fullscreenDialog = NewFullscreenDialog(vw.parentWindow)
		vw.fullscreenDialog.SetVideoWidget(vw)
		vw.fullscreenDialog.SetGStreamerService(vw.gstreamerService)
		if vw.usbClient != nil {
			vw.fullscreenDialog.SetUSBClient(vw.usbClient)
		}
	}

	vw.fullscreenDialog.Show()
}

// HandleVirtualKeyboard обрабатывает открытие/закрытие виртуальной клавиатуры (публичный метод)
func (vw *VideoWidget) HandleVirtualKeyboard() {
	// Ленивая инициализация клавиатуры при первом использовании
	if vw.virtualKeyboard == nil {
		if vw.parentWindow == nil {
			logrus.Warn("⚠️ Parent window is not set")
			return
		}
		vw.virtualKeyboard = NewVirtualKeyboard(vw.parentWindow, vw.handleVirtualKeyPress, vw.handlePhysicalRunePress)
	}

	// Определяем платформу
	isAndroid := fyne.CurrentDevice().IsMobile()

	if vw.virtualKeyboard.IsVisible() {
		vw.virtualKeyboard.Hide()

		if isAndroid {
			// Android: скрываем встроенную клавиатуру
			vw.contentContainer.Hide()
			vw.container.Refresh()
			logrus.Info("⌨️ Virtual keyboard hidden (Android mode)")
		} else {
			// Desktop: закрываем отдельное окно (обрабатывается в Hide())
			logrus.Info("⌨️ Virtual keyboard hidden (desktop mode)")
		}
	} else {
		if isAndroid {
			// Android: показываем клавиатуру под видео
			keyboardLayout := vw.virtualKeyboard.GetKeyboardLayout()
			logrus.Infof("⌨️ [DEBUG] keyboardLayout MinSize: %v", keyboardLayout.MinSize())
			keyboardLayout.Show()
			vw.virtualKeyboard.SetVisibleState(true)

			// ВАЖНО: для NewWithoutLayout нужно установить размер и позицию
			canvasSize := vw.parentWindow.Canvas().Size()
			logrus.Infof("⌨️ [DEBUG] Canvas Size: %v", canvasSize)

			keyboardSize := fyne.NewSize(canvasSize.Width, 300) // Высота клавиатуры
			keyboardLayout.Resize(keyboardSize)
			keyboardLayout.Move(fyne.NewPos(0, 0))
			logrus.Infof("⌨️ [DEBUG] keyboardLayout after resize: size=%v, position=%v", keyboardLayout.Size(), keyboardLayout.Position())

			vw.contentContainer.Objects = []fyne.CanvasObject{keyboardLayout}
			vw.contentContainer.Resize(keyboardSize)
			vw.contentContainer.Show()
			vw.container.Refresh()
			logrus.Infof("⌨️ [DEBUG] contentContainer: Size=%v, Visible=%v", vw.contentContainer.Size(), vw.contentContainer.Visible())
			logrus.Info("⌨️ Virtual keyboard shown below video (Android mode)")
		} else {
			// Desktop: открываем в отдельном окне
			vw.virtualKeyboard.ShowInSeparateWindow()
			logrus.Info("⌨️ Virtual keyboard shown in a separate window (desktop mode)")
		}
	}
}

// updateStats обновляет статистику
func (vw *VideoWidget) updateStats() {
	vw.frameMutex.RLock()
	lastFrameTime := vw.lastFrameTime
	vw.frameMutex.RUnlock()

	// Получаем статистику от декодера
	decoderStats := vw.frameDecoder.GetFrameStats()
	fps := decoderStats["fps"].(float64)

	stats := fmt.Sprintf("FPS: %.1f | %s",
		fps,
		lastFrameTime.Format("15:04:05"))

	vw.statsLabel.SetText(stats)
}

// SetParentWindow устанавливает родительское окно для диалогов
func (vw *VideoWidget) SetParentWindow(window fyne.Window) {
	vw.parentWindow = window

	// НЕ создаем диалоги здесь - они будут созданы лениво при первом использовании
	// Это необходимо для избежания threading ошибок на Android

	// На macOS Canvas.SetOnTypedKey ненадёжен — пересылаем клавиши через виджет с фокусом
	vw.touchpadWrapper.SetKeyHandlers(vw.handlePhysicalKeyPress, vw.handlePhysicalRunePress)
	vw.touchpadWrapper.SetWindowForFocus(window)

	// Добавляем обработчик горячих клавиш для полноэкранного режима
	window.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyF11 && vw.isStreaming {
			logrus.Info("🔍 F11 pressed, entering fullscreen mode")
			if vw.fullscreenDialog != nil {
				vw.fullscreenDialog.Show()
			}
		}
	})
}

// handlePhysicalKeyPress обрабатывает нажатия физической клавиатуры (оконный и полноэкранный режим).
// Печатаемые клавиши (буквы, цифры, пробел и т.д.) не обрабатываем здесь — для них приходит
// TypedRune с уже учтёнными модификаторами (Shift+A → 'A'). Иначе получится двойная отправка
// (сначала "a", потом "A") и на хосте случайный регистр.
func (vw *VideoWidget) handlePhysicalKeyPress(event *fyne.KeyEvent) {
	if vw.usbClient == nil {
		return
	}
	if event.Name == fyne.KeyF11 {
		return
	}
	if IsPrintableKey(event.Name) {
		return
	}
	go vw.sendPhysicalKeyToRemote(event)
}

// handlePhysicalRunePress обрабатывает ввод символов с физической клавиатуры
func (vw *VideoWidget) handlePhysicalRunePress(r rune) {
	if vw.usbClient == nil {
		return
	}
	go vw.sendRuneToRemote(r)
}

// sendPhysicalKeyToRemote конвертирует fyne.KeyEvent в HID и отправляет (только для непечатаемых клавиш:
// F-клавиши, стрелки, Backspace, Escape и т.д.). Вызывается только когда IsPrintableKey == false.
func (vw *VideoWidget) sendPhysicalKeyToRemote(event *fyne.KeyEvent) {
	keyCode := GetKeyCodeFromPhysical(event.Physical)
	if keyCode == 0 {
		keyCode = GetKeyCode(event.Name)
	}
	if keyCode == 0 {
		return
	}
	if err := vw.usbClient.SendKey(keyCode); err != nil {
		logrus.Errorf("⌨️ Failed to send key: %v", err)
	}
}

// sendRuneToRemote отправляет символ на удалённую машину
func (vw *VideoWidget) sendRuneToRemote(r rune) {
	// Enter на части платформ приходит и как KeyReturn, и как rune '\n' — не дублируем
	if r == '\n' || r == '\r' {
		return
	}
	keyCode, modifiers := GetRuneKeyCodeWithModifiers(r)
	if keyCode == 0 {
		return
	}
	var err error
	if modifiers > 0 {
		err = vw.usbClient.SendCombo(modifiers, keyCode)
	} else {
		err = vw.usbClient.SendKey(keyCode)
	}
	if err != nil {
		logrus.Errorf("⌨️ Failed to send rune: %v", err)
	}
}

// handleVirtualKeyPress обрабатывает нажатия виртуальной клавиатуры
func (vw *VideoWidget) handleVirtualKeyPress(keyCode int, modifiers int) {
	logrus.Infof("⌨️ Virtual keyboard: received key %d with modifiers %d", keyCode, modifiers)

	// Всегда пытаемся отправить клавишу на хост, если USB клиент доступен
	if vw.usbClient == nil {
		logrus.Warnf("⌨️ USB client is not connected, ignoring key: %d", keyCode)
		return
	}

	logrus.Infof("⌨️ Sending key to remote machine: code=%d, modifiers=%d", keyCode, modifiers)
	// Отправляем клавишу на удаленную машину
	go vw.sendKeyToRemote(keyCode, modifiers)
}

// sendKeyToRemote отправляет клавишу на удаленную машину через HID
func (vw *VideoWidget) sendKeyToRemote(keyCode int, modifiers int) {
	logrus.Infof("⌨️ sendKeyToRemote: sending key %d with modifiers %d", keyCode, modifiers)

	// Отправляем клавишу
	var err error
	if modifiers > 0 {
		// Отправляем комбинацию клавиш
		err = vw.usbClient.SendCombo(modifiers, keyCode)
		logrus.Infof("⌨️ Combination sent: modifiers=%d, key=%d", modifiers, keyCode)
	} else {
		// Отправляем одиночную клавишу
		logrus.Infof("⌨️ Sending single key: %d", keyCode)
		err = vw.usbClient.SendKey(keyCode)
		logrus.Infof("⌨️ Key sent: %d, result=%v", keyCode, err)
	}

	if err != nil {
		logrus.Errorf("⚠️ Failed to send key: %v", err)
	} else {
		logrus.Infof("✅ Key sent successfully: code=%d, modifiers=%d", keyCode, modifiers)
	}
}

// UpdateClient обновляет USB клиент
func (vw *VideoWidget) UpdateClient(usbClient *api.USBClient) {
	vw.usbClient = usbClient
	// Обновляем USB клиент в диалогах
	if vw.fullscreenDialog != nil {
		vw.fullscreenDialog.SetUSBClient(usbClient)
	}
	// Обновляем кнопки при смене клиента
	vw.updateButtons()
}

// SetFRPService устанавливает FRP сервис (для проверки режима туннеля)
func (vw *VideoWidget) SetFRPService(frp *service.FRPService) {
	vw.frpService = frp
}

// GetContainer возвращает контейнер виджета
func (vw *VideoWidget) GetContainer() *fyne.Container {
	return vw.container
}

// IsStreaming возвращает состояние захвата
func (vw *VideoWidget) IsStreaming() bool {
	return vw.isStreaming
}

// SetStreaming устанавливает состояние захвата
func (vw *VideoWidget) SetStreaming(streaming bool) {
	vw.isStreaming = streaming
	vw.updateButtons()
}

// clearVideo очищает видео
func (vw *VideoWidget) clearVideo() {
	// Очищаем текущий кадр
	vw.frameMutex.Lock()
	vw.currentFrame = nil
	vw.frameCount = 0
	vw.frameMutex.Unlock()

	// Очищаем canvas
	fyne.Do(func() {
		vw.videoCanvas.Resource = nil
		vw.videoCanvas.Refresh()
	})
}

// GetCurrentFrame возвращает текущий кадр для полноэкранного режима
func (vw *VideoWidget) GetCurrentFrame() image.Image {
	vw.frameMutex.RLock()
	defer vw.frameMutex.RUnlock()
	return vw.currentFrame
}

// GetFrameDecoder возвращает декодер кадров для полноэкранного режима
func (vw *VideoWidget) GetFrameDecoder() *VideoFrameDecoder {
	return vw.frameDecoder
}

// startDesktopMousePolling запускает горутину polling для плавного управления мышью
func (vw *VideoWidget) startDesktopMousePolling() {
	// Останавливаем предыдущую горутину если была
	vw.stopDesktopMousePolling()

	// Создаем канал для остановки
	vw.mousePollingQuit = make(chan bool)

	logrus.Info("🖱️ Starting desktop mouse polling (60 FPS)")

	go vw.processDesktopMousePolling()
}

// stopDesktopMousePolling останавливает горутину polling
func (vw *VideoWidget) stopDesktopMousePolling() {
	if vw.mousePollingQuit != nil {
		close(vw.mousePollingQuit)
		vw.mousePollingQuit = nil
		logrus.Info("🖱️ Desktop mouse polling stopped")
	}
}

// processDesktopMousePolling обрабатывает перемещение мыши с фиксированной частотой
func (vw *VideoWidget) processDesktopMousePolling() {
	ticker := time.NewTicker(16 * time.Millisecond) // 60 FPS
	defer ticker.Stop()

	for {
		select {
		case <-vw.mousePollingQuit:
			return
		case <-ticker.C:
			vw.processMouseMovement()
		}
	}
}

// processMouseMovement обрабатывает текущее перемещение мыши
func (vw *VideoWidget) processMouseMovement() {
	if !vw.isMouseConnected {
		return
	}
	if vw.dragButton == 0 {
		return
	}

	// Тачскрин и absolute: события шлём только из обработчиков ввода, не из polling
	if vw.GetMouseInputMode() == "touchscreen" || vw.GetMouseInputMode() == "absolute" {
		return
	}

	// Мышь: относительное перемещение
	rawDx := vw.currentMouseX - vw.lastMouseX
	rawDy := vw.currentMouseY - vw.lastMouseY
	if rawDx == 0 && rawDy == 0 {
		return
	}
	vw.lastMouseX = vw.currentMouseX
	vw.lastMouseY = vw.currentMouseY
	if !vw.isDragging {
		vw.isDragging = true
		logrus.Debugf("🖱️ ✨ Drag/swipe STARTED (desktop touchpad mode, polling)")
	}
	const desktopSensitivity = 1.0
	dx := int(float32(rawDx) * desktopSensitivity)
	dy := int(float32(rawDy) * desktopSensitivity)
	if dx == 0 && dy == 0 {
		return
	}
	if dx < -127 {
		dx = -127
	} else if dx > 127 {
		dx = 127
	}
	if dy < -127 {
		dy = -127
	} else if dy > 127 {
		dy = 127
	}
	if err := vw.usbClient.SendMouseMove(dx, dy); err != nil {
		logrus.Errorf("❌ Error sending mouse move: %v", err)
	}
}

// GetMouseInputMode возвращает тип манипулятора: "mouse" (мышь/тачпад), "touchscreen" (тачскрин) или "absolute".
// Задаётся при запуске устройства на экране устройств.
func (vw *VideoWidget) GetMouseInputMode() string {
	if vw.mouseInputMode == "" {
		vw.mouseInputMode = "mouse"
	}
	return vw.mouseInputMode
}

// SetMouseInputMode задаёт тип манипулятора: "mouse", "touchscreen" или "absolute" (вызывается после старта устройства).
func (vw *VideoWidget) SetMouseInputMode(mode string) {
	if mode != "mouse" && mode != "touchscreen" && mode != "absolute" {
		mode = "mouse"
	}
	vw.mouseInputMode = mode
	logrus.Debugf("🖱️ Pointer mode: %s", mode)
}

// SendAbsolutePosition отправляет абсолютную позицию с небольшим дебаунсом, чтобы убрать микродрожание.
// force=true используется для кликов/тапов — позиция всегда синхронизируется.
func (vw *VideoWidget) SendAbsolutePosition(x, y int, force bool) {
	if vw.usbClient == nil {
		return
	}
	const deadzone = 2 // минимальный шаг в абсолютных координатах (0..4095)
	const minInterval = 8 * time.Millisecond

	dx := x - vw.lastAbsX
	if dx < 0 {
		dx = -dx
	}
	dy := y - vw.lastAbsY
	if dy < 0 {
		dy = -dy
	}

	if !force {
		if dx < deadzone && dy < deadzone {
			return
		}
		if !vw.lastAbsSentTime.IsZero() && time.Since(vw.lastAbsSentTime) < minInterval {
			return
		}
	}

	vw.lastAbsX = x
	vw.lastAbsY = y
	vw.lastAbsSentTime = time.Now()
	_ = vw.usbClient.SendTouchPositionOnly(x, y, false)
}

// SetAbsoluteButton обновляет битмаску кнопок для absolute режима.
// button: 1=left, 2=right, 3=middle.
func (vw *VideoWidget) SetAbsoluteButton(button int, pressed bool) {
	var bit uint8
	switch button {
	case 1:
		bit = 0x01
	case 2:
		bit = 0x02
	case 3:
		bit = 0x04
	default:
		return
	}
	if pressed {
		vw.absButtons |= bit
	} else {
		vw.absButtons &^= bit
	}
}

// SendAbsoluteEvent отправляет атомарное абсолютное событие (позиция + кнопки + колесо).
func (vw *VideoWidget) SendAbsoluteEvent(x, y int, scroll int, force bool) {
	if vw.usbClient == nil {
		return
	}
	vw.lastAbsX = x
	vw.lastAbsY = y
	vw.lastAbsSentTime = time.Now()
	_ = vw.usbClient.SendAbsoluteEvent(x, y, vw.absButtons, scroll)
}

// CancelTouchDownDelay отменяет отложенную отправку touch(down). Вызывать из Tapped, чтобы один тап давал только пару down+up из Tapped.
func (vw *VideoWidget) CancelTouchDownDelay() {
	vw.touchDownDelayMu.Lock()
	defer vw.touchDownDelayMu.Unlock()
	if vw.touchDownDelayTimer != nil {
		vw.touchDownDelayTimer.Stop()
		vw.touchDownDelayTimer = nil
	}
}

// StartTouchDownDelay планирует отправку touch(down) через ~120ms. button: 1 = левая (touch+BTN_LEFT), 2 = правая (только позиция, клик потом).
// Если до этого придёт Tapped — отменяем (CancelTouchDownDelay).
func (vw *VideoWidget) StartTouchDownDelay(x, y int, button int) {
	vw.touchDownDelayMu.Lock()
	if vw.touchDownDelayTimer != nil {
		vw.touchDownDelayTimer.Stop()
		vw.touchDownDelayTimer = nil
	}
	vw.touchDownDelayTimer = time.AfterFunc(120*time.Millisecond, func() {
		vw.touchDownDelayMu.Lock()
		vw.touchDownDelayTimer = nil
		vw.touchDownDelayMu.Unlock()
		vw.touchActive = true
		vw.lastTouchX = x
		vw.lastTouchY = y
		vw.lastTouchDownTime = time.Now()
		if button == 2 {
			_ = vw.usbClient.SendTouchPositionOnly(x, y, true)
		} else {
			_ = vw.usbClient.SendTouch(x, y, true)
		}
	})
	vw.touchDownDelayMu.Unlock()
}

// TryRecordTouchDown записывает «отправляем touch(down)» и возвращает true, если нужно отправить; false — дубликат.
func (vw *VideoWidget) TryRecordTouchDown(x, y int) bool {
	const samePointRadius = 5
	vw.touchDedupMu.Lock()
	defer vw.touchDedupMu.Unlock()
	dx := x - vw.lastTouchX
	if dx < 0 {
		dx = -dx
	}
	dy := y - vw.lastTouchY
	if dy < 0 {
		dy = -dy
	}
	if dx <= samePointRadius && dy <= samePointRadius && time.Since(vw.lastTouchDownTime) < 120*time.Millisecond {
		return false
	}
	vw.lastTouchDownTime = time.Now()
	vw.lastTouchX = x
	vw.lastTouchY = y
	return true
}

// UpdateTouchpadAndContentRect обновляет размер области ввода и прямоугольник видео (ImageFillContain).
// Вызывается из обработчика ввода при каждом событии, чтобы координаты соответствовали области видео.
func (vw *VideoWidget) UpdateTouchpadAndContentRect(w, h float32) {
	if w <= 0 || h <= 0 {
		return
	}
	vw.touchpadSizeW = w
	vw.touchpadSizeH = h
	vw.contentRectX = 0
	vw.contentRectY = 0
	vw.contentRectW = w
	vw.contentRectH = h
	if vw.videoCanvas != nil && vw.videoCanvas.Image != nil {
		b := vw.videoCanvas.Image.Bounds()
		imgW := float32(b.Dx())
		imgH := float32(b.Dy())
		if imgW > 0 && imgH > 0 {
			scale := w / imgW
			if h/imgH < scale {
				scale = h / imgH
			}
			renderW := imgW * scale
			renderH := imgH * scale
			vw.contentRectX = (w - renderW) / 2
			vw.contentRectY = (h - renderH) / 2
			vw.contentRectW = renderW
			vw.contentRectH = renderH
		}
	}
}

// PositionToAbsolute переводит координаты из области ввода в абсолютные 0..4095 (тачскрин HID).
// Учитывает прямоугольник видео (content rect): клик по видео масштабируется в логическое пространство 0..4095.
func (vw *VideoWidget) PositionToAbsolute(px, py float32) (x, y int) {
	if vw.touchpadSizeW <= 0 || vw.touchpadSizeH <= 0 {
		return 0, 0
	}
	var u, v float32
	if vw.contentRectW > 0 && vw.contentRectH > 0 {
		u = (px - vw.contentRectX) / vw.contentRectW
		v = (py - vw.contentRectY) / vw.contentRectH
	} else {
		u = px / vw.touchpadSizeW
		v = py / vw.touchpadSizeH
	}
	if u < 0 {
		u = 0
	} else if u > 1 {
		u = 1
	}
	if v < 0 {
		v = 0
	} else if v > 1 {
		v = 1
	}
	x = int(u * 4095)
	y = int(v * 4095)
	if x > 4095 {
		x = 4095
	}
	if y > 4095 {
		y = 4095
	}
	return x, y
}

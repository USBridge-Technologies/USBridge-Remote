package controller

import (
	"image"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/graphics"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"github.com/sirupsen/logrus"
)

// FullscreenDialog диалог полноэкранного режима
type FullscreenDialog struct {
	parent           fyne.Window
	videoWidget      *VideoWidget
	isFullscreen     bool
	nativeFullscreen bool
	gstreamerService *service.GStreamerService
	usbClient        *api.USBClient
	keyboardEnabled  bool
	fullscreenWindow fyne.Window
	virtualKeyboard  *graphics.VirtualKeyboard
	videoImage       *canvas.Image
	touchpadWrapper  *TouchpadWrapper
	nativeCapture    nativeFullscreenCapture
	lastFrame        image.Image
	frameMutex       sync.RWMutex
	originalContent  *fyne.Container
	originalTitle    string
	ui               *view.FullscreenUI
}

// NewFullscreenDialog создает новый диалог полноэкранного режима
func NewFullscreenDialog(parent fyne.Window) *FullscreenDialog {
	return &FullscreenDialog{parent: parent}
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

	deviceInfo, err := fd.usbClient.GetDeviceInfo()
	if err != nil {
		logrus.Warnf("⚠️ Ошибка получения информации об устройствах: %v", err)
		fd.keyboardEnabled = false
		return
	}

	logrus.Infof("⌨️ Получена информация об устройствах: %d устройств", len(deviceInfo.Devices))

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

	if !fd.keyboardEnabled {
		logrus.Warn("⌨️ Клавиатура не найдена в списке устройств, попробуем принудительно включить")
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
	if fd.videoWidget == nil {
		logrus.Warn("⚠️ Видео виджет не установлен")
		return
	}
	if !fd.videoWidget.IsStreaming() {
		logrus.Warn("⚠️ Видео не запущено")
		return
	}

	fd.enterFullscreen()
}

// enterFullscreen переводит вывод в fullscreen с минимизацией лишних UI-перерисовок.
func (fd *FullscreenDialog) enterFullscreen() {
	logrus.Info("🔍 Вход в полноэкранный режим с GStreamer")

	restartWindowPipeline := false
	if fd.gstreamerService != nil && fd.gstreamerService.SupportsNativeFullscreen() {
		restartWindowPipeline = true
		if err := fd.gstreamerService.StartNativeFullscreen(); err == nil {
			fd.nativeCapture = newNativeFullscreenCapture(fd.usbClient, fd.exitFullscreen)
			if fd.nativeCapture != nil {
				if captureErr := fd.nativeCapture.Start(); captureErr != nil {
					logrus.Warnf("⚠️ Native fullscreen input capture unavailable, stopping native fullscreen: %v", captureErr)
					_ = fd.gstreamerService.StopNativeFullscreen()
					fd.nativeCapture = nil
				} else {
					fd.isFullscreen = true
					fd.nativeFullscreen = true
					logrus.Info("✅ Активирован native fullscreen video sink с macOS input capture")
					return
				}
			}
		} else {
			logrus.Warnf("⚠️ Native fullscreen недоступен, используем Fyne fallback: %v", err)
		}
	}
	fd.isFullscreen = true
	fd.nativeFullscreen = false
	fd.createFullscreenWindow()

	if fd.gstreamerService != nil && fd.videoWidget != nil {
		fd.gstreamerService.SetOnFrameReceived(func(frame image.Image) {
			fd.videoWidget.handleVideoFrame(frame)
			fd.updateVideoFrame(frame)
		})
		logrus.Info("✅ Fullscreen получает кадры без перерисовки скрытого основного canvas")
		if restartWindowPipeline {
			go func() {
				if err := fd.gstreamerService.ConnectToRTP(); err != nil {
					logrus.Warnf("⚠️ Не удалось восстановить video pipeline для Fyne fullscreen fallback: %v", err)
				}
			}()
		}
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
	videoImg := fd.videoImage
	touchpad := fd.touchpadWrapper
	frameCount := fd.videoWidget.frameCount
	fd.frameMutex.Unlock()

	if videoImg == nil {
		logrus.Warn("⚠️ updateVideoFrame: videoImg is nil")
		return
	}

	if frameCount%30 == 0 {
		logrus.Infof("🖼️ Полноэкранный режим: обновление кадра %d", frameCount)
	}

	fyne.Do(func() {
		if videoImg.Image != frame {
			videoImg.Image = frame
		}
		videoImg.Refresh()
		if touchpad != nil {
			touchpad.Refresh()
		}
	})
}

// createFullscreenWindow создает полноэкранное окно с видео
func (fd *FullscreenDialog) createFullscreenWindow() {
	logrus.Info("🔍 Создание полноэкранного окна с видео")

	fd.platformInitWindow()

	currentFrame := fd.videoWidget.GetCurrentFrame()
	if currentFrame != nil {
		bounds := currentFrame.Bounds()
		logrus.Infof("✅ Установлен начальный кадр в полноэкранное окно: %dx%d", bounds.Dx(), bounds.Dy())
	} else {
		logrus.Warn("⚠️ Нет текущего кадра для полноэкранного окна - будет черный экран до первого кадра")
	}

	fd.videoImage = canvas.NewImageFromImage(currentFrame)
	fd.videoImage.FillMode = canvas.ImageFillContain
	fd.videoImage.ScaleMode = canvas.ImageScaleFastest
	fd.touchpadWrapper = NewTouchpadWrapperWithImage(fd.videoWidget, fd.videoImage)
	fd.videoWidget.platformRegisterGestureTarget()
	fd.touchpadWrapper.SetKeyHandlers(fd.handleKeyPress, fd.handleRunePress)
	fd.touchpadWrapper.SetWindowForFocus(fd.fullscreenWindow)
	logrus.Info("✅ TouchpadWrapper создан для полноэкранного режима")

	logrus.Info("⌨️ [DEBUG] Создание виртуальной клавиатуры для полноэкранного режима")
	fd.virtualKeyboard = graphics.NewVirtualKeyboard(fd.fullscreenWindow, fd.handleVirtualKeyPress, fd.handleRunePress)
	fd.virtualKeyboard.SetOnTextTyped(func(text string) {
		if fd.usbClient != nil {
			if err := fd.usbClient.SendText(text); err != nil {
				logrus.Errorf("⚠️ Ошибка отправки текста: %v", err)
			}
		}
	})
	fd.platformSetupUI()

	keyboardLayout := fd.virtualKeyboard.GetKeyboardLayout()
	logrus.Infof("⌨️ [DEBUG] keyboardLayout получен: %v, MinSize: %v", keyboardLayout != nil, keyboardLayout.MinSize())
	keyboardLayout.Hide()
	logrus.Infof("⌨️ [DEBUG] keyboardLayout.Hide() вызван, Visible: %v", keyboardLayout.Visible())
	logrus.Info("⌨️ [DEBUG] Создание overlay layout для размещения клавиатуры поверх видео")

	fd.ui = view.NewFullscreenUI(fd.videoImage, fd.touchpadWrapper, keyboardLayout, func() {
		logrus.Info("⌨️ ========== НАЖАТА КНОПКА КЛАВИАТУРЫ В ПОЛНОЭКРАННОМ РЕЖИМЕ ==========")

		if keyboardLayout.Visible() {
			logrus.Info("⌨️ Клавиатура видима - скрываем")
			fd.virtualKeyboard.Hide()
			fd.ui.KeyboardButton.SetIcon(assets.KeyboardIconActive)
		} else {
			logrus.Info("⌨️ Клавиатура скрыта - показываем")
			fd.virtualKeyboard.SetVisibleState(true)
			fd.virtualKeyboard.FocusInput() // Принудительный фокус для Android
			fd.ui.KeyboardButton.SetIcon(assets.KeyboardIcon)
		}

		// Resize принудительно пересчитывает BorderLayout (Refresh только перерисовывает без layout-прохода)
		fd.ui.VideoWithKeyboard.Resize(fd.ui.VideoWithKeyboard.Size())
		fd.ui.VideoWithKeyboard.Refresh()
		fd.fullscreenWindow.Canvas().Refresh(fd.fullscreenWindow.Content())
		logrus.Infof("⌨️ После переключения - Visible: %v, Size: %v, Position: %v",
			keyboardLayout.Visible(), keyboardLayout.Size(), keyboardLayout.Position())
	})

	logrus.Info("⌨️ [DEBUG] Stack контейнер создан с overlay элементами")
	fd.fullscreenWindow.SetContent(fd.ui.MainContainer)

	updatePositions := func() {
		canvasSize := fd.fullscreenWindow.Canvas().Size()
		logrus.Infof("🔍 [DEBUG] updatePositions - Canvas Size: %v", canvasSize)

		buttonSize := fyne.NewSize(50, 40)
		fd.ui.KeyboardButton.Resize(buttonSize)
		fd.ui.KeyboardButton.Move(fyne.NewPos(10, 10))

		keyboardHeight := float32(300)
		keyboardSize := fyne.NewSize(canvasSize.Width, view.MobileFooterBottomInset(keyboardHeight))
		keyboardLayout.Resize(keyboardSize)

		logrus.Infof("⌨️ [DEBUG] Позиции установлены:")
		logrus.Infof("⌨️ [DEBUG]   Canvas Size: %v", canvasSize)
		logrus.Infof("⌨️ [DEBUG]   Button Position: %v, Size: %v", fd.ui.KeyboardButton.Position(), fd.ui.KeyboardButton.Size())
		logrus.Infof("⌨️ [DEBUG]   Keyboard Height: %v", keyboardHeight)
		logrus.Infof("⌨️ [DEBUG]   Full Keyboard Height (with insets): %v", keyboardSize.Height)
		logrus.Infof("⌨️ [DEBUG]   Keyboard Size: %v", keyboardLayout.Size())
	}

	fd.fullscreenWindow.Canvas().SetOnTypedKey(func(event *fyne.KeyEvent) {
		if event.Name == fyne.KeyEscape || string(event.Name) == "Back" {
			logrus.Infof("🔍 Нажата клавиша выхода (%s) - выход из полноэкранного режима", event.Name)
			fd.exitFullscreen()
			return
		}
		fd.handleKeyPress(event)
	})

	fd.fullscreenWindow.Canvas().SetOnTypedRune(func(r rune) {
		fd.handleRunePress(r)
	})

	go func() {
		time.Sleep(150 * time.Millisecond)
		fyne.Do(updatePositions)

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		lastSize := fyne.NewSize(0, 0)
		for range ticker.C {
			if !fd.isFullscreen {
				return
			}

			fyne.Do(func() {
				if !fd.isFullscreen || fd.fullscreenWindow == nil {
					return
				}
				currentSize := fd.fullscreenWindow.Canvas().Size()
				if currentSize != lastSize {
					logrus.Infof("🔄 Обнаружено изменение размера экрана: %v -> %v", lastSize, currentSize)
					// Если canvas вырос по высоте — экранная клавиатура закрылась.
					// Сбрасываем IME-spacer на случай если onUnfocused не сработал
					// (например, при закрытии keyboard кнопкой «назад»).
					if currentSize.Height > lastSize.Height && lastSize.Height > 0 && fd.virtualKeyboard != nil {
						fd.virtualKeyboard.ResetIMEState()
					}
					updatePositions()
					lastSize = currentSize
				}
			})
		}
	}()

	logrus.Infof("⌨️ [DEBUG] Overlay контейнер создан")
	logrus.Info("🔍 Полноэкранное окно создано")
	fd.platformShow()
	logrus.Info("🔍 Полноэкранное окно показано")

	if fd.videoImage != nil && fd.videoImage.Image != nil {
		fd.videoImage.Refresh()
		logrus.Info("🔍 Изображение обновлено после показа окна")
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !fd.isFullscreen || fd.fullscreenWindow == nil || fd.touchpadWrapper == nil {
			return
		}
		// На десктопе фокусируемся автоматически, на мобилках фокус управляется системно
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
}

// exitFullscreen выходит из полноэкранного режима
func (fd *FullscreenDialog) exitFullscreen() {
	if !fd.isFullscreen {
		return
	}

	logrus.Info("🔍 Выход из полноэкранного режима")

	fd.isFullscreen = false
	if fd.nativeFullscreen {
		fd.nativeFullscreen = false
		if fd.nativeCapture != nil {
			if err := fd.nativeCapture.Stop(); err != nil {
				logrus.Warnf("⚠️ Ошибка остановки native input capture: %v", err)
			}
			fd.nativeCapture = nil
		}
		if fd.gstreamerService != nil {
			if err := fd.gstreamerService.StopNativeFullscreen(); err != nil {
				logrus.Warnf("⚠️ Ошибка остановки native fullscreen: %v", err)
			}
			if fd.videoWidget != nil {
				fd.gstreamerService.SetOnFrameReceived(func(frame image.Image) {
					fd.videoWidget.handleVideoFrame(frame)
				})
				go func() {
					if err := fd.gstreamerService.ConnectToRTP(); err != nil {
						logrus.Warnf("⚠️ Не удалось восстановить оконный video pipeline после native fullscreen: %v", err)
					}
				}()
			}
		}
		logrus.Info("✅ Native fullscreen деактивирован")
		return
	}

	fd.frameMutex.Lock()
	fd.videoImage = nil
	fd.touchpadWrapper = nil
	fd.frameMutex.Unlock()
	fd.ui = nil

	if fd.virtualKeyboard != nil {
		fd.virtualKeyboard.Hide()
		fd.virtualKeyboard = nil
	}

	fd.platformExit()

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

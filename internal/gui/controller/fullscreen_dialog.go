package controller

import (
	"image"
	"sync"
	"sync/atomic"
	"time"

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
	parent                fyne.Window
	videoWidget           *VideoWidget
	isFullscreen          bool
	nativeFullscreen      bool
	videoClient      service.VideoClient
	fullscreenWindow      fyne.Window
	virtualKeyboard       *graphics.VirtualKeyboard
	videoImage            *canvas.Image
	touchpadWrapper       *TouchpadWrapper
	nativeCapture         nativeFullscreenCapture
	lastFrame             image.Image
	frameMutex            sync.RWMutex
	originalContent       *fyne.Container
	originalTitle         string
	ui                    *view.FullscreenUI
	keyboardModifierState atomic.Int32
	suppressRuneUntilNS   atomic.Int64
	audioMuted            bool
}

// NewFullscreenDialog создает новый диалог полноэкранного режима
func NewFullscreenDialog(parent fyne.Window) *FullscreenDialog {
	return &FullscreenDialog{parent: parent}
}

// SetVideoWidget устанавливает ссылку на видео виджет
func (fd *FullscreenDialog) SetVideoWidget(videoWidget *VideoWidget) {
	fd.videoWidget = videoWidget
}

// SetVideoClient устанавливает ссылку на видео сервис
func (fd *FullscreenDialog) SetVideoClient(videoClient service.VideoClient) {
	fd.videoClient = videoClient
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
	if fd.videoClient != nil && fd.videoClient.SupportsNativeFullscreen() {
		restartWindowPipeline = true
		if err := fd.videoClient.StartNativeFullscreen(); err == nil {
			fd.nativeCapture = newNativeFullscreenCapture(fd.videoWidget.GetMoonlightInput, fd.exitFullscreen)
			if fd.nativeCapture != nil {
				if captureErr := fd.nativeCapture.Start(); captureErr != nil {
					logrus.Warnf("⚠️ Native fullscreen input capture unavailable, stopping native fullscreen: %v", captureErr)
					_ = fd.videoClient.StopNativeFullscreen()
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

	if fd.videoClient != nil && fd.videoWidget != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			fd.videoWidget.handleVideoFrame(frame)
			fd.updateVideoFrame(frame)
		})
		logrus.Info("✅ Fullscreen получает кадры без перерисовки скрытого основного canvas")
		if restartWindowPipeline {
			go func() {
				if err := fd.videoClient.ConnectToRTP(); err != nil {
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

	// When native overlay (Metal/GL) is rendering, it has its own frame source.
	// Skip canvas updates entirely to prevent picture-in-picture artefacts.
	if fd.videoWidget != nil && fd.videoWidget.isNativeVideoActive() {
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

	// When native GPU overlay (Metal/GL) is active, Go-side currentFrame is stale
	// (set from early frames before the overlay took over). Passing it to fd.videoImage
	// causes a ghost background frame in fullscreen alongside the Metal overlay.
	var currentFrame image.Image
	if !fd.videoWidget.isNativeVideoActive() {
		currentFrame = fd.videoWidget.GetCurrentFrame()
	}
	if currentFrame != nil {
		bounds := currentFrame.Bounds()
		logrus.Infof("✅ Установлен начальный кадр в полноэкранное окно: %dx%d", bounds.Dx(), bounds.Dy())
	} else {
		logrus.Info("⚠️ Нет текущего кадра для fullscreen canvas (ожидается: native overlay или первый кадр)")
	}

	fd.videoImage = canvas.NewImageFromImage(currentFrame)
	fd.videoImage.FillMode = canvas.ImageFillContain
	fd.videoImage.ScaleMode = canvas.ImageScaleFastest
	fd.touchpadWrapper = NewTouchpadWrapperWithImage(fd.videoWidget, fd.videoImage)
	fd.videoWidget.platformRegisterGestureTarget()
	fd.touchpadWrapper.SetKeyHandlers(fd.handleKeyDown, fd.handleKeyUp, fd.handleKeyPress, fd.handleRunePress)
	fd.touchpadWrapper.SetWindowForFocus(fd.fullscreenWindow)
	logrus.Info("✅ TouchpadWrapper создан для полноэкранного режима")

	logrus.Info("⌨️ [DEBUG] Создание виртуальной клавиатуры для полноэкранного режима")
	fd.virtualKeyboard = graphics.NewVirtualKeyboard(fd.fullscreenWindow, fd.handleVirtualKeyPress, fd.handleRunePress)
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
			if fd.ui.KeyboardButton != nil {
				fd.ui.KeyboardButton.SetIcon(assets.KeyboardIconActive)
			}
			if fd.videoWidget != nil {
				fd.videoWidget.SetBottomInset(graphics.GetLastIMEH())
			}
		} else {
			logrus.Info("⌨️ Клавиатура скрыта - показываем")
			fd.virtualKeyboard.SetVisibleState(true)
			fd.virtualKeyboard.FocusInput() // Принудительный фокус для Android
			if fd.ui.KeyboardButton != nil {
				fd.ui.KeyboardButton.SetIcon(assets.KeyboardIcon)
			}
			if fd.videoWidget != nil {
				h := keyboardLayout.MinSize().Height
				if h < 50 { // Если размер еще не просчитан, используем разумное значение по умолчанию
					h = 145
				}
				fd.videoWidget.SetBottomInset(h)
			}
		}

		// Resize принудительно пересчитывает BorderLayout (Refresh только перерисовывает без layout-прохода)
		fd.ui.VideoWithKeyboard.Resize(fd.ui.VideoWithKeyboard.Size())
		fd.ui.VideoWithKeyboard.Refresh()
		fd.fullscreenWindow.Canvas().Refresh(fd.fullscreenWindow.Content())
		logrus.Infof("⌨️ После переключения - Visible: %v, Size: %v, Position: %v",
			keyboardLayout.Visible(), keyboardLayout.Size(), keyboardLayout.Position())
	}, func() {
		logrus.Info("🔊 ========== НАЖАТА КНОПКА АУДИО В ПОЛНОЭКРАННОМ РЕЖИМЕ ==========")
		fd.toggleAudioMuted()
	})

	logrus.Info("⌨️ [DEBUG] Stack контейнер создан с overlay элементами")
	fd.fullscreenWindow.SetContent(fd.ui.MainContainer)

	// Установить начальный вид иконки аудио
	fd.updateAudioButtonIcon()

	updatePositions := func() {
		canvasSize := fd.fullscreenWindow.Canvas().Size()
		logrus.Infof("🔍 [DEBUG] updatePositions - Canvas Size: %v", canvasSize)

		buttonSize := fyne.NewSize(50, 40)
		if fd.ui.KeyboardButton != nil {
			fd.ui.KeyboardButton.Resize(buttonSize)
			fd.ui.KeyboardButton.Move(fyne.NewPos(10, 10))
		}
		if fd.ui.AudioButton != nil {
			fd.ui.AudioButton.Resize(buttonSize)
			fd.ui.AudioButton.Move(fyne.NewPos(65, 10))
		}

		keyboardHeight := float32(300)
		keyboardSize := fyne.NewSize(canvasSize.Width, view.MobileFooterBottomInset(keyboardHeight))
		keyboardLayout.Resize(keyboardSize)

		logrus.Infof("⌨️ [DEBUG] Позиции установлены:")
		logrus.Infof("⌨️ [DEBUG]   Canvas Size: %v", canvasSize)
		if fd.ui.KeyboardButton != nil {
			logrus.Infof("⌨️ [DEBUG]   Button Position: %v, Size: %v", fd.ui.KeyboardButton.Position(), fd.ui.KeyboardButton.Size())
		}
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
		// handleKeyPress already guards against Moonlight-active path (where keys go via
		// KeyDown/KeyUp instead). Canvas.SetOnTypedKey fires on every OS key-repeat, so
		// without the guard we'd get turbo: this call is only meaningful for non-Moonlight.
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

	// Switch Metal overlay to the fullscreen window (full-window coverage).
	// Delay slightly so macOS finishes the fullscreen animation and the
	// contentView.bounds reflect the actual screen size before we create the overlay.
	if fd.videoWidget != nil {
		fsWin := fd.fullscreenWindow
		vw := fd.videoWidget
		fsImg := fd.videoImage
		// Once the native overlay is live, clear the Fyne canvas so only Metal renders.
		// Without this the Go canvas shows its last frame behind the overlay (PiP).
		vw.onNativeReady = func() {
			if fsImg != nil {
				fsImg.Image = nil
				fsImg.Refresh()
			}
		}
		time.AfterFunc(250*time.Millisecond, func() {
			vw.metalVideoEnterFullscreen(fsWin)
		})
	}

	if fd.videoImage != nil && fd.videoImage.Image != nil {
		fd.videoImage.Refresh()
		logrus.Info("🔍 Изображение обновлено после показа окна")
	}

	go func() {
		time.Sleep(500 * time.Millisecond)
		if !fd.isFullscreen || fd.fullscreenWindow == nil || fd.touchpadWrapper == nil {
			return
		}
		// На десктопе и на мобилках фокус на touchpad нужен для перехвата кнопки "Назад" без первого тапа.
		fyne.Do(func() {
			fd.fullscreenWindow.RequestFocus()
			fd.fullscreenWindow.Canvas().Focus(fd.touchpadWrapper)
		})
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
		if fd.videoClient != nil {
			if err := fd.videoClient.StopNativeFullscreen(); err != nil {
				logrus.Warnf("⚠️ Ошибка остановки native fullscreen: %v", err)
			}
			if fd.videoWidget != nil {
				fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
					fd.videoWidget.handleVideoFrame(frame)
				})
				go func() {
					if err := fd.videoClient.ConnectToRTP(); err != nil {
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
		fd.virtualKeyboard.UnregisterAsIMETarget()
		fd.virtualKeyboard.Hide()
		fd.virtualKeyboard = nil
	}

	// Destroy the native overlay before the window closes, unless the platform
	// keeps a single overlay alive across fullscreen transitions (Android Vulkan).
	if fd.videoWidget != nil && !fd.videoWidget.keepNativeVideoAliveForFullscreenTransition() {
		fd.videoWidget.stopMetalVideo()
	}

	fd.platformExit()

	if fd.videoClient != nil && fd.videoWidget != nil {
		fd.videoClient.SetOnFrameReceived(func(frame image.Image) {
			fd.videoWidget.handleVideoFrame(frame)
		})
		logrus.Info("✅ Восстановлена подписка на кадры для основного окна")
		fd.videoWidget.ensureInputFocusAsync("exit-fullscreen", 150*time.Millisecond)
	}
	fd.lastFrame = nil

	// Restore Metal overlay on the main window.
	if fd.videoWidget != nil {
		fd.videoWidget.metalVideoExitFullscreen()
	}

	logrus.Info("✅ Полноэкранный режим деактивирован")
}

// IsFullscreen возвращает состояние полноэкранного режима
func (fd *FullscreenDialog) IsFullscreen() bool {
	return fd.isFullscreen
}

// toggleAudioMuted переключает состояние звука
func (fd *FullscreenDialog) toggleAudioMuted() {
	fd.audioMuted = !fd.audioMuted
	if ms, ok := fd.videoClient.(interface{ SetAudioMuted(bool) }); ok {
		ms.SetAudioMuted(fd.audioMuted)
		logrus.Infof("🔊 Audio muted=%v", fd.audioMuted)
	}
	fd.updateAudioButtonIcon()
}

// updateAudioButtonIcon обновляет иконку кнопки аудио в соответствии с состоянием
func (fd *FullscreenDialog) updateAudioButtonIcon() {
	if fd.ui == nil || fd.ui.AudioButton == nil {
		return
	}
	if fd.audioMuted {
		fd.ui.AudioButton.SetIcon(assets.AudioMuteIconActive)
	} else {
		fd.ui.AudioButton.SetIcon(assets.AudioIcon)
	}
}

// SetAudioMuted устанавливает состояние звука
func (fd *FullscreenDialog) SetAudioMuted(muted bool) {
	fd.audioMuted = muted
	fd.updateAudioButtonIcon()
}

// HandleUIClick checks if (x, y) in Fyne dp coords hits a fullscreen UI button.
// If so, it triggers the button and returns true — caller should suppress the event.
func (fd *FullscreenDialog) HandleUIClick(x, y float32) bool {
	if fd.ui == nil {
		return false
	}
	if btn := fd.ui.KeyboardButton; btn != nil {
		pos := btn.Position()
		sz := btn.Size()
		if x >= pos.X && x <= pos.X+sz.Width && y >= pos.Y && y <= pos.Y+sz.Height {
			btn.Tapped(&fyne.PointEvent{Position: fyne.NewPos(x-pos.X, y-pos.Y)})
			return true
		}
	}
	if btn := fd.ui.AudioButton; btn != nil {
		pos := btn.Position()
		sz := btn.Size()
		if x >= pos.X && x <= pos.X+sz.Width && y >= pos.Y && y <= pos.Y+sz.Height {
			btn.Tapped(&fyne.PointEvent{Position: fyne.NewPos(x-pos.X, y-pos.Y)})
			return true
		}
	}
	return false
}

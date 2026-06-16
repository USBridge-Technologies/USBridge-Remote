//go:build android || ios

package controller

import (
	"usbridge-client/internal/gui/graphics"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (fd *FullscreenDialog) platformInitWindow() {
	logrus.Info("🔍 Mobile: используем основное окно для полноэкранного режима")
	fd.fullscreenWindow = fd.parent
	fd.originalContent = fd.parent.Content().(*fyne.Container)
	fd.originalTitle = fd.parent.Title()
	
	// На мобилках убираем заголовок и включаем настоящий полноэкранный режим
	fd.fullscreenWindow.SetTitle("")
	fd.fullscreenWindow.SetFullScreen(true)
}

func (fd *FullscreenDialog) platformSetupUI() {
	if fd.virtualKeyboard == nil {
		return
	}
	
	// Регистрируем как получателя нативных Android IME-событий (KeyboardBridge → JNI → Go)
	fd.virtualKeyboard.RegisterAsIMETarget()

	// Когда Android IME открывается/закрывается, обновляем layout fullscreen окна.
	fd.virtualKeyboard.SetOnIMEChanged(func(imeHeightDp float32) {
		fyne.Do(func() {
			if fd.ui != nil {
				// Вычисляем суммарный инсет: кнопки виртуальной клавиатуры + системный IME/NavBar
				var inset float32
				if fd.ui.KeyboardLayout.Visible() {
					// Если виртуальная клавиатура показана, её высота уже включает в себя системный imeSpacer
					inset = fd.ui.KeyboardLayout.MinSize().Height
				} else {
					// Если скрыта — оставляем только системный отступ (например, NavBar)
					inset = graphics.GetLastIMEH()
				}
				
				logrus.Infof("⌨️ [IME] Change detected. Setting bottom inset to %.1f (imeH=%.1f)", inset, imeHeightDp)
				if fd.videoWidget != nil {
					fd.videoWidget.SetBottomInset(inset)
				}

				// Принудительно обновляем все дерево
				size := fd.fullscreenWindow.Canvas().Size()
				fd.ui.VideoWithKeyboard.Resize(size)
				fd.ui.VideoWithKeyboard.Refresh()
				fd.virtualKeyboard.UpdatePosition(size)
			}
		})
	})
}

func (fd *FullscreenDialog) platformShow() {
	fd.fullscreenWindow.Show()
	// На мобилках фокус на ввод обычно управляется через виртуальную клавиатуру
}

func (fd *FullscreenDialog) platformExit() {
	logrus.Info("🔍 Mobile: восстанавливаем оригинальное содержимое основного окна")
	fd.parent.SetFullScreen(false)
	if fd.originalContent != nil {
		fd.parent.SetContent(fd.originalContent)
		fd.parent.SetTitle(fd.originalTitle)
		logrus.Info("✅ Оригинальное содержимое восстановлено")
	}
}

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
	fd.fullscreenWindow.SetTitle("")
	// SetFullScreen вызывается в platformShow() после SetContent(), чтобы
	// Android не мигал старым контентом (вкладки, панель) до перехода на Vulkan.
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
	// SetFullScreen здесь, а не в platformInitWindow — контент уже подменён,
	// поэтому Android сразу рисует новый (прозрачный) контент без мигания старым UI.
	fd.fullscreenWindow.SetFullScreen(true)
	fd.fullscreenWindow.Show()
	// Unfocus any widgets (e.g. TouchpadWrapper) that might trigger the IME keyboard automatically.
	if canvas := fd.fullscreenWindow.Canvas(); canvas != nil {
		canvas.Unfocus()
	}
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

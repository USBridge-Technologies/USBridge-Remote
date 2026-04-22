//go:build android || ios

package controller

import (
	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (fd *FullscreenDialog) platformInitWindow() {
	logrus.Info("🔍 Mobile: используем основное окно для полноэкранного режима")
	fd.fullscreenWindow = fd.parent
	fd.originalContent = fd.parent.Content().(*fyne.Container)
	fd.originalTitle = fd.parent.Title()
	
	// На мобилках убираем заголовок в полноэкранном режиме
	fd.fullscreenWindow.SetTitle("")
}

func (fd *FullscreenDialog) platformSetupUI() {
	if fd.virtualKeyboard == nil {
		return
	}
	
	// Регистрируем как получателя нативных Android IME-событий (KeyboardBridge → JNI → Go)
	fd.virtualKeyboard.RegisterAsIMETarget()

	// Когда Android IME открывается/закрывается, обновляем layout fullscreen окна.
	fd.virtualKeyboard.SetOnIMEChanged(func(_ bool) {
		fyne.Do(func() {
			if fd.ui != nil {
				// Resize принудительно пересчитывает BorderLayout (Refresh только перерисовывает)
				fd.ui.VideoWithKeyboard.Resize(fd.ui.VideoWithKeyboard.Size())
				fd.ui.VideoWithKeyboard.Refresh()
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
	if fd.originalContent != nil {
		fd.parent.SetContent(fd.originalContent)
		fd.parent.SetTitle(fd.originalTitle)
		logrus.Info("✅ Оригинальное содержимое восстановлено")
	}
}

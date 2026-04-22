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
				// Используем актуальный размер Canvas для принудительного пересчета всего дерева объектов.
				// Это заставляет BorderLayout сжать видео-контейнер, освобождая место под кнопки и IME.
				size := fd.fullscreenWindow.Canvas().Size()
				fd.ui.VideoWithKeyboard.Resize(size)
				fd.ui.VideoWithKeyboard.Refresh()
				
				// Дополнительно обновляем положение кнопки переключения
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
	if fd.originalContent != nil {
		fd.parent.SetContent(fd.originalContent)
		fd.parent.SetTitle(fd.originalTitle)
		logrus.Info("✅ Оригинальное содержимое восстановлено")
	}
}

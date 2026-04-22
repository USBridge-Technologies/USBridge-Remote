//go:build !android && !ios

package controller

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

func (fd *FullscreenDialog) platformInitWindow() {
	logrus.Info("🔍 Desktop: создаем новое окно для полноэкранного режима")
	fd.fullscreenWindow = fyne.CurrentApp().NewWindow("")
	fd.fullscreenWindow.SetTitle(i18n.Current.FullscreenWindowTitle)
	fd.fullscreenWindow.SetFullScreen(true)
	
	fd.fullscreenWindow.SetCloseIntercept(func() {
		logrus.Info("🔍 Перехвачена попытка закрытия окна - выход из полноэкранного режима")
		fd.exitFullscreen()
	})
	fd.fullscreenWindow.SetOnClosed(func() {
		logrus.Info("🔍 Окно полноэкранного режима закрыто")
	})
}

func (fd *FullscreenDialog) platformSetupUI() {
	// На десктопе обычно не требуется спец. обработки IME
}

func (fd *FullscreenDialog) platformShow() {
	fd.fullscreenWindow.Show()
	fd.fullscreenWindow.RequestFocus()
	
	// Фокусируемся на тачпаде для захвата клавиш
	if fd.touchpadWrapper != nil {
		fd.fullscreenWindow.Canvas().Focus(fd.touchpadWrapper)
	}
}

func (fd *FullscreenDialog) platformExit() {
	logrus.Info("🔍 Desktop: закрываем полноэкранное окно")
	if fd.fullscreenWindow != nil {
		fd.fullscreenWindow.Close()
		fd.fullscreenWindow = nil
	}
}

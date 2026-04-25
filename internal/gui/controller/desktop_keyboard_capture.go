package controller

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

var _ desktop.Keyable = (*desktopKeyboardCapture)(nil)

type desktopKeyboardCapture struct {
	widget.BaseWidget
	videoWidget *VideoWidget
	bg          *canvas.Rectangle
}

func newDesktopKeyboardCapture(videoWidget *VideoWidget) *desktopKeyboardCapture {
	capture := &desktopKeyboardCapture{
		videoWidget: videoWidget,
		bg:          canvas.NewRectangle(color.NRGBA{R: 0, G: 0, B: 0, A: 0}),
	}
	capture.ExtendBaseWidget(capture)
	return capture
}

func (c *desktopKeyboardCapture) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.bg)
}

func (c *desktopKeyboardCapture) FocusGained() {
	logrus.Infof("⌨️ [DESKTOP-CAPTURE][FOCUS] gained")
}

func (c *desktopKeyboardCapture) FocusLost() {
	logrus.Infof("⌨️ [DESKTOP-CAPTURE][FOCUS] lost")
}

func (c *desktopKeyboardCapture) TypedKey(event *fyne.KeyEvent) {
	logrus.Infof("⌨️ [DESKTOP-CAPTURE][PRESS] key=%q physical=%+v", event.Name, event.Physical)
	if event.Name == fyne.KeyF11 && c.videoWidget != nil && c.videoWidget.isStreaming {
		logrus.Info("⌨️ [DESKTOP-CAPTURE][PRESS] opening fullscreen")
		c.videoWidget.ShowFullscreen()
		return
	}
	c.videoWidget.handlePhysicalKeyPress(event)
}

func (c *desktopKeyboardCapture) TypedRune(r rune) {
	logrus.Infof("⌨️ [DESKTOP-CAPTURE][RUNE] rune=%q", string(r))
	c.videoWidget.handlePhysicalRunePress(r)
}

func (c *desktopKeyboardCapture) KeyDown(event *fyne.KeyEvent) {
	logrus.Infof("⌨️ [DESKTOP-CAPTURE][DOWN] key=%q physical=%+v", event.Name, event.Physical)
	c.videoWidget.handlePhysicalKeyDown(event)
}

func (c *desktopKeyboardCapture) KeyUp(event *fyne.KeyEvent) {
	logrus.Infof("⌨️ [DESKTOP-CAPTURE][UP] key=%q physical=%+v", event.Name, event.Physical)
	c.videoWidget.handlePhysicalKeyUp(event)
}

package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/config"
)

type Window struct {
	app fyne.App
	cfg config.Config
}

func NewWindow(app fyne.App, cfg config.Config) *Window {
	return &Window{app: app, cfg: cfg}
}

func (w *Window) ShowAndRun(onClose func()) {
	win := w.app.NewWindow("USBridge Agent")
	win.Resize(fyne.NewSize(760, 420))
	info := widget.NewLabel(fmt.Sprintf("HTTP: %s:%d\nFRP QUIC: %s:%d\nVideo UDP: %d\nCapture: %s\nFFmpeg: %s",
		w.cfg.ListenHost, w.cfg.HTTPPort,
		w.cfg.FRPBindHost, w.cfg.FRPBindPort,
		w.cfg.VideoUDPPort,
		w.cfg.VideoCapture,
		w.cfg.FFmpegPath,
	))
	notes := widget.NewRichTextFromMarkdown(`- Совместим с ` + "`usbridge_client`" + `
- HTTP API проксируется через FRP ` + "`http_srv`" + `
- Видео идёт через FRP ` + "`video_sudp`" + `
- NBD visitors поднимаются динамически
- HID ввод выполняется через Windows ` + "`SendInput`")
	notes.Wrapping = fyne.TextWrapWord
	win.SetContent(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("USBridge Agent", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		info,
		notes,
		widget.NewButton("Close", func() { win.Close() }),
	)))
	win.SetCloseIntercept(func() {
		if onClose != nil {
			onClose()
		}
		win.Close()
	})
	win.Show()
	w.app.Run()
}

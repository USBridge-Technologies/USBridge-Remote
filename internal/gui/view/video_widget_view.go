package view

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

type VideoWidgetUI struct {
	Container        *fyne.Container
	VideoCanvas      *canvas.Image
	Controls         *fyne.Container
	StartBtn         *widget.Button
	StopBtn          *widget.Button
	StatusLabel      *widget.Label
	InfoLabel        *widget.Label
	StatsLabel       *widget.Label
	ContentContainer *fyne.Container
}

func NewVideoWidgetUI(touchpad fyne.CanvasObject, onStart, onStop, onFullscreen func()) *VideoWidgetUI {
	videoCanvas := canvas.NewImageFromResource(nil)
	videoCanvas.FillMode = canvas.ImageFillContain

	statusLabel := widget.NewLabel(i18n.Current.VideoNotStarted)
	infoLabel := widget.NewLabel("")
	statsLabel := widget.NewLabel("")

	startBtn := widget.NewButton(i18n.Current.StartVideoButton, onStart)
	stopBtn := widget.NewButton(i18n.Current.StopVideoButton, onStop)
	fullscreenBtn := widget.NewButton(i18n.Current.FullscreenButton, onFullscreen)
	controls := container.NewHBox(startBtn, stopBtn, fullscreenBtn)

	contentContainer := container.NewWithoutLayout()
	contentContainer.Hide()

	videoContainer := container.NewMax(touchpad)
	mainContainer := container.NewBorder(
		container.NewHBox(statsLabel, layout.NewSpacer(), controls),
		contentContainer,
		nil,
		nil,
		videoContainer,
	)

	return &VideoWidgetUI{
		Container:        mainContainer,
		VideoCanvas:      videoCanvas,
		Controls:         controls,
		StartBtn:         startBtn,
		StopBtn:          stopBtn,
		StatusLabel:      statusLabel,
		InfoLabel:        infoLabel,
		StatsLabel:       statsLabel,
		ContentContainer: contentContainer,
	}
}

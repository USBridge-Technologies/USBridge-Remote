package view

import (
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type VideoWidgetUI struct {
	Container        *fyne.Container
	VideoCanvas      *canvas.Image
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

	contentContainer := container.NewWithoutLayout()
	contentContainer.Hide()

	videoContainer := container.NewMax(touchpad)
	mainContainer := container.NewBorder(nil, contentContainer, nil, nil, videoContainer)

	return &VideoWidgetUI{
		Container:        mainContainer,
		VideoCanvas:      videoCanvas,
		StatusLabel:      statusLabel,
		InfoLabel:        infoLabel,
		StatsLabel:       statsLabel,
		ContentContainer: contentContainer,
	}
}

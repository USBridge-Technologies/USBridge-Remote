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
	// SpinnerIcon/SpinnerOverlay: the Moonlight-style "connecting" spinner
	// shown centered over the video area between starting a session and
	// the first real frame arriving -- see video_widget_spinner.go, which
	// owns cycling SpinnerIcon.Resource through frames and
	// showing/hiding SpinnerOverlay. A plain Fyne canvas object stacked
	// into the same video container as the touchpad, so it renders
	// identically on every platform (desktop, mobile web, native
	// Android/iOS) without any platform-specific code -- native GPU video
	// compositing (Vulkan/Metal) only takes over once a stream is
	// actually live, well after this would already be hidden.
	SpinnerIcon    *canvas.Image
	SpinnerOverlay *fyne.Container
}

func NewVideoWidgetUI(touchpad fyne.CanvasObject, keyboardCapture fyne.CanvasObject, onStart, onStop, onFullscreen func()) *VideoWidgetUI {
	videoCanvas := canvas.NewImageFromResource(nil)
	videoCanvas.FillMode = canvas.ImageFillContain
	videoCanvas.ScaleMode = canvas.ImageScaleFastest

	statusLabel := widget.NewLabel(i18n.Current.VideoNotStarted)
	infoLabel := widget.NewLabel("")
	statsLabel := widget.NewLabel("")

	contentContainer := container.NewStack()
	contentContainer.Hide()

	spinnerIcon := canvas.NewImageFromResource(nil)
	spinnerIcon.FillMode = canvas.ImageFillContain
	spinnerIcon.SetMinSize(fyne.NewSize(56, 56))
	spinnerOverlay := container.NewCenter(spinnerIcon)
	spinnerOverlay.Hide()

	videoObjects := []fyne.CanvasObject{touchpad}
	if keyboardCapture != nil {
		videoObjects = append(videoObjects, keyboardCapture)
	}
	videoObjects = append(videoObjects, spinnerOverlay)
	videoContainer := container.NewMax(videoObjects...)
	mainContainer := container.NewBorder(nil, contentContainer, nil, nil, videoContainer)

	return &VideoWidgetUI{
		Container:        mainContainer,
		VideoCanvas:      videoCanvas,
		StatusLabel:      statusLabel,
		InfoLabel:        infoLabel,
		StatsLabel:       statsLabel,
		ContentContainer: contentContainer,
		SpinnerIcon:      spinnerIcon,
		SpinnerOverlay:   spinnerOverlay,
	}
}

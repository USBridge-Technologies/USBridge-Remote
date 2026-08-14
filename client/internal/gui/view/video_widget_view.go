package view

import (
	"usbridge-client/internal/gui/design"
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

	// videoBackground: a solid, always-present dark backdrop for the video
	// area. Before this existed, "dark background while connecting" was
	// just an accident of the DOM <video> element itself defaulting to
	// black even with no source -- true right up until the fix that
	// hides that element until it actually has decoded pixels
	// (video_widget_dom_overlay_wasm.go's videoWidth gate), which then
	// left nothing painting anything behind the spinner overlay below,
	// so whatever's behind the Fyne canvas (the page's own white
	// background) showed through instead. This rectangle is the video
	// area's real background now, independent of stream/DOM-overlay
	// state, matching the app's own dark theme (design.ColorBackground)
	// rather than depending on incidental browser default styling.
	videoBackground := canvas.NewRectangle(design.ColorBackground)

	// The spinner's own SVG frames (assets.VideoConnectingFrames /
	// VideoConnectingGearFrames) already bake in a soft, semi-transparent
	// dark backdrop disc behind the dots/gear shape -- see
	// spinnerBackdropSVG in assets/onboarding.go. That replaced an
	// earlier attempt to layer a separate canvas.Circle (sized via a
	// nil-resource canvas.Image MinSize hack) underneath the icon here,
	// which instead produced a stray opaque white square in the wasm
	// canvas backend. Sizing the icon up to 84x84 (vs. the underlying
	// 16x16 viewBox's icon-only content) is what makes that baked-in
	// backdrop actually read as a badge rather than a tight halo.
	spinnerIcon := canvas.NewImageFromResource(nil)
	spinnerIcon.FillMode = canvas.ImageFillContain
	spinnerIcon.SetMinSize(fyne.NewSize(84, 84))

	spinnerOverlay := container.NewCenter(spinnerIcon)
	spinnerOverlay.Hide()

	videoObjects := []fyne.CanvasObject{videoBackground, touchpad}
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

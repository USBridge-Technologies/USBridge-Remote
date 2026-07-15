package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

type FullscreenUI struct {
	VideoImage        *canvas.Image
	KeyboardLayout    *fyne.Container
	VideoWithKeyboard *fyne.Container
	MainContainer     *fyne.Container
}

func NewFullscreenUI(videoImage *canvas.Image, touchpad fyne.CanvasObject, keyboardLayout *fyne.Container) *FullscreenUI {
	videoContainer := container.NewStack(container.NewClip(touchpad))
	themedKeyboard := container.NewThemeOverride(keyboardLayout, design.NewBrandTheme())

	var mainContainer *fyne.Container
	if fyne.CurrentDevice().IsMobile() {
		bg := canvas.NewRectangle(color.Black)
		mainContentItems := []fyne.CanvasObject{bg, videoContainer}
		mainContentItems = append(mainContentItems, container.NewBorder(nil, themedKeyboard, nil, nil))
		mainContent := container.NewStack(mainContentItems...)
		mainContainer = mainContent

		return &FullscreenUI{
			VideoImage:        videoImage,
			KeyboardLayout:    keyboardLayout,
			VideoWithKeyboard: mainContent,
			MainContainer:     mainContainer,
		}
	}
	bg := canvas.NewRectangle(color.Black)
	mainContainer = container.NewStack(bg, videoContainer)
	return &FullscreenUI{
		VideoImage:        videoImage,
		VideoWithKeyboard: mainContainer,
		MainContainer:     mainContainer,
	}
}

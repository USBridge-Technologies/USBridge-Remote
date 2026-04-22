package view

import (
	"usbridge-client/internal/gui/assets"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type FullscreenUI struct {
	VideoImage        *canvas.Image
	KeyboardButton    *widget.Button
	KeyboardLayout    *fyne.Container
	VideoWithKeyboard *fyne.Container
	MainContainer     *fyne.Container
}

func NewFullscreenUI(videoImage *canvas.Image, touchpad fyne.CanvasObject, keyboardLayout *fyne.Container, onToggleKeyboard func()) *FullscreenUI {
	videoContainer := container.NewMax(touchpad)
	
	// Используем Border layout: видео в центре (растягивается), клавиатура снизу (занимает только нужную высоту)
	// Это гарантирует, что видео всегда будет ВЫШЕ кнопок и они не перекроются.
	videoWithKeyboard := container.NewBorder(nil, keyboardLayout, nil, nil, videoContainer)

	keyboardButton := widget.NewButtonWithIcon("", assets.KeyboardIconActive, onToggleKeyboard)
	keyboardButton.Importance = widget.MediumImportance

	// Оборачиваем кнопку в контейнер без лейаута для ручного позиционирования,
	// но саму структуру видео/клавиатуры держим в Stack для правильной отрисовки.
	mainContainer := container.NewStack(videoWithKeyboard, container.NewWithoutLayout(keyboardButton))

	return &FullscreenUI{
		VideoImage:        videoImage,
		KeyboardButton:    keyboardButton,
		KeyboardLayout:    keyboardLayout,
		VideoWithKeyboard: videoWithKeyboard,
		MainContainer:     mainContainer,
	}
}

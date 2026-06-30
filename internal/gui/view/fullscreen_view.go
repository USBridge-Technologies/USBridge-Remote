package view

import (
	"image/color"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

type FullscreenUI struct {
	VideoImage        *canvas.Image
	KeyboardButton    *widget.Button
	AudioButton       *widget.Button
	KeyboardLayout    *fyne.Container
	VideoWithKeyboard *fyne.Container
	MainContainer     *fyne.Container
}

func NewFullscreenUI(videoImage *canvas.Image, touchpad fyne.CanvasObject, keyboardLayout *fyne.Container, onToggleKeyboard func(), onToggleAudio func()) *FullscreenUI {
	// Видео теперь всегда занимает всё доступное пространство окна.
	// Обрезаем его по размеру контейнера.
	videoContainer := container.NewStack(container.NewClip(touchpad))

	// Оборачиваем keyboardLayout в ThemeOverride, чтобы кнопки гарантированно были в темной теме
	themedKeyboard := container.NewThemeOverride(keyboardLayout, design.NewBrandTheme())

	bg := canvas.NewRectangle(color.Black)

	// Используем Stack, чтобы кнопки ГАРАНТИРОВАННО были поверх видео.
	mainContentItems := []fyne.CanvasObject{bg, videoContainer}
	if fyne.CurrentDevice().IsMobile() {
		mainContentItems = append(mainContentItems, container.NewBorder(nil, themedKeyboard, nil, nil))
	}
	mainContent := container.NewStack(mainContentItems...)

	keyboardButton := widget.NewButtonWithIcon("", assets.KeyboardIconActive, onToggleKeyboard)
	keyboardButton.Importance = widget.MediumImportance
	themedKeyboardButton := container.NewThemeOverride(keyboardButton, design.NewBrandTheme())

	audioButton := widget.NewButtonWithIcon("", assets.AudioIcon, onToggleAudio)
	audioButton.Importance = widget.MediumImportance
	themedAudioButton := container.NewThemeOverride(audioButton, design.NewBrandTheme())

	var mainContainer *fyne.Container
	if fyne.CurrentDevice().IsMobile() {
		// Place buttons side by side at top-left (both in a WithoutLayout, positioned manually)
		overlayContainer := container.NewWithoutLayout(themedKeyboardButton, themedAudioButton)
		mainContainer = container.NewStack(mainContent, overlayContainer)
	} else {
		mainContainer = container.NewStack(mainContent)
	}

	return &FullscreenUI{
		VideoImage:        videoImage,
		KeyboardButton:    keyboardButton,
		AudioButton:       audioButton,
		KeyboardLayout:    keyboardLayout,
		VideoWithKeyboard: mainContent,
		MainContainer:     mainContainer,
	}
}

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
	KeyboardLayout    *fyne.Container
	VideoWithKeyboard *fyne.Container
	MainContainer     *fyne.Container
}

func NewFullscreenUI(videoImage *canvas.Image, touchpad fyne.CanvasObject, keyboardLayout *fyne.Container, onToggleKeyboard func()) *FullscreenUI {
	// Видео теперь всегда занимает всё доступное пространство окна.
	// Обрезаем его по размеру контейнера.
	videoContainer := container.NewStack(container.NewClip(touchpad))
	
	// Оборачиваем keyboardLayout в ThemeOverride, чтобы кнопки гарантированно были в темной теме
	themedKeyboard := container.NewThemeOverride(keyboardLayout, design.NewBrandTheme())

	bg := canvas.NewRectangle(color.Black)

	// Используем Stack, чтобы кнопки ГАРАНТИРОВАННО были поверх видео.
	// Мы используем BorderLayout только для того, чтобы прижать keyboardLayout к низу,
	// но сам этот контейнер не ограничивает видео.
	mainContent := container.NewStack(
		bg,
		videoContainer,
		container.NewBorder(nil, themedKeyboard, nil, nil),
	)

	keyboardButton := widget.NewButtonWithIcon("", assets.KeyboardIconActive, onToggleKeyboard)
	keyboardButton.Importance = widget.MediumImportance

	mainContainer := container.NewStack(mainContent, container.NewWithoutLayout(keyboardButton))

	return &FullscreenUI{
		VideoImage:        videoImage,
		KeyboardButton:    keyboardButton,
		KeyboardLayout:    keyboardLayout,
		VideoWithKeyboard: mainContent,
		MainContainer:     mainContainer,
	}
}

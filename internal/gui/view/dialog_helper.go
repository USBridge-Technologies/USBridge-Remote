// Package view содержит Fyne-компоненты и хелперы для интерфейса.
package view

import (
	"sync"

	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// minWidthLayout задаёт минимальную ширину контента (для мобильных в портрете)
type minWidthLayout struct {
	minWidth float32
}

func (m *minWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

func (m *minWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(0, 0)
	for _, o := range objects {
		childMin := o.MinSize()
		if childMin.Width > min.Width {
			min.Width = childMin.Width
		}
		if childMin.Height > min.Height {
			min.Height = childMin.Height
		}
	}
	if m.minWidth > 0 && min.Width < m.minWidth {
		min.Width = m.minWidth
	}
	return min
}

// ShowConfirmYesLeft показывает диалог подтверждения с кнопкой Yes слева, No справа.
// Использует локализованные строки i18n.Current.Yes и i18n.Current.No.
// На мобильных в вертикальной ориентации диалог шире (как edit connection).
func ShowConfirmYesLeft(title, message string, callback func(bool), parent fyne.Window) {
	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	var d dialog.Dialog
	var once sync.Once
	invokeCallback := func(ok bool) {
		once.Do(func() {
			if callback != nil {
				callback(ok)
			}
		})
	}

	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		invokeCallback(true)
		if d != nil {
			d.Hide()
		}
	})
	yesBtn.Importance = widget.HighImportance
	yesBtn.SetIcon(theme.ConfirmIcon())

	noBtn := widget.NewButton(i18n.Current.No, func() {
		invokeCallback(false)
		if d != nil {
			d.Hide()
		}
	})
	noBtn.SetIcon(theme.CancelIcon())

	// Yes слева, No справа
	buttons := container.NewGridWithColumns(2, yesBtn, noBtn)
	inner := container.NewVBox(
		label,
		widget.NewSeparator(),
		buttons,
	)

	// Минимальная ширина: на мобильных — 85% экрана (min 280), на десктопе — 400px
	var content fyne.CanvasObject = inner
	if parent != nil {
		var minW float32 = 400 // десктоп — нормальная ширина
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		if minW > 0 {
			content = container.New(&minWidthLayout{minWidth: minW}, inner)
		}
	}

	d = dialog.NewCustomWithoutButtons(title, content, parent)
	d.SetOnClosed(func() {
		invokeCallback(false)
	})
	d.Show()
}

// ShowConfirmYesLeftDanger — то же, что ShowConfirmYesLeft, но кнопка «Да» красная (DangerImportance).
// Используется для подтверждения питания и перезагрузки.
func ShowConfirmYesLeftDanger(title, message string, callback func(bool), parent fyne.Window) {
	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	var d dialog.Dialog
	var once sync.Once
	invokeCallback := func(ok bool) {
		once.Do(func() {
			if callback != nil {
				callback(ok)
			}
		})
	}

	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		invokeCallback(true)
		if d != nil {
			d.Hide()
		}
	})
	yesBtn.Importance = widget.DangerImportance
	yesBtn.SetIcon(theme.ConfirmIcon())

	noBtn := widget.NewButton(i18n.Current.No, func() {
		invokeCallback(false)
		if d != nil {
			d.Hide()
		}
	})
	noBtn.SetIcon(theme.CancelIcon())

	buttons := container.NewGridWithColumns(2, yesBtn, noBtn)
	inner := container.NewVBox(
		label,
		widget.NewSeparator(),
		buttons,
	)

	var content fyne.CanvasObject = inner
	if parent != nil {
		var minW float32 = 400
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		if minW > 0 {
			content = container.New(&minWidthLayout{minWidth: minW}, inner)
		}
	}

	d = dialog.NewCustomWithoutButtons(title, content, parent)
	d.SetOnClosed(func() {
		invokeCallback(false)
	})
	d.Show()
}

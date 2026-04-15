// Package view содержит Fyne-компоненты и хелперы для интерфейса.
package view

import (
	"image/color"
	"strings"
	"sync"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
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

type confirmDialogTitleLayout struct{}

type confirmDialogButtonsLayout struct {
	gap float32
}

func (l *confirmDialogTitleLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	title := objects[0]
	closeBtn := objects[1]
	titleMin := title.MinSize()
	closeMin := closeBtn.MinSize()

	title.Move(fyne.NewPos(maxFloat32(0, (size.Width-titleMin.Width)/2), maxFloat32(0, (size.Height-titleMin.Height)/2)))
	title.Resize(titleMin)

	closeBtn.Move(fyne.NewPos(maxFloat32(0, size.Width-closeMin.Width), maxFloat32(0, (size.Height-closeMin.Height)/2)))
	closeBtn.Resize(closeMin)
}

func (l *confirmDialogTitleLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	titleMin := objects[0].MinSize()
	closeMin := objects[1].MinSize()
	width := maxFloat32(titleMin.Width, closeMin.Width*2) + closeMin.Width
	height := maxFloat32(titleMin.Height, closeMin.Height)
	return fyne.NewSize(width, height)
}

func (l *confirmDialogButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	left := objects[0]
	right := objects[1]
	leftWidth := (size.Width - l.gap) / 2
	if leftWidth < 0 {
		leftWidth = 0
	}
	rightWidth := size.Width - leftWidth - l.gap
	if rightWidth < 0 {
		rightWidth = 0
	}

	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(leftWidth, size.Height))

	right.Move(fyne.NewPos(leftWidth+l.gap, 0))
	right.Resize(fyne.NewSize(rightWidth, size.Height))
}

func (l *confirmDialogButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	leftMin := objects[0].MinSize()
	rightMin := objects[1].MinSize()
	height := maxFloat32(leftMin.Height, rightMin.Height)
	return fyne.NewSize(leftMin.Width+rightMin.Width+l.gap, height)
}

func newConfirmDialogCloseButton(onTapped func()) *widget.Button {
	btn := widget.NewButtonWithIcon("", theme.CancelIcon(), onTapped)
	btn.Importance = widget.LowImportance
	return btn
}

func showConfirmDialog(title, message string, callback func(bool), parent fyne.Window, danger bool) {
	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	var popup *widget.PopUp
	var once sync.Once
	invokeCallback := func(ok bool) {
		once.Do(func() {
			if callback != nil {
				callback(ok)
			}
		})
	}
	closePopup := func(ok bool) {
		invokeCallback(ok)
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := NewBrandText(title, 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter

	closeBtn := newConfirmDialogCloseButton(func() {
		closePopup(false)
	})
	titleBar := container.New(&confirmDialogTitleLayout{}, titleText, closeBtn)

	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		closePopup(true)
	})
	if danger {
		yesBtn.Importance = widget.DangerImportance
	} else {
		yesBtn.Importance = widget.HighImportance
	}

	noBtn := widget.NewButton(i18n.Current.No, func() {
		closePopup(false)
	})

	buttons := container.New(&confirmDialogButtonsLayout{gap: 12}, noBtn, yesBtn)
	body := container.NewVBox(
		titleBar,
		NewInset(label, 0, 0, 16, 14),
		buttons,
	)

	panelContent := body
	if parent != nil {
		var minW float32 = 408
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		panelContent = container.New(&minWidthLayout{minWidth: minW}, body)
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		NewInset(panelContent, 18, 18, 16, 16),
		border,
	)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 408), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
}

// ShowConfirmYesLeft показывает диалог подтверждения с кнопкой Yes слева, No справа.
// Использует локализованные строки i18n.Current.Yes и i18n.Current.No.
// На мобильных в вертикальной ориентации диалог шире (как edit connection).
func ShowConfirmYesLeft(title, message string, callback func(bool), parent fyne.Window) {
	showConfirmDialog(title, message, callback, parent, false)
}

// ShowConfirmYesLeftDanger — то же, что ShowConfirmYesLeft, но кнопка «Да» красная (DangerImportance).
// Используется для подтверждения питания и перезагрузки.
func ShowConfirmYesLeftDanger(title, message string, callback func(bool), parent fyne.Window) {
	showConfirmDialog(title, message, callback, parent, true)
}

func ShowErrorDialog(err error, parent fyne.Window) {
	if err == nil {
		return
	}

	message := err.Error()

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := NewBrandText(i18n.Current.Error, 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter

	closeBtn := newConfirmDialogCloseButton(closePopup)
	titleBar := container.New(&confirmDialogTitleLayout{}, titleText, closeBtn)

	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	okBtn := widget.NewButton(i18n.Current.OK, closePopup)
	copyBtn := widget.NewButton(i18n.Current.Copy, func() {
		if parent != nil && parent.Clipboard() != nil {
			parent.Clipboard().SetContent(message)
		}
	})

	buttons := container.New(&confirmDialogButtonsLayout{gap: 12}, copyBtn, okBtn)
	body := container.NewVBox(
		titleBar,
		NewInset(label, 0, 0, 16, 14),
		buttons,
	)

	panelContent := fyne.CanvasObject(body)
	if parent != nil {
		var minW float32 = 408
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		panelContent = container.New(&minWidthLayout{minWidth: minW}, body)
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		NewInset(panelContent, 18, 18, 16, 16),
		border,
	)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 408), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
}

func ShowInfoDialog(title, message string, parent fyne.Window) {
	if strings.TrimSpace(message) == "" {
		return
	}

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := NewBrandText(title, 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter

	closeBtn := newConfirmDialogCloseButton(closePopup)
	titleBar := container.New(&confirmDialogTitleLayout{}, titleText, closeBtn)

	label := widget.NewLabel(message)
	label.Wrapping = fyne.TextWrapWord

	okBtn := widget.NewButton(i18n.Current.OK, closePopup)
	okBtn.Importance = widget.MediumImportance
	buttons := container.NewCenter(container.NewGridWrap(fyne.NewSize(260, okBtn.MinSize().Height), okBtn))
	body := container.NewVBox(
		titleBar,
		NewInset(label, 0, 0, 16, 14),
		buttons,
	)

	panelContent := fyne.CanvasObject(body)
	if parent != nil {
		var minW float32 = 408
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		panelContent = container.New(&minWidthLayout{minWidth: minW}, body)
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		NewInset(panelContent, 18, 18, 16, 16),
		border,
	)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 408), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
}

func ShowBusyDialog(title, message string, parent fyne.Window) *widget.PopUp {
	if parent == nil {
		return nil
	}

	titleText := NewBrandText(title, 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter

	label := widget.NewLabel(message)
	label.Alignment = fyne.TextAlignCenter
	label.Wrapping = fyne.TextWrapWord

	body := container.NewVBox(
		titleText,
		NewInset(label, 0, 0, 14, 0),
	)

	panelContent := fyne.CanvasObject(body)
	var minW float32 = 408
	if fyne.CurrentDevice().IsMobile() {
		canvasSize := parent.Canvas().Size()
		minW = canvasSize.Width * 0.85
		if minW < 280 {
			minW = 280
		}
	}
	panelContent = container.New(&minWidthLayout{minWidth: minW}, body)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		NewInset(panelContent, 18, 18, 16, 16),
		border,
	)

	return ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 408), maxWidth)
			return fyne.NewSize(panelWidth, panelMin.Height)
		},
	})
}

func ShowDeleteImageConfirm(fileName string, callback func(bool), parent fyne.Window) {
	var popup *widget.PopUp
	var once sync.Once
	invokeCallback := func(ok bool) {
		once.Do(func() {
			if callback != nil {
				callback(ok)
			}
		})
	}
	closePopup := func(ok bool) {
		invokeCallback(ok)
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := NewBrandText("Delete image?", 19, design.ColorTextLight, true)
	closeBtn := newConfirmDialogCloseButton(func() {
		closePopup(false)
	})
	titleBar := container.New(&confirmDialogTitleLayout{}, titleText, closeBtn)

	leadText := widget.NewLabel("Are you sure you want to delete:")
	leadText.Wrapping = fyne.TextWrapWord

	fileLabel := widget.NewLabel(fileName + "?")
	fileLabel.Wrapping = fyne.TextWrapWord
	fileLabel.TextStyle = fyne.TextStyle{Bold: true}

	warnIcon := canvas.NewImageFromResource(assets.WarningTriangleIcon)
	warnIcon.FillMode = canvas.ImageFillContain
	warnIcon.SetMinSize(fyne.NewSize(18, 18))

	warnText := canvas.NewText("This action cannot be undone.", design.ColorTextLight)
	warnText.TextSize = 13
	warnText.TextStyle = fyne.TextStyle{Italic: true}

	warningRow := container.NewHBox(
		warnIcon,
		NewInset(warnText, 8, 0, 0, 0),
	)

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		closePopup(false)
	})

	deleteBtn := widget.NewButton("Delete", func() {
		closePopup(true)
	})
	deleteBtn.Importance = widget.DangerImportance

	buttons := container.New(&confirmDialogButtonsLayout{gap: 12}, cancelBtn, deleteBtn)
	body := container.NewVBox(
		titleBar,
		NewInset(leadText, 0, 0, 14, 0),
		NewInset(fileLabel, 0, 0, 0, 16),
		NewInset(warningRow, 4, 0, 0, 18),
		buttons,
	)

	panelContent := body
	if parent != nil {
		var minW float32 = 408
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		panelContent = container.New(&minWidthLayout{minWidth: minW}, body)
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		NewInset(panelContent, 18, 18, 16, 16),
		border,
	)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 408), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
}

func ShowUploadImageConfirm(fileName string, callback func(bool), parent fyne.Window) {
	var popup *widget.PopUp
	var once sync.Once
	invokeCallback := func(ok bool) {
		once.Do(func() {
			if callback != nil {
				callback(ok)
			}
		})
	}
	closePopup := func(ok bool) {
		invokeCallback(ok)
		if popup != nil {
			popup.Hide()
		}
	}

	titleText := NewBrandText("Upload image?", 19, design.ColorTextLight, true)
	closeBtn := newConfirmDialogCloseButton(func() {
		closePopup(false)
	})
	titleBar := container.New(&confirmDialogTitleLayout{}, titleText, closeBtn)

	leadText := widget.NewLabel("Do you want to upload this file to the device?")
	leadText.Wrapping = fyne.TextWrapWord

	fileLabel := widget.NewLabel(fileName)
	fileLabel.Wrapping = fyne.TextWrapWord
	fileLabel.TextStyle = fyne.TextStyle{Bold: true}

	infoIcon := canvas.NewImageFromResource(assets.WarningInfoIcon)
	infoIcon.FillMode = canvas.ImageFillContain
	infoIcon.SetMinSize(fyne.NewSize(18, 18))

	infoText := canvas.NewText("This process may take some time.", design.ColorTextLight)
	infoText.TextSize = 13
	infoText.TextStyle = fyne.TextStyle{Italic: true}

	infoRow := container.NewHBox(
		infoIcon,
		NewInset(infoText, 8, 0, 0, 0),
	)

	cancelBtn := widget.NewButton(i18n.Current.Cancel, func() {
		closePopup(false)
	})

	uploadBtn := widget.NewButton("Upload", func() {
		closePopup(true)
	})
	uploadBtn.Importance = widget.HighImportance

	buttons := container.New(&confirmDialogButtonsLayout{gap: 12}, cancelBtn, uploadBtn)
	body := container.NewVBox(
		titleBar,
		NewInset(leadText, 0, 0, 14, 0),
		NewInset(fileLabel, 0, 0, 0, 16),
		NewInset(infoRow, 4, 0, 0, 18),
		buttons,
	)

	panelContent := body
	if parent != nil {
		var minW float32 = 408
		if fyne.CurrentDevice().IsMobile() {
			canvasSize := parent.Canvas().Size()
			minW = canvasSize.Width * 0.85
			if minW < 280 {
				minW = 280
			}
		}
		panelContent = container.New(&minWidthLayout{minWidth: minW}, body)
	}

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		NewInset(panelContent, 18, 18, 16, 16),
		border,
	)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
			maxWidth := canvasSize.Width - margin*2
			maxHeight := canvasSize.Height - margin*2
			if maxWidth <= 0 {
				maxWidth = canvasSize.Width
			}
			if maxHeight <= 0 {
				maxHeight = canvasSize.Height
			}

			panelMin := panel.MinSize()
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 408), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
}

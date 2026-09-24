package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// showErrorDialog displays a unified branded error modal: dark card, header
// with "Error" title and close 'X' button, word-wrapped error message, and
// NO footer. Clicking 'X' or outside dismisses the dialog. Safe to call
// from any goroutine.
func showErrorDialog(err error, parent fyne.Window) {
	if err == nil || parent == nil {
		return
	}
	fyne.Do(func() {
		if parent == nil {
			return
		}
		var popup *widget.PopUp
		closeDialog := func() {
			if popup != nil {
				popup.Hide()
			}
		}

		title := loc().ErrorTitle
		if title == "" {
			title = "Error"
		}

		msg := widget.NewLabel(err.Error())
		msg.Wrapping = fyne.TextWrapWord
		msg.Alignment = fyne.TextAlignLeading
		body := wrapDialogLabel(msg, 11, design.ColorTextLight)

		// nil footer produces a card with header and body only (no footer separator/buttons)
		panel := newBrandedDialogPanelInsets(title, statusDialogWidth, 20, 10, body, nil, closeDialog)
		popup = showOverlayPopup(parent, overlayPopupSpec{
			Panel:        panel,
			OnOutsideTap: closeDialog,
		})
	})
}

// showInfoDialog displays a unified branded info modal: dark card, header
// with the given title and close 'X' button, word-wrapped body message, and
// NO footer. Clicking 'X' or outside dismisses the dialog. Safe to call
// from any goroutine.
func showInfoDialog(title, message string, parent fyne.Window) {
	if parent == nil {
		return
	}
	fyne.Do(func() {
		if parent == nil {
			return
		}
		var popup *widget.PopUp
		closeDialog := func() {
			if popup != nil {
				popup.Hide()
			}
		}

		msg := widget.NewLabel(message)
		msg.Wrapping = fyne.TextWrapWord
		msg.Alignment = fyne.TextAlignLeading
		body := wrapDialogLabel(msg, 11, design.ColorTextLight)

		panel := newBrandedDialogPanelInsets(title, statusDialogWidth, 20, 10, body, nil, closeDialog)
		popup = showOverlayPopup(parent, overlayPopupSpec{
			Panel:        panel,
			OnOutsideTap: closeDialog,
		})
	})
}

// showConfirmDialog displays a branded confirmation modal: header with title
// and close 'X' button, message body, and centered Yes/No buttons in the footer.
// Safe to call from any goroutine.
func showConfirmDialog(title, message string, onResult func(bool), parent fyne.Window) {
	if parent == nil {
		return
	}
	fyne.Do(func() {
		if parent == nil {
			return
		}
		var popup *widget.PopUp
		closeDialog := func(result bool) {
			if popup != nil {
				popup.Hide()
			}
			if onResult != nil {
				onResult(result)
			}
		}

		msg := widget.NewLabel(message)
		msg.Wrapping = fyne.TextWrapWord
		msg.Alignment = fyne.TextAlignLeading
		body := wrapDialogLabel(msg, 11, design.ColorTextLight)

		noBtn := newIconActionButton(loc().No, nil, func() {
			closeDialog(false)
		})
		noBtn.Compact = true

		yesBtn := newDialogCTA(loc().Yes, func() {
			closeDialog(true)
		})

		minW := float32(70)
		lockNo := canvas.NewRectangle(color.Transparent)
		lockNo.SetMinSize(fyne.NewSize(minW, 1))
		lockYes := canvas.NewRectangle(color.Transparent)
		lockYes.SetMinSize(fyne.NewSize(minW, 1))

		footer := container.NewCenter(container.New(&tightHBoxLayout{gap: 12},
			container.NewStack(lockNo, noBtn),
			container.NewStack(lockYes, yesBtn),
		))

		panel := newBrandedDialogPanelInsets(title, statusDialogWidth, 20, 10, body, footer, func() {
			closeDialog(false)
		})
		popup = showOverlayPopup(parent, overlayPopupSpec{
			Panel:        panel,
			OnOutsideTap: func() { closeDialog(false) },
		})
	})
}

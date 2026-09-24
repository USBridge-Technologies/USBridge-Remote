package ui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// showUSBBrokerDialog displays the branded confirmation modal when clicking
// on USB Broker, styled identically to the Autostart at Boot info dialog
// with Yes/No actions in the footer.
func (w *Window) showUSBBrokerDialog(parent fyne.Window, onResult func(bool)) {
	showUSBBrokerDialog(parent, onResult)
}

// showUSBBrokerDialog displays the modal popup with the application design.
func showUSBBrokerDialog(parent fyne.Window, onResult func(bool)) {
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	title := loc().USBBrokerConsentTitle
	if title == "" {
		title = "Enable USB passthrough?"
	}

	bodyText := loc().USBBrokerConsentBody
	if bodyText == "" {
		bodyText = "USB passthrough is powered by a separate, closed-source component (not open-source like the rest of this agent). It stays off until you enable it here. Once enabled, keyboard, mouse, and gamepad passthrough is free; other USB devices (drives, audio, tablets, etc.) require a Pro or Enterprise subscription."
	}

	msg := widget.NewLabel(bodyText)
	msg.Wrapping = fyne.TextWrapWord
	msg.Alignment = fyne.TextAlignLeading
	body := wrapDialogLabel(msg, 11, design.ColorTextLight)

	noBtn := newIconActionButton(loc().No, nil, func() {
		closeDialog()
		if onResult != nil {
			onResult(false)
		}
	})
	noBtn.Compact = true

	yesBtn := newDialogCTA(loc().Yes, func() {
		closeDialog()
		if onResult != nil {
			onResult(true)
		}
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

	panel := newBrandedDialogPanelInsets(title, statusDialogWidth, 20, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel:        panel,
		OnOutsideTap: closeDialog,
	})
}

// tappableBox is a simple container wrapper that intercepts taps and displays a pointer cursor.
type tappableBox struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTappableBox(content fyne.CanvasObject, onTap func()) *tappableBox {
	b := &tappableBox{content: content, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *tappableBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}

func (b *tappableBox) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *tappableBox) TappedSecondary(*fyne.PointEvent) {}

func (b *tappableBox) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

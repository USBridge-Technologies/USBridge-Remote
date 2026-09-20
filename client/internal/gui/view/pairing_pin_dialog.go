package view

import (
	"image/color"
	"strings"
	"sync/atomic"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

const pairingPINDialogWidth = float32(360)

// PairingPINDialog is the Sunshine/GameStream manual-pairing overlay —
// same chrome as Video Parameters / Account (accent bar, close X, teal
// wait bar), not Fyne's stock light dialog. Hide() dismisses without
// treating it as a user cancel (pairing finished or failed on its own);
// the close X / Cancel / dim tap call onCancel instead.
type PairingPINDialog struct {
	popup     *widget.PopUp
	onCancel  func()
	dismissed atomic.Bool
}

// Hide dismisses the overlay if it is still showing. Safe to call twice.
func (d *PairingPINDialog) Hide() {
	if d == nil || !d.dismissed.CompareAndSwap(false, true) {
		return
	}
	if d.popup != nil {
		d.popup.Hide()
	}
}

func (d *PairingPINDialog) userCancel() {
	if d == nil || !d.dismissed.CompareAndSwap(false, true) {
		return
	}
	if d.popup != nil {
		d.popup.Hide()
	}
	if d.onCancel != nil {
		d.onCancel()
	}
}

// ShowPairingPINDialog shows the PIN the human must type on the host's
// pairing page. onCancel runs if they close the card (X, Cancel, or tap
// the dim) so the in-flight Pair() request can be aborted.
func ShowPairingPINDialog(parent fyne.Window, pin string, onCancel func()) *PairingPINDialog {
	if parent == nil {
		return nil
	}
	d := &PairingPINDialog{onCancel: onCancel}

	title := NewBrandText(i18n.Current.PairingPINTitle, 13, design.ColorTextLight, true)
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   d.userCancel,
	})

	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	footerSep := canvas.NewRectangle(headerSepLine)
	footerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 44, 9, 4), headerSep)

	msg := newVideoDialogWrapText(
		pairingPINDialogWidth-52,
		11,
		false,
		videoDialogWrapSpan{Text: i18n.Current.PairingPINMessage, Color: videoDialogHintColor},
	)

	pinText := canvas.NewText(pin, design.ColorConnectionAddFill)
	pinText.TextSize = 36
	pinText.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	pinText.Alignment = fyne.TextAlignCenter

	prog := widget.NewProgressBarInfinite()
	styledProg := container.NewThemeOverride(prog, &pairingProgressTheme{Theme: theme.DefaultTheme()})
	progBar := NewFixedHeight(styledProg, 5)

	waiting := canvas.NewText(i18n.Current.PairingPINWaiting, videoDialogHintColor)
	waiting.TextSize = 11
	waiting.Alignment = fyne.TextAlignCenter

	body := container.NewVBox(
		msg,
		videoDialogVSpace(16),
		container.NewCenter(pinText),
		videoDialogVSpace(16),
		progBar,
		videoDialogVSpace(10),
		container.NewCenter(waiting),
	)

	cancelBtn := newVideoDialogCancelButton(i18n.Current.Cancel, d.userCancel)
	footerButtons := container.NewBorder(nil, nil, container.NewCenter(cancelBtn), nil, nil)
	footerBlock := container.NewVBox(footerSep, NewInsetExact(footerButtons, 12, 18, 6, 0))

	form := container.NewBorder(headerBlock, footerBlock, nil, nil, NewInset(body, 18, 18, 14, 8))
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 8), cornerBtn, border)

	d.popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:        panel,
		DimColor:     color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		OnOutsideTap: d.userCancel,
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
			w := minFloat32(maxFloat32(panelMin.Width, pairingPINDialogWidth), maxWidth)
			h := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(w, h)
		},
	})
	return d
}

// pairingProgressTheme is the same teal 5px infinite bar the Account
// dialog uses while waiting on Google login.
type pairingProgressTheme struct {
	fyne.Theme
}

func (t *pairingProgressTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0xff}
	}
	if name == theme.ColorNameInputBackground || name == theme.ColorNameButton || name == theme.ColorNameScrollBarBackground {
		return color.NRGBA{R: 0x2e, G: 0x9e, B: 0x8a, A: 0xff}
	}
	return t.Theme.Color(name, v)
}

func (t *pairingProgressTheme) Size(name fyne.ThemeSizeName) float32 {
	if strings.HasSuffix(string(name), "Radius") {
		return 3
	}
	return t.Theme.Size(name)
}

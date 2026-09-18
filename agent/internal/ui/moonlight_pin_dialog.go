package ui

import (
	"image/color"
	"log"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const moonlightPINDialogWidth float32 = 340

func (w *Window) showMoonlightPINDialog(parent fyne.Window) {
	if w.token == nil || parent == nil {
		return
	}

	entry := widget.NewEntry()
	entry.SetPlaceHolder(loc().PINPlaceholder)

	status := canvas.NewText("", design.ColorAlert)
	status.TextSize = 11
	status.Hide()

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	var submit *iconActionButton
	runSubmit := func() {
		if submit == nil || submit.Disabled() {
			return
		}
		pin := strings.TrimSpace(entry.Text)
		if pin == "" {
			status.Text = loc().EnterPINShown
			status.Show()
			status.Refresh()
			return
		}
		status.Hide()
		submit.Disable()
		go func() {
			err := w.token.SubmitMoonlightPIN(pin)
			fyne.Do(func() {
				if err != nil {
					status.Text = err.Error()
					status.Show()
					status.Refresh()
					submit.Enable()
					return
				}
				closeDialog()
			})
		}()
	}
	submit = newIconActionButton(loc().Submit, nil, runSubmit)
	submit.CTA = true
	submit.Compact = true
	entry.OnSubmitted = func(string) { runSubmit() }

	hint := widget.NewLabel(loc().MoonlightPINHint)
	hint.Wrapping = fyne.TextWrapWord
	hint.Alignment = fyne.TextAlignLeading

	body := container.NewVBox(
		wrapPINHint(hint),
		spacerSize(1, 6),
		wrapPINField(entry),
		status,
	)
	footer := container.NewCenter(submit)
	panel := newBrandedDialogPanelInsets(loc().PairMoonlight, moonlightPINDialogWidth, 24, 6, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func (w *Window) showMoonlightClientsDialog(parent fyne.Window) {
	if w.token == nil || parent == nil {
		return
	}

	listBox := container.NewVBox()
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	var refreshList func()
	refreshList = func() {
		go func() {
			clients, err := w.token.ListSunshineClients()
			fyne.Do(func() {
				listBox.RemoveAll()
				if err != nil {
					errText := canvas.NewText(err.Error(), design.ColorAlert)
					errText.TextSize = 11
					listBox.Add(errText)
					listBox.Refresh()
					return
				}
				if len(clients) == 0 {
					empty := canvas.NewText(loc().NoPairedClients, design.ColorEmptyHint)
					empty.TextSize = 12
					empty.Alignment = fyne.TextAlignCenter
					listBox.Add(container.NewCenter(empty))
				} else {
					for _, c := range clients {
						c := c
						displayName := strings.TrimSpace(c.Name)
						if displayName == "" {
							displayName = c.UniqueID
						}
						nameLabel := widget.NewLabel(displayName)
						nameLabel.Truncation = fyne.TextTruncateEllipsis
						removeBtn := newDangerGlyphButton(func() {
							go func() {
								if err := w.token.UnpairSunshineClient(c.UniqueID); err != nil {
									log.Printf("[ui] unpair moonlight client: %v", err)
								} else {
									_ = w.token.RestartSunshine()
								}
								refreshList()
							}()
						})
						row := container.NewBorder(nil, nil, nil, removeBtn,
							wrapDialogLabel(nameLabel, 12, design.ColorTextLight))
						listBox.Add(row)
					}
				}
				listBox.Refresh()
			})
		}()
	}

	panel := newBrandedDialogPanelInsets(loc().MoonlightClients, moonlightPINDialogWidth, 24, 8, listBox, nil, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
	refreshList()
}

func wrapDialogLabel(lbl *widget.Label, size float32, col color.Color) fyne.CanvasObject {
	return container.NewThemeOverride(lbl, &dialogLabelTheme{Theme: design.NewBrandTheme(), textSize: size, textColor: col})
}

type dialogLabelTheme struct {
	fyne.Theme
	textSize  float32
	textColor color.Color
}

func (t *dialogLabelTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground && t.textColor != nil {
		return t.textColor
	}
	return t.Theme.Color(name, v)
}

func (t *dialogLabelTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding, theme.SizeNameInnerPadding, theme.SizeNameLineSpacing:
		return 0
	case theme.SizeNameText:
		return t.textSize
	}
	return t.Theme.Size(name)
}

func wrapPINHint(lbl *widget.Label) fyne.CanvasObject {
	return container.NewThemeOverride(lbl, &pinHintTheme{Theme: design.NewBrandTheme()})
}

type pinHintTheme struct {
	fyne.Theme
}

func (t *pinHintTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return design.ColorMutedOlive
	}
	return t.Theme.Color(name, v)
}

func (t *pinHintTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding, theme.SizeNameInnerPadding, theme.SizeNameLineSpacing:
		return 0
	case theme.SizeNameText:
		return 12
	}
	return t.Theme.Size(name)
}

func wrapPINField(entry *widget.Entry) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	themed := container.NewThemeOverride(entry, &pinFieldTheme{Theme: design.NewBrandTheme()})
	return container.NewStack(bg, newExactInset(themed, 8, 8, 4, 4))
}

type pinFieldTheme struct {
	fyne.Theme
}

func (t *pinFieldTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground, theme.ColorNameDisabledButton:
		return color.Transparent
	case theme.ColorNameInputBorder, theme.ColorNameFocus, theme.ColorNamePrimary:
		return color.Transparent
	case theme.ColorNamePlaceHolder:
		return design.ColorEmptyHint
	case theme.ColorNameForeground:
		return design.ColorTextLight
	}
	return t.Theme.Color(name, v)
}

func (t *pinFieldTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding, theme.SizeNameInnerPadding:
		return 0
	case theme.SizeNameText:
		return 9
	case theme.SizeNameCaptionText:
		return 9
	}
	return t.Theme.Size(name)
}

package view

import (
	"fmt"
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"

	"usbridge-client/internal/gui/design"
)

var connectionShareIcon = fyne.NewStaticResource("connection-share.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M16 11c1.66 0 2.99-1.34 2.99-3S17.66 5 16 5c-1.66 0-3 1.34-3 3s1.34 3 3 3zm-8 0c1.66 0 2.99-1.34 2.99-3S9.66 5 8 5C6.34 5 5 6.34 5 8s1.34 3 3 3zm0 2c-2.33 0-7 1.17-7 3.5V19h14v-2.5c0-2.33-4.67-3.5-7-3.5zm8 0c-.29 0-.62.02-.97.05 1.16.84 1.97 1.97 1.97 3.45V19h6v-2.5c0-2.33-4.67-3.5-7-3.5z"/></svg>`))

// newConnectionShareChip is the people icon on a contact card with the number
// of people who have access; a dot after the number means someone has yet to
// accept or is waiting for the host's OK. Tapping it opens the Share dialog.
func newConnectionShareChip(count, waiting int, disabled bool, onTap func()) fyne.CanvasObject {
	btn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		Stroke:     color.Transparent,
		NormalIcon: connectionShareIcon,
		IconSize:   fyne.NewSize(13, 13),
		ButtonSize: fyne.NewSize(22, 20),
		OnTapped: func() {
			if !disabled && onTap != nil {
				onTap()
			}
		},
	})
	if count <= 0 {
		return btn
	}
	text := fmt.Sprintf("%d", count)
	textColor := design.ColorConnectionBadgeText
	if waiting > 0 {
		text += " •"
		textColor = design.ColorAccent
	}
	return container.NewHBox(btn, container.NewCenter(NewBrandText(text, 9, textColor, true)))
}

package view

import (
	"math"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
)

// commandFieldLineChars is about how many characters of the field's monospace
// text fit on one row of the dialog; it only sets the field's starting height.
const commandFieldLineChars = 50

// CommandField shows terminal commands read-only. Unlike a disabled entry it
// takes focus and keeps full-contrast text, so a part can be selected and copied.
type CommandField struct {
	StyledEntry
}

// NewCommandField builds a field tall enough for text.
func NewCommandField(text string) *CommandField {
	f := &CommandField{}
	f.MultiLine = true
	f.Wrapping = fyne.TextWrapWord
	f.TextStyle.Monospace = true
	f.ExtendBaseWidget(f)
	f.SetText(text)
	rows := 0
	for _, line := range strings.Split(text, "\n") {
		rows += max(1, int(math.Ceil(float64(len(line))/commandFieldLineChars)))
	}
	f.SetMinRowsVisible(rows)
	return f
}

// TypedRune drops typing.
func (f *CommandField) TypedRune(rune) {}

// TypedKey keeps only the keys that move the cursor or the selection.
func (f *CommandField) TypedKey(key *fyne.KeyEvent) {
	switch key.Name {
	case fyne.KeyUp, fyne.KeyDown, fyne.KeyLeft, fyne.KeyRight,
		fyne.KeyHome, fyne.KeyEnd, fyne.KeyPageUp, fyne.KeyPageDown:
		f.Entry.TypedKey(key)
	}
}

// TypedShortcut keeps only Copy and Select All.
func (f *CommandField) TypedShortcut(shortcut fyne.Shortcut) {
	switch shortcut.(type) {
	case *fyne.ShortcutCopy, *fyne.ShortcutSelectAll:
		f.Entry.TypedShortcut(shortcut)
	}
}

// TappedSecondary opens the app's entry menu with only what a read-only field can do.
func (f *CommandField) TappedSecondary(*fyne.PointEvent) {
	if c := fyne.CurrentApp().Driver().CanvasForObject(f); c != nil {
		c.Focus(f)
	}
	clipboard := fyne.CurrentApp().Clipboard()
	showStyledMenu(f, []StyledMenuItem{
		{Label: "Copy", OnTap: func() { f.TypedShortcut(&fyne.ShortcutCopy{Clipboard: clipboard}) }},
		{Label: "Select All", OnTap: func() { f.TypedShortcut(&fyne.ShortcutSelectAll{}) }},
	}, StyledMenuOptions{
		TextColor:         design.ColorConnectionBadgeText,
		TextSize:          10,
		RowHeight:         26,
		IgnoreAnchorWidth: true,
	})
}

// commandDialogWidth fits a whole `sudo snap connect` line in the field.
const commandDialogWidth = float32(540)

// ShowCommandDialog is a brand dialog with terminal commands in a CommandField:
// part of them can be selected and copied there, and the Apply button (onCopy)
// takes them all and closes it.
func ShowCommandDialog(title, message, command, closeLabel, copyLabel string, onCopy func(), parent fyne.Window) {
	textWidth := commandDialogWidth - (videoDialogBodyInsetLR+videoDialogBodyInsetQuirk)*2
	ShowBrandFormDialog(BrandFormDialogSpec{
		Parent: parent,
		Title:  title,
		Body: container.NewVBox(
			newVideoDialogWrapText(textWidth, 14, false, videoDialogWrapSpan{Text: message, Color: design.ColorTextLight}),
			NewDialogVSpace(8),
			NewCommandField(command),
		),
		CancelText: closeLabel,
		ApplyText:  copyLabel,
		OnApply:    onCopy,
		MinWidth:   commandDialogWidth,
	})
}

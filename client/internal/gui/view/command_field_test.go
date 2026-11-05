package view

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func TestCommandFieldIsReadOnlyButCopies(t *testing.T) {
	app := test.NewApp()
	app.Settings().SetTheme(theme.DefaultTheme())
	const text = "sudo snap connect usbridge-client:camera\nsudo snap connect usbridge-client:alsa"
	f := NewCommandField(text)
	w := app.NewWindow("test")
	w.SetContent(f)
	w.Resize(fyne.NewSize(480, 200))
	w.Canvas().Focus(f)
	if w.Canvas().Focused() != f {
		t.Fatal("the field can't take focus, so nothing in it can be selected")
	}

	test.Type(f, "x")
	f.TypedKey(&fyne.KeyEvent{Name: fyne.KeyBackspace})
	f.TypedKey(&fyne.KeyEvent{Name: fyne.KeyDelete})
	f.TypedKey(&fyne.KeyEvent{Name: fyne.KeyReturn})
	clip := app.Clipboard()
	f.TypedShortcut(&fyne.ShortcutSelectAll{})
	f.TypedShortcut(&fyne.ShortcutCut{Clipboard: clip})
	f.TypedShortcut(&fyne.ShortcutPaste{Clipboard: clip})
	if f.Text != text {
		t.Fatalf("the text changed to %q", f.Text)
	}

	f.TypedShortcut(&fyne.ShortcutCopy{Clipboard: clip})
	if got := clip.Content(); got != text {
		t.Fatalf("clipboard = %q, want the selected text", got)
	}

}

package gui

import (
	"strings"
	"testing"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/netutil"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
)

// The header button stays only while a plug is disconnected, and its notice lists
// those plugs; its button copies them as one line.
func TestSnapPlugsButtonAndNotice(t *testing.T) {
	i18n.Init("en")
	app := test.NewTempApp(t)
	w := app.NewWindow("test")
	w.Resize(fyne.NewSize(900, 700))
	mw := &MainWindow{app: app, window: w}
	mw.snapPlugsBtn = view.NewHeaderActionButton(mw.handleSnapPlugsTapped)
	mw.snapPlugsBtn.Hide()

	plugs := []string{"camera", "joystick"}
	mw.setSnapMissingPlugs(plugs)
	if !mw.snapPlugsBtn.Visible() {
		t.Fatal("button hidden with plugs missing")
	}

	mw.handleSnapPlugsTapped()
	overlay := w.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("no notice opened")
	}
	var copyBtn fyne.Tappable
	var shownText strings.Builder
	for _, o := range test.LaidOutObjects(overlay) {
		switch v := o.(type) {
		case interface {
			fyne.Tappable
			Text() string
		}:
			if v.Text() == i18n.Current.CopyCommand {
				copyBtn = v
			}
		case *canvas.Text:
			shownText.WriteString(v.Text + " ")
		case *view.CommandField:
			shownText.WriteString(v.Text)
		}
	}
	for _, plug := range plugs {
		if !strings.Contains(shownText.String(), "sudo snap connect usbridge-client:"+plug) {
			t.Fatalf("notice doesn't list %s: %q", plug, shownText.String())
		}
	}
	if copyBtn == nil {
		t.Fatalf("no %q button", i18n.Current.CopyCommand)
	}
	test.Tap(copyBtn)
	if got, want := app.Clipboard().Content(), netutil.SnapConnectCommand(plugs); got != want {
		t.Fatalf("clipboard = %q, want %q", got, want)
	}

	mw.setSnapMissingPlugs(nil)
	if mw.snapPlugsBtn.Visible() {
		t.Fatal("button still shown with every plug connected")
	}
}

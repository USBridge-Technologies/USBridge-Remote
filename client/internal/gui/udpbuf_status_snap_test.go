//go:build linux && !android

package gui

import (
	"strings"
	"testing"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/netutil"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

// In a snap the UDP buffer button can't run pkexec: it opens a notice with the
// terminal command, whose button copies that command.
func TestUDPBufferGrantInSnapShowsCopyableCommand(t *testing.T) {
	t.Setenv("SNAP", "/snap/usbridge-client/x1")
	i18n.Init("en")
	app := test.NewTempApp(t)
	w := app.NewWindow("test")
	w.Resize(fyne.NewSize(900, 700))
	mw := &MainWindow{app: app, window: w}
	mw.udpBufGrantBtn = view.NewHeaderActionButton(mw.handleUDPBufferGrantTapped)

	mw.handleUDPBufferGrantTapped()

	overlay := w.Canvas().Overlays().Top()
	if overlay == nil {
		t.Fatal("no notice opened")
	}
	cmd := netutil.UDPBufferCommand()
	var copyBtn *widget.Button
	var shownText strings.Builder
	for _, o := range test.LaidOutObjects(overlay) {
		switch v := o.(type) {
		case *widget.Button:
			if v.Text == i18n.Current.CopyCommand {
				copyBtn = v
			}
		case *widget.RichText:
			shownText.WriteString(v.String())
		}
	}
	if !strings.Contains(shownText.String(), cmd) {
		t.Fatalf("notice doesn't show the command %q; shows %q", cmd, shownText.String())
	}
	if !strings.Contains(shownText.String(), i18n.Current.UDPBufferRunCommand) {
		t.Fatal("notice lacks the explanation")
	}
	if copyBtn == nil {
		t.Fatalf("no %q button", i18n.Current.CopyCommand)
	}
	test.Tap(copyBtn)
	if got := app.Clipboard().Content(); got != cmd {
		t.Fatalf("clipboard = %q, want the command %q", got, cmd)
	}
}

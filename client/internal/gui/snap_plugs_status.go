package gui

import (
	"strings"
	"time"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/netutil"

	"fyne.io/fyne/v2"
)

// watchSnapPlugs keeps the header button in step with `snap connect`, which
// happens in a terminal while the app runs, so a connected plug drops off the
// list without a restart.
func (mw *MainWindow) watchSnapPlugs() {
	if !netutil.InSnap() {
		return
	}
	go func() {
		for {
			missing := netutil.SnapMissingPlugs()
			fyne.Do(func() { mw.setSnapMissingPlugs(missing) })
			time.Sleep(15 * time.Second)
		}
	}()
}

func (mw *MainWindow) setSnapMissingPlugs(missing []string) {
	mw.snapMissingPlugs = missing
	if len(missing) == 0 {
		mw.snapPlugsBtn.Hide()
	} else {
		mw.snapPlugsBtn.Show()
	}
}

// handleSnapPlugsTapped shows what is still disconnected, one command per line,
// so one of them can be copied alone. The button copies them all as a single line
// (netutil.SnapConnectCommand).
func (mw *MainWindow) handleSnapPlugsTapped() {
	missing := mw.snapMissingPlugs
	if len(missing) == 0 {
		return
	}
	lines := make([]string, len(missing))
	for i, plug := range missing {
		lines[i] = netutil.SnapConnectCommand([]string{plug})
	}
	cmd := netutil.SnapConnectCommand(missing)
	view.ShowCommandDialog(i18n.Current.SnapConnectDevices, i18n.Current.SnapConnectRunCommand, strings.Join(lines, "\n"),
		i18n.Current.Close, i18n.Current.CopyCommand, func() {
			if mw.app != nil {
				mw.app.Clipboard().SetContent(cmd)
			}
		}, mw.window)
}

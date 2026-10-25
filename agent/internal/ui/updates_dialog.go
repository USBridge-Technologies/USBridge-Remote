package ui

import (
	"context"
	"fmt"

	"usbridge_agent/internal/update"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge_agent/internal/ui/design"
)

const agentUpdatesDialogWidth float32 = 380

// updatesBackend drives the Updates dialog behind the footer version ->
// What's new. Set only by an engine-owning window (see
// installUpdatesBackend); nil hides the entry point.
type updatesBackendFuncs struct {
	testChannel    func() bool
	setTestChannel func(test bool)
	check          func() (newVersion string, err error) // blocks; "" = up to date
	install        func()
}

var updatesBackend *updatesBackendFuncs

// startWhatsNewSync shows the cached release notes at once and refreshes
// them from the selected channel's GitHub release in the background; the
// embedded catalog covers a first offline launch.
func startWhatsNewSync() {
	if data := update.CachedWhatsNew(); data != nil {
		if err := setRemoteWhatsNew(data); err != nil {
			logrus.WithField("component", "update").WithError(err).Warn("ignoring cached release notes")
		}
	}
	go func() {
		data, err := update.FetchWhatsNew(context.Background())
		if err != nil {
			logrus.WithField("component", "update").WithError(err).Debug("release notes not refreshed (offline or not published yet)")
			return
		}
		if err := setRemoteWhatsNew(data); err != nil {
			logrus.WithField("component", "update").WithError(err).Warn("ignoring downloaded release notes")
		}
	}()
}

// installUpdatesBackend wires the dialog to internal/update. An attach-only
// GUI never calls it: applying an update there would not touch the headless
// instance that runs the engine (see docs/AUTO_UPDATE.md).
func (w *Window) installUpdatesBackend(parent fyne.Window) {
	var pending *update.Manifest
	updatesBackend = &updatesBackendFuncs{
		testChannel: func() bool { return update.GetChannel() == update.ChannelTest },
		setTestChannel: func(test bool) {
			if test {
				update.SetChannel(update.ChannelTest)
			} else {
				update.SetChannel(update.ChannelStable)
			}
			startWhatsNewSync()
		},
		check: func() (string, error) {
			manifest, err := update.CheckNow(context.Background(), appVersion)
			pending = manifest
			if manifest == nil {
				return "", err
			}
			return manifest.Version, nil
		},
		install: func() {
			manifest := pending
			if manifest == nil {
				return
			}
			fyne.Do(func() {
				progress := showUpdateProgressDialog(parent, manifest.Version)
				go func() {
					err := update.DownloadAndApply(context.Background(), manifest, progress.Update)
					progress.Close()
					if err != nil {
						logrus.WithField("component", "update").WithError(err).Error("failed to apply update")
						showErrorDialog(err, parent)
					}
				}()
			})
		},
	}
}

// showUpdatesDialog lets the user pick Release or Test builds and update on
// demand. It checks as soon as it opens and again on every switch.
func showUpdatesDialog(parent fyne.Window) {
	b := updatesBackend
	if parent == nil || b == nil {
		return
	}
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	current := widget.NewLabel(fmt.Sprintf(loc().UpdatesCurrent, appVersion))
	current.Wrapping = fyne.TextWrapWord
	hint := widget.NewLabel(loc().UpdatesChannelHint)
	hint.Wrapping = fyne.TextWrapWord
	status := canvas.NewText("", design.ColorTextLight)
	status.TextSize = 11

	var (
		channelRow = container.NewStack()
		actionHost = container.NewStack()
		generation int // drops results of a check the user already moved past
		runCheck   func()
	)
	var selectChannel func(test bool)
	renderChannel := func() {
		test := b.testChannel()
		mk := func(label string, selected bool, onTap func()) *iconActionButton {
			if selected {
				return newDialogCTA(label, onTap)
			}
			btn := newIconActionButton(label, nil, onTap)
			btn.Compact = true
			return btn
		}
		stable := mk(loc().UpdatesChannelStable, !test, func() { selectChannel(false) })
		testBtn := mk(loc().UpdatesChannelTest, test, func() { selectChannel(true) })
		channelRow.Objects = []fyne.CanvasObject{container.New(&tightHBoxLayout{gap: 8}, stable, testBtn)}
		channelRow.Refresh()
	}
	selectChannel = func(test bool) {
		if b.testChannel() == test {
			return
		}
		b.setTestChannel(test)
		renderChannel()
		runCheck()
	}
	setStatus := func(text string, action fyne.CanvasObject) {
		status.Text = text
		status.Refresh()
		actionHost.Objects = nil
		if action != nil {
			actionHost.Objects = []fyne.CanvasObject{action}
		}
		actionHost.Refresh()
		if popup != nil {
			popup.Refresh()
		}
	}
	again := func() fyne.CanvasObject {
		btn := newIconActionButton(loc().UpdatesCheckAgain, nil, func() { runCheck() })
		btn.Compact = true
		return btn
	}
	runCheck = func() {
		generation++
		mine := generation
		setStatus(loc().UpdatesChecking, nil)
		go func() {
			newVersion, err := b.check()
			fyne.Do(func() {
				if mine != generation {
					return
				}
				switch {
				case err != nil:
					setStatus(fmt.Sprintf(loc().UpdatesCheckFailed, err.Error()), again())
				case newVersion == "":
					setStatus(loc().UpdatesUpToDate, again())
				default:
					setStatus(fmt.Sprintf(loc().UpdatesAvailable, newVersion),
						newDialogCTA(loc().Update, func() {
							closeDialog()
							b.install()
						}))
				}
			})
		}()
	}
	renderChannel()

	body := container.NewVBox(
		wrapDialogLabel(current, 11, design.ColorTextLight),
		channelRow,
		wrapDialogLabel(hint, 10, design.ColorMutedOlive),
		status,
		actionHost,
	)
	panel := newBrandedDialogPanelInsets(loc().UpdatesTitle, agentUpdatesDialogWidth, 20, 10, body, nil, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{
		Panel:        panel,
		OnOutsideTap: closeDialog,
	})
	runCheck()
}

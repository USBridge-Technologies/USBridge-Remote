package view

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

const updatesDialogPanelWidth = float32(380)

// UpdatesBackend is what the Updates dialog drives. It is wired once at
// startup (cmd/main.go) so this package doesn't import internal/update. A
// nil Updates hides the dialog's entry point (self-update compiled out).
type UpdatesBackend struct {
	// TestChannel reports whether the pre-release channel is selected.
	TestChannel func() bool
	// SetTestChannel persists the channel choice.
	SetTestChannel func(test bool)
	// Check blocks on the network. newVersion == "" means up to date.
	Check func() (newVersion string, err error)
	// Install downloads and applies the version Check last returned.
	Install func()
}

// Updates is the backend behind the footer-version "Updates" entry.
var Updates *UpdatesBackend

// ShowUpdatesDialog lets the user pick Release or Test builds and update
// on demand. It checks as soon as it opens and again on every switch.
func ShowUpdatesDialog(parent fyne.Window) {
	b := Updates
	if parent == nil || b == nil {
		return
	}
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	title := NewBrandText(i18n.Current.UpdatesTitle, 13, design.ColorTextLight, true)
	closeBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: videoDialogCancelIconSVG,
		HoverIcon:  videoDialogCancelIconSVG,
		IconSize:   fyne.NewSize(18, 18),
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   closeDialog,
	})
	headerSepLine := color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	headerSep := canvas.NewRectangle(headerSepLine)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.NewVBox(newVideoDialogTopAccentBar(), NewInset(title, 21, 44, 9, 4), headerSep)

	textW := updatesDialogPanelWidth - 52
	current := newVideoDialogWrapText(textW, 11, false, videoDialogWrapSpan{
		Text:  fmt.Sprintf(i18n.Current.UpdatesCurrent, AppVersion()),
		Color: design.ColorTextLight,
	})
	hint := newVideoDialogWrapText(textW, 10, false, videoDialogWrapSpan{
		Text:  i18n.Current.UpdatesChannelHint,
		Color: videoDialogHintColor,
	})
	status := canvas.NewText("", design.ColorTextLight)
	status.TextSize = 11

	var (
		actionHost = container.NewStack()
		channelRow = container.NewStack()
		generation int // drops results of a check the user already moved past
		runCheck   func()
	)

	var selectChannel func(test bool)
	renderChannel := func() {
		test := b.TestChannel()
		mk := func(label string, selected bool, onTap func()) *videoDialogPillButton {
			if selected {
				return newVideoDialogApplyButton(label, onTap)
			}
			return newVideoDialogCancelButton(label, onTap)
		}
		stableBtn := mk(i18n.Current.UpdatesChannelStable, !test, func() { selectChannel(false) })
		testBtn := mk(i18n.Current.UpdatesChannelTest, test, func() { selectChannel(true) })
		channelRow.Objects = []fyne.CanvasObject{container.New(&DeviceRowControlsLayout{Gap: 8}, stableBtn, testBtn)}
		channelRow.Refresh()
	}
	selectChannel = func(test bool) {
		if b.TestChannel() == test {
			return
		}
		b.SetTestChannel(test)
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
	runCheck = func() {
		generation++
		mine := generation
		setStatus(i18n.Current.UpdatesChecking, nil)
		go func() {
			newVersion, err := b.Check()
			fyne.Do(func() {
				if mine != generation {
					return
				}
				switch {
				case err != nil:
					setStatus(fmt.Sprintf(i18n.Current.UpdatesCheckFailed, err.Error()),
						newVideoDialogCancelButton(i18n.Current.UpdatesCheckAgain, runCheck))
				case newVersion == "":
					setStatus(i18n.Current.UpdatesUpToDate,
						newVideoDialogCancelButton(i18n.Current.UpdatesCheckAgain, runCheck))
				default:
					setStatus(fmt.Sprintf(i18n.Current.UpdatesAvailable, newVersion),
						newVideoDialogApplyButton(i18n.Current.UpdateNowButton, func() {
							closeDialog()
							b.Install()
						}))
				}
			})
		}()
	}
	renderChannel()

	body := container.NewVBox(
		current, videoDialogVSpace(10),
		channelRow, videoDialogVSpace(6), hint, videoDialogVSpace(12),
		status, videoDialogVSpace(8), actionHost,
	)
	form := container.NewBorder(headerBlock, nil, nil, nil, NewInset(body, 18, 18, 14, 12))
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&videoDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(bg, NewInsetExact(form, 0, 0, 0, 8), cornerBtn, border)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:        panel,
		DimColor:     color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		OnOutsideTap: closeDialog,
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
			w := minFloat32(maxFloat32(panelMin.Width, updatesDialogPanelWidth), maxWidth)
			h := minFloat32(panelMin.Height, maxHeight)
			return fyne.NewSize(w, h)
		},
	})
	runCheck()
}

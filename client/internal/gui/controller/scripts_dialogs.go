package controller

import (
	"image/color"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// Script editor / log reuse the Add Connection panel chrome: teal-lime
// accent hairline, left title+subtitle, corner X, gray-900 card, compact
// pill footer. They stay larger than the 408px Add Connection card because
// they are workspace windows (code / log), not a short form.

var (
	scriptDialogFloppyIcon     = fyne.NewStaticResource("script-dialog-floppy.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#111111" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>`))
	scriptDialogPlayIcon       = fyne.NewStaticResource("script-dialog-play.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c4e77a"><path d="M8 5v14l11-7z"/></svg>`))
	scriptDialogStopIcon       = fyne.NewStaticResource("script-dialog-stop.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c4e77a"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`))
	scriptDialogCopyIconLight  = fyne.NewStaticResource("script-dialog-copy-light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M16 1H4c-1.1 0-2 .9-2 2v14h2V3h12V1zm3 4H8c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h11c1.1 0 2-.9 2-2V7c0-1.1-.9-2-2-2zm0 16H8V7h11v14z"/></svg>`))
	scriptDialogPasteIconLight = fyne.NewStaticResource("script-dialog-paste-light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M19 2h-4.18C14.4.84 13.3 0 12 0c-1.3 0-2.4.84-2.82 2H5c-1.1 0-2 .9-2 2v16c0 1.1.9 2 2 2h14c1.1 0 2-.9 2-2V4c0-1.1-.9-2-2-2zm-7 0c.55 0 1 .45 1 1s-.45 1-1 1-1-.45-1-1 .45-1 1-1zm7 18H5V4h2v3h10V4h2v16z"/></svg>`))
	scriptDialogClearIconLight = fyne.NewStaticResource("script-dialog-clear-light.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c5c8b5"><path d="M15 16h4v2h-4zm0-8h7v2h-7zm0 4h6v2h-6zM3 18c0 1.1.9 2 2 2h6c1.1 0 2-.9 2-2V8H3v10zM14 5h-3l-1-1H6L5 5H2v2h12z"/></svg>`))
	scriptDialogStopHoverIcon  = fyne.NewStaticResource("script-dialog-stop-hover.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#fda4af"><rect x="6" y="6" width="12" height="12" rx="2"/></svg>`))
)

type brandedOverlayDialogSpec struct {
	parent        fyne.Window
	title         string
	subtitle      string
	body          fyne.CanvasObject
	rightButtons  []fyne.CanvasObject
	panelSize     func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	compactFooter bool
	// beforeClose, if set, runs instead of closing when the user taps X or
	// Cancel. Call proceed() to actually dismiss (Save/Run still close directly).
	beforeClose func(proceed func())
	onClose     func()
}

func showBrandedOverlayDialog(spec brandedOverlayDialogSpec) (*widget.PopUp, func()) {
	title := view.NewBrandText(spec.title, 13, design.ColorTextLight, true)
	var titleCol fyne.CanvasObject = title
	if spec.subtitle != "" {
		subtitleLbl := widget.NewLabel(spec.subtitle)
		subtitleLbl.Wrapping = fyne.TextWrapWord
		subtitleThemed := container.NewThemeOverride(subtitleLbl, &mutedForegroundTheme{design.NewBrandTheme()})
		nudgedSubtitle := container.New(&subtitleLeftNudgeLayout{Amount: 8}, subtitleThemed)
		titleCol = container.New(&tightHeaderVBoxLayout{Gap: -2}, title, nudgedSubtitle)
	}

	var popup *widget.PopUp
	closePopup := func() {
		if popup != nil {
			popup.Hide()
		}
		if spec.onClose != nil {
			spec.onClose()
		}
	}
	requestClose := func() {
		if spec.beforeClose != nil {
			spec.beforeClose(closePopup)
			return
		}
		closePopup()
	}

	closeBtn := newConnectionDialogIconButton(connectionDialogCancelIconRes, requestClose)
	topAccent := newConnectionDialogTopAccentBar()
	sep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	sep.SetMinSize(fyne.NewSize(0, 1))
	sepFooter := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	sepFooter.SetMinSize(fyne.NewSize(0, 1))
	headerBlock := container.New(&tightHeaderVBoxLayout{Gap: 0}, topAccent, view.NewInset(titleCol, 21, 44, 9, 4), sep)

	cancelBtn := newScriptDialogGhostButton(i18n.Current.Cancel, requestClose)
	rightItems := make([]fyne.CanvasObject, 0, len(spec.rightButtons))
	for _, btn := range spec.rightButtons {
		if btn != nil {
			rightItems = append(rightItems, btn)
		}
	}
	rightGroup := container.New(&view.DeviceRowControlsLayout{Gap: connectionDialogButtonsGap}, rightItems...)
	buttons := container.NewBorder(nil, nil, container.NewCenter(cancelBtn), rightGroup)
	footerTop, panelBottom := float32(14), float32(16)
	if spec.compactFooter {
		footerTop, panelBottom = 8, 12
	}
	footerBlock := container.NewVBox(
		sepFooter,
		view.NewInset(buttons, 12, 18, footerTop, 0),
	)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	inner := container.NewBorder(
		headerBlock,
		footerBlock,
		nil, nil,
		view.NewInset(spec.body, 18, 18, 8, 8),
	)
	cornerBtn := container.New(&dialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(
		bg,
		view.NewInset(inner, 0, 0, 0, panelBottom),
		cornerBtn,
		border,
	)

	sizeFn := spec.panelSize
	if sizeFn == nil {
		sizeFn = scriptWorkspacePanelSize
	}
	popup = view.ShowOverlayPopup(spec.parent, view.OverlayPopupSpec{
		Panel:     panel,
		DimColor:  connectionDialogDimColor(),
		PanelSize: sizeFn,
	})
	return popup, closePopup
}

func scriptWorkspacePanelSize(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
	maxW := canvasSize.Width - margin*2
	maxH := canvasSize.Height - margin*2
	if maxW < 0 {
		maxW = canvasSize.Width
	}
	if maxH < 0 {
		maxH = canvasSize.Height
	}
	w := minFloat32(maxW, maxFloat32(560, maxW*0.86))
	h := minFloat32(maxH, maxFloat32(380, maxH*0.86))
	return fyne.NewSize(w, h)
}

func newScriptDialogSurface(content fyne.CanvasObject) fyne.CanvasObject {
	surface, _ := newScriptDialogFocusSurface(content)
	return surface
}

func newScriptDialogFocusSurface(content fyne.CanvasObject) (fyne.CanvasObject, func(bool)) {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	setFocused := func(on bool) {
		if on {
			bg.StrokeColor = design.ColorConnectionBadgeText
		} else {
			bg.StrokeColor = design.ColorTailscaleChipBorder
		}
		bg.Refresh()
	}
	return container.NewStack(bg, view.NewInset(content, 10, 10, 8, 8)), setFocused
}

func newScriptDialogGhostButton(label string, onTapped func()) *connectionDialogSecondaryButton {
	btn := &connectionDialogSecondaryButton{
		labelText:      label,
		onTapped:       onTapped,
		compact:        true,
		fillColor:      color.Transparent,
		borderColor:    color.Transparent,
		textColor:      color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff},
		hoverFillColor: color.Transparent,
		hoverTextColor: design.ColorTextLight,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newScriptDialogTealButton(label string, icon fyne.Resource, onTapped func()) *connectionDialogSecondaryButton {
	btn := &connectionDialogSecondaryButton{
		labelText:         label,
		onTapped:          onTapped,
		compact:           true,
		fillColor:         design.ColorConnectionBadgeText,
		borderColor:       color.Transparent,
		textColor:         design.ColorGray950,
		hoverFillColor:    color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff},
		hoverTextColor:    design.ColorGray950,
		hoverBorderColor:  color.Transparent,
		iconRes:           icon,
		hoverIconRes:      icon,
		disabledFillColor: connectionDialogTealDisabled,
		disabledTextColor: design.ColorGray950,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newScriptDialogLimeButton(label string, icon fyne.Resource, onTapped func()) *connectionDialogSecondaryButton {
	btn := &connectionDialogSecondaryButton{
		labelText:        label,
		onTapped:         onTapped,
		compact:          true,
		fillColor:        color.NRGBA{R: 0x22, G: 0x26, B: 0x2a, A: 0xff},
		borderColor:      design.ColorTailscaleChipBorder,
		textColor:        design.ColorConnectionAddFill,
		hoverFillColor:   color.NRGBA{R: 0x31, G: 0x35, B: 0x39, A: 0xff},
		hoverTextColor:   design.ColorConnectionAddFill,
		hoverBorderColor: design.ColorConnectionAddFill,
		iconRes:          icon,
		hoverIconRes:     icon,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newScriptDialogCopyIconButton(onTapped func()) *connectionDialogIconButton {
	return newScriptDialogNeutralIconButton(scriptDialogCopyIconLight, onTapped)
}

func newScriptDialogPasteIconButton(onTapped func()) *connectionDialogIconButton {
	return newScriptDialogNeutralIconButton(scriptDialogPasteIconLight, onTapped)
}

func newScriptDialogClearIconButton(onTapped func()) *connectionDialogIconButton {
	return newScriptDialogNeutralIconButton(scriptDialogClearIconLight, onTapped)
}

func newScriptDialogNeutralIconButton(icon fyne.Resource, onTapped func()) *connectionDialogIconButton {
	btn := newConnectionDialogIconButton(icon, onTapped)
	btn.buttonSize = fyne.NewSize(32, 32)
	btn.iconSize = fyne.NewSize(14, 14)
	btn.customNormalFill = color.Transparent
	btn.customHoverFill = color.NRGBA{R: 0x26, G: 0x2a, B: 0x2e, A: 0xff}
	btn.customNormalBorder = design.ColorTailscaleChipBorder
	btn.customHoverBorder = design.ColorTailscaleChipBorder
	btn.opaqueIcon = true
	return btn
}

type scriptLogTheme struct{ fyne.Theme }

func (t *scriptLogTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return scriptEditorTextSize
	}
	return t.Theme.Size(name)
}

// ShowScriptLogDialog presents a live log viewer in the Add Connection panel chrome.
func ShowScriptLogDialog(parent fyne.Window, client *api.USBClient, path, displayName string) {
	if parent == nil || client == nil {
		return
	}

	const waitingText = "(waiting for output...)"
	logLabel := widget.NewLabel(waitingText)
	logLabel.Wrapping = fyne.TextWrapWord
	logLabel.TextStyle = fyne.TextStyle{Monospace: true}
	logScroll := container.NewVScroll(container.NewThemeOverride(logLabel, &scriptLogTheme{fyne.CurrentApp().Settings().Theme()}))

	statusLabel := canvas.NewText("", design.ColorTextMuted)
	statusLabel.TextSize = 11
	statusRow := view.NewInset(statusLabel, 0, 0, 0, 8)
	statusRow.Hide()

	stopPoll := make(chan struct{})
	var stopOnce sync.Once
	stop := func() { stopOnce.Do(func() { close(stopPoll) }) }
	var offset int

	appendLines := func(lines []string) {
		if len(lines) == 0 {
			return
		}
		cur := logLabel.Text
		if cur == waitingText {
			cur = ""
		}
		for _, l := range lines {
			cur += l + "\n"
		}
		logLabel.SetText(cur)
		logScroll.ScrollToBottom()
	}

	updateStatus := func(statuses []models.ScriptRunStatus) {
		for _, st := range statuses {
			if st.Path != path {
				continue
			}
			if st.Running {
				statusLabel.Text = "Running"
				statusLabel.Color = color.NRGBA{R: 0x4c, G: 0xd9, B: 0x64, A: 0xff}
			} else if st.Error != "" {
				statusLabel.Text = "Error: " + st.Error
				statusLabel.Color = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
			} else {
				statusLabel.Text = "Finished"
				statusLabel.Color = design.ColorTextMuted
			}
			statusLabel.Show()
			statusLabel.Refresh()
			statusRow.Show()
			statusRow.Refresh()
			return
		}
		statusLabel.Text = ""
		statusRow.Hide()
		statusRow.Refresh()
	}

	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopPoll:
				return
			case <-ticker.C:
			}

			logResp, err := client.GetScriptLog(path, offset)
			if err == nil && len(logResp.Lines) > 0 {
				lines := logResp.Lines
				newOffset := logResp.Total
				fyne.Do(func() {
					appendLines(lines)
					offset = newOffset
				})
			}

			statuses, err := client.GetScriptStatus()
			if err == nil {
				fyne.Do(func() { updateStatus(statuses) })
			}
		}
	}()

	clearBtn := newScriptDialogClearIconButton(func() {
		logLabel.SetText("")
		offset = 0
	})
	copyBtn := newScriptDialogCopyIconButton(func() {
		textToCopy := logLabel.Text
		if textToCopy == waitingText {
			textToCopy = ""
		}
		if parent.Clipboard() != nil {
			parent.Clipboard().SetContent(textToCopy)
		}
	})

	subtitle := displayName
	if subtitle == "" {
		subtitle = path
	}
	body := container.NewBorder(
		statusRow,
		nil, nil, nil,
		newScriptDialogSurface(logScroll),
	)

	popup, _ := showBrandedOverlayDialog(brandedOverlayDialogSpec{
		parent:       parent,
		title:        "Script log",
		subtitle:     subtitle,
		body:         body,
		rightButtons: []fyne.CanvasObject{clearBtn, copyBtn},
		onClose:      stop,
	})

	go func() {
		for {
			var isNil bool
			done := make(chan struct{})
			fyne.Do(func() { isNil = popup == nil; close(done) })
			<-done
			if !isNil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		for {
			var visible bool
			done := make(chan struct{})
			fyne.Do(func() {
				if popup != nil {
					visible = popup.Visible()
				}
				close(done)
			})
			<-done
			if !visible {
				break
			}
			time.Sleep(120 * time.Millisecond)
		}
		stop()
	}()
}

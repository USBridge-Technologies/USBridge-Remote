package view

import (
	"image/color"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ShowScriptLogDialog presents a log viewer modal popup for a script execution.
// Includes a close button ('X'), status indicator, clear log button, and copy log button.
func ShowScriptLogDialog(parent fyne.Window, client *api.USBClient, path, displayName string) {
	if parent == nil || client == nil {
		return
	}

	titleText := NewBrandText("> "+displayName+" log", 15, design.ColorAccent, true)

	logEntry := widget.NewMultiLineEntry()
	logEntry.TextStyle = fyne.TextStyle{Monospace: true}
	logEntry.Wrapping = fyne.TextWrapWord
	logEntry.Disable()
	logEntry.SetText("(waiting for output...)")

	logScroll := container.NewVScroll(logEntry)

	statusLabel := canvas.NewText("", design.ColorTextMuted)
	statusLabel.TextSize = 11

	var popup *widget.PopUp
	stopPoll := make(chan struct{})
	var offset int

	appendLines := func(lines []string) {
		if len(lines) == 0 {
			return
		}
		cur := logEntry.Text
		if cur == "(waiting for output...)" {
			cur = ""
		}
		for _, l := range lines {
			cur += l + "\n"
		}
		logEntry.SetText(cur)
		logScroll.ScrollToBottom()
	}

	updateStatus := func(statuses []models.ScriptRunStatus) {
		for _, st := range statuses {
			if st.Path == path {
				if st.Running {
					statusLabel.Text = "● Running"
					statusLabel.Color = color.NRGBA{R: 0x4c, G: 0xd9, B: 0x64, A: 0xff}
				} else if st.Error != "" {
					statusLabel.Text = "✖ Error: " + st.Error
					statusLabel.Color = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
				} else {
					statusLabel.Text = "✔ Finished"
					statusLabel.Color = design.ColorTextMuted
				}
				statusLabel.Refresh()
				return
			}
		}
		statusLabel.Text = ""
		statusLabel.Refresh()
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

	clearBtn := widget.NewButton("Clear", func() {
		logEntry.SetText("")
		offset = 0
	})
	clearBtn.Importance = widget.LowImportance

	copyBtn := widget.NewButtonWithIcon("Copy Log", theme.ContentCopyIcon(), func() {
		textToCopy := logEntry.Text
		if textToCopy == "(waiting for output...)" {
			textToCopy = ""
		}
		if parent.Clipboard() != nil {
			parent.Clipboard().SetContent(textToCopy)
		}
		ShowInfoDialog("Copied", i18n.Current.TextCopiedToClipboard, parent)
	})
	copyBtn.Importance = widget.LowImportance

	closeBtn := NewDialogCloseButton(func() {
		close(stopPoll)
		if popup != nil {
			popup.Hide()
		}
	})

	headerContent := container.NewBorder(nil, nil, nil, closeBtn,
		container.NewVBox(titleText, statusLabel),
	)
	headerDivider := canvas.NewRectangle(design.ColorBorder)
	headerDivider.SetMinSize(fyne.NewSize(0, 1))
	header := container.NewVBox(NewInset(headerContent, 0, 0, 8, 8), headerDivider)

	footerDivider := canvas.NewRectangle(design.ColorBorder)
	footerDivider.SetMinSize(fyne.NewSize(0, 1))
	footer := container.NewVBox(footerDivider, NewInset(
		container.NewHBox(clearBtn, copyBtn, layout.NewSpacer()),
		0, 0, 8, 8,
	))

	body := container.NewBorder(header, footer, nil, nil, logScroll)

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	accent := canvas.NewRectangle(color.Transparent)
	accent.CornerRadius = design.RadiusMD
	accent.StrokeColor = design.ColorBorder
	accent.StrokeWidth = 1
	panel := container.NewStack(bg, NewInset(body, 16, 16, 12, 12), accent)

	popup = ShowOverlayPopup(parent, OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
			const margin float32 = 16
			return fyne.NewSize(canvasSize.Width-margin*2, canvasSize.Height-margin*2)
		},
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
		select {
		case <-stopPoll:
		default:
			close(stopPoll)
		}
	}()
}

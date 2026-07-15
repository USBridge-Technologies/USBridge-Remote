package controller

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// ScriptsTabWidget provides the fourth "Scripts" tab combining MCP Proxy controls
// and Automation Scripts management in a single persistent scrollable view.
type ScriptsTabWidget struct {
	window fyne.Window
	mu     sync.Mutex

	usbClient *api.USBClient
	mcpProxy  api.MCPProxy
	mcpPort   int

	// Root container returned by GetContainer.
	outerContainer *fyne.Container

	// Scripts section body — VBox of script rows, rebuilt on client change.
	scriptsBodyContainer *fyne.Container

	// Per-script status updaters, keyed by path; rebuilt alongside the row list.
	rowUpdaters map[string]func(bool, string)

	// Stop channel for the background script-status polling goroutine.
	stopPollCh chan struct{}

	// MCP Proxy live UI elements.
	mcpURLLabel  *canvas.Text
	mcpToggleBtn *widget.Button
	mcpCopyBtn   *widget.Button
}

// NewScriptsTabWidget creates the widget and builds the persistent UI tree.
func NewScriptsTabWidget(window fyne.Window) *ScriptsTabWidget {
	w := &ScriptsTabWidget{
		window:      window,
		mcpPort:     api.DefaultMCPProxyPort,
		rowUpdaters: make(map[string]func(bool, string)),
	}
	w.build()
	return w
}

// GetContainer returns the permanent container used as the tab content.
func (w *ScriptsTabWidget) GetContainer() *fyne.Container {
	return w.outerContainer
}

// SetClient updates the device client and refreshes the tab content.
// Pass nil to reflect a disconnected state.
func (w *ScriptsTabWidget) SetClient(c *api.USBClient) {
	w.stopStatusPoll()

	w.mu.Lock()
	w.usbClient = c
	w.mu.Unlock()

	fyne.Do(func() {
		w.refreshMCPStatus()
	})

	if c != nil {
		w.refreshScriptsList()
		w.startStatusPoll()
	} else {
		fyne.Do(func() { w.showEmptyScriptsState() })
	}
}

// ─── Build ────────────────────────────────────────────────────────────────────

func (w *ScriptsTabWidget) build() {
	mcpCard := w.buildMCPCard()

	w.scriptsBodyContainer = container.NewVBox()
	w.showEmptyScriptsState()

	newEmmcBtn := widget.NewButtonWithIcon("New (eMMC)", theme.ContentAddIcon(), func() {
		w.showNewScriptDialog("/mnt/emmc/scripts/", w.refreshScriptsList)
	})
	newEmmcBtn.Importance = widget.MediumImportance

	newSDBtn := widget.NewButtonWithIcon("New (SD)", theme.ContentAddIcon(), func() {
		w.showNewScriptDialog("/mnt/sdcard/scripts/", w.refreshScriptsList)
	})
	newSDBtn.Importance = widget.LowImportance

	scriptsActions := container.NewHBox(newEmmcBtn, newSDBtn)
	scriptsCard := w.buildSectionCard("", scriptsActions, w.scriptsBodyContainer)

	content := container.New(&fillWidthVBoxLayout{gap: 0},
		view.NewInset(mcpCard, 12, 12, 8, 0),
		view.NewInset(scriptsCard, 12, 12, 0, 8),
	)
	scroll := container.NewVScroll(content)
	w.outerContainer = container.NewStack(scroll)
}

func (w *ScriptsTabWidget) buildMCPCard() fyne.CanvasObject {
	w.mcpURLLabel = canvas.NewText(
		fmt.Sprintf("http://127.0.0.1:%d/api/mcp", w.mcpPort),
		design.ColorTextMuted,
	)
	w.mcpURLLabel.TextSize = 11

	w.mcpCopyBtn = widget.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), func() {
		if w.window != nil {
			w.window.Clipboard().SetContent(w.mcpURLLabel.Text)
		}
	})
	w.mcpCopyBtn.Importance = widget.LowImportance
	w.mcpCopyBtn.Disable()

	w.mcpToggleBtn = widget.NewButton("Start", w.toggleMCPProxy)
	w.mcpToggleBtn.Importance = widget.MediumImportance
	w.mcpToggleBtn.Disable()

	descLabel := widget.NewLabel("Forwards /api/mcp to the device with signed requests. Local AI tools connect unsigned.")
	descLabel.Wrapping = fyne.TextWrapWord
	descLabel.Importance = widget.LowImportance

	urlRow := container.NewBorder(nil, nil, nil, w.mcpCopyBtn, w.mcpURLLabel)
	body := view.NewInset(container.NewVBox(
		view.NewInset(urlRow, 0, 0, 6, 0),
		view.NewInset(descLabel, 0, 0, 6, 0),
		view.NewInset(w.mcpToggleBtn, 0, 0, 6, 0),
	), 8, 8, 4, 4)

	return w.buildSectionCard("", nil, body)
}

// buildSectionCard creates a labelled card matching the Snapshots tab visual style.
func (w *ScriptsTabWidget) buildSectionCard(eyebrow string, trailingAction fyne.CanvasObject, body fyne.CanvasObject) fyne.CanvasObject {
	var header fyne.CanvasObject
	if eyebrow != "" && trailingAction != nil {
		eyebrowText := view.NewBrandText(strings.ToUpper(eyebrow), 11, design.ColorTextMuted, true)
		header = view.NewInset(
			container.NewBorder(nil, nil, eyebrowText, trailingAction, nil),
			4, 4, 0, 6,
		)
	} else if eyebrow != "" {
		eyebrowText := view.NewBrandText(strings.ToUpper(eyebrow), 11, design.ColorTextMuted, true)
		header = view.NewInset(eyebrowText, 6, 6, 0, 6)
	} else if trailingAction != nil {
		header = view.NewInset(
			container.NewBorder(nil, nil, nil, trailingAction, nil),
			4, 4, 0, 6,
		)
	}

	card := view.NewInset(
		view.NewCompactSurfacePanel(body, design.ColorSurface, design.RadiusMD+2),
		0, 0, 0, 3,
	)
	if header == nil {
		return card
	}
	return container.NewVBox(header, card)
}

// ─── MCP Proxy ────────────────────────────────────────────────────────────────

func (w *ScriptsTabWidget) toggleMCPProxy() {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()

	if w.mcpProxy.Running() {
		w.mcpProxy.Stop()
	} else {
		if client == nil {
			return
		}
		if err := w.mcpProxy.Start(w.mcpPort, client); err != nil {
			view.ShowErrorDialog(err, w.window)
			return
		}
	}
	w.refreshMCPStatus()
}

func (w *ScriptsTabWidget) refreshMCPStatus() {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()

	running := w.mcpProxy.Running()
	if running {
		w.mcpURLLabel.Text = fmt.Sprintf("http://127.0.0.1:%d/api/mcp", w.mcpProxy.Port())
		w.mcpURLLabel.Color = design.ColorAccent
		w.mcpToggleBtn.SetText("Stop")
		w.mcpToggleBtn.Importance = widget.MediumImportance
		w.mcpToggleBtn.Enable()
		w.mcpCopyBtn.Enable()
	} else {
		w.mcpURLLabel.Text = fmt.Sprintf("http://127.0.0.1:%d/api/mcp", w.mcpPort)
		w.mcpURLLabel.Color = design.ColorTextMuted
		w.mcpToggleBtn.SetText("Start")
		w.mcpToggleBtn.Importance = widget.MediumImportance
		if client != nil {
			w.mcpToggleBtn.Enable()
		} else {
			w.mcpToggleBtn.Disable()
		}
		w.mcpCopyBtn.Disable()
	}
	w.mcpURLLabel.Refresh()
	w.mcpToggleBtn.Refresh()
	w.mcpCopyBtn.Refresh()
}

// ─── Scripts list ─────────────────────────────────────────────────────────────

func (w *ScriptsTabWidget) showEmptyScriptsState() {
	msg := canvas.NewText("Not connected", design.ColorTextMuted)
	msg.TextSize = 13
	msg.Alignment = fyne.TextAlignCenter
	w.scriptsBodyContainer.Objects = []fyne.CanvasObject{
		view.NewInset(container.NewCenter(msg), 20, 20, 20, 20),
	}
	w.scriptsBodyContainer.Refresh()
}

func (w *ScriptsTabWidget) refreshScriptsList() {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()

	if client == nil {
		fyne.Do(func() { w.showEmptyScriptsState() })
		return
	}

	go func() {
		scripts, err := client.ListScripts()
		if err != nil {
			if w.window != nil {
				view.ShowErrorDialog(err, w.window)
			}
			return
		}
		fyne.Do(func() {
			w.rowUpdaters = make(map[string]func(bool, string))
			rows := make([]fyne.CanvasObject, 0, len(scripts))
			for _, s := range scripts {
				rows = append(rows, w.buildScriptRow(s))
			}
			if len(rows) == 0 {
				empty := canvas.NewText("No scripts found", design.ColorTextMuted)
				empty.TextSize = 13
				empty.Alignment = fyne.TextAlignCenter
				rows = append(rows, view.NewInset(container.NewCenter(empty), 16, 16, 16, 16))
			}
			w.scriptsBodyContainer.Objects = rows
			w.scriptsBodyContainer.Refresh()
		})
	}()
}

func (w *ScriptsTabWidget) buildScriptRow(s models.ScriptInfo) fyne.CanvasObject {
	name := s.Name
	if name == "" {
		name = filepath.Base(s.Path)
	}

	nameLabel := view.NewBrandText(name, 14, design.ColorTextLight, true)

	var srcIconRes fyne.Resource
	if strings.HasPrefix(s.Path, "/mnt/sdcard") || strings.HasPrefix(s.Path, "/mnt/sd/") {
		srcIconRes = assets.SDCardIcon
	} else {
		srcIconRes = assets.MemoryChipIcon
	}
	srcIcon := canvas.NewImageFromResource(srcIconRes)
	srcIcon.SetMinSize(fyne.NewSize(14, 14))
	srcIcon.FillMode = canvas.ImageFillContain

	statusDot := canvas.NewCircle(color.Transparent)
	statusDot.Move(fyne.NewPos(0, 0))
	statusDot.Resize(fyne.NewSize(8, 8))

	nameRow := container.NewHBox(srcIcon, nameLabel, statusDot)

	runBtn := widget.NewButtonWithIcon("", theme.MediaPlayIcon(), nil)
	runBtn.Importance = widget.LowImportance

	stopBtn := widget.NewButtonWithIcon("", theme.MediaStopIcon(), nil)
	stopBtn.Importance = widget.LowImportance
	stopBtn.Hide()

	runBtn.OnTapped = func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return
		}
		if err := client.RunScript(s.Path); err != nil {
			view.ShowErrorDialog(err, w.window)
		}
	}
	stopBtn.OnTapped = func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return
		}
		if err := client.StopScript(s.Path); err != nil {
			view.ShowErrorDialog(err, w.window)
		}
	}

	logBtn := widget.NewButtonWithIcon("", theme.ListIcon(), func() {
		time.AfterFunc(40*time.Millisecond, func() {
			fyne.Do(func() { w.showScriptLogDialog(s.Path, name) })
		})
	})
	logBtn.Importance = widget.LowImportance

	editBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		w.showScriptEditor(s.Path, s.Name, w.refreshScriptsList)
	})
	editBtn.Importance = widget.LowImportance

	deleteBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), func() {
		view.ShowConfirmYesLeftDanger("Delete Script", fmt.Sprintf("Delete \"%s\"?", name), func(ok bool) {
			if !ok {
				return
			}
			w.mu.Lock()
			client := w.usbClient
			w.mu.Unlock()
			if client == nil {
				return
			}
			if err := client.DeleteScript(s.Path); err != nil {
				view.ShowErrorDialog(err, w.window)
			} else {
				w.refreshScriptsList()
			}
		}, w.window)
	})
	deleteBtn.Importance = widget.LowImportance

	w.rowUpdaters[s.Path] = func(running bool, errStr string) {
		if running {
			statusDot.FillColor = color.NRGBA{R: 0x4c, G: 0xd9, B: 0x64, A: 0xff}
			runBtn.Hide()
			stopBtn.Show()
		} else if errStr != "" {
			statusDot.FillColor = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
			runBtn.Show()
			stopBtn.Hide()
		} else {
			statusDot.FillColor = color.Transparent
			runBtn.Show()
			stopBtn.Hide()
		}
		statusDot.Refresh()
	}

	btns := container.NewHBox(logBtn, runBtn, stopBtn, editBtn, deleteBtn)
	return view.NewCompactSurfacePanel(
		view.NewInset(container.NewBorder(nil, nil, nil, btns, nameRow), 8, 12, 4, 4),
		design.ColorGray950,
		design.RadiusMD,
	)
}

// ─── Background polling ───────────────────────────────────────────────────────

func (w *ScriptsTabWidget) startStatusPoll() {
	w.mu.Lock()
	stop := make(chan struct{})
	w.stopPollCh = stop
	w.mu.Unlock()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}

			w.mu.Lock()
			client := w.usbClient
			w.mu.Unlock()
			if client == nil {
				return
			}

			statuses, err := client.GetScriptStatus()
			if err != nil {
				continue
			}
			runMap := make(map[string]models.ScriptRunStatus, len(statuses))
			for _, st := range statuses {
				runMap[st.Path] = st
			}
			fyne.Do(func() {
				for path, upd := range w.rowUpdaters {
					if st, ok := runMap[path]; ok {
						upd(st.Running, st.Error)
					} else {
						upd(false, "")
					}
				}
			})
		}
	}()
}

func (w *ScriptsTabWidget) stopStatusPoll() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.stopPollCh != nil {
		close(w.stopPollCh)
		w.stopPollCh = nil
	}
}

// ─── Script dialogs (moved from PCPanelWidget) ───────────────────────────────

func scriptSafeName(raw string) string {
	name := strings.TrimSuffix(strings.TrimSpace(raw), ".star")
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '_'
	}, name)
}

func (w *ScriptsTabWidget) showNewScriptDialog(dir string, onCreated func()) {
	if w.window == nil {
		return
	}

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("my_script")

	descEntry := widget.NewEntry()
	descEntry.SetPlaceHolder("What does this script do?")

	hintLabel := canvas.NewText("", design.ColorTextMuted)
	hintLabel.TextSize = 11

	var createBtn *widget.Button
	var popup *widget.PopUp

	validateName := func(raw string) (safe string, ok bool) {
		safe = scriptSafeName(raw)
		if safe == "" || strings.Trim(safe, "_-") == "" {
			hintLabel.Text = "Enter a valid name (letters, digits, _ -)"
			hintLabel.Color = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
			hintLabel.Refresh()
			if createBtn != nil {
				createBtn.Disable()
			}
			return "", false
		}
		hintLabel.Text = "→ will be saved as:  " + safe + ".star"
		hintLabel.Color = design.ColorTextMuted
		hintLabel.Refresh()
		if createBtn != nil {
			createBtn.Enable()
		}
		return safe, true
	}

	nameEntry.OnChanged = func(raw string) { validateName(raw) }

	createScript := func() {
		safe, ok := validateName(nameEntry.Text)
		if !ok {
			return
		}
		desc := strings.TrimSpace(descEntry.Text)
		if desc == "" {
			desc = "No description"
		}
		path := dir + safe + ".star"
		tmpl := fmt.Sprintf("# name: %s\n# desc: %s\n\ndef main():\n    pass\n\nmain()\n", safe, desc)
		if popup != nil {
			popup.Hide()
		}
		time.AfterFunc(50*time.Millisecond, func() {
			fyne.Do(func() { w.showScriptEditorWithContent(path, safe, tmpl, onCreated) })
		})
	}

	nameEntry.OnSubmitted = func(_ string) { createScript() }

	titleText := view.NewBrandText("New Script", 17, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter

	validateName("")

	createBtn = widget.NewButton("Create & Edit", func() { createScript() })
	createBtn.Importance = widget.HighImportance
	createBtn.Disable()

	cancelBtn := widget.NewButton("Cancel", func() {
		if popup != nil {
			popup.Hide()
		}
	})

	closeBtn := newPCPanelDialogCloseButton(func() {
		if popup != nil {
			popup.Hide()
		}
	})
	titleBar := container.NewBorder(nil, nil, nil, closeBtn, container.NewCenter(titleText))

	nameLbl := canvas.NewText("Name (.star)  *", design.ColorTextLight)
	nameLbl.TextSize = 12
	nameLbl.TextStyle = fyne.TextStyle{Bold: true}
	descLbl := canvas.NewText("Description", design.ColorTextLight)
	descLbl.TextSize = 12
	descLbl.TextStyle = fyne.TextStyle{Bold: true}

	body := container.NewVBox(
		titleBar,
		widget.NewSeparator(),
		view.NewInset(container.NewVBox(
			nameLbl, nameEntry, hintLabel,
			widget.NewSeparator(),
			descLbl, descEntry,
		), 0, 0, 8, 4),
		widget.NewSeparator(),
		container.NewHBox(layout.NewSpacer(), cancelBtn, createBtn),
	)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	panel := container.NewStack(bg, view.NewInset(body, 18, 18, 16, 16), border)

	popup = view.ShowOverlayPopup(w.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			panelMin := panel.MinSize()
			pw := minFloat32(maxFloat32(panelMin.Width, 360), canvasSize.Width-48)
			ph := minFloat32(maxFloat32(panelMin.Height, 0), canvasSize.Height-48)
			return fyne.NewSize(pw, ph)
		},
	})
}

func (w *ScriptsTabWidget) showScriptLogDialog(path, displayName string) {
	if w.window == nil {
		return
	}
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()
	if client == nil {
		return
	}

	titleText := view.NewBrandText("> "+displayName+" log", 15, design.ColorAccent, true)

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

	closeBtn := newPCPanelDialogCloseButton(func() {
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
	header := container.NewVBox(view.NewInset(headerContent, 0, 0, 8, 8), headerDivider)

	footerDivider := canvas.NewRectangle(design.ColorBorder)
	footerDivider.SetMinSize(fyne.NewSize(0, 1))
	footer := container.NewVBox(footerDivider, view.NewInset(
		container.NewHBox(clearBtn, layout.NewSpacer()),
		0, 0, 8, 8,
	))

	body := container.NewBorder(header, footer, nil, nil, logScroll)

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	accent := canvas.NewRectangle(color.Transparent)
	accent.CornerRadius = design.RadiusMD
	accent.StrokeColor = design.ColorBorder
	accent.StrokeWidth = 1
	panel := container.NewStack(bg, view.NewInset(body, 16, 16, 12, 12), accent)

	popup = view.ShowOverlayPopup(w.window, view.OverlayPopupSpec{
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

func (w *ScriptsTabWidget) showScriptEditor(path, name string, onClose func()) {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()
	if client == nil {
		return
	}
	content, err := client.GetScriptContent(path)
	if err != nil {
		view.ShowErrorDialog(err, w.window)
		return
	}
	w.showScriptEditorWithContent(path, name, content, onClose)
}

func (w *ScriptsTabWidget) showScriptEditorWithContent(path, name, content string, onClose func()) {
	displayName := name
	if displayName == "" {
		displayName = filepath.Base(path)
	}

	var popup *widget.PopUp
	var debounceTimer *time.Timer
	var editorScroll *container.Scroll

	closePopup := func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		if popup != nil {
			popup.Hide()
		}
		if onClose != nil {
			onClose()
		}
	}

	editor := widget.NewMultiLineEntry()
	editor.SetText(content)
	editor.TextStyle = fyne.TextStyle{Monospace: true}
	editor.Wrapping = fyne.TextWrapOff
	editor.Scroll = fyne.ScrollNone

	richView := widget.NewRichText()
	richView.Wrapping = fyne.TextWrapOff

	refreshHighlight := func(text string) {
		richView.Segments = starlarkHighlight(text)
		richView.Refresh()
	}
	refreshHighlight(content)

	editor.OnChanged = func(text string) {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
		debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
			fyne.Do(func() { refreshHighlight(text) })
		})
	}

	editor.OnCursorChanged = func() {
		if editorScroll == nil {
			return
		}
		th := fyne.CurrentApp().Settings().Theme()
		textSize := th.Size(theme.SizeNameText)
		lineHeight := fyne.MeasureText("M", textSize, fyne.TextStyle{Monospace: true}).Height +
			th.Size(theme.SizeNameLineSpacing)
		cursorTop := float32(editor.CursorRow) * lineHeight
		cursorBot := cursorTop + lineHeight
		off := editorScroll.Offset
		viewH := editorScroll.Size().Height
		if cursorTop < off.Y {
			editorScroll.ScrollToOffset(fyne.NewPos(off.X, cursorTop))
		} else if cursorBot > off.Y+viewH {
			editorScroll.ScrollToOffset(fyne.NewPos(off.X, cursorBot-viewH))
		}
	}

	overlayTheme := &transparentEntryTheme{fyne.CurrentApp().Settings().Theme()}
	editorStack := container.NewStack(richView, container.NewThemeOverride(editor, overlayTheme))
	editorScroll = container.NewScroll(editorStack)

	titleLabel := view.NewBrandText("> "+displayName, 13, design.ColorAccent, true)
	pathLabel := widget.NewLabel(path)
	pathLabel.TextStyle = fyne.TextStyle{Monospace: true}
	pathLabel.Importance = widget.LowImportance

	closeBtn := newPCPanelDialogCloseButton(func() {
		if popup != nil {
			popup.Hide()
		}
	})
	headerContent := container.NewBorder(nil, nil, nil, closeBtn,
		container.NewVBox(titleLabel, pathLabel),
	)
	headerDivider := canvas.NewRectangle(design.ColorBorder)
	headerDivider.SetMinSize(fyne.NewSize(0, 1))
	header := container.NewVBox(view.NewInset(headerContent, 0, 0, 8, 8), headerDivider)

	cancelBtn := widget.NewButton("Cancel", func() {
		if popup != nil {
			popup.Hide()
		}
	})

	saveBtn := widget.NewButton("Save", func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return
		}
		if err := client.SaveScript(path, editor.Text); err != nil {
			view.ShowErrorDialog(err, w.window)
		}
	})

	okBtn := widget.NewButton("OK", func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return
		}
		if err := client.SaveScript(path, editor.Text); err != nil {
			view.ShowErrorDialog(err, w.window)
		} else {
			closePopup()
		}
	})
	okBtn.Importance = widget.HighImportance

	runBtn := widget.NewButtonWithIcon("Run", theme.MediaPlayIcon(), func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return
		}
		if err := client.SaveScript(path, editor.Text); err != nil {
			view.ShowErrorDialog(err, w.window)
			return
		}
		if err := client.RunScript(path); err != nil {
			view.ShowErrorDialog(err, w.window)
		} else {
			closePopup()
		}
	})

	footerDivider := canvas.NewRectangle(design.ColorBorder)
	footerDivider.SetMinSize(fyne.NewSize(0, 1))
	footerBtns := container.NewHBox(layout.NewSpacer(), cancelBtn, saveBtn, okBtn, runBtn)
	footer := container.NewVBox(footerDivider, view.NewInset(footerBtns, 0, 0, 8, 8))

	body := container.NewBorder(header, footer, nil, nil, editorScroll)

	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	accent := canvas.NewRectangle(color.Transparent)
	accent.CornerRadius = design.RadiusMD
	accent.StrokeColor = design.ColorAccent
	accent.StrokeWidth = 1
	panel := container.NewStack(bg, view.NewInset(body, 16, 16, 12, 12), accent)

	popup = view.ShowOverlayPopup(w.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x88},
		PanelSize: func(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
			const margin float32 = 16
			return fyne.NewSize(canvasSize.Width-margin*2, canvasSize.Height-margin*2)
		},
	})
}

// fillWidthVBoxLayout stacks objects vertically like container.NewVBox but
// always reports MinSize.Width = 0. This prevents any child's minimum width
// from causing horizontal overflow inside a VScroll on narrow screens.
type fillWidthVBoxLayout struct{ gap float32 }

func (l *fillWidthVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		h := o.MinSize().Height
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, h))
		y += h + l.gap
	}
}

func (l *fillWidthVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	visible := 0
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		h += o.MinSize().Height
		visible++
	}
	if visible > 1 {
		h += l.gap * float32(visible-1)
	}
	return fyne.NewSize(0, h)
}

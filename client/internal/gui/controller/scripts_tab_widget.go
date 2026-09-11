package controller

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/api"
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
// and Automation Scripts management in a Devices-style two-column layout.
type ScriptsTabWidget struct {
	window fyne.Window
	mu     sync.Mutex

	usbClient *api.USBClient
	mcpProxy  api.MCPProxy
	mcpPort   int
	agentOS   string // OS reported by the connected agent (empty/"usbridge" = real hardware)

	outerContainer *fyne.Container
	body           *fyne.Container
	busySpinner    *view.DeviceDashboardBusySpinner

	scripts          []models.ScriptInfo
	lastStatus       map[string]models.ScriptRunStatus
	lockedMessage    string
	newScriptEnabled bool

	// Per-script status updaters, keyed by path; rebuilt alongside the table.
	rowUpdaters map[string]func(bool, string)

	// Stop channel for the background script-status polling goroutine.
	stopPollCh chan struct{}
	isClosing  atomic.Bool
}

// NewScriptsTabWidget creates the widget and builds the persistent UI tree.
func NewScriptsTabWidget(window fyne.Window) *ScriptsTabWidget {
	w := &ScriptsTabWidget{
		window:      window,
		mcpPort:     api.DefaultMCPProxyPort,
		rowUpdaters: make(map[string]func(bool, string)),
		lastStatus:  make(map[string]models.ScriptRunStatus),
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
//
// MCP is agent-agnostic: the proxy just forwards signed /api/mcp requests
// to whatever client is set, and both the hardware KVM and the software
// Agent (agent/internal/api's mcp() handler) now answer it -- the earlier
// whole-tab lock predated the Agent having an MCP server to talk to at all,
// and blocked it along with the genuinely hardware-only Scripts section
// below. Only that Scripts section still checks agentOS.
func (w *ScriptsTabWidget) SetClient(c *api.USBClient) {
	w.stopStatusPoll()

	w.mu.Lock()
	w.usbClient = c
	w.agentOS = ""
	w.mu.Unlock()

	// Keep an already-running proxy pointed at the current device
	// connection. Start() only wires p.client in on first launch (a manual
	// "enable" toggle) and no-ops on every later call, so without this an
	// already-running proxy would keep signing/forwarding MCP calls with
	// whatever client (and master key) was active when it was first
	// started -- surviving reconnects and even a key change in the
	// connection manager.
	w.mcpProxy.UpdateClient(c)

	if w.isClosing.Load() {
		return
	}

	if c == nil {
		fyne.Do(func() { w.showScriptsLocked("Not connected") })
		return
	}

	fyne.Do(func() {
		w.lockedMessage = ""
		w.newScriptEnabled = false
		w.rebuild()
	})

	go func() {
		agentOS := ""
		if info, err := c.GetDeviceInfo(); err == nil && info != nil {
			agentOS = info.AgentOS
		}

		w.mu.Lock()
		stillCurrent := w.usbClient == c
		if stillCurrent {
			w.agentOS = agentOS
		}
		w.mu.Unlock()
		if !stillCurrent || w.isClosing.Load() {
			return // superseded by a newer SetClient call, or the app is quitting
		}

		if !isUSBridgeAgentOS(agentOS) {
			if w.isClosing.Load() {
				return
			}
			fyne.Do(func() { w.showScriptsLocked("Scripts are available on USBridge hardware only.") })
			return
		}

		w.refreshScriptsList()
		w.startStatusPoll()
	}()
}

// showScriptsLocked replaces the Scripts table with a centered notice
// and disables New Script -- SD/eMMC storage doesn't exist on a software
// Agent. The MCP card stays fully usable regardless of agent type.
func (w *ScriptsTabWidget) showScriptsLocked(msg string) {
	w.lockedMessage = msg
	w.newScriptEnabled = false
	w.scripts = nil
	w.rowUpdaters = make(map[string]func(bool, string))
	w.rebuild()
}

// ─── Build ────────────────────────────────────────────────────────────────────

func (w *ScriptsTabWidget) build() {
	w.busySpinner = view.NewDeviceDashboardBusySpinner()
	w.body = container.NewMax()
	footer := view.NewDeviceDashboardFooter(view.AppVersion(), nil, w.busySpinner)
	w.outerContainer = container.NewBorder(nil, footer, nil, nil, w.body)
	w.lockedMessage = "Not connected"
	if app := fyne.CurrentApp(); app != nil {
		w.applyLocalUIParseSetting(app.Preferences().Bool(localUIParseEnabledPrefKey))
	}
	w.rebuild()
}

func (w *ScriptsTabWidget) rebuild() {
	if w.body == nil {
		return
	}
	w.body.Objects = []fyne.CanvasObject{view.NewScriptsSection(w.sectionData())}
	w.body.Refresh()
}

func (w *ScriptsTabWidget) rebuildSoon() {
	time.AfterFunc(10*time.Millisecond, func() {
		if w.isClosing.Load() {
			return
		}
		fyne.Do(w.rebuild)
	})
}

func (w *ScriptsTabWidget) setBusy(busy bool) {
	if w.busySpinner == nil {
		return
	}
	if busy {
		w.busySpinner.Start()
		return
	}
	w.busySpinner.Stop()
}

func (w *ScriptsTabWidget) sectionData() view.ScriptsSectionData {
	w.mu.Lock()
	client := w.usbClient
	scripts := append([]models.ScriptInfo(nil), w.scripts...)
	status := w.lastStatus
	locked := w.lockedMessage
	newEnabled := w.newScriptEnabled
	w.mu.Unlock()

	localUI := false
	if app := fyne.CurrentApp(); app != nil {
		localUI = app.Preferences().Bool(localUIParseEnabledPrefKey)
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/api/mcp", w.mcpPort)
	if w.mcpProxy.Running() {
		url = fmt.Sprintf("http://127.0.0.1:%d/api/mcp", w.mcpProxy.Port())
	}

	rows := make([]view.ScriptTableRow, 0, len(scripts))
	w.rowUpdaters = make(map[string]func(bool, string), len(scripts))
	for _, s := range scripts {
		script := s
		st := status[script.Path]
		path := script.Path
		name := script.Name
		if name == "" {
			name = filepath.Base(path)
		}
		rows = append(rows, view.ScriptTableRow{
			Name:     name,
			Source:   scriptSourceLabel(path),
			Running:  st.Running,
			Error:    st.Error,
			OnRun:    func() { w.runScript(path) },
			OnStop:   func() { w.stopScript(path) },
			OnLog:    func() { w.openScriptLog(path, name) },
			OnEdit:   func() { w.showScriptEditor(path, name, w.refreshScriptsList) },
			OnDelete: func() { w.deleteScript(path, name) },
			BindStatus: func(upd func(bool, string)) {
				w.rowUpdaters[path] = upd
			},
		})
	}

	return view.ScriptsSectionData{
		MCP: view.ScriptsMCPData{
			URL:     url,
			Running: w.mcpProxy.Running(),
			Enabled: client != nil,
			LocalUI: localUI,
			OnToggle: func() {
				w.toggleMCPProxy()
			},
			OnCopy: func() {
				if w.window != nil && w.window.Clipboard() != nil {
					w.window.Clipboard().SetContent(url)
				}
			},
			OnLocalUI: func(on bool) {
				if app := fyne.CurrentApp(); app != nil {
					app.Preferences().SetBool(localUIParseEnabledPrefKey, on)
				}
				w.applyLocalUIParseSetting(on)
				w.rebuildSoon()
			},
		},
		ScriptCount:   len(scripts),
		NewEnabled:    newEnabled,
		OnNewEMMC:     func() { w.showNewScriptDialog(w.refreshScriptsList, false) },
		OnNewSD:       func() { w.showNewScriptDialog(w.refreshScriptsList, true) },
		Rows:          rows,
		LockedMessage: locked,
	}
}

func scriptSourceLabel(path string) string {
	if strings.Contains(path, "/sdcard") || strings.Contains(path, "/mnt/sd/") {
		return "SD"
	}
	return "eMMC"
}

func (w *ScriptsTabWidget) runScript(path string) {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()
	if client == nil {
		return
	}
	if err := client.RunScript(path); err != nil {
		view.ShowErrorDialog(err, w.window)
	}
}

func (w *ScriptsTabWidget) stopScript(path string) {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()
	if client == nil {
		return
	}
	if err := client.StopScript(path); err != nil {
		view.ShowErrorDialog(err, w.window)
	}
}

func (w *ScriptsTabWidget) openScriptLog(path, name string) {
	time.AfterFunc(40*time.Millisecond, func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		fyne.Do(func() { view.ShowScriptLogDialog(w.window, client, path, name) })
	})
}

func (w *ScriptsTabWidget) deleteScript(path, name string) {
	view.ShowConfirmToast(fmt.Sprintf("Delete %s? This cannot be undone.", name), func(ok bool) {
		if !ok {
			return
		}
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return
		}
		if err := client.DeleteScript(path); err != nil {
			view.ShowErrorDialog(err, w.window)
		} else {
			w.refreshScriptsList()
		}
	}, w.window)
}

// ─── MCP Proxy ────────────────────────────────────────────────────────────────

const localUIParseEnabledPrefKey = "local_ui_parse_enabled"

// applyLocalUIParseSetting wires (or unwires) the local ui.parse backend
// immediately -- the MCP proxy's interceptor (tryLocalUIParse) reads the
// installed parser live on every request, so no proxy restart is needed
// either way. Building the ONNX sessions takes up to a few seconds, so
// enabling runs in the background; ui.parse keeps forwarding to the device
// until it's ready.
func (w *ScriptsTabWidget) applyLocalUIParseSetting(enabled bool) {
	if !enabled {
		api.SetLocalUIParser(nil)
		return
	}
	cfg := models.DefaultConfig()
	cfg.LocalUIParseEnabled = true
	api.InitLocalUIParseFromConfig(cfg)
}

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
	w.rebuildSoon()
}

func (w *ScriptsTabWidget) refreshScriptsList() {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()

	if client == nil {
		if w.isClosing.Load() {
			return
		}
		fyne.Do(func() { w.showScriptsLocked("Not connected") })
		return
	}

	if w.isClosing.Load() {
		return
	}
	fyne.Do(func() { w.setBusy(true) })
	go func() {
		scripts, err := client.ListScripts()
		if w.isClosing.Load() {
			return
		}
		fyne.Do(func() { w.setBusy(false) })
		if err != nil {
			if w.window != nil {
				view.ShowErrorDialog(err, w.window)
			}
			return
		}
		if w.isClosing.Load() {
			return
		}
		fyne.Do(func() {
			w.mu.Lock()
			w.scripts = scripts
			w.lockedMessage = ""
			w.newScriptEnabled = true
			w.mu.Unlock()
			w.rebuild()
		})
	}()
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
			if client == nil || w.isClosing.Load() {
				return
			}

			statuses, err := client.GetScriptStatus()
			if err != nil || w.isClosing.Load() {
				continue
			}
			runMap := make(map[string]models.ScriptRunStatus, len(statuses))
			for _, st := range statuses {
				runMap[st.Path] = st
			}
			if w.isClosing.Load() {
				continue
			}
			fyne.Do(func() {
				if w.isClosing.Load() {
					return
				}
				w.mu.Lock()
				w.lastStatus = runMap
				w.mu.Unlock()
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

// Shutdown stops the script-status poller and the local MCP HTTP server
// without queueing Fyne UI work. Call this before app.Quit -- a live poller
// or MCP listener otherwise keeps the process (or the Fyne loop) alive
// after "quitting app".
func (w *ScriptsTabWidget) Shutdown() {
	w.isClosing.Store(true)
	w.stopStatusPoll()
	w.mu.Lock()
	w.usbClient = nil
	w.mu.Unlock()
	w.mcpProxy.UpdateClient(nil)
	w.mcpProxy.Stop()
	api.SetLocalUIParser(nil)
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

func (w *ScriptsTabWidget) showNewScriptDialog(onCreated func(), sdCard bool) {
	if w.window == nil {
		return
	}

	dir := "/mnt/emmc/scripts/"
	title := "New eMMC Script"
	if sdCard {
		dir = "/mnt/sdcard/scripts/"
		title = "New SD Script"
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

	titleText := view.NewBrandText(title, 17, design.ColorTextLight, true)
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

	closeBtn := view.NewDialogCloseButton(func() {
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

	closeBtn := view.NewDialogCloseButton(func() {
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

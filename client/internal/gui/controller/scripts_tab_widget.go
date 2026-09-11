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
	busySpinner     *view.DeviceDashboardBusySpinner
	connectingHint  *view.DeviceDashboardBusySpinner
	footerChip      *view.ScriptFooterStatus
	footerChips     []*view.ScriptFooterStatus

	scripts          []models.ScriptInfo
	lastStatus       map[string]models.ScriptRunStatus
	lockedMessage    string
	newScriptEnabled bool
	footerWasRunning bool
	footerShowDone   bool
	footerDismissed  bool

	// Per-script status updaters, keyed by path; rebuilt alongside the table.
	rowUpdaters map[string]func(bool, string)

	// editorStatusHook is set while the script editor is open so Run/Stop
	// in that dialog can follow the same live status as the table row.
	editorStatusHook func(path string, running bool, errStr string)

	// ignoreErrorUntilRun suppresses a sticky device Error after the user
	// taps Stop, until they start that script again.
	ignoreErrorUntilRun map[string]bool

	// Stop channel for the background script-status polling goroutine.
	stopPollCh chan struct{}
	isClosing  atomic.Bool
}

// NewScriptsTabWidget creates the widget and builds the persistent UI tree.
func NewScriptsTabWidget(window fyne.Window) *ScriptsTabWidget {
	w := &ScriptsTabWidget{
		window:              window,
		mcpPort:             api.DefaultMCPProxyPort,
		rowUpdaters:         make(map[string]func(bool, string)),
		lastStatus:          make(map[string]models.ScriptRunStatus),
		ignoreErrorUntilRun: make(map[string]bool),
	}
	w.build()
	return w
}

// GetContainer returns the permanent container used as the tab content.
func (w *ScriptsTabWidget) GetContainer() *fyne.Container {
	return w.outerContainer
}

// ConnectingHint is this tab's "connecting device" spinner, driven by
// DiskWidget so gadget mount/unmount is visible from Scripts too.
func (w *ScriptsTabWidget) ConnectingHint() *view.DeviceDashboardBusySpinner {
	if w == nil {
		return nil
	}
	return w.connectingHint
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
	w.mu.Lock()
	w.lastStatus = make(map[string]models.ScriptRunStatus)
	w.ignoreErrorUntilRun = make(map[string]bool)
	w.footerWasRunning = false
	w.footerShowDone = false
	w.footerDismissed = false
	w.mu.Unlock()
	w.rebuild()
	w.syncFooterStatus()
}

// AttachFooterStatus registers an extra footer chip (Devices / Snapshots /
// Control) so script run state is visible from every connected-session tab.
func (w *ScriptsTabWidget) AttachFooterStatus(chip *view.ScriptFooterStatus) {
	if chip == nil {
		return
	}
	chip.SetOnDismiss(w.dismissFooterHint)
	w.mu.Lock()
	w.footerChips = append(w.footerChips, chip)
	kind := w.computeFooterKindLocked()
	w.mu.Unlock()
	chip.SetKind(kind)
}

func (w *ScriptsTabWidget) dismissFooterHint() {
	w.mu.Lock()
	w.footerDismissed = true
	w.footerShowDone = false
	w.footerWasRunning = false
	w.mu.Unlock()
	w.syncFooterStatus()
}

func (w *ScriptsTabWidget) computeFooterKindLocked() view.ScriptFooterKind {
	anyRunning := false
	anyErr := false
	for _, st := range w.lastStatus {
		if st.Running {
			anyRunning = true
			continue
		}
		if strings.TrimSpace(st.Error) != "" {
			anyErr = true
		}
	}
	if anyRunning {
		w.footerDismissed = false
		w.footerWasRunning = true
		w.footerShowDone = false
		return view.ScriptFooterRunning
	}
	if w.footerDismissed {
		return view.ScriptFooterIdle
	}
	if anyErr {
		w.footerWasRunning = false
		w.footerShowDone = false
		return view.ScriptFooterError
	}
	if w.footerWasRunning {
		w.footerWasRunning = false
		w.footerShowDone = true
	}
	if w.footerShowDone {
		return view.ScriptFooterDone
	}
	return view.ScriptFooterIdle
}

func (w *ScriptsTabWidget) syncFooterStatus() {
	w.mu.Lock()
	kind := w.computeFooterKindLocked()
	chip := w.footerChip
	chips := append([]*view.ScriptFooterStatus(nil), w.footerChips...)
	w.mu.Unlock()
	if chip != nil {
		chip.SetKind(kind)
	}
	for _, c := range chips {
		if c != nil {
			c.SetKind(kind)
		}
	}
}

// ─── Build ────────────────────────────────────────────────────────────────────

func (w *ScriptsTabWidget) build() {
	w.busySpinner = view.NewDeviceDashboardBusySpinner()
	w.connectingHint = view.NewDeviceDashboardBusyHint("connecting device")
	w.footerChip = view.NewScriptFooterStatus()
	w.footerChip.SetOnDismiss(w.dismissFooterHint)
	w.body = container.NewMax()
	footer := view.NewAppFooter(view.AppVersion(), nil, w.busySpinner, w.connectingHint, w.footerChip)
	w.outerContainer = view.NewEdgeStack(nil, footer, w.body)
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
		return
	}
	w.mu.Lock()
	delete(w.ignoreErrorUntilRun, path)
	w.mu.Unlock()
	w.applyScriptStatus(path, true, "")
}

func (w *ScriptsTabWidget) stopScript(path string) {
	w.mu.Lock()
	client := w.usbClient
	w.mu.Unlock()
	if client == nil {
		return
	}
	if err := client.StopScript(path); err != nil && !isBenignScriptStopError(err) {
		view.ShowErrorDialog(err, w.window)
	}
	w.mu.Lock()
	if w.ignoreErrorUntilRun == nil {
		w.ignoreErrorUntilRun = make(map[string]bool)
	}
	w.ignoreErrorUntilRun[path] = true
	w.mu.Unlock()
	w.applyScriptStatus(path, false, "")
}

func isBenignScriptStopError(err error) bool {
	if err == nil {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not running") ||
		strings.Contains(s, "not found") ||
		strings.Contains(s, "no such")
}

// applyScriptStatus paints one row (and the open editor, if any) immediately,
// rather than waiting for the 2s status poll.
func (w *ScriptsTabWidget) applyScriptStatus(path string, running bool, errStr string) {
	w.mu.Lock()
	if w.lastStatus == nil {
		w.lastStatus = make(map[string]models.ScriptRunStatus)
	}
	st := w.lastStatus[path]
	st.Path = path
	st.Running = running
	st.Error = errStr
	w.lastStatus[path] = st
	upd := w.rowUpdaters[path]
	hook := w.editorStatusHook
	w.mu.Unlock()
	if upd != nil {
		upd(running, errStr)
	}
	if hook != nil {
		hook(path, running, errStr)
	}
	w.syncFooterStatus()
}

func (w *ScriptsTabWidget) openScriptLog(path, name string) {
	time.AfterFunc(40*time.Millisecond, func() {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		fyne.Do(func() { ShowScriptLogDialog(w.window, client, path, name) })
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
				for path, st := range runMap {
					if w.ignoreErrorUntilRun[path] {
						if st.Running {
							delete(w.ignoreErrorUntilRun, path)
						} else {
							st.Error = ""
							runMap[path] = st
						}
					}
				}
				w.lastStatus = runMap
				updaters := w.rowUpdaters
				hook := w.editorStatusHook
				w.mu.Unlock()
				for path, upd := range updaters {
					if st, ok := runMap[path]; ok {
						upd(st.Running, st.Error)
					} else {
						upd(false, "")
					}
				}
				if hook != nil {
					for path, st := range runMap {
						hook(path, st.Running, st.Error)
					}
				}
				w.syncFooterStatus()
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
	title := "New eMMC script"
	subtitle := "Starlark job stored on the device eMMC."
	if sdCard {
		dir = "/mnt/sdcard/scripts/"
		title = "New SD script"
		subtitle = "Starlark job stored on the SD card."
	}

	fields, nameEntry, descEntry := view.NewScriptCreateFieldsBox()

	hintColor := color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}
	hintLabel := canvas.NewText("", hintColor)
	hintLabel.TextSize = 9

	var createBtn *connectionDialogSecondaryButton
	var closePopup func()

	validateName := func(raw string) (safe string, ok bool) {
		safe = scriptSafeName(raw)
		if safe == "" || strings.Trim(safe, "_-") == "" {
			hintLabel.Text = "Enter a valid name (letters, digits, _ -)"
			hintLabel.Color = hintColor
			hintLabel.Refresh()
			if createBtn != nil {
				createBtn.SetDisabled(true)
			}
			return "", false
		}
		hintLabel.Text = "will be saved as  " + safe + ".star"
		hintLabel.Color = hintColor
		hintLabel.Refresh()
		if createBtn != nil {
			createBtn.SetDisabled(false)
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
		if closePopup != nil {
			closePopup()
		}
		time.AfterFunc(50*time.Millisecond, func() {
			fyne.Do(func() { w.showScriptEditorWithContent(path, safe, tmpl, onCreated) })
		})
	}

	nameEntry.OnSubmitted = func(_ string) { createScript() }

	createBtn = newScriptDialogTealButton("Create & Edit", nil, createScript)
	createBtn.SetDisabled(true)
	validateName("")

	form := container.NewVBox(
		fields,
		view.NewInset(hintLabel, 0, 0, 8, 4),
	)

	_, closePopup = showBrandedOverlayDialog(brandedOverlayDialogSpec{
		parent:        w.window,
		title:         title,
		subtitle:      subtitle,
		body:          form,
		rightButtons:  []fyne.CanvasObject{createBtn},
		compactFooter: true,
		panelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			return connectionDialogPanelSize(panel, canvasSize)
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

	var editorScroll *container.Scroll

	editor := &scriptEditorEntry{}
	editor.MultiLine = true
	editor.Wrapping = fyne.TextWrapOff
	editor.Scroll = fyne.ScrollNone
	editor.TextStyle = fyne.TextStyle{Monospace: true}
	editor.ExtendBaseWidget(editor)
	editor.SetText(content)

	richView := widget.NewRichText()
	richView.Wrapping = fyne.TextWrapOff

	refreshHighlight := func(text string) {
		richView.Segments = starlarkHighlight(text)
		richView.Refresh()
	}
	refreshHighlight(content)

	editor.OnChanged = func(text string) {
		refreshHighlight(text)
	}

	overlayTheme := &transparentEntryTheme{fyne.CurrentApp().Settings().Theme()}
	caret := canvas.NewRectangle(design.ColorConnectionBadgeText)
	caret.Hide()
	editorBox := container.New(&scriptEditorStackLayout{entry: editor}, richView, editor, caret)

	caretStop := make(chan struct{})
	var caretOnce sync.Once
	stopCaret := func() { caretOnce.Do(func() { close(caretStop) }) }
	go func() {
		ticker := time.NewTicker(530 * time.Millisecond)
		defer ticker.Stop()
		on := true
		for {
			select {
			case <-caretStop:
				return
			case <-ticker.C:
				on = !on
				fyne.Do(func() {
					if !editor.focused() {
						caret.Hide()
						caret.Refresh()
						return
					}
					if on {
						caret.Show()
					} else {
						caret.Hide()
					}
					caret.Refresh()
				})
			}
		}
	}()

	placeCaret := func() {
		lineH := fyne.MeasureText("M", scriptEditorTextSize, fyne.TextStyle{Monospace: true}).Height
		if lineH < 10 {
			lineH = 10
		}
		caret.Resize(fyne.NewSize(1.5, lineH))
		caret.Move(editor.CursorPosition())
		if editor.focused() {
			caret.Show()
		} else {
			caret.Hide()
		}
		caret.Refresh()
	}

	var placingCaret bool
	editor.OnCursorChanged = func() {
		if placingCaret {
			return
		}
		placingCaret = true
		placeCaret()
		placingCaret = false
		if editorScroll == nil {
			return
		}
		lineHeight := fyne.MeasureText("M", scriptEditorTextSize, fyne.TextStyle{Monospace: true}).Height +
			overlayTheme.Size(theme.SizeNameLineSpacing)
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

	editorStack := container.NewThemeOverride(editorBox, overlayTheme)
	editorScroll = container.NewScroll(editorStack)

	pathLabel := canvas.NewText(path, design.ColorConnectionsSectionSubtitle)
	pathLabel.TextSize = 10
	pathLabel.TextStyle.Monospace = true

	savedText := content
	saveScript := func() bool {
		w.mu.Lock()
		client := w.usbClient
		w.mu.Unlock()
		if client == nil {
			return false
		}
		if err := client.SaveScript(path, editor.Text); err != nil {
			view.ShowErrorDialog(err, w.window)
			return false
		}
		savedText = editor.Text
		return true
	}

	var closeEditor func()
	saveBtn := newScriptDialogTealButton("Save", scriptDialogFloppyIcon, func() {
		if !saveScript() {
			return
		}
		if closeEditor != nil {
			closeEditor()
		}
	})
	runBtn := newScriptDialogLimeButton("Run", scriptDialogPlayIcon, func() {
		if !saveScript() {
			return
		}
		w.runScript(path)
	})
	stopBtn := newScriptDialogLimeButton("Stop", scriptDialogStopIcon, func() {
		w.stopScript(path)
	})
	stopBtn.hoverIconRes = scriptDialogStopHoverIcon
	stopBtn.hoverTextColor = color.NRGBA{R: 0xfd, G: 0xa4, B: 0xaf, A: 0xff}
	stopBtn.hoverBorderColor = color.NRGBA{R: 0xfd, G: 0xa4, B: 0xaf, A: 0xff}
	copyBtn := newScriptDialogCopyIconButton(func() {
		if w.window != nil && w.window.Clipboard() != nil {
			w.window.Clipboard().SetContent(editor.Text)
		}
	})
	pasteBtn := newScriptDialogPasteIconButton(func() {
		view.PasteClipboardIntoEntry(&editor.Entry)
	})
	setEditorRunState := func(running bool, errStr string) {
		showStop := running || strings.TrimSpace(errStr) != ""
		if showStop {
			runBtn.Hide()
			stopBtn.Show()
		} else {
			stopBtn.Hide()
			runBtn.Show()
		}
		runBtn.Refresh()
		stopBtn.Refresh()
	}
	w.mu.Lock()
	st := w.lastStatus[path]
	w.editorStatusHook = func(p string, running bool, errStr string) {
		if p != path {
			return
		}
		setEditorRunState(running, errStr)
	}
	w.mu.Unlock()
	stopBtn.Hide()
	setEditorRunState(st.Running, st.Error)

	surface, setFocused := newScriptDialogFocusSurface(editorScroll)
	editor.onFocusChanged = func(on bool) {
		setFocused(on)
		if on {
			caret.Show()
		} else {
			caret.Hide()
		}
		caret.Refresh()
	}

	body := container.NewBorder(
		view.NewInset(pathLabel, 0, 0, 0, 8),
		nil, nil, nil,
		surface,
	)

	var confirmingClose bool
	_, closeEditor = showBrandedOverlayDialog(brandedOverlayDialogSpec{
		parent:       w.window,
		title:        "Edit script",
		subtitle:     displayName,
		body:         body,
		rightButtons: []fyne.CanvasObject{copyBtn, pasteBtn, saveBtn, runBtn, stopBtn},
		beforeClose: func(proceed func()) {
			if editor.Text == savedText {
				proceed()
				return
			}
			if confirmingClose {
				return
			}
			confirmingClose = true
			view.ShowConfirmToast("Changes will not be saved. Close anyway?", func(ok bool) {
				confirmingClose = false
				if ok {
					proceed()
				}
			}, w.window)
		},
		onClose: func() {
			w.mu.Lock()
			w.editorStatusHook = nil
			w.mu.Unlock()
			stopCaret()
			if onClose != nil {
				onClose()
			}
		},
	})
	if w.window != nil && w.window.Canvas() != nil {
		w.window.Canvas().Focus(editor)
	}
}

type scriptEditorEntry struct {
	widget.Entry
	onFocusChanged func(bool)
	hasFocus       bool
}

func (e *scriptEditorEntry) focused() bool { return e.hasFocus }

func (e *scriptEditorEntry) FocusGained() {
	e.hasFocus = true
	e.Entry.FocusGained()
	if e.onFocusChanged != nil {
		e.onFocusChanged(true)
	}
}

func (e *scriptEditorEntry) FocusLost() {
	e.hasFocus = false
	e.Entry.FocusLost()
	if e.onFocusChanged != nil {
		e.onFocusChanged(false)
	}
}

type scriptEditorStackLayout struct {
	entry *scriptEditorEntry
}

func (l *scriptEditorStackLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := len(objects)
	for i, o := range objects {
		if i == n-1 {
			continue
		}
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
	if n < 3 || l.entry == nil {
		return
	}
	caret := objects[n-1]
	lineH := fyne.MeasureText("M", scriptEditorTextSize, fyne.TextStyle{Monospace: true}).Height
	if lineH < 10 {
		lineH = 10
	}
	caret.Resize(fyne.NewSize(1.5, lineH))
	caret.Move(l.entry.CursorPosition())
}

func (l *scriptEditorStackLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(0, 0)
	n := len(objects)
	for i, o := range objects {
		if i == n-1 {
			continue
		}
		s := o.MinSize()
		if s.Width > min.Width {
			min.Width = s.Width
		}
		if s.Height > min.Height {
			min.Height = s.Height
		}
	}
	return min
}

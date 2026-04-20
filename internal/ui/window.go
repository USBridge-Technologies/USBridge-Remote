package ui

import (
	"context"
	"fmt"
	"image/color"
	"net"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
	qrcode "github.com/skip2/go-qrcode"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui/design"
)

type Window struct {
	app   fyne.App
	cfg   config.Config
	token interface {
		RegenerateFRPToken() (config.Config, error)
	}
	perms interface {
		AccessibilityGranted() bool
		ScreenRecordingGranted() bool
		RequestAccessibility() bool
		RequestScreenRecording() bool
		OpenPrivacySettings() error
	}
	ts interface {
		Status(context.Context) (*tailscale.Status, error)
		StartLogin(context.Context) (string, error)
		Logout(context.Context) error
		ApplyConfig(userspace bool, stateDir string) error
	}

	// UI components
	accessLabel *widget.Label
	accessBtn   *widget.Button
	screenLabel *widget.Label
	screenBtn   *widget.Button
	permInfo    *widget.Label

	tsInfo    *widget.Label
	tsPeers   *widget.RichText
	tsAuthBtn *widget.Button
	tsMode    *widget.Select

	statusInfo *widget.Label
}

type uiStatus struct {
	tsStatus      *tailscale.Status
	accessGranted bool
	screenGranted bool
}

func NewWindow(app fyne.App, cfg config.Config, perms *permissions.Service, ts *tailscale.Service, tokenManager interface {
	RegenerateFRPToken() (config.Config, error)
}) *Window {
	return &Window{app: app, cfg: cfg, perms: perms, ts: ts, token: tokenManager}
}

func (w *Window) applyTailscaleMode(mode string) {
	newMode := config.TailscaleModeUserspace
	if mode == "System" {
		newMode = config.TailscaleModeSystem
	}

	if w.cfg.TailscaleMode == newMode {
		return
	}

	w.cfg.TailscaleMode = newMode
	// Save config
	configPath := filepath.Join(w.cfg.StateDir, "config.yaml")
	_ = config.Save(configPath, w.cfg)

	if w.ts != nil {
		_ = w.ts.ApplyConfig(newMode == config.TailscaleModeUserspace, w.cfg.StateDir)
	}

	logrus.Infof("🛰️ [UI] Tailscale mode changed to %s", mode)
	w.performRefresh()
}

func (w *Window) ShowAndRun(onClose func()) {
	win := w.app.NewWindow("USBridge Agent")
	win.SetPadded(false)
	win.Resize(fyne.NewSize(640, 400))
	win.CenterOnScreen()

	tokenBtn := newIconActionButton("TOKEN", theme.SettingsIcon(), func() {
		w.showTokenDialog(win)
	})
	// Уменьшаем отступ между иконкой и текстом через локальную тему
	closeBtn := newDangerButton("CLOSE", func() { win.Close() })
	header := newHeaderBar(tokenBtn, closeBtn)

	// Column 1: Permissions
	accessLabelBase := "Accessibility"
	if runtime.GOOS == "linux" {
		accessLabelBase = "Input Control"
	}
	w.accessLabel = widget.NewLabel(accessLabelBase)
	w.accessBtn = widget.NewButton("Request", func() {
		if w.perms == nil {
			return
		}
		w.accessBtn.Disable()
		go func() {
			defer fyne.Do(func() {
				if w.accessBtn != nil {
					w.accessBtn.Enable()
				}
			})
			_ = w.perms.RequestAccessibility()
			w.performRefresh()
		}()
	})
	w.accessBtn.Importance = widget.HighImportance

	w.screenLabel = widget.NewLabel("Screen Recording")
	w.screenBtn = widget.NewButton("Request", func() {
		if w.perms != nil {
			_ = w.perms.RequestScreenRecording()
			w.performRefresh()
		}
	})
	w.screenBtn.Importance = widget.HighImportance
	w.permInfo = widget.NewLabel("")
	w.permInfo.Wrapping = fyne.TextWrapWord

	// Adjust for OS
	showButtons := runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && capture.GetLinuxEnv() == "Wayland")

	if !showButtons {
		w.accessBtn.Hide()
		w.screenBtn.Hide()
	} else {
		w.accessBtn.Resize(fyne.NewSize(80, 24))
		w.screenBtn.Resize(fyne.NewSize(80, 24))
		w.accessBtn.Show()
		w.screenBtn.Show()
	}

	permContent := newTightVBox(
		container.NewHBox(w.accessLabel, layout.NewSpacer(), w.accessBtn),
		container.NewHBox(w.screenLabel, layout.NewSpacer(), w.screenBtn),
	)
	if !showButtons {
		permContent = w.permInfo
	}
	permBlock := newPanel("Permissions", permContent)

	// Column 2: Stats & Tailscale
	displayCapture := w.cfg.VideoCapture
	if runtime.GOOS == "linux" {
		mode := strings.ToLower(strings.TrimSpace(displayCapture))
		if mode == "" || mode == "auto" || mode == "dxgi" || (mode == "x11grab" && capture.GetLinuxEnv() == "Wayland") {
			if capture.GetLinuxEnv() == "Wayland" {
				displayCapture = "pipewire (auto)"
			} else {
				displayCapture = "x11grab (auto)"
			}
		}
	}

	w.statusInfo = widget.NewLabel(fmt.Sprintf(
		"OS: %s\nHTTP Port: %d\nVideo UDP Port: %d\nCapture: %s",
		capture.GetOSInfo(),
		w.cfg.HTTPPort,
		w.cfg.VideoUDPPort,
		displayCapture,
	))
	w.statusInfo.Wrapping = fyne.TextWrapWord

	statsBlock := newPanel("Status", w.statusInfo)

	w.tsMode = widget.NewSelect([]string{"Userspace", "System"}, func(mode string) {
		w.applyTailscaleMode(mode)
	})
	if w.cfg.TailscaleMode == config.TailscaleModeUserspace {
		w.tsMode.SetSelected("Userspace")
	} else {
		w.tsMode.SetSelected("System")
	}

	w.tsInfo = widget.NewLabel("Status: checking...\nAccount: not connected\nAddress: unavailable")
	w.tsInfo.Wrapping = fyne.TextWrapWord
	w.tsPeers = widget.NewRichTextFromMarkdown("")
	w.tsPeers.Wrapping = fyne.TextWrapWord

	w.tsAuthBtn = widget.NewButton("Sign In With Google", func() {
		if w.ts == nil {
			w.setTailscaleInfo("service unavailable", "", "")
			return
		}

		w.tsAuthBtn.Disable()
		go func() {
			defer fyne.Do(w.tsAuthBtn.Enable)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			status, statusErr := w.ts.Status(ctx)
			if statusErr == nil && status != nil && status.LoggedIn {
				if err := w.ts.Logout(ctx); err != nil {
					fyne.Do(func() {
						w.setTailscaleInfo(fmt.Sprintf("logout error: %v", err), "", "")
					})
				}
				w.performRefresh()
				return
			}

			fyne.Do(func() { w.setTailscaleInfo("starting login flow...", "", "") })
			authURL, err := w.ts.StartLogin(ctx)
			if err != nil {
				fyne.Do(func() { w.setTailscaleInfo(fmt.Sprintf("error: %v", err), "", "") })
				return
			}

			if strings.TrimSpace(authURL) != "" {
				if parsed, parseErr := url.Parse(strings.TrimSpace(authURL)); parseErr == nil {
					logrus.Infof("tailscale ui: captured auth URL: %s", parsed.String())
					fyne.Do(func() {
						if w.app != nil {
							_ = w.app.OpenURL(parsed)
						}
						w.setTailscaleInfo("login link opened in browser", "", "")
					})
				} else {
					logrus.Errorf("tailscale ui: failed to parse auth URL %q: %v", authURL, parseErr)
					fyne.Do(func() { w.setTailscaleInfo("invalid login URL received", "", "") })
				}
			} else {
				fyne.Do(func() { w.setTailscaleInfo("waiting for system login...", "", "") })
			}
		}()
	})

	tsPanel := newPanel("Tailscale", newTightVBox(
		container.NewBorder(nil, nil, nil, container.NewVBox(w.tsAuthBtn), container.NewVBox(w.tsMode, w.tsInfo)),
		w.tsPeers,
	))

	// Layout construction
	col1 := container.NewVBox(permBlock)
	col2 := container.NewVBox(statsBlock)

	mainGrid := container.NewGridWithColumns(2, col1, col2)

	content := container.NewVBox(
		tsPanel,
		mainGrid,
		layout.NewSpacer(),
	)

	// Initial refresh
	w.performRefresh()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if win.Canvas() == nil {
				return
			}
			w.performRefresh()
		}
	}()

	bg := canvas.NewRectangle(design.ColorPanel)
	bodyContent := container.NewBorder(nil, nil, layout.NewSpacer(), layout.NewSpacer(), content)
	body := container.NewBorder(header, nil, nil, nil, container.NewPadded(bodyContent))
	win.SetContent(container.NewStack(bg, body))
	win.SetCloseIntercept(func() {
		if onClose != nil {
			onClose()
		}
		win.Close()
	})
	win.Show()
	w.app.Run()
}

func (w *Window) performRefresh() {
	go func() {
		status := uiStatus{}
		if w.ts != nil {
			status.tsStatus, _ = w.ts.Status(context.Background())
		}
		if w.perms != nil {
			status.accessGranted = w.perms.AccessibilityGranted()
			status.screenGranted = w.perms.ScreenRecordingGranted()
		}

		fyne.Do(func() {
			if w.accessLabel != nil {
				accessName := "Accessibility"
				if runtime.GOOS == "linux" {
					accessName = "Input Control"
				}
				if status.accessGranted {
					w.accessLabel.SetText(accessName + ": ✅")
					if w.accessBtn != nil {
						w.accessBtn.Hide()
					}
				} else {
					w.accessLabel.SetText(accessName + ": ❌")
					if (runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && capture.GetLinuxEnv() == "Wayland")) && w.accessBtn != nil {
						w.accessBtn.Show()
					}
				}
			}
			if w.screenLabel != nil {
				if status.screenGranted {
					w.screenLabel.SetText("Screen Recording: ✅")
					if w.screenBtn != nil {
						w.screenBtn.Hide()
					}
				} else {
					w.screenLabel.SetText("Screen Recording: ❌")
					if (runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && capture.GetLinuxEnv() == "Wayland")) && w.screenBtn != nil {
						w.screenBtn.Show()
					}
				}
			}
			if runtime.GOOS != "darwin" && w.permInfo != nil && w.accessLabel != nil && w.screenLabel != nil {
				w.permInfo.SetText(fmt.Sprintf("%s\n%s", w.accessLabel.Text, w.screenLabel.Text))
			}
			w.refreshTailscaleWithStatus(status.tsStatus)
		})
	}()
}

func (w *Window) refreshTailscaleWithStatus(status *tailscale.Status) {
	if w.tsPeers == nil || w.tsInfo == nil {
		return
	}
	if w.ts == nil || status == nil {
		w.setTailscaleInfo("unavailable", "unavailable", "unavailable")
		w.tsPeers.ParseMarkdown("")
		if w.tsAuthBtn != nil {
			w.tsAuthBtn.SetText("Sign In With Google")
		}
		return
	}

	if !status.LoggedIn {
		w.setTailscaleInfo("signed out", "sign in required", "sign in to publish this agent")
		w.tsPeers.ParseMarkdown("")
		if w.tsAuthBtn != nil {
			w.tsAuthBtn.SetText("Sign In With Google")
		}
		return
	}

	endpoint := status.Self.IP4
	if endpoint == "127.0.0.1" || strings.TrimSpace(endpoint) == "" {
		endpoint = status.Self.DNSName
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = status.Self.HostName
	}

	w.setTailscaleInfo(
		strings.ToLower(status.Backend),
		fallbackValue(status.Self.UserLogin, "connected"),
		fmt.Sprintf("%s (%s)", endpoint, fallbackValue(mapUserspace(status.Userspace), "embedded")),
	)

	if w.tsAuthBtn != nil {
		w.tsAuthBtn.SetText("Sign Out")
	}

	// Update active sessions
	var activePeers []string
	for _, p := range status.Peers {
		if !isActiveTailscalePeer(p) {
			continue
		}
		connType := "Relay (DERP)"
		if p.CurAddr != "" {
			connType = fmt.Sprintf("P2P DIRECT (%s)", p.CurAddr)
		} else if p.Relay != "" {
			connType = fmt.Sprintf("Relay (DERP %s)", p.Relay)
		}
		activePeers = append(activePeers, fmt.Sprintf("* **%s** (%s) - %s", fallbackValue(p.UserLogin, p.HostName), p.IP4, connType))
	}

	if len(activePeers) > 0 {
		w.tsPeers.ParseMarkdown(fmt.Sprintf("### Active Sessions:\n%s", strings.Join(activePeers, "\n")))
		w.tsPeers.Show()
	} else {
		w.tsPeers.ParseMarkdown("*No active remote controllers*")
		// Keep it visible but simple
		w.tsPeers.Show()
	}
}

func (w *Window) setTailscaleInfo(status, account, address string) {
	if w.tsInfo != nil {
		w.tsInfo.SetText(fmt.Sprintf("Status: %s\nAccount: %s\nAddress: %s", status, account, address))
	}
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func isActiveTailscalePeer(p tailscale.Peer) bool {
	if p.Active {
		return true
	}
	if strings.TrimSpace(p.CurAddr) != "" {
		return true
	}
	return false
}

func mapUserspace(userspace bool) string {
	if userspace {
		return "embedded"
	}
	return "system"
}

func (w *Window) showTokenDialog(parent fyne.Window) {
	if parent == nil {
		return
	}

	linkLabel := widget.NewLabel("")
	linkLabel.Alignment = fyne.TextAlignCenter
	linkLabel.Wrapping = fyne.TextWrapBreak

	qrImage := canvas.NewImageFromResource(nil)
	qrImage.FillMode = canvas.ImageFillContain
	qrImage.SetMinSize(fyne.NewSize(200, 200))
	qrMessage := widget.NewLabel("")
	qrMessage.Alignment = fyne.TextAlignCenter
	qrMessage.Wrapping = fyne.TextWrapWord
	qrContent := container.NewCenter(qrImage)
	qrMessage.Hide()
	qrPanelBody := container.NewVBox(qrContent, qrMessage)

	copyLinkBtn := newIconActionButton("Copy Link", theme.ContentCopyIcon(), func() {
		token := strings.TrimSpace(w.cfg.FRPToken)
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, token, protocol)
		if link != "" {
			parent.Clipboard().SetContent(link)
		}
	})
	regenerateBtn := newIconActionButton("Regenerate Token", theme.ViewRefreshIcon(), nil)

	topGap := spacerSize(1, 8)
	linkGap := spacerSize(1, 2)
	buttonGap := spacerSize(1, 6)
	closeTopGap := spacerSize(1, 8)
	closeBottomGap := spacerSize(1, 0)

	copyLinkSlot := container.NewCenter(container.NewGridWrap(fyne.NewSize(260, copyLinkBtn.MinSize().Height), copyLinkBtn))
	regenerateSlot := container.NewCenter(container.NewGridWrap(fyne.NewSize(260, regenerateBtn.MinSize().Height), regenerateBtn))
	linkActions := container.NewCenter(container.NewGridWithColumns(2,
		copyLinkSlot,
		regenerateSlot,
	))

	var tokenDialog *widget.PopUp
	closeDialogBtn := widget.NewButton("Close", func() {
		if tokenDialog != nil {
			tokenDialog.Hide()
		}
	})

	contentWidth := canvas.NewRectangle(color.Transparent)
	contentWidth.SetMinSize(fyne.NewSize(620, 1))
	contentBody := container.NewVBox(
		contentWidth,
		topGap,
		qrPanelBody,
		linkGap,
		container.NewPadded(linkLabel),
		buttonGap,
		linkActions,
		closeTopGap,
		container.NewCenter(closeDialogBtn),
		closeBottomGap,
	)
	// Убираем верхний отступ полностью, чтобы поднять QR-код
	pL := canvas.NewRectangle(color.Transparent)
	pL.SetMinSize(fyne.NewSize(8, 1))
	pR := canvas.NewRectangle(color.Transparent)
	pR.SetMinSize(fyne.NewSize(8, 1))
	pB := canvas.NewRectangle(color.Transparent)
	pB.SetMinSize(fyne.NewSize(1, 0))
	dialogContent := container.NewBorder(nil, pB, pL, pR, contentBody)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	dialogCard := container.NewStack(cardBG, dialogContent)
	dialogBody := container.NewCenter(dialogCard)

	// Создаем локальную тему с нулевыми отступами только для этого диалога
	compactTheme := &compactTheme{Theme: design.NewBrandTheme()}
	tokenDialog = widget.NewModalPopUp(container.NewThemeOverride(dialogBody, compactTheme), parent.Canvas())

	tokenDialog.Resize(parent.Canvas().Size())

	refreshDialogContent := func() {
		token := strings.TrimSpace(w.cfg.FRPToken)
		if token == "" {
			token = "unavailable"
		}
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, token, protocol)

		linkLabel.SetText(link)
		if link == "" {
			copyLinkBtn.Disable()
		} else {
			copyLinkBtn.Enable()
		}

		if link == "" {
			qrImage.Resource = nil
			qrImage.Hide()
			qrMessage.Show()
			qrMessage.SetText("QR link unavailable until the agent has a reachable address.")
			return
		}

		pngBytes, err := qrcode.Encode(link, qrcode.Medium, 320)
		if err != nil {
			qrImage.Resource = nil
			qrImage.Hide()
			qrMessage.Show()
			qrMessage.SetText(fmt.Sprintf("QR unavailable: %v", err))
			return
		}

		qrImage.Resource = fyne.NewStaticResource("agent-token-qr.png", pngBytes)
		qrImage.Show()
		qrImage.Refresh()
		qrMessage.Hide()
		qrMessage.SetText("")
	}

	regenerateBtn.OnTapped = func() {
		if w.token == nil {
			return
		}
		cfg, err := w.token.RegenerateFRPToken()
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		w.cfg = cfg
		refreshDialogContent()
	}

	refreshDialogContent()
	tokenDialog.Show()
}

func buildQuickConnectLink(internalHost, tailscaleHost, token, protocol string) string {
	token = strings.TrimSpace(token)
	if token == "" || token == "unavailable" {
		return ""
	}
	if strings.TrimSpace(internalHost) == "" && strings.TrimSpace(tailscaleHost) == "" {
		return ""
	}

	values := url.Values{}
	if strings.TrimSpace(internalHost) != "" {
		values.Set("internal_host", strings.TrimSpace(internalHost))
	}
	if strings.TrimSpace(tailscaleHost) != "" {
		values.Set("tailscale_host", strings.TrimSpace(tailscaleHost))
	}
	values.Set("token", token)
	if strings.TrimSpace(protocol) != "" {
		values.Set("protocol", strings.TrimSpace(protocol))
	}
	return fmt.Sprintf("usbridge://connect?%s", values.Encode())
}

func (w *Window) quickConnectTargets() (internalHost string, tailscaleHost string, protocol string) {
	internalHost = localQuickConnectIPv4()
	if w.ts != nil {
		if status, err := w.ts.Status(context.Background()); err == nil && status != nil && status.LoggedIn {
			switch {
			case strings.TrimSpace(status.Self.IP4) != "":
				tailscaleHost = strings.TrimSpace(status.Self.IP4)
			case strings.TrimSpace(status.Self.DNSName) != "":
				tailscaleHost = strings.TrimSpace(status.Self.DNSName)
			}
		}
	}

	if tailscaleHost != "" {
		return internalHost, tailscaleHost, "tailscale"
	}
	if internalHost != "" {
		return internalHost, "", "quic"
	}
	return "", "", ""
}

func localQuickConnectIPv4() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	type candidate struct {
		name string
		ip   string
	}

	candidates := make([]candidate, 0, 8)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip, _, err := net.ParseCIDR(addr.String())
			if err != nil || ip == nil {
				continue
			}
			ip4 := ip.To4()
			if ip4 == nil || ip4.IsLoopback() {
				continue
			}
			if ip4[0] == 169 && ip4[1] == 254 {
				continue
			}
			candidates = append(candidates, candidate{name: iface.Name, ip: ip4.String()})
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].name == candidates[j].name {
			return candidates[i].ip < candidates[j].ip
		}
		return candidates[i].name < candidates[j].name
	})
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0].ip
}

func newPanel(title string, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorHeader)
	bg.CornerRadius = design.RadiusMD
	bg.SetMinSize(fyne.NewSize(0, 1))

	shadow := canvas.NewRectangle(design.ColorShadow)
	shadow.CornerRadius = design.RadiusMD
	shadowTopGap := canvas.NewRectangle(color.Transparent)
	shadowTopGap.SetMinSize(fyne.NewSize(0, 4))
	shadowLeftGap := canvas.NewRectangle(color.Transparent)
	shadowLeftGap.SetMinSize(fyne.NewSize(1, 0))

	// Применяем плотную тему (нулевые отступы) только к контенту внутри панели
	denseContent := container.NewThemeOverride(content, &headerButtonTheme{Theme: design.NewBrandTheme(), padding: 0})

	card := container.NewStack(
		container.NewBorder(shadowTopGap, nil, shadowLeftGap, nil, shadow),
		container.NewStack(
			bg,
			container.NewBorder(
				spacerSize(6, 6),
				spacerSize(6, 6),
				spacerSize(6, 6),
				spacerSize(6, 6),
				denseContent,
			),
		),
	)

	if strings.TrimSpace(title) == "" {
		return card
	}

	titleText := canvas.NewText(strings.ToUpper(title), design.ColorTextMuted)
	titleText.TextSize = 11
	titleText.TextStyle.Bold = true

	titleIndent := canvas.NewRectangle(color.Transparent)
	titleIndent.SetMinSize(fyne.NewSize(8, 0))
	titleRow := container.NewBorder(nil, nil, titleIndent, nil, titleText)

	return container.NewVBox(titleRow, card)
}

func newTightVBox(items ...fyne.CanvasObject) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(items)*2)
	for i, item := range items {
		if item == nil {
			continue
		}
		if len(rows) > 0 && i > 0 {
			gap := canvas.NewRectangle(color.Transparent)
			gap.SetMinSize(fyne.NewSize(0, 2))
			rows = append(rows, gap)
		}
		rows = append(rows, item)
	}
	return container.NewVBox(rows...)
}

func spacerSize(width, height float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(width, height))
	return spacer
}

func newKeyValueRow(label string, value *widget.Label) fyne.CanvasObject {
	title := canvas.NewText(label+":", design.ColorTextLight)
	title.TextStyle.Bold = true
	title.TextSize = 14

	if value == nil {
		value = widget.NewLabel("")
	}
	value.Wrapping = fyne.TextWrapWord

	titleSlot := container.NewGridWrap(fyne.NewSize(72, title.MinSize().Height), title)
	return container.NewBorder(nil, nil, titleSlot, nil, value)
}

func newHeaderBar(left fyne.CanvasObject, right fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorHeader)
	bg.SetMinSize(fyne.NewSize(0, 48))

	leftBox := container.NewHBox()
	if left != nil {
		leftBox.Add(left)
	}

	rightBox := container.NewHBox()
	if right != nil {
		rightBox.Add(right)
	}

	bar := container.NewPadded(container.NewBorder(nil, nil, leftBox, rightBox, nil))
	return container.NewStack(bg, bar)
}

func newBadge(text string, size fyne.Size) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorAccent)
	bg.CornerRadius = design.RadiusMD

	label := canvas.NewText(text, design.ColorBackground)
	label.TextStyle.Bold = true
	label.TextSize = 14

	return container.NewGridWrap(size, container.NewStack(bg, container.NewCenter(label)))
}

func wrapDialogButton(btn *widget.Button) fyne.CanvasObject {
	if btn == nil {
		return layout.NewSpacer()
	}
	slot := canvas.NewRectangle(color.Transparent)
	slot.SetMinSize(fyne.NewSize(0, 48))
	return container.NewStack(slot, btn)
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

type compactTheme struct {
	fyne.Theme
}

func (t *compactTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNamePadding {
		return 4
	}
	return t.Theme.Size(name)
}

func newLabelValue(labelText string, valueText *canvas.Text) fyne.CanvasObject {
	title := canvas.NewText(strings.ToUpper(labelText)+":", design.ColorTextMuted)
	title.TextSize = 10
	title.TextStyle.Bold = true

	// Контейнер для заголовка с фиксированной шириной
	titleBox := container.NewGridWrap(fyne.NewSize(55, 16), container.NewCenter(title))

	return container.NewHBox(titleBox, valueText)
}

type labelTheme struct {
	fyne.Theme
	textColor color.Color
	textSize  float32
}

func (t *labelTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.textColor
	}
	return t.Theme.Color(name, v)
}

func (t *labelTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return t.textSize
	}
	if name == theme.SizeNamePadding {
		return 0
	}
	return t.Theme.Size(name)
}

type headerButtonTheme struct {
	fyne.Theme
	padding float32
}

func (t *headerButtonTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNamePadding {
		return t.padding
	}
	return t.Theme.Size(name)
}

type iconActionButton struct {
	widget.DisableableWidget
	Text     string
	Icon     fyne.Resource
	OnTapped func()
	hovered  bool
}

func newIconActionButton(label string, icon fyne.Resource, tapped func()) *iconActionButton {
	b := &iconActionButton{Text: label, Icon: icon, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *iconActionButton) SetText(text string) {
	b.Text = text
	b.Refresh()
}

func (b *iconActionButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorSurfaceLight)
	bg.CornerRadius = design.RadiusMD

	icon := canvas.NewImageFromResource(b.Icon)
	icon.FillMode = canvas.ImageFillContain

	text := canvas.NewText(b.Text, design.ColorTextLight)
	text.TextStyle.Bold = true
	text.TextSize = 13

	objects := []fyne.CanvasObject{bg, icon, text}
	return &iconActionButtonRenderer{
		bg:      bg,
		icon:    icon,
		text:    text,
		button:  b,
		objects: objects,
	}
}

func (b *iconActionButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *iconActionButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *iconActionButton) MouseMoved(*desktop.MouseEvent) {}

func (b *iconActionButton) Tapped(*fyne.PointEvent) {
	if b.Disabled() {
		return
	}
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

type iconActionButtonRenderer struct {
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	button  *iconActionButton
	objects []fyne.CanvasObject
}

func (r *iconActionButtonRenderer) Destroy() {}

func (r *iconActionButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)

	iconSize := float32(20)
	gap := float32(8)
	paddingX := float32(14)
	textSize := r.text.MinSize()
	contentWidth := iconSize + gap + textSize.Width

	startX := paddingX
	if size.Width > contentWidth+paddingX*2 {
		startX = (size.Width - contentWidth) / 2
	}

	r.icon.Resize(fyne.NewSize(iconSize, iconSize))
	r.icon.Move(fyne.NewPos(startX, (size.Height-iconSize)/2))
	r.text.Move(fyne.NewPos(startX+iconSize+gap, (size.Height-textSize.Height)/2))
	r.text.Resize(textSize)
}

func (r *iconActionButtonRenderer) MinSize() fyne.Size {
	iconSize := float32(20)
	gap := float32(8)
	paddingX := float32(14)
	paddingY := float32(10)
	textSize := r.text.MinSize()
	return fyne.NewSize(iconSize+gap+textSize.Width+paddingX*2, fyne.Max(iconSize, textSize.Height)+paddingY*2)
}

func (r *iconActionButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *iconActionButtonRenderer) Refresh() {
	r.text.Text = r.button.Text
	if r.button.Disabled() {
		r.bg.FillColor = design.ColorSurface
		r.text.Color = design.ColorBorder
	} else if r.button.hovered {
		r.bg.FillColor = design.ColorHover
		r.text.Color = design.ColorTextLight
	} else {
		r.bg.FillColor = design.ColorSurfaceLight
		r.text.Color = design.ColorTextLight
	}
	r.bg.Refresh()
	r.text.Refresh()
	r.icon.Refresh()
}

type closeButton struct {
	widget.BaseWidget
	Text     string
	OnTapped func()
	hovered  bool
}

func newDangerButton(label string, tapped func()) fyne.CanvasObject {
	b := &closeButton{Text: label, OnTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *closeButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.Transparent)
	bg.CornerRadius = design.RadiusMD
	bg.StrokeWidth = 1
	bg.StrokeColor = design.ColorError

	text := canvas.NewText(b.Text, design.ColorError)
	text.Alignment = fyne.TextAlignCenter
	text.TextStyle.Bold = true
	text.TextSize = 13

	content := container.NewStack(bg, container.NewPadded(text))
	return &closeButtonRenderer{
		bg:      bg,
		text:    text,
		button:  b,
		objects: []fyne.CanvasObject{content},
	}
}

type closeButtonRenderer struct {
	bg      *canvas.Rectangle
	text    *canvas.Text
	button  *closeButton
	objects []fyne.CanvasObject
}

func (r *closeButtonRenderer) Destroy() {}
func (r *closeButtonRenderer) Layout(size fyne.Size) {
	r.objects[0].Resize(size)
}
func (r *closeButtonRenderer) MinSize() fyne.Size {
	return fyne.NewSize(80, 32)
}
func (r *closeButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}
func (r *closeButtonRenderer) Refresh() {
	if r.button.hovered {
		r.bg.FillColor = design.ColorError
		r.bg.StrokeColor = color.Transparent
		r.text.Color = color.Black
	} else {
		r.bg.FillColor = color.Transparent
		r.bg.StrokeColor = design.ColorError
		r.text.Color = design.ColorError
	}
	r.bg.Refresh()
	r.text.Refresh()
}

func (b *closeButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}
func (b *closeButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}
func (b *closeButton) MouseMoved(*desktop.MouseEvent) {}
func (b *closeButton) Tapped(*fyne.PointEvent) {
	if b.OnTapped != nil {
		b.OnTapped()
	}
}

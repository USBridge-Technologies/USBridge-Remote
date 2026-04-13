package ui

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"runtime"
	"sort"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	qrcode "github.com/skip2/go-qrcode"
	"github.com/sirupsen/logrus"

	"usbridge_agent/internal/config"
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
	}
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

func (w *Window) ShowAndRun(onClose func()) {
	win := w.app.NewWindow("USBridge Agent")
	win.SetPadded(false)
	win.Resize(fyne.NewSize(620, 420))
	win.CenterOnScreen()

	title := canvas.NewText("USBridge Agent", design.ColorTextLight)
	title.TextSize = 22
	title.TextStyle.Bold = true

	subtitle := canvas.NewText("Compact desktop backend for usbridge_client", design.ColorTextMuted)
	subtitle.TextSize = 12

	tokenBtn := widget.NewButton("TOKEN", func() {
		w.showTokenDialog(win)
	})
	header := container.NewBorder(nil, nil, nil, container.NewGridWrap(fyne.NewSize(86, 32), tokenBtn), container.NewVBox(title, subtitle))

	httpCard := newStatCard("HTTP", fmt.Sprintf("127.0.0.1:%d", w.cfg.HTTPPort), "API")
	videoCard := newStatCard("VIDEO", fmt.Sprintf("127.0.0.1:%d", w.cfg.VideoUDPPort), "RTP")
	captureCard := newStatCard("CAPTURE", w.cfg.VideoCapture, strings.ToUpper(runtime.GOOS))
	
	tsState := widget.NewLabel("Tailscale status: checking...")
	tsAddress := widget.NewRichTextFromMarkdown("Tailnet address: `unavailable`")
	tsAddress.Wrapping = fyne.TextWrapWord
	tsAccount := widget.NewLabel("Google account: not connected")

	var tsAuthBtn *widget.Button
	tsAuthBtn = widget.NewButton("Sign In With Google", func() {
		if w.ts == nil {
			tsState.SetText("Tailscale status: service unavailable")
			return
		}

		tsAuthBtn.Disable()
		go func() {
			defer fyne.Do(tsAuthBtn.Enable)

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()

			status, statusErr := w.ts.Status(ctx)
			if statusErr == nil && status != nil && status.LoggedIn {
				if err := w.ts.Logout(ctx); err != nil {
					fyne.Do(func() { tsState.SetText(fmt.Sprintf("Tailscale status: logout error: %v", err)) })
				}
				w.performRefresh(nil, nil, tsState, tsAddress, tsAccount, tsAuthBtn)
				return
			}

			fyne.Do(func() { tsState.SetText("Tailscale status: starting login flow...") })
			authURL, err := w.ts.StartLogin(ctx)
			if err != nil {
				fyne.Do(func() { tsState.SetText(fmt.Sprintf("Tailscale status: error: %v", err)) })
				return
			}

			if strings.TrimSpace(authURL) != "" {
				if parsed, parseErr := url.Parse(strings.TrimSpace(authURL)); parseErr == nil {
					logrus.Infof("tailscale ui: captured auth URL: %s", parsed.String())
					fyne.Do(func() {
						if w.app != nil {
							_ = w.app.OpenURL(parsed)
						}
						tsState.SetText("Tailscale status: login link opened in browser")
					})
				} else {
					logrus.Errorf("tailscale ui: failed to parse auth URL %q: %v", authURL, parseErr)
					fyne.Do(func() { tsState.SetText("Tailscale status: invalid login URL received") })
				}
			} else {
				fyne.Do(func() { tsState.SetText("Tailscale status: waiting for system login...") })
			}
		}()
	})
	tsPanel := newPanel("Tailscale", container.NewVBox(
		tsState,
		tsAccount,
		tsAddress,
		container.NewHBox(tsAuthBtn),
	))

	accessStatus := widget.NewLabel("")
	screenStatus := widget.NewLabel("")

	accessPanel := newPermissionPanel(
		"Accessibility",
		"Needed for keyboard and mouse injection.",
		accessStatus,
		func() {
			if w.perms != nil {
				_ = w.perms.RequestAccessibility()
				w.performRefresh(accessStatus, screenStatus, tsState, tsAddress, tsAccount, tsAuthBtn)
			}
		},
	)
	screenPanel := newPermissionPanel(
		"Screen Recording",
		"Needed for screen capture and video streaming.",
		screenStatus,
		func() {
			if w.perms != nil {
				_ = w.perms.RequestScreenRecording()
				w.performRefresh(accessStatus, screenStatus, tsState, tsAddress, tsAccount, tsAuthBtn)
			}
		},
	)

	openSettingsBtn := widget.NewButton("Open Privacy Settings", func() {
		if w.perms != nil {
			_ = w.perms.OpenPrivacySettings()
			w.performRefresh(accessStatus, screenStatus, tsState, tsAddress, tsAccount, tsAuthBtn)
		}
	})
	closeBtn := widget.NewButton("Close", func() { win.Close() })
	closeBtn.Importance = widget.HighImportance

	infoText := widget.NewRichTextFromMarkdown(strings.TrimSpace(
		"`usbridge_agent` stays compatible with `usbridge_client` and exposes the same control API surface.",
	))
	infoText.Wrapping = fyne.TextWrapWord
	executablePath := "unknown"
	if path, err := os.Executable(); err == nil && strings.TrimSpace(path) != "" {
		executablePath = path
	}
	execText := widget.NewRichTextFromMarkdown(fmt.Sprintf("Executable: `%s`", executablePath))
	execText.Wrapping = fyne.TextWrapWord

	content := container.NewVBox(
		newPanel("", header),
		container.NewGridWithColumns(3, httpCard, videoCard, captureCard),
		tsPanel,
		container.NewGridWithColumns(2, accessPanel, screenPanel),
		newPanel("Compatibility", infoText),
		newPanel("Runtime", execText),
		container.NewHBox(openSettingsBtn, layout.NewSpacer(), closeBtn),
	)

	// Initial refresh
	w.performRefresh(accessStatus, screenStatus, tsState, tsAddress, tsAccount, tsAuthBtn)

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if win.Canvas() == nil {
				return
			}
			w.performRefresh(accessStatus, screenStatus, tsState, tsAddress, tsAccount, tsAuthBtn)
		}
	}()

	bg := canvas.NewRectangle(design.ColorPanel)
	win.SetContent(container.NewStack(bg, container.NewPadded(content)))
	win.SetCloseIntercept(func() {
		if onClose != nil {
			onClose()
		}
		win.Close()
	})
	win.Show()
	w.app.Run()
}

func (w *Window) performRefresh(accessLabel, screenLabel *widget.Label, tsState *widget.Label, tsAddress *widget.RichText, tsAccount *widget.Label, tsAuthBtn *widget.Button) {
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
			if accessLabel != nil {
				if status.accessGranted {
					accessLabel.SetText("Status: granted")
				} else {
					accessLabel.SetText("Status: required")
				}
			}
			if screenLabel != nil {
				if status.screenGranted {
					screenLabel.SetText("Status: granted")
				} else {
					screenLabel.SetText("Status: required")
				}
			}
			w.refreshTailscaleWithStatus(status.tsStatus, tsState, tsAddress, tsAccount, tsAuthBtn)
		})
	}()
}

func (w *Window) refreshTailscaleWithStatus(status *tailscale.Status, state *widget.Label, address *widget.RichText, account *widget.Label, action *widget.Button) {
	if state == nil || address == nil || account == nil {
		return
	}
	if w.ts == nil || status == nil {
		state.SetText("Tailscale status: unavailable")
		account.SetText("Google account: unavailable")
		address.ParseMarkdown("Tailnet address: `unavailable`")
		if action != nil {
			action.SetText("Sign In With Google")
		}
		return
	}

	if !status.LoggedIn {
		state.SetText("Tailscale status: signed out")
		account.SetText("Google account: sign in required")
		address.ParseMarkdown("Tailnet address: `sign in to publish this agent`")
		if action != nil {
			action.SetText("Sign In With Google")
		}
		return
	}

	endpoint := status.Self.DNSName
	if strings.TrimSpace(endpoint) == "" {
		endpoint = status.Self.IP4
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = status.Self.HostName
	}
	state.SetText(fmt.Sprintf("Tailscale status: %s", strings.ToLower(status.Backend)))
	account.SetText(fmt.Sprintf("Google account: %s", fallbackValue(status.Self.UserLogin, "connected")))
	address.ParseMarkdown(fmt.Sprintf("Tailnet address: `%s:%d` (%s)", endpoint, w.cfg.HTTPPort, fallbackValue(mapUserspace(status.Userspace), "embedded")))
	if action != nil {
		action.SetText("Sign Out")
	}
}

func (w *Window) refreshTailscale(state *widget.Label, address *widget.RichText, account *widget.Label, action *widget.Button) {
	if w.ts == nil {
		w.refreshTailscaleWithStatus(nil, state, address, account, action)
		return
	}
	status, _ := w.ts.Status(context.Background())
	w.refreshTailscaleWithStatus(status, state, address, account, action)
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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

	linkEntry := widget.NewEntry()
	linkEntry.Disable()

	qrImage := canvas.NewImageFromResource(nil)
	qrImage.FillMode = canvas.ImageFillContain
	qrImage.SetMinSize(fyne.NewSize(240, 240))
	qrMessage := widget.NewLabel("")
	qrMessage.Wrapping = fyne.TextWrapWord
	qrContent := container.NewCenter(qrImage)
	qrPanelBody := container.NewVBox(qrContent, qrMessage)

	copyTokenBtn := widget.NewButton("Copy Token", func() {
		token := strings.TrimSpace(w.cfg.FRPToken)
		if token == "" {
			return
		}
		parent.Clipboard().SetContent(token)
	})
	copyInternalBtn := widget.NewButton("Copy Internal", func() {
		internalHost, _, _ := w.quickConnectTargets()
		if strings.TrimSpace(internalHost) != "" {
			parent.Clipboard().SetContent(internalHost)
		}
	})
	copyTailscaleBtn := widget.NewButton("Copy Tailscale", func() {
		_, tailscaleHost, _ := w.quickConnectTargets()
		if strings.TrimSpace(tailscaleHost) != "" {
			parent.Clipboard().SetContent(tailscaleHost)
		}
	})
	copyLinkBtn := widget.NewButton("Copy Link", func() {
		token := strings.TrimSpace(w.cfg.FRPToken)
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, token, protocol)
		if link != "" {
			parent.Clipboard().SetContent(link)
		}
	})
	regenerateBtn := widget.NewButton("Regenerate Token", nil)

	title := canvas.NewText("Quick Connect", design.ColorTextLight)
	title.TextSize = 18
	title.TextStyle.Bold = true
	header := container.NewBorder(nil, nil, nil, newBadge("QR", fyne.NewSize(52, 28)), title)

	copyTokenBtn.Icon = theme.ContentCopyIcon()
	copyInternalBtn.Icon = theme.ContentCopyIcon()
	copyTailscaleBtn.Icon = theme.ContentCopyIcon()
	copyLinkBtn.Icon = theme.ContentCopyIcon()
	regenerateBtn.Icon = theme.ViewRefreshIcon()

	body := container.NewVBox(
		newPanel("", header),
		newPanel("QR", qrPanelBody),
		newPanel("Link", container.NewVBox(
			linkEntry,
			container.NewGridWithColumns(2, copyLinkBtn, regenerateBtn),
			container.NewGridWithColumns(2, copyInternalBtn, copyTailscaleBtn),
			container.NewHBox(copyTokenBtn, layout.NewSpacer()),
		)),
	)
	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(400, 620))
	content := container.NewThemeOverride(scroll, design.NewBrandTheme())

	d := dialog.NewCustom("Token / QR", "Close", content, parent)
	d.Resize(fyne.NewSize(440, 680))

	refreshDialogContent := func() {
		token := strings.TrimSpace(w.cfg.FRPToken)
		if token == "" {
			token = "unavailable"
		}
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, token, protocol)

		linkEntry.SetText(link)
		if strings.TrimSpace(internalHost) == "" {
			copyInternalBtn.Disable()
		} else {
			copyInternalBtn.Enable()
		}
		if strings.TrimSpace(tailscaleHost) == "" {
			copyTailscaleBtn.Disable()
		} else {
			copyTailscaleBtn.Enable()
		}
		if link == "" {
			copyLinkBtn.Disable()
		} else {
			copyLinkBtn.Enable()
		}
		if token == "" || token == "unavailable" {
			copyTokenBtn.Disable()
		} else {
			copyTokenBtn.Enable()
		}

		if link == "" {
			qrImage.Resource = nil
			qrImage.Hide()
			qrMessage.SetText("QR link unavailable until the agent has a reachable address.")
			return
		}

		pngBytes, err := qrcode.Encode(link, qrcode.Medium, 280)
		if err != nil {
			qrImage.Resource = nil
			qrImage.Hide()
			qrMessage.SetText(fmt.Sprintf("QR unavailable: %v", err))
			return
		}

		qrImage.Resource = fyne.NewStaticResource("agent-token-qr.png", pngBytes)
		qrImage.Show()
		qrImage.Refresh()
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
	d.Show()
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
			case strings.TrimSpace(status.Self.DNSName) != "":
				tailscaleHost = strings.TrimSpace(status.Self.DNSName)
			case strings.TrimSpace(status.Self.IP4) != "":
				tailscaleHost = strings.TrimSpace(status.Self.IP4)
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
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = 12
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	body := container.NewVBox(content)
	if strings.TrimSpace(title) != "" {
		titleText := canvas.NewText(strings.ToUpper(title), design.ColorTextMuted)
		titleText.TextSize = 11
		titleText.TextStyle.Bold = true
		body = container.NewVBox(titleText, widget.NewSeparator(), content)
	}
	return container.NewStack(bg, container.NewPadded(body))
}

func newStatCard(label, value, hint string) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = 12
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	labelText := canvas.NewText(strings.ToUpper(label), design.ColorTextMuted)
	labelText.TextSize = 10
	labelText.TextStyle.Bold = true

	valueText := canvas.NewText(value, design.ColorTextLight)
	valueText.TextSize = 15
	valueText.TextStyle.Bold = true

	hintText := canvas.NewText(hint, design.ColorTextMuted)
	hintText.TextSize = 10

	body := container.NewVBox(labelText, valueText, hintText)
	return container.NewStack(bg, container.NewPadded(body))
}

func newBadge(text string, size fyne.Size) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorAccent)
	bg.CornerRadius = design.RadiusMD

	label := canvas.NewText(text, design.ColorBackground)
	label.TextStyle.Bold = true
	label.TextSize = 14

	return container.NewGridWrap(size, container.NewStack(bg, container.NewCenter(label)))
}

func newPermissionPanel(title, description string, status *widget.Label, onRequest func()) fyne.CanvasObject {
	desc := widget.NewLabel(description)
	desc.Wrapping = fyne.TextWrapWord

	requestBtn := widget.NewButton("Request", onRequest)
	requestBtn.Importance = widget.HighImportance

	return newPanel(title, container.NewVBox(
		status,
		desc,
		requestBtn,
	))
}

func refreshPermissionLabels(accessLabel, screenLabel *widget.Label, perms interface {
	AccessibilityGranted() bool
	ScreenRecordingGranted() bool
}) {
	if accessLabel != nil {
		if perms != nil && perms.AccessibilityGranted() {
			accessLabel.SetText("Status: granted")
		} else {
			accessLabel.SetText("Status: required")
		}
	}
	if screenLabel != nil {
		if perms != nil && perms.ScreenRecordingGranted() {
			screenLabel.SetText("Status: granted")
		} else {
			screenLabel.SetText("Status: required")
		}
	}
}

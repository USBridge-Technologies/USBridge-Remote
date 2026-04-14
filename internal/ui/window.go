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

	// UI components
	accessLabel *widget.Label
	accessBtn   *widget.Button
	screenLabel *widget.Label
	screenBtn   *widget.Button

	tsState   *widget.Label
	tsAddress *widget.RichText
	tsAccount *widget.Label
	tsAuthBtn *widget.Button

	httpStat    *widget.Label
	videoStat   *widget.Label
	captureStat *widget.Label
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
	win.Resize(fyne.NewSize(640, 400))
	win.CenterOnScreen()

	// Header
	title := canvas.NewText("USBridge Agent", design.ColorTextLight)
	title.TextSize = 20
	title.TextStyle.Bold = true
	subtitle := canvas.NewText("Compact desktop backend", design.ColorTextMuted)
	subtitle.TextSize = 11
	
	tokenBtn := widget.NewButtonWithIcon("TOKEN", theme.SettingsIcon(), func() {
		w.showTokenDialog(win)
	})
	tokenBtn.Importance = widget.LowImportance
	header := container.NewBorder(nil, nil, nil, container.NewGridWrap(fyne.NewSize(100, 32), tokenBtn), container.NewVBox(title, subtitle))

	// Column 1: Permissions
	w.accessLabel = widget.NewLabel("Accessibility")
	w.accessBtn = widget.NewButton("Request", func() {
		if w.perms != nil {
			_ = w.perms.RequestAccessibility()
			w.performRefresh()
		}
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

	// Adjust for OS
	if runtime.GOOS != "darwin" {
		w.accessBtn.Hide()
		w.screenBtn.Hide()
	} else {
		w.accessBtn.Resize(fyne.NewSize(80, 24))
		w.screenBtn.Resize(fyne.NewSize(80, 24))
	}

	permBlock := newPanel("Permissions", container.NewVBox(
		container.NewHBox(w.accessLabel, layout.NewSpacer(), w.accessBtn),
		widget.NewSeparator(),
		container.NewHBox(w.screenLabel, layout.NewSpacer(), w.screenBtn),
	))

	// Column 2: Stats & Tailscale
	w.httpStat = widget.NewLabel(fmt.Sprintf("HTTP: 127.0.0.1:%d", w.cfg.HTTPPort))
	w.videoStat = widget.NewLabel(fmt.Sprintf("VIDEO: 127.0.0.1:%d", w.cfg.VideoUDPPort))
	w.captureStat = widget.NewLabel(fmt.Sprintf("CAPTURE: %s (%s)", w.cfg.VideoCapture, strings.ToUpper(runtime.GOOS)))

	statsBlock := newPanel("Status", container.NewVBox(
		w.httpStat,
		w.videoStat,
		w.captureStat,
	))

	w.tsState = widget.NewLabel("Status: checking...")
	w.tsAddress = widget.NewRichTextFromMarkdown("Address: `unavailable`")
	w.tsAddress.Wrapping = fyne.TextWrapWord
	w.tsAccount = widget.NewLabel("Account: not connected")

	w.tsAuthBtn = widget.NewButton("Sign In With Google", func() {
		if w.ts == nil {
			w.tsState.SetText("Status: service unavailable")
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
					fyne.Do(func() { w.tsState.SetText(fmt.Sprintf("Status: logout error: %v", err)) })
				}
				w.performRefresh()
				return
			}

			fyne.Do(func() { w.tsState.SetText("Status: starting login flow...") })
			authURL, err := w.ts.StartLogin(ctx)
			if err != nil {
				fyne.Do(func() { w.tsState.SetText(fmt.Sprintf("Status: error: %v", err)) })
				return
			}

			if strings.TrimSpace(authURL) != "" {
				if parsed, parseErr := url.Parse(strings.TrimSpace(authURL)); parseErr == nil {
					logrus.Infof("tailscale ui: captured auth URL: %s", parsed.String())
					fyne.Do(func() {
						if w.app != nil {
							_ = w.app.OpenURL(parsed)
						}
						w.tsState.SetText("Status: login link opened in browser")
					})
				} else {
					logrus.Errorf("tailscale ui: failed to parse auth URL %q: %v", authURL, parseErr)
					fyne.Do(func() { w.tsState.SetText("Status: invalid login URL received") })
				}
			} else {
				fyne.Do(func() { w.tsState.SetText("Status: waiting for system login...") })
			}
		}()
	})
	tsPanel := newPanel("Tailscale", container.NewVBox(
		w.tsState,
		w.tsAccount,
		w.tsAddress,
		container.NewHBox(layout.NewSpacer(), w.tsAuthBtn),
	))

	closeBtn := widget.NewButton("CLOSE", func() { win.Close() })
	closeBtn.Importance = widget.DangerImportance

	// Layout construction
	col1 := container.NewVBox(permBlock, layout.NewSpacer())
	col2 := container.NewVBox(statsBlock, tsPanel, layout.NewSpacer())

	mainGrid := container.NewGridWithColumns(2, col1, col2)

	content := container.NewVBox(
		newPanel("", header),
		mainGrid,
		container.NewHBox(layout.NewSpacer(), closeBtn),
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
				if status.accessGranted {
					w.accessLabel.SetText("Accessibility: ✅")
					if w.accessBtn != nil {
						w.accessBtn.Hide()
					}
				} else {
					w.accessLabel.SetText("Accessibility: ❌")
					if runtime.GOOS == "darwin" && w.accessBtn != nil {
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
					if runtime.GOOS == "darwin" && w.screenBtn != nil {
						w.screenBtn.Show()
					}
				}
			}
			w.refreshTailscaleWithStatus(status.tsStatus)
		})
	}()
}

func (w *Window) refreshTailscaleWithStatus(status *tailscale.Status) {
	if w.tsState == nil || w.tsAddress == nil || w.tsAccount == nil {
		return
	}
	if w.ts == nil || status == nil {
		w.tsState.SetText("Status: unavailable")
		w.tsAccount.SetText("Account: unavailable")
		w.tsAddress.ParseMarkdown("Address: `unavailable`")
		if w.tsAuthBtn != nil {
			w.tsAuthBtn.SetText("Sign In With Google")
		}
		return
	}

	if !status.LoggedIn {
		w.tsState.SetText("Status: signed out")
		w.tsAccount.SetText("Account: sign in required")
		w.tsAddress.ParseMarkdown("Address: `sign in to publish this agent`")
		if w.tsAuthBtn != nil {
			w.tsAuthBtn.SetText("Sign In With Google")
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
	w.tsState.SetText(fmt.Sprintf("Status: %s", strings.ToLower(status.Backend)))
	w.tsAccount.SetText(fmt.Sprintf("Account: %s", fallbackValue(status.Self.UserLogin, "connected")))
	w.tsAddress.ParseMarkdown(fmt.Sprintf("Address: `%s:%d` (%s)", endpoint, w.cfg.HTTPPort, fallbackValue(mapUserspace(status.Userspace), "embedded")))
	if w.tsAuthBtn != nil {
		w.tsAuthBtn.SetText("Sign Out")
	}
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

func newBadge(text string, size fyne.Size) fyne.CanvasObject {

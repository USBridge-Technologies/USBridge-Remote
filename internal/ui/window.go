package ui

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
	qrcode "github.com/skip2/go-qrcode"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui/design"
)

type Window struct {
	app   fyne.App
	cfg   config.Config
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

func NewWindow(app fyne.App, cfg config.Config, perms *permissions.Service, ts *tailscale.Service) *Window {
	return &Window{app: app, cfg: cfg, perms: perms, ts: ts}
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
		status, statusErr := w.ts.Status(context.Background())
		if statusErr == nil && status != nil && status.LoggedIn {
			if err := w.ts.Logout(context.Background()); err != nil {
				tsState.SetText(fmt.Sprintf("Tailscale status: %v", err))
			}
			w.refreshTailscale(tsState, tsAddress, tsAccount, tsAuthBtn)
			return
		}

		authURL, err := w.ts.StartLogin(context.Background())
		if err != nil {
			tsState.SetText(fmt.Sprintf("Tailscale status: %v", err))
			return
		}
		if strings.TrimSpace(authURL) != "" && w.app != nil {
			if parsed, parseErr := url.Parse(authURL); parseErr == nil {
				_ = w.app.OpenURL(parsed)
			}
		}
		tsState.SetText("Tailscale status: login flow started in browser")
		tsAuthBtn.SetText("Sign In With Google")
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
				refreshPermissionLabels(accessStatus, screenStatus, w.perms)
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
				refreshPermissionLabels(accessStatus, screenStatus, w.perms)
			}
		},
	)

	openSettingsBtn := widget.NewButton("Open Privacy Settings", func() {
		if w.perms != nil {
			_ = w.perms.OpenPrivacySettings()
			refreshPermissionLabels(accessStatus, screenStatus, w.perms)
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

	refreshPermissionLabels(accessStatus, screenStatus, w.perms)
	w.refreshTailscale(tsState, tsAddress, tsAccount, tsAuthBtn)
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if win.Canvas() == nil {
				return
			}
			fyne.Do(func() {
				refreshPermissionLabels(accessStatus, screenStatus, w.perms)
				w.refreshTailscale(tsState, tsAddress, tsAccount, tsAuthBtn)
			})
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

func (w *Window) refreshTailscale(state *widget.Label, address *widget.RichText, account *widget.Label, action *widget.Button) {
	if state == nil || address == nil || account == nil {
		return
	}
	if w.ts == nil {
		state.SetText("Tailscale status: unavailable")
		account.SetText("Google account: unavailable")
		address.ParseMarkdown("Tailnet address: `unavailable`")
		if action != nil {
			action.SetText("Sign In With Google")
		}
		return
	}

	status, err := w.ts.Status(context.Background())
	if err != nil {
		state.SetText(fmt.Sprintf("Tailscale status: %v", err))
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

	token := strings.TrimSpace(w.cfg.FRPToken)
	if token == "" {
		token = "unavailable"
	}

	host, protocol := w.quickConnectTarget()
	link := ""
	if host != "" && token != "unavailable" {
		values := url.Values{}
		values.Set("host", host)
		values.Set("token", token)
		if protocol != "" {
			values.Set("protocol", protocol)
		}
		link = fmt.Sprintf("usbridge://connect?%s", values.Encode())
	}

	tokenEntry := widget.NewEntry()
	tokenEntry.SetText(token)
	tokenEntry.Disable()

	linkEntry := widget.NewEntry()
	linkEntry.SetText(link)
	linkEntry.Disable()

	var qrBlock fyne.CanvasObject
	if link != "" {
		pngBytes, err := qrcode.Encode(link, qrcode.Medium, 280)
		if err == nil {
			image := canvas.NewImageFromResource(fyne.NewStaticResource("agent-token-qr.png", pngBytes))
			image.FillMode = canvas.ImageFillContain
			image.SetMinSize(fyne.NewSize(240, 240))
			qrBlock = container.NewCenter(image)
		} else {
			qrBlock = widget.NewLabel(fmt.Sprintf("QR unavailable: %v", err))
		}
	} else {
		qrBlock = widget.NewLabel("QR link unavailable until the agent has a reachable address.")
	}

	copyTokenBtn := widget.NewButton("Copy Token", func() {
		parent.Clipboard().SetContent(token)
	})
	copyLinkBtn := widget.NewButton("Copy Link", func() {
		if link != "" {
			parent.Clipboard().SetContent(link)
		}
	})
	if link == "" {
		copyLinkBtn.Disable()
	}

	content := container.NewVBox(
		widget.NewLabelWithStyle("Quick Connect", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel("Token"),
		tokenEntry,
		widget.NewLabel("Link"),
		linkEntry,
		qrBlock,
		container.NewHBox(copyTokenBtn, copyLinkBtn),
	)

	d := dialog.NewCustom("Token / QR", "Close", content, parent)
	d.Resize(fyne.NewSize(380, 560))
	d.Show()
}

func (w *Window) quickConnectTarget() (host string, protocol string) {
	if w.ts != nil {
		if status, err := w.ts.Status(context.Background()); err == nil && status != nil && status.LoggedIn {
			switch {
			case strings.TrimSpace(status.Self.DNSName) != "":
				return strings.TrimSpace(status.Self.DNSName), "tailscale"
			case strings.TrimSpace(status.Self.IP4) != "":
				return strings.TrimSpace(status.Self.IP4), "tailscale"
			}
		}
	}

	host = strings.TrimSpace(w.cfg.EffectiveListenHost())
	switch host {
	case "", "127.0.0.1", "localhost", "::1":
		return "", ""
	default:
		return host, "quic"
	}
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

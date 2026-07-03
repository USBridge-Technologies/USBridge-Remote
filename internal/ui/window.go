package ui

import (
	"context"
	"fmt"
	"image/color"
	"log"
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

	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/permissions"
	"usbridge_agent/internal/sunshine"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui/design"
)

type Window struct {
	app   fyne.App
	cfg   config.Config
	token interface {
		RegenerateMasterKey() (config.Config, error)
		SunshineCaptureMode() string
		SetSunshineCaptureMode(mode string) error
		KMSCaptureGranted() bool
		RequestKMSCapture() bool
		RestartSunshine() error
		SunshineRunning() bool
		RestartSunshineElevated() error
		ListSunshineClients() ([]sunshine.Client, error)
		UnpairSunshineClient(uniqueID string) error
		SubmitMoonlightPIN(pin string) error
	}
	perms interface {
		AccessibilityGranted() bool
		ScreenRecordingGranted() bool
		RequestAccessibility() bool
		RequestScreenRecording() bool
		OpenPrivacySettings() error
		OpenScreenRecordingSettings() error
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
	permInfo    *widget.Label

	// Screen Capture: a single unified control for how video gets captured.
	// On Linux this is Sunshine's capture backend (Portal vs. KMS/root); on
	// other platforms it's just the OS screen-recording permission. The
	// status label and request button always reflect whichever method is
	// currently selected — there is only ever one active method at a time.
	screenCaptureLabel  *widget.Label
	screenCaptureSelect *widget.Select
	screenCaptureBtn    *widget.Button

	// sunshineLabel / sunshineBtn are Windows-only: show Sunshine status and
	// offer a UAC-elevation launch button when Sunshine isn't running.
	sunshineLabel *widget.Label
	sunshineBtn   *widget.Button

	// moonlightBtn shows the paired-device count; clicking opens the clients dialog.
	moonlightBtn *widget.Button

	tsInfo    *widget.Label
	tsPeers   *widget.RichText
	tsAuthBtn *widget.Button
	tsMode    *widget.Select

	statusInfo *widget.Label
}

type uiStatus struct {
	tsStatus       *tailscale.Status
	accessGranted  bool
	moonlightCount int
}

func NewWindow(app fyne.App, cfg config.Config, perms *permissions.Service, ts *tailscale.Service, tokenManager interface {
	RegenerateMasterKey() (config.Config, error)
	SunshineCaptureMode() string
	SetSunshineCaptureMode(mode string) error
	KMSCaptureGranted() bool
	RequestKMSCapture() bool
	RestartSunshine() error
	SunshineRunning() bool
	RestartSunshineElevated() error
	ListSunshineClients() ([]sunshine.Client, error)
	UnpairSunshineClient(uniqueID string) error
	SubmitMoonlightPIN(pin string) error
}) *Window {
	return &Window{app: app, cfg: cfg, perms: perms, ts: ts, token: tokenManager}
}

var captureModeLabels = map[string]string{
	"":       "Portal",
	"portal": "Portal",
	"kms":    "KMS (root)",
}

func captureModeFromLabel(label string) string {
	if label == "KMS (root)" {
		return "kms"
	}
	return "portal"
}

// linuxCaptureUIEnabled reports whether the Screen Capture row should show
// the Linux method dropdown (Sunshine capture backend) rather than the
// simple OS screen-recording permission toggle used on other platforms.
func (w *Window) linuxCaptureUIEnabled() bool {
	return runtime.GOOS == "linux" && w.token != nil
}

func (w *Window) applyCaptureMode(label string) {
	if w.token == nil {
		return
	}
	mode := captureModeFromLabel(label)
	if err := w.token.SetSunshineCaptureMode(mode); err != nil {
		logrus.Errorf("🎥 [UI] Failed to set Sunshine capture mode: %v", err)
		return
	}
	logrus.Infof("🎥 [UI] Sunshine capture mode changed to %s (restart Sunshine to apply)", mode)
	w.refreshScreenCaptureUI()
}

// refreshScreenCaptureUI updates the status label and shows/hides the
// request button based on whichever capture method is currently selected —
// never both at once, so there's no ambiguity about which grant a click
// affects.
func (w *Window) refreshScreenCaptureUI() {
	if w.screenCaptureLabel == nil {
		return
	}

	if w.linuxCaptureUIEnabled() {
		mode := w.token.SunshineCaptureMode()
		if mode == "kms" {
			if w.token.KMSCaptureGranted() {
				w.screenCaptureLabel.SetText("Screen Capture: ✅")
				if w.screenCaptureBtn != nil {
					w.screenCaptureBtn.Hide()
				}
			} else {
				w.screenCaptureLabel.SetText("Screen Capture: ❌")
				if w.screenCaptureBtn != nil {
					w.screenCaptureBtn.Show()
				}
			}
			return
		}

		// Portal capture needs no root, but on Wayland the portal permission
		// can (and should) be pre-approved ahead of time — same
		// InitPortalSession() flow used before Sunshine existed — so the
		// system dialog doesn't surprise the user on first capture.
		granted := w.perms != nil && w.perms.ScreenRecordingGranted()
		if granted {
			w.screenCaptureLabel.SetText("Screen Capture: ✅")
		} else {
			w.screenCaptureLabel.SetText("Screen Capture: ❌")
		}
		if w.screenCaptureBtn != nil {
			if !granted && capture.GetLinuxEnv() == "Wayland" {
				w.screenCaptureBtn.Show()
			} else {
				w.screenCaptureBtn.Hide()
			}
		}
		return
	}

	if w.perms == nil {
		return
	}
	if w.perms.ScreenRecordingGranted() {
		w.screenCaptureLabel.SetText("Screen Capture: ✅")
		if w.screenCaptureBtn != nil {
			w.screenCaptureBtn.Hide()
		}
	} else {
		w.screenCaptureLabel.SetText("Screen Capture: ❌")
		if (runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && capture.GetLinuxEnv() == "Wayland")) && w.screenCaptureBtn != nil {
			w.screenCaptureBtn.Show()
		}
	}
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
	// Routes through the same shutdown path as the OS window-close button
	// (SetCloseIntercept below) so Sunshine/ffmpeg/etc. actually get stopped
	// instead of being orphaned when the user clicks this button directly.
	closeBtn := newDangerButton("CLOSE", func() {
		if onClose != nil {
			onClose()
		}
		win.Close()
	})
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

	// Screen Capture: single row covering however the video actually gets
	// captured. On Linux that's Sunshine's backend (Portal, no root vs. KMS,
	// root); elsewhere it's just the OS screen-recording permission.
	w.screenCaptureLabel = widget.NewLabel("Screen Capture")
	w.screenCaptureBtn = widget.NewButton("Request", func() {
		w.screenCaptureBtn.Disable()
		go func() {
			defer fyne.Do(func() {
				if w.screenCaptureBtn != nil {
					w.screenCaptureBtn.Enable()
				}
			})
			if w.linuxCaptureUIEnabled() && w.token.SunshineCaptureMode() == "kms" {
				w.token.RequestKMSCapture()
			} else if runtime.GOOS == "darwin" && w.perms != nil {
				// On macOS, screen recording must be granted to Sunshine
				// (a separate process) via System Preferences — we can't
				// request it on Sunshine's behalf. Open the Settings pane
				// so the user can enable it, then restart Sunshine to pick
				// up the new permission.
				_ = w.perms.OpenScreenRecordingSettings()
				if w.token != nil {
					_ = w.token.RestartSunshine()
				}
			} else if w.perms != nil {
				_ = w.perms.RequestScreenRecording()
			}
			fyne.Do(w.refreshScreenCaptureUI)
		}()
	})
	w.screenCaptureBtn.Importance = widget.HighImportance
	w.permInfo = widget.NewLabel("")
	w.permInfo.Wrapping = fyne.TextWrapWord

	// Adjust for OS. Linux always gets the interactive Screen Capture row
	// (the method dropdown and its request button are meaningful regardless
	// of X11/Wayland — KMS bypasses the display server entirely); other
	// platforms only show request buttons where the permission is actually
	// requestable (macOS, or Wayland's portal flow).
	showButtons := runtime.GOOS == "darwin" || (runtime.GOOS == "linux" && capture.GetLinuxEnv() == "Wayland")
	linuxCapture := w.linuxCaptureUIEnabled()

	if !showButtons && !linuxCapture {
		w.accessBtn.Hide()
		w.screenCaptureBtn.Hide()
	} else {
		w.accessBtn.Resize(fyne.NewSize(80, 24))
		w.screenCaptureBtn.Resize(fyne.NewSize(80, 24))
		if showButtons {
			w.accessBtn.Show()
		} else {
			w.accessBtn.Hide()
		}
	}

	screenCaptureRow := container.NewHBox(w.screenCaptureLabel, layout.NewSpacer())
	if linuxCapture {
		// Build without OnChanged so the initial SetSelected below (which Fyne
		// fires through OnChanged like any other selection) doesn't trigger an
		// unwanted sunshine.conf write on every app start.
		w.screenCaptureSelect = widget.NewSelect([]string{"Portal", "KMS (root)"}, nil)
		w.screenCaptureSelect.SetSelected(captureModeLabels[w.token.SunshineCaptureMode()])
		w.screenCaptureSelect.OnChanged = w.applyCaptureMode
		w.screenCaptureSelect.Resize(fyne.NewSize(120, 24))
		screenCaptureRow.Add(w.screenCaptureSelect)
	}
	screenCaptureRow.Add(w.screenCaptureBtn)

	permRows := []fyne.CanvasObject{
		container.NewHBox(w.accessLabel, layout.NewSpacer(), w.accessBtn),
		screenCaptureRow,
	}
	if !showButtons && !linuxCapture {
		permRows = []fyne.CanvasObject{w.permInfo}
	}

	// Windows: Sunshine elevation row
	if runtime.GOOS == "windows" {
		w.sunshineLabel = widget.NewLabel("Sunshine")
		w.sunshineBtn = widget.NewButton("Launch (Admin)", func() {
			w.sunshineBtn.Disable()
			go func() {
				defer fyne.Do(func() {
					if w.sunshineBtn != nil {
						w.sunshineBtn.Enable()
					}
				})
				if w.token != nil {
					if err := w.token.RestartSunshineElevated(); err != nil {
						log.Printf("[ui] elevated sunshine launch: %v", err)
					}
				}
				w.performRefresh()
			}()
		})
		w.sunshineBtn.Importance = widget.HighImportance
		sunshineRow := container.NewHBox(w.sunshineLabel, layout.NewSpacer(), w.sunshineBtn)
		permRows = append(permRows, sunshineRow)
	}

	// Moonlight Clients — add (+) opens PIN dialog; icon+count opens list; ✕ removes all.
	moonlightAddBtn := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		w.showMoonlightPINDialog(win)
	})
	w.moonlightBtn = widget.NewButtonWithIcon("0", theme.AccountIcon(), func() {
		w.showMoonlightClientsDialog(win)
	})
	moonlightDeleteAllBtn := newDangerGlyphButton(func() {
		dialog.ShowConfirm("Remove All Clients",
			"Remove all paired Moonlight devices?",
			func(yes bool) {
				if !yes || w.token == nil {
					return
				}
				go func() {
					clients, err := w.token.ListSunshineClients()
					if err != nil {
						return
					}
					for _, c := range clients {
						_ = w.token.UnpairSunshineClient(c.UniqueID)
					}
					fyne.Do(func() {
						if w.moonlightBtn != nil {
							w.moonlightBtn.SetText("0")
						}
					})
				}()
			}, win)
	})
	permRows = append(permRows, container.NewHBox(
		widget.NewLabel("Moonlight Clients"),
		layout.NewSpacer(),
		moonlightAddBtn,
		w.moonlightBtn,
		moonlightDeleteAllBtn,
	))

	permContent := newTightVBox(permRows...)
	permBlock := newPanel("Permissions", permContent)

	// Column 2: Stats & Tailscale
	sunshinePort := w.cfg.SunshinePort
	if sunshinePort == 0 {
		sunshinePort = 47990
	}
	w.statusInfo = widget.NewLabel(fmt.Sprintf(
		"OS: %s\nHTTP: %s:%d\nSunshine: 0.0.0.0:%d",
		capture.GetOSInfo(),
		w.cfg.EffectiveListenHost(), w.cfg.HTTPPort,
		sunshinePort,
	))
	w.statusInfo.Wrapping = fyne.TextWrapWord

	sunshineWebBtn := widget.NewButtonWithIcon("", theme.VisibilityIcon(), func() {
		w.showSunshineWebDialog(win, sunshinePort)
	})
	sunshineWebRow := container.NewHBox(
		widget.NewLabel("Sunshine Web"),
		layout.NewSpacer(),
		sunshineWebBtn,
	)

	statsBlock := newPanel("Status", newTightVBox(w.statusInfo, sunshineWebRow))

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
		container.NewBorder(nil, nil, nil, container.NewVBox(w.tsAuthBtn),
			container.NewVBox(container.NewHBox(w.tsMode), w.tsInfo),
		),
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
	w.refreshScreenCaptureUI()

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
		}
		if w.token != nil {
			if clients, err := w.token.ListSunshineClients(); err == nil {
				status.moonlightCount = len(clients)
			}
		}
		var sunshineRunning bool
		if runtime.GOOS == "windows" && w.token != nil {
			sunshineRunning = w.token.SunshineRunning()
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
			if runtime.GOOS == "windows" && w.sunshineLabel != nil {
				if sunshineRunning {
					w.sunshineLabel.SetText("Sunshine: ✅")
				} else {
					w.sunshineLabel.SetText("Sunshine: ❌")
				}
			}
			if w.moonlightBtn != nil {
				w.moonlightBtn.SetText(fmt.Sprintf("%d", status.moonlightCount))
			}
			w.refreshScreenCaptureUI()
			if runtime.GOOS != "darwin" && w.permInfo != nil && w.accessLabel != nil && w.screenCaptureLabel != nil {
				w.permInfo.SetText(fmt.Sprintf("%s\n%s", w.accessLabel.Text, w.screenCaptureLabel.Text))
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
		masterKey := strings.TrimSpace(w.cfg.MasterKey)
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, masterKey, protocol)
		if link != "" {
			parent.Clipboard().SetContent(link)
		}
	})
	regenerateBtn := newIconActionButton("Regenerate Key", theme.ViewRefreshIcon(), nil)

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
		masterKey := strings.TrimSpace(w.cfg.MasterKey)
		if masterKey == "" {
			masterKey = "unavailable"
		}
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, masterKey, protocol)

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
		cfg, err := w.token.RegenerateMasterKey()
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

func buildQuickConnectLink(internalHost, tailscaleHost, masterKey, protocol string) string {
	masterKey = strings.TrimSpace(masterKey)
	if masterKey == "" || masterKey == "unavailable" {
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
	values.Set("master_key", masterKey)
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

func (w *Window) showMoonlightClientsDialog(parent fyne.Window) {
	if w.token == nil || parent == nil {
		return
	}

	listBox := container.NewVBox()
	var dlg *widget.PopUp
	var refreshList func()

	refreshList = func() {
		go func() {
			clients, err := w.token.ListSunshineClients()
			fyne.Do(func() {
				listBox.RemoveAll()
				if err != nil {
					listBox.Add(widget.NewLabel("Error: " + err.Error()))
					listBox.Refresh()
					return
				}
				if len(clients) == 0 {
					emptyLabel := widget.NewLabel("No paired clients")
					emptyLabel.Alignment = fyne.TextAlignCenter
					listBox.Add(emptyLabel)
				} else {
					for _, c := range clients {
						c := c
						displayName := strings.TrimSpace(c.Name)
						if displayName == "" {
							// Sunshine often leaves the name blank — show the UUID instead
							displayName = c.UniqueID
						}
						nameLabel := widget.NewLabel(displayName)
						nameLabel.Truncation = fyne.TextTruncateEllipsis
						removeBtn := newDangerGlyphButton(func() {
							go func() {
								if err := w.token.UnpairSunshineClient(c.UniqueID); err != nil {
									log.Printf("[ui] unpair moonlight client: %v", err)
								}
								refreshList()
							}()
						})
						row := container.NewBorder(nil, nil, nil, removeBtn, nameLabel)
						listBox.Add(row)
					}
				}
				listBox.Refresh()
			})
		}()
	}

	closeBtn := widget.NewButton("Close", func() {
		if dlg != nil {
			dlg.Hide()
		}
	})

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(340, 1))

	titleLabel := canvas.NewText("MOONLIGHT CLIENTS", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true

	content := container.NewVBox(
		titleLabel,
		minWidth,
		widget.NewSeparator(),
		listBox,
		widget.NewSeparator(),
		container.NewCenter(closeBtn),
	)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	card := container.NewStack(cardBG, container.NewPadded(content))

	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	refreshList()
	dlg.Show()
}

func (w *Window) showMoonlightPINDialog(parent fyne.Window) {
	if w.token == nil || parent == nil {
		return
	}

	entry := widget.NewEntry()
	entry.SetPlaceHolder("4-digit PIN from Moonlight")

	statusLabel := widget.NewLabel("")
	statusLabel.Alignment = fyne.TextAlignCenter
	statusLabel.Hide()

	var dlg *widget.PopUp

	var submitBtn *widget.Button
	submitBtn = widget.NewButton("Submit", func() {
		pin := strings.TrimSpace(entry.Text)
		if len(pin) == 0 {
			statusLabel.SetText("Enter the PIN shown in Moonlight")
			statusLabel.Show()
			return
		}
		submitBtn.Disable()
		go func() {
			err := w.token.SubmitMoonlightPIN(pin)
			fyne.Do(func() {
				if err != nil {
					statusLabel.SetText("Error: " + err.Error())
					statusLabel.Show()
					submitBtn.Enable()
				} else {
					if dlg != nil {
						dlg.Hide()
					}
				}
			})
		}()
	})
	submitBtn.Importance = widget.HighImportance

	cancelBtn := widget.NewButton("Cancel", func() {
		if dlg != nil {
			dlg.Hide()
		}
	})

	titleLabel := canvas.NewText("PAIR MOONLIGHT CLIENT", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(320, 1))

	content := container.NewVBox(
		titleLabel,
		minWidth,
		widget.NewSeparator(),
		widget.NewLabel("Open Moonlight → Add PC → enter PIN shown here:"),
		entry,
		statusLabel,
		widget.NewSeparator(),
		container.NewCenter(container.NewHBox(submitBtn, cancelBtn)),
	)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	card := container.NewStack(cardBG, container.NewPadded(content))
	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	dlg.Show()
}

func (w *Window) showSunshineWebDialog(parent fyne.Window, port int) {
	if parent == nil {
		return
	}

	sunshineURL := fmt.Sprintf("https://127.0.0.1:%d", port)

	makeRow := func(labelText, valueText string) fyne.CanvasObject {
		lbl := canvas.NewText(labelText, design.ColorTextMuted)
		lbl.TextSize = 11
		lbl.TextStyle.Bold = true
		lblBox := container.NewGridWrap(fyne.NewSize(60, 16), lbl)

		val := widget.NewLabel(valueText)
		val.Truncation = fyne.TextTruncateEllipsis

		copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			parent.Clipboard().SetContent(valueText)
		})

		return container.NewBorder(nil, nil, lblBox, copyBtn, val)
	}

	var dlg *widget.PopUp

	openBtn := widget.NewButtonWithIcon("Open in Browser", theme.ComputerIcon(), func() {
		if parsed, err := url.Parse(sunshineURL); err == nil {
			_ = w.app.OpenURL(parsed)
		}
	})

	closeBtn := widget.NewButton("Close", func() {
		if dlg != nil {
			dlg.Hide()
		}
	})

	titleLabel := canvas.NewText("SUNSHINE WEB UI", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(360, 1))

	content := container.NewVBox(
		titleLabel,
		minWidth,
		widget.NewSeparator(),
		makeRow("URL:", sunshineURL),
		makeRow("Login:", sunshine.AdminUser),
		makeRow("Pass:", sunshine.AdminPassword),
		widget.NewSeparator(),
		container.NewCenter(container.NewHBox(openBtn, closeBtn)),
	)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	card := container.NewStack(cardBG, container.NewPadded(content))
	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	dlg.Show()
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

// redGlyphTheme overrides the foreground colour to the error/red colour so that
// icons and text inside the themed container appear red while the background
// remains the standard button colour (no DangerImportance red fill).
type redGlyphTheme struct{ fyne.Theme }

func (t *redGlyphTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return design.ColorError
	}
	return t.Theme.Color(name, v)
}

// newDangerGlyphButton returns a cancel-icon (✕) button with a red glyph on a
// normal (non-red) background — use instead of DangerImportance when only the
// icon should be red, not the whole button background.
func newDangerGlyphButton(tapped func()) fyne.CanvasObject {
	btn := widget.NewButtonWithIcon("", theme.CancelIcon(), tapped)
	return container.NewThemeOverride(btn, &redGlyphTheme{design.NewBrandTheme()})
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

package ui

import (
	"context"
	"fmt"
	"image/color"
	"log"
	"net"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
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

	"usbridge_agent/internal/autostart"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/netutil"
	"usbridge_agent/internal/sunshine"
	"usbridge_agent/internal/tailscale"
	"usbridge_agent/internal/ui/design"
)

// TokenProvider is whatever owns the agent's config/Sunshine lifecycle —
// normally *app.App when the GUI starts its own engine, or *adminapi.Client
// when it's attaching to an already-running headless instance instead.
type TokenProvider interface {
	RegenerateMasterKey() (config.Config, error)
	SaveConfig(config.Config) error
	SunshineCaptureMode() string
	SetSunshineCaptureMode(mode string) error
	KMSCaptureGranted() bool
	RequestKMSCapture() bool
	RestartSunshine() error
	ListSunshineClients() ([]sunshine.Client, error)
	UnpairSunshineClient(uniqueID string) error
	SubmitMoonlightPIN(pin string) error
	UpdateListenAddr(host string, port int) (config.Config, error)
	UpdateSunshinePort(port int) (config.Config, error)
	UpdateSunshineStreamAddr(host string, streamPort int) (config.Config, error)
}

// PermsProvider is satisfied by *permissions.Service (embedded engine) or
// *adminapi.Client (thin client attaching to a headless instance).
type PermsProvider interface {
	AccessibilityGranted() bool
	ScreenRecordingGranted() bool
	RequestAccessibility() bool
	RequestScreenRecording() bool
	OpenPrivacySettings() error
	OpenScreenRecordingSettings() error
}

// TailscaleProvider is satisfied by *tailscale.Service (embedded engine) or
// *adminapi.Client (thin client attaching to a headless instance).
type TailscaleProvider interface {
	Status(context.Context) (*tailscale.Status, error)
	StartLogin(context.Context) (string, error)
	Logout(context.Context) error
	SetAuthURLHandler(func(string))
}

// appVersion is set once at startup via SetAppVersion (ldflags -X in each
// platform's build script) and shown as a small "vX.Y.Z" tag in the
// bottom-right corner of the main window.
var appVersion string

// SetAppVersion records the running build's version string.
func SetAppVersion(v string) {
	appVersion = strings.TrimSpace(v)
}

type Window struct {
	app   fyne.App
	cfg   config.Config
	token TokenProvider
	perms PermsProvider
	ts    TailscaleProvider

	// awaitingLocalLogin is true only while the local "Sign In With Google"
	// button has an interactive login in flight. It gates auto-opening a
	// browser from the AuthURL handler: tsnet can also produce an AuthURL from
	// a remote client's sync/register request or from its own first-boot
	// auto-login, and those must NOT pop a browser on this (possibly headless,
	// possibly actively streaming) machine — only the local button's own
	// request should.
	awaitingLocalLogin atomic.Bool

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

	// moonlightBtn shows the paired-device count; clicking opens the clients dialog.
	moonlightBtn *widget.Button

	tsInfo    *widget.Label
	tsPeers   *widget.RichText
	tsAuthBtn *widget.Button

	autostartCheck *widget.Check
}

type uiStatus struct {
	tsStatus       *tailscale.Status
	accessGranted  bool
	moonlightCount int
}

func NewWindow(app fyne.App, cfg config.Config, perms PermsProvider, ts TailscaleProvider, tokenManager TokenProvider) *Window {
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

func (w *Window) ShowAndRun(onClose func()) {
	win := w.app.NewWindow("USBridge Agent")
	win.SetPadded(false)
	win.Resize(fyne.NewSize(640, 400))
	win.CenterOnScreen()

	tokenBtn := newIconActionButton("TOKEN", theme.SettingsIcon(), func() {
		w.showTokenDialog(win)
	})
	header := newHeaderBar(nil, tokenBtn)

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

	// Autostart at Boot: installs the OS-native autostart mechanism (a
	// system-wide systemd unit on Linux — so it starts at boot before any
	// graphical session, which is what KMS capture needs; a LaunchAgent
	// plist on macOS; a Run registry value on Windows — see
	// internal/autostart). The registered command always launches with
	// --headless, so a later normal launch of this same binary/AppImage
	// attaches a GUI to that instance instead of starting a second engine —
	// see app.Start. On Linux this shells out via pkexec, same as the KMS
	// capability grant, so expect a polkit prompt on toggle.
	w.autostartCheck = widget.NewCheck("", nil)
	w.autostartCheck.Checked = autostart.IsEnabled()
	w.autostartCheck.Refresh()
	w.autostartCheck.OnChanged = func(checked bool) {
		w.autostartCheck.Disable()
		go func() {
			var err error
			if checked {
				err = autostart.Enable()
			} else {
				err = autostart.Disable()
			}
			fyne.Do(func() {
				if w.autostartCheck == nil {
					return
				}
				w.autostartCheck.Enable()
				if err != nil {
					logrus.Errorf("[ui] autostart toggle failed: %v", err)
					w.autostartCheck.Checked = !checked
					w.autostartCheck.Refresh()
					dialog.ShowError(err, win)
				}
			})
		}()
	}

	// Autostart at Boot is always shown, regardless of platform — unlike the
	// access/screen-capture rows below, it's never collapsed into permInfo's
	// plain-text summary. Windows (and any other platform with neither
	// interactive request buttons nor the Linux capture dropdown) used to
	// fall into the permInfo-only branch below, which replaced the entire
	// permRows slice and silently dropped the Autostart checkbox with it.
	autostartRow := container.NewHBox(widget.NewLabel("Autostart at Boot"), layout.NewSpacer(), w.autostartCheck)

	var permRows []fyne.CanvasObject
	if !showButtons && !linuxCapture {
		permRows = []fyne.CanvasObject{autostartRow, w.permInfo}
	} else {
		permRows = []fyne.CanvasObject{
			autostartRow,
			container.NewHBox(w.accessLabel, layout.NewSpacer(), w.accessBtn),
			screenCaptureRow,
		}
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

	osLabel := container.NewHBox(makeStatusLabel("OS:"), widget.NewLabel(capture.GetOSInfo()))

	// HTTP listen row
	httpVal := widget.NewLabel(fmt.Sprintf("%s:%d", w.cfg.EffectiveListenHost(), w.cfg.HTTPPort))
	httpWarn := makeWarningBadge()
	if !needsWarnBadge(w.cfg.EffectiveListenHost()) {
		httpWarn.Hide()
	}
	httpEditBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		w.showEditHTTPAddrDialog(win, httpVal, httpWarn)
	})
	httpRow := container.NewBorder(nil, nil,
		container.NewHBox(makeStatusLabel("HTTP:"), httpVal, httpWarn),
		httpEditBtn, nil)

	// Declare sunWebVal first so sunStreamEditBtn closure can capture it
	sunWebVal := widget.NewLabel(fmt.Sprintf("127.0.0.1:%d", sunshinePort))

	// Sunshine GameStream row — Moonlight clients connect here (port = web-1)
	sunStreamPort := sunshinePort - 1
	sunStreamIP := sunshine.ExternalIP()
	if sunStreamIP == "" {
		sunStreamIP = "0.0.0.0"
	}
	sunStreamVal := widget.NewLabel(fmt.Sprintf("%s:%d", sunStreamIP, sunStreamPort))
	sunStreamWarn := makeWarningBadge()
	if !needsWarnBadge(sunStreamIP) {
		sunStreamWarn.Hide()
	}
	sunStreamEditBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		w.showEditSunStreamDialog(win, sunStreamVal, sunStreamWarn, sunWebVal)
	})
	sunStreamRow := container.NewBorder(nil, nil,
		container.NewHBox(makeStatusLabel("Sunshine:"), sunStreamVal, sunStreamWarn),
		sunStreamEditBtn, nil)

	// Sun web / admin API row — always localhost; port editable (remove initial decl moved above)
	sunWebEyeBtn := widget.NewButtonWithIcon("", theme.VisibilityIcon(), func() {
		port := w.cfg.SunshinePort
		if port == 0 {
			port = 47990
		}
		w.showSunshineWebDialog(win, port)
	})
	sunWebEditBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		w.showEditSunPortDialog(win, sunWebVal, sunStreamVal)
	})
	sunWebRow := container.NewBorder(nil, nil,
		container.NewHBox(makeStatusLabel("Sun web:"), sunWebVal),
		container.NewHBox(sunWebEyeBtn, sunWebEditBtn), nil)

	statsBlock := newPanel("Status", newTightVBox(osLabel, httpRow, sunStreamRow, sunWebRow))

	w.tsInfo = widget.NewLabel("Status: checking...\nAccount: not connected\nAddress: unavailable")
	w.tsInfo.Wrapping = fyne.TextWrapWord
	w.tsPeers = widget.NewRichTextFromMarkdown("")
	w.tsPeers.Wrapping = fyne.TextWrapWord

	// The actual "open a browser" action is centralized in the AuthURL handler
	// registered below (via SetAuthURLHandler) — it fires whenever tsnet
	// produces a fresh AuthURL, no matter what triggered it. This button only
	// sets awaitingLocalLogin so the handler knows THIS particular AuthURL was
	// asked for locally, and nudges the login so one actually gets generated.
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

			w.awaitingLocalLogin.Store(true)
			fyne.Do(func() { w.setTailscaleInfo("starting login flow...", "", "") })
			if _, err := w.ts.StartLogin(ctx); err != nil {
				w.awaitingLocalLogin.Store(false)
				fyne.Do(func() { w.setTailscaleInfo(fmt.Sprintf("error: %v", err), "", "") })
			}
		}()
	})

	if w.ts != nil {
		w.ts.SetAuthURLHandler(func(authURL string) {
			if !w.awaitingLocalLogin.CompareAndSwap(true, false) {
				// Triggered by something other than this window's own button (a
				// remote client's sync/register request, or tsnet's first-boot
				// auto-login) — don't pop a browser on this machine unasked.
				logrus.Infof("tailscale ui: AuthURL produced by a non-local trigger, not opening: %s", authURL)
				return
			}
			parsed, parseErr := url.Parse(strings.TrimSpace(authURL))
			if parseErr != nil {
				logrus.Errorf("tailscale ui: failed to parse auth URL %q: %v", authURL, parseErr)
				fyne.Do(func() { w.setTailscaleInfo("invalid login URL received", "", "") })
				return
			}
			logrus.Infof("tailscale ui: captured auth URL: %s", parsed.String())
			fyne.Do(func() {
				if w.app != nil {
					_ = w.app.OpenURL(parsed)
				}
				w.setTailscaleInfo("login link opened in browser", "", "")
			})
		})
	}

	tsPanel := newPanel("Tailscale", newTightVBox(
		container.NewBorder(nil, nil, nil, container.NewVBox(w.tsAuthBtn),
			container.NewVBox(w.tsInfo),
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
	stackLayers := []fyne.CanvasObject{bg, body}
	if v := strings.TrimSpace(appVersion); v != "" {
		versionLabel := canvas.NewText("v"+v, design.ColorTextMuted)
		versionLabel.TextSize = 10
		versionLabel.Alignment = fyne.TextAlignTrailing
		versionCorner := container.NewBorder(nil,
			container.NewPadded(container.NewHBox(layout.NewSpacer(), versionLabel)),
			nil, nil, nil)
		stackLayers = append(stackLayers, versionCorner)
	}
	win.SetContent(container.NewStack(stackLayers...))
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
		fmt.Sprintf("%s (embedded)", endpoint),
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
	// Remove the top margin completely to lift the QR code
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

	// Create a local theme with zero padding only for this dialog
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
		return internalHost, "", "direct"
	}
	return "", "", ""
}

func localQuickConnectIPv4() string {
	return netutil.PreferredIPv4()
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

	var dlg *widget.PopUp

	copyRow := func(labelText, valueText string) fyne.CanvasObject {
		lbl := canvas.NewText(labelText, design.ColorTextMuted)
		lbl.TextSize = 11
		lbl.TextStyle.Bold = true
		lblBox := container.NewGridWrap(fyne.NewSize(52, 16), lbl)
		val := widget.NewLabel(valueText)
		val.Truncation = fyne.TextTruncateEllipsis
		copyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
			parent.Clipboard().SetContent(valueText)
		})
		return container.NewBorder(nil, nil, lblBox, copyBtn, val)
	}

	openBtn := widget.NewButtonWithIcon("Open in Browser", theme.ComputerIcon(), func() {
		if parsed, err := url.Parse(sunshineURL); err == nil {
			_ = w.app.OpenURL(parsed)
		}
	})

	titleLabel := canvas.NewText("SUNSHINE WEB UI", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true

	xBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		if dlg != nil {
			dlg.Hide()
		}
	})
	titleRow := container.NewBorder(nil, nil, titleLabel, xBtn, nil)

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(360, 1))

	// Password row: built dynamically so it reflects the value set by the
	// async bootstrap goroutine even if the dialog opens before it completes.
	passLabel := widget.NewLabel(sunshine.AdminPass())
	passLabel.Truncation = fyne.TextTruncateEllipsis
	passLbl := canvas.NewText("Pass:", design.ColorTextMuted)
	passLbl.TextSize = 11
	passLbl.TextStyle.Bold = true
	passLblBox := container.NewGridWrap(fyne.NewSize(52, 16), passLbl)
	passCopyBtn := widget.NewButtonWithIcon("", theme.ContentCopyIcon(), func() {
		parent.Clipboard().SetContent(sunshine.AdminPass())
	})
	passRow := container.NewBorder(nil, nil, passLblBox, passCopyBtn, passLabel)
	// If password is not yet available (bootstrap still running), poll until ready.
	if passLabel.Text == "" {
		go func() {
			for i := 0; i < 40; i++ {
				time.Sleep(500 * time.Millisecond)
				if p := sunshine.AdminPass(); p != "" {
					fyne.Do(func() { passLabel.SetText(p) })
					return
				}
			}
		}()
	}

	content := container.NewVBox(
		titleRow,
		minWidth,
		widget.NewSeparator(),
		copyRow("URL:", sunshineURL),
		copyRow("Login:", sunshine.AdminUser),
		passRow,
		widget.NewSeparator(),
		container.NewCenter(openBtn),
	)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	card := container.NewStack(cardBG, container.NewPadded(content))
	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	dlg.Show()
}

// showEditSunStreamDialog lets the user change the IP Sunshine advertises to
// Moonlight clients (external_ip in sunshine.conf) and the streaming port.
// The web/admin port is always streamPort+1. Changes are applied by restarting
// Sunshine immediately.
func (w *Window) showEditSunStreamDialog(parent fyne.Window, streamLabel *widget.Label, streamWarn *canvas.Text, webLabel *widget.Label) {
	if parent == nil {
		return
	}

	var dlg *widget.PopUp

	currentStreamPort := w.cfg.SunshinePort - 1
	if currentStreamPort <= 0 {
		currentStreamPort = 47989
	}
	currentIP := sunshine.ExternalIP()
	if currentIP == "" {
		currentIP = "0.0.0.0"
	}

	displays, valueFor, currentDisplay := ipSelectOptions(currentIP)
	hostSelect := widget.NewSelect(displays, nil)
	hostSelect.SetSelected(currentDisplay)

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(currentStreamPort))

	errLabel := widget.NewLabel("")
	errLabel.Alignment = fyne.TextAlignCenter
	errLabel.Hide()

	var saveBtn *widget.Button
	saveBtn = widget.NewButton("Save", func() {
		host := valueFor[hostSelect.Selected]
		if host == "" {
			host = "0.0.0.0"
		}
		streamPort, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || streamPort < 1 || streamPort > 65534 {
			errLabel.SetText("Invalid port (1–65534)")
			errLabel.Show()
			return
		}
		saveBtn.Disable()
		go func() {
			if w.token == nil {
				fyne.Do(func() { saveBtn.Enable() })
				return
			}
			cfg, err := w.token.UpdateSunshineStreamAddr(host, streamPort)
			fyne.Do(func() {
				if err != nil {
					errLabel.SetText("Error: " + err.Error())
					errLabel.Show()
					saveBtn.Enable()
					return
				}
				w.cfg = cfg
				streamLabel.SetText(fmt.Sprintf("%s:%d", host, streamPort))
				if needsWarnBadge(host) {
					streamWarn.Show()
					streamWarn.Refresh()
				} else {
					streamWarn.Hide()
					streamWarn.Refresh()
				}
				webLabel.SetText(fmt.Sprintf("127.0.0.1:%d", streamPort+1))
				if dlg != nil {
					dlg.Hide()
				}
			})
		}()
	})
	saveBtn.Importance = widget.HighImportance

	xBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		if dlg != nil {
			dlg.Hide()
		}
	})
	titleLabel := canvas.NewText("SUNSHINE STREAMING", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true
	titleRow := container.NewBorder(nil, nil, titleLabel, xBtn, nil)

	noteLabel := canvas.NewText("Sets external_ip + port in sunshine.conf · restarts Sunshine", design.ColorTextMuted)
	noteLabel.TextSize = 10

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(340, 1))

	content := container.NewVBox(
		titleRow, minWidth,
		widget.NewSeparator(),
		widget.NewLabel("IP (advertised to Moonlight clients):"), hostSelect,
		widget.NewLabel("Streaming port:"), portEntry,
		noteLabel, errLabel,
		widget.NewSeparator(),
		container.NewCenter(saveBtn),
	)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	card := container.NewStack(cardBG, container.NewPadded(content))
	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	dlg.Show()
}

// needsWarnBadge reports whether a host binding warrants a ⚠ warning.
// Warns for all-interfaces (0.0.0.0/"") and LAN IPs — not for Tailscale or loopback.
func needsWarnBadge(host string) bool {
	if host == "" || host == "0.0.0.0" {
		return true
	}
	p := net.ParseIP(host)
	if p == nil {
		return false
	}
	if p.IsLoopback() || isTailscaleIP(p) {
		return false
	}
	return true // LAN, public, or unknown — show warning
}

// makeWarningBadge returns a yellow ⚠ badge for rows that listen on 0.0.0.0.
func makeWarningBadge() *canvas.Text {
	t := canvas.NewText("⚠", color.RGBA{R: 255, G: 185, B: 0, A: 255})
	t.TextSize = 13
	return t
}

// makeStatusLabel returns a small bold muted text node for status row labels.
func makeStatusLabel(text string) fyne.CanvasObject {
	t := canvas.NewText(text, design.ColorTextMuted)
	t.TextSize = 12
	t.TextStyle.Bold = true
	return t
}

// ipOption pairs a display string (annotated) with the actual IP value.
type ipOption struct{ display, value string }

func isTailscaleIP(ip net.IP) bool {
	_, cidr, _ := net.ParseCIDR("100.64.0.0/10")
	return cidr != nil && cidr.Contains(ip)
}

func isPrivateLANIP(ip net.IP) bool {
	for _, s := range []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"} {
		_, lan, _ := net.ParseCIDR(s)
		if lan != nil && lan.Contains(ip) {
			return true
		}
	}
	return false
}

func annotateIP(ipStr string) string {
	if ipStr == "0.0.0.0" {
		return "0.0.0.0  (all interfaces)"
	}
	p := net.ParseIP(ipStr)
	if p == nil {
		return ipStr
	}
	switch {
	case p.IsLoopback():
		return ipStr + "  (loopback)"
	case isTailscaleIP(p):
		return ipStr + "  (Tailscale)"
	case isPrivateLANIP(p):
		return ipStr + "  (LAN)"
	default:
		return ipStr
	}
}

// localIPOptions returns annotated IP options for host-binding dropdowns.
// Always includes 0.0.0.0 and 127.0.0.1 first, then active interface IPs.
func localIPOptions() []ipOption {
	raw := []string{"0.0.0.0", "127.0.0.1"}
	seen := map[string]bool{"0.0.0.0": true, "127.0.0.1": true}
	if ifaces, err := net.Interfaces(); err == nil {
		for _, iface := range ifaces {
			if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
				continue
			}
			addrs, _ := iface.Addrs()
			for _, addr := range addrs {
				ip, _, err := net.ParseCIDR(addr.String())
				if err != nil || ip.To4() == nil {
					continue
				}
				s := ip.String()
				if !seen[s] {
					seen[s] = true
					raw = append(raw, s)
				}
			}
		}
	}
	opts := make([]ipOption, 0, len(raw))
	for _, ip := range raw {
		opts = append(opts, ipOption{display: annotateIP(ip), value: ip})
	}
	return opts
}

// ipSelectOptions builds display list + value lookup for a widget.Select,
// and finds the display string matching currentVal.
func ipSelectOptions(currentVal string) (displays []string, valueFor map[string]string, currentDisplay string) {
	opts := localIPOptions()
	displays = make([]string, len(opts))
	valueFor = make(map[string]string, len(opts))
	for i, o := range opts {
		displays[i] = o.display
		valueFor[o.display] = o.value
		if o.value == currentVal {
			currentDisplay = o.display
		}
	}
	if currentDisplay == "" {
		currentDisplay = currentVal
	}
	return
}

// showEditHTTPAddrDialog opens a modal to change the agent's HTTP listen
// host and port. Updates valLabel and warnBadge immediately after save.
// The HTTP server itself is NOT hot-reloaded — the user must restart the app.
func (w *Window) showEditHTTPAddrDialog(parent fyne.Window, valLabel *widget.Label, warnBadge *canvas.Text) {
	if parent == nil {
		return
	}

	var dlg *widget.PopUp

	displays, valueFor, currentDisplay := ipSelectOptions(w.cfg.EffectiveListenHost())
	hostSelect := widget.NewSelect(displays, nil)
	hostSelect.SetSelected(currentDisplay)

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(w.cfg.HTTPPort))

	errLabel := widget.NewLabel("")
	errLabel.Alignment = fyne.TextAlignCenter
	errLabel.Hide()

	var saveBtn *widget.Button
	saveBtn = widget.NewButton("Save", func() {
		host := valueFor[hostSelect.Selected]
		if host == "" {
			host = "0.0.0.0"
		}
		port, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || port < 1 || port > 65535 {
			errLabel.SetText("Invalid port (1–65535)")
			errLabel.Show()
			return
		}
		saveBtn.Disable()
		go func() {
			if w.token == nil {
				fyne.Do(func() { saveBtn.Enable() })
				return
			}
			cfg, err := w.token.UpdateListenAddr(host, port)
			fyne.Do(func() {
				if err != nil {
					errLabel.SetText("Error: " + err.Error())
					errLabel.Show()
					saveBtn.Enable()
					return
				}
				w.cfg = cfg
				valLabel.SetText(fmt.Sprintf("%s:%d", cfg.EffectiveListenHost(), cfg.HTTPPort))
				if needsWarnBadge(cfg.EffectiveListenHost()) {
					warnBadge.Show()
					warnBadge.Refresh()
				} else {
					warnBadge.Hide()
					warnBadge.Refresh()
				}
				if dlg != nil {
					dlg.Hide()
				}
			})
		}()
	})
	saveBtn.Importance = widget.HighImportance

	xBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		if dlg != nil {
			dlg.Hide()
		}
	})
	titleLabel := canvas.NewText("HTTP LISTEN ADDRESS", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true
	titleRow := container.NewBorder(nil, nil, titleLabel, xBtn, nil)

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(300, 1))

	content := container.NewVBox(
		titleRow, minWidth,
		widget.NewSeparator(),
		widget.NewLabel("Host:"), hostSelect,
		widget.NewLabel("Port:"), portEntry,
		errLabel,
		widget.NewSeparator(),
		container.NewCenter(saveBtn),
	)

	cardBG := canvas.NewRectangle(design.ColorPanel)
	card := container.NewStack(cardBG, container.NewPadded(content))
	dlg = widget.NewModalPopUp(container.NewCenter(card), parent.Canvas())
	dlg.Show()
}

// showEditSunPortDialog opens a modal to change the Sunshine admin API port.
// Updates valLabel (Sun web) and streamLabel (Sunshine GameStream) immediately;
// also writes sunshine.conf and restarts Sunshine.
func (w *Window) showEditSunPortDialog(parent fyne.Window, valLabel *widget.Label, streamLabel *widget.Label) {
	if parent == nil {
		return
	}

	var dlg *widget.PopUp

	currentPort := w.cfg.SunshinePort
	if currentPort == 0 {
		currentPort = 47990
	}

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(currentPort))

	errLabel := widget.NewLabel("")
	errLabel.Alignment = fyne.TextAlignCenter
	errLabel.Hide()

	var saveBtn *widget.Button
	saveBtn = widget.NewButton("Save", func() {
		port, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || port < 1 || port > 65535 {
			errLabel.SetText("Invalid port (1–65535)")
			errLabel.Show()
			return
		}
		saveBtn.Disable()
		go func() {
			if w.token == nil {
				fyne.Do(func() { saveBtn.Enable() })
				return
			}
			cfg, err := w.token.UpdateSunshinePort(port)
			fyne.Do(func() {
				if err != nil {
					errLabel.SetText("Error: " + err.Error())
					errLabel.Show()
					saveBtn.Enable()
					return
				}
				w.cfg = cfg
				valLabel.SetText(fmt.Sprintf("127.0.0.1:%d", port))
				if streamLabel != nil {
					streamLabel.SetText(fmt.Sprintf("0.0.0.0:%d", port-1))
				}
				if dlg != nil {
					dlg.Hide()
				}
			})
		}()
	})
	saveBtn.Importance = widget.HighImportance

	xBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), func() {
		if dlg != nil {
			dlg.Hide()
		}
	})
	titleLabel := canvas.NewText("SUNSHINE ADMIN PORT", design.ColorTextMuted)
	titleLabel.TextSize = 11
	titleLabel.TextStyle.Bold = true
	titleRow := container.NewBorder(nil, nil, titleLabel, xBtn, nil)

	noteLabel := canvas.NewText("Restarts Sunshine to apply", design.ColorTextMuted)
	noteLabel.TextSize = 11

	minWidth := canvas.NewRectangle(color.Transparent)
	minWidth.SetMinSize(fyne.NewSize(280, 1))

	content := container.NewVBox(
		titleRow, minWidth,
		widget.NewSeparator(),
		widget.NewLabel("Port:"), portEntry,
		noteLabel, errLabel,
		widget.NewSeparator(),
		container.NewCenter(saveBtn),
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

	// Apply a dense theme (zero padding) only to the content inside the panel
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

	// Container for the title with a fixed width
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

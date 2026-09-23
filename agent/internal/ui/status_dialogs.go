package ui

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const statusDialogWidth float32 = 360

// showSunshineWebDialog is the Status-card eye icon: URL / login / password
// with copy, plus Open in Browser. Same chrome as Token / Pair Moonlight.
func (w *Window) showSunshineWebDialog(parent fyne.Window, port int) {
	if parent == nil {
		return
	}

	sunshineURL := fmt.Sprintf("https://127.0.0.1:%d", port)
	adminUser := ""
	adminPass := ""
	if w.token != nil {
		adminUser = w.token.AdminUser()
		adminPass = w.token.AdminPass()
	}

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	labelW := dialogFormLabelWidth("URL", "Login", "Password")

	urlRow := newDialogCopyRow("URL", sunshineURL, labelW, func() string { return sunshineURL }, parent)
	loginRow := newDialogCopyRow("Login", adminUser, labelW, func() string {
		if w.token == nil {
			return ""
		}
		return w.token.AdminUser()
	}, parent)
	passRow, passLabel := newDialogCopyRowLive("Password", adminPass, labelW, func() string {
		if w.token == nil {
			return ""
		}
		return w.token.AdminPass()
	}, parent)

	if passLabel.Text == "" {
		go func() {
			for i := 0; i < 40; i++ {
				time.Sleep(500 * time.Millisecond)
				if w.token == nil {
					return
				}
				if p := w.token.AdminPass(); p != "" {
					fyne.Do(func() { passLabel.SetText(p) })
					return
				}
			}
		}()
	}

	openBtn := newDialogCTA(loc().OpenInBrowser, func() {
		if parsed, err := url.Parse(sunshineURL); err == nil && w.app != nil {
			_ = w.app.OpenURL(parsed)
		}
	})

	body := container.New(&tightVBoxLayout{gap: 8},
		urlRow,
		loginRow,
		passRow,
	)
	footer := container.NewCenter(openBtn)
	panel := newBrandedDialogPanelInsets(loc().SunshineWebUI, statusDialogWidth, 20, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

// showWebClientInfoDialog is the Status-card info glyph next to the
// RustShine web URL: what the link is, with copy and Open in Browser.
func (w *Window) showWebClientInfoDialog(parent fyne.Window) {
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	hint := widget.NewLabel(loc().WebClientHint)
	hint.Wrapping = fyne.TextWrapWord
	hint.Alignment = fyne.TextAlignLeading

	urlRow := newDialogCopyRow("URL", rustshineWebURL, dialogFormLabelWidth("URL"), func() string { return rustshineWebURL }, parent)
	openBtn := newDialogCTA(loc().OpenInBrowser, func() {
		if parsed, err := url.Parse(rustshineWebURL); err == nil && w.app != nil {
			_ = w.app.OpenURL(parsed)
		}
	})

	body := container.New(&tightVBoxLayout{gap: 10},
		wrapDialogLabel(hint, 11, design.ColorMutedOlive),
		urlRow,
	)
	footer := container.NewCenter(openBtn)
	panel := newBrandedDialogPanelInsets(loc().WebClient, statusDialogWidth, 20, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

// showEditSunStreamDialog changes the IP Sunshine advertises to Moonlight
// (external_ip) and the streaming port. The web/admin port is streamPort+1.
func (w *Window) showEditSunStreamDialog(parent fyne.Window, streamLabel *canvas.Text, streamWarn *canvas.Text, webLabel *canvas.Text) {
	if parent == nil {
		return
	}

	currentStreamPort := w.cfg.SunshinePort - 1
	if currentStreamPort <= 0 {
		currentStreamPort = 47989
	}
	currentIP := ""
	if w.token != nil {
		currentIP = w.token.SunshineStreamHost()
	}
	if currentIP == "" {
		currentIP = "0.0.0.0"
	}

	displays, valueFor, currentDisplay := ipSelectOptions(currentIP)
	hostDrop := newDialogDropdown(displays, currentDisplay, nil)

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(currentStreamPort))

	errLabel := dialogErrorText()
	var popup *widget.PopUp
	var saveBtn *iconActionButton
	closeDialog := func() {
		if hostDrop != nil {
			hostDrop.closePopup()
		}
		if popup != nil {
			popup.Hide()
		}
	}

	runSave := func() {
		if saveBtn == nil || saveBtn.Disabled() {
			return
		}
		host := valueFor[hostDrop.Selected]
		if host == "" {
			host = "0.0.0.0"
		}
		streamPort, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || streamPort < 1 || streamPort > 65534 {
			setDialogError(errLabel, loc().InvalidPortWide)
			return
		}
		setDialogError(errLabel, "")
		saveBtn.Disable()
		go func() {
			if w.token == nil {
				fyne.Do(func() { saveBtn.Enable() })
				return
			}
			cfg, err := w.token.UpdateSunshineStreamAddr(host, streamPort)
			fyne.Do(func() {
				if err != nil {
					setDialogError(errLabel, err.Error())
					saveBtn.Enable()
					return
				}
				w.cfg = cfg
				streamLabel.Text = fmt.Sprintf("%s:%d", host, streamPort)
				streamLabel.Refresh()
				if needsWarnBadge(host) {
					streamWarn.Show()
					streamWarn.Refresh()
				} else {
					streamWarn.Hide()
					streamWarn.Refresh()
				}
				webLabel.Text = fmt.Sprintf("127.0.0.1:%d", streamPort+1)
				webLabel.Refresh()
				closeDialog()
			})
		}()
	}
	saveBtn = newDialogCTA(loc().Save, runSave)
	portEntry.OnSubmitted = func(string) { runSave() }

	note := dialogNoteText(loc().SetsExternalIP)
	labelW := dialogFormLabelWidth("IP", "Port")
	body := container.New(&tightVBoxLayout{gap: 8},
		newDialogFormRow("IP", labelW, hostDrop),
		newDialogFormRow("Port", labelW, wrapDialogFieldCompact(portEntry)),
		note,
		errLabel,
	)
	footer := container.NewCenter(saveBtn)
	panel := newBrandedDialogPanelInsets(loc().SunshineStreaming, statusDialogWidth, 20, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

// showEditHTTPAddrDialog changes the agent's HTTP listen host/port and its
// HTTPS listen port/enabled flag (see config.Config's TLSPort/TLSEnabled
// doc comments) -- both servers hot-restart immediately (App.restartMainHTTP,
// App.restartTLS), no app restart needed.
func (w *Window) showEditHTTPAddrDialog(parent fyne.Window, valLabel *canvas.Text, warnBadge *canvas.Text) {
	if parent == nil {
		return
	}

	displays, valueFor, currentDisplay := ipSelectOptions(w.cfg.EffectiveListenHost())
	hostDrop := newDialogDropdown(displays, currentDisplay, nil)

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(w.cfg.HTTPPort))

	// HTTPS listener (internal/tlshost, internal/devicecert) -- required for
	// the browser-based web client (loaded from https://web.usbridge.io) to
	// reach this agent at all; see config.Config's TLSPort/TLSEnabled doc
	// comments. Checked by default (TLSEnabledOK's nil-means-true
	// convention) so existing installs keep it on unless a user explicitly
	// unchecks it here.
	tlsPortEntry := widget.NewEntry()
	tlsPort := w.cfg.TLSPort
	if tlsPort == 0 {
		tlsPort = 8443
	}
	tlsPortEntry.SetText(strconv.Itoa(tlsPort))
	tlsCheck := widget.NewCheck("Enable HTTPS", nil)
	tlsCheck.SetChecked(w.cfg.TLSEnabledOK())

	errLabel := dialogErrorText()
	var popup *widget.PopUp
	var saveBtn *iconActionButton
	closeDialog := func() {
		if hostDrop != nil {
			hostDrop.closePopup()
		}
		if popup != nil {
			popup.Hide()
		}
	}

	runSave := func() {
		if saveBtn == nil || saveBtn.Disabled() {
			return
		}
		host := valueFor[hostDrop.Selected]
		if host == "" {
			host = "0.0.0.0"
		}
		port, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || port < 1 || port > 65535 {
			setDialogError(errLabel, loc().InvalidPort)
			return
		}
		tlsPort, err := strconv.Atoi(strings.TrimSpace(tlsPortEntry.Text))
		if err != nil || tlsPort < 1 || tlsPort > 65535 {
			setDialogError(errLabel, loc().InvalidPort)
			return
		}
		setDialogError(errLabel, "")
		saveBtn.Disable()
		go func() {
			if w.token == nil {
				fyne.Do(func() { saveBtn.Enable() })
				return
			}
			cfg, err := w.token.UpdateListenAddr(host, port)
			if err == nil {
				cfg, err = w.token.UpdateTLSAddr(tlsPort, tlsCheck.Checked)
			}
			fyne.Do(func() {
				if err != nil {
					setDialogError(errLabel, err.Error())
					saveBtn.Enable()
					return
				}
				w.cfg = cfg
				valLabel.Text = fmt.Sprintf("%s:%d", cfg.EffectiveListenHost(), cfg.HTTPPort)
				valLabel.Refresh()
				if needsWarnBadge(cfg.EffectiveListenHost()) {
					warnBadge.Show()
					warnBadge.Refresh()
				} else {
					warnBadge.Hide()
					warnBadge.Refresh()
				}
				closeDialog()
			})
		}()
	}
	saveBtn = newDialogCTA(loc().Save, runSave)
	portEntry.OnSubmitted = func(string) { runSave() }
	tlsPortEntry.OnSubmitted = func(string) { runSave() }

	labelW := dialogFormLabelWidth("HTTPS Port", "Host")
	body := container.New(&tightVBoxLayout{gap: 8},
		newDialogFormRow("Host", labelW, hostDrop),
		newDialogFormRow("Port", labelW, wrapDialogFieldCompact(portEntry)),
		newDialogFormRow("HTTPS Port", labelW, wrapDialogFieldCompact(tlsPortEntry)),
		newDialogFormRow("", labelW, tlsCheck),
		errLabel,
	)
	footer := container.NewCenter(saveBtn)
	panel := newBrandedDialogPanelInsets("HTTP/HTTPS Listen Address", statusDialogWidth, 20, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

// showEditSunPortDialog changes the Sunshine admin API port, writes
// sunshine.conf, and restarts Sunshine.
func (w *Window) showEditSunPortDialog(parent fyne.Window, valLabel *canvas.Text, streamLabel *canvas.Text) {
	if parent == nil {
		return
	}

	currentPort := w.cfg.SunshinePort
	if currentPort == 0 {
		currentPort = 47990
	}

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(currentPort))

	errLabel := dialogErrorText()
	var popup *widget.PopUp
	var saveBtn *iconActionButton
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	runSave := func() {
		if saveBtn == nil || saveBtn.Disabled() {
			return
		}
		port, err := strconv.Atoi(strings.TrimSpace(portEntry.Text))
		if err != nil || port < 1 || port > 65535 {
			setDialogError(errLabel, loc().InvalidPort)
			return
		}
		setDialogError(errLabel, "")
		saveBtn.Disable()
		go func() {
			if w.token == nil {
				fyne.Do(func() { saveBtn.Enable() })
				return
			}
			cfg, err := w.token.UpdateSunshinePort(port)
			fyne.Do(func() {
				if err != nil {
					setDialogError(errLabel, err.Error())
					saveBtn.Enable()
					return
				}
				w.cfg = cfg
				valLabel.Text = fmt.Sprintf("127.0.0.1:%d", port)
				valLabel.Refresh()
				if streamLabel != nil {
					streamLabel.Text = fmt.Sprintf("0.0.0.0:%d", port-1)
					streamLabel.Refresh()
				}
				closeDialog()
			})
		}()
	}
	saveBtn = newDialogCTA(loc().Save, runSave)
	portEntry.OnSubmitted = func(string) { runSave() }

	note := dialogNoteText(loc().RestartsSunshine)
	labelW := dialogFormLabelWidth("Port")
	body := container.New(&tightVBoxLayout{gap: 8},
		newDialogFormRow("Port", labelW, wrapDialogFieldCompact(portEntry)),
		note,
		errLabel,
	)
	footer := container.NewCenter(saveBtn)
	panel := newBrandedDialogPanelInsets(loc().SunshineAdminPort, statusDialogWidth, 20, 10, body, footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func newDialogCopyRow(label, value string, labelW float32, getValue func() string, parent fyne.Window) fyne.CanvasObject {
	row, _ := newDialogCopyRowLive(label, value, labelW, getValue, parent)
	return row
}

func newDialogCopyRowLive(label, value string, labelW float32, getValue func() string, parent fyne.Window) (fyne.CanvasObject, *widget.Label) {
	val := widget.NewLabel(value)
	val.Truncation = fyne.TextTruncateEllipsis
	copyBtn := newTinyGlyphButtonColored(theme.ContentCopyIcon(), design.ColorNameMutedOlive, func() {
		s := value
		if getValue != nil {
			s = getValue()
		}
		if parent != nil {
			parent.Clipboard().SetContent(s)
		}
	})
	field := container.NewBorder(nil, nil, nil, container.NewCenter(copyBtn),
		wrapDialogValueBox(wrapDialogLabel(val, 10, design.ColorAddress)))
	return newDialogFormRow(label, labelW, field), val
}

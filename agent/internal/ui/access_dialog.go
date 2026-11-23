package ui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	qrcode "github.com/skip2/go-qrcode"

	"usbridge_agent/internal/guestauth"
	"usbridge_agent/internal/ui/design"
)

const accessDialogWidth float32 = 460

// showAccessDialog opens the unified Access, Policies & Tokens dialog:
//  1. Scoped Access (self-hosted local tokens with full TPN checkbox matrix + QR)
//  2. Security & Admin PIN (Argon2id PIN, Always Require PIN, Disable Master Key Remote)
//  3. Master Key (Root QR/link, guarded by Admin PIN when configured)
func (w *Window) showAccessDialog(parent fyne.Window) {
	if parent == nil {
		parent = w.guiWin
	}
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	// Shared PIN prompt helper: if Admin PIN is configured and required
	// (AlwaysRequirePIN or active restricted session), prompts before running fn.
	withAdminPIN := func(fn func(pin string)) {
		if w.token == nil {
			return
		}
		configured, alwaysReq, _ := w.token.AdminPINStatus()
		restricted := len(w.token.ActiveRestrictedSessions()) > 0
		if !configured || (!alwaysReq && !restricted) {
			fn(w.cachedAdminPIN)
			return
		}
		if w.cachedAdminPIN != "" && w.token.VerifyAdminPIN(w.cachedAdminPIN) {
			fn(w.cachedAdminPIN)
			return
		}
		w.promptAdminPINDialog(parent, func(pin string) {
			w.cachedAdminPIN = pin
			fn(pin)
		})
	}

	scopedTab := w.buildScopedAccessTab(parent, withAdminPIN)
	securityTab := w.buildSecurityTab(parent, withAdminPIN)
	rootTab := w.buildMasterKeyTab(parent, withAdminPIN)

	tabs := container.NewAppTabs(
		container.NewTabItem("Scoped Access", scopedTab),
		container.NewTabItem("Security & PIN", securityTab),
		container.NewTabItem("Master Key (Root)", rootTab),
	)
	tabs.SetTabLocation(container.TabLocationTop)

	panel := newBrandedDialogPanelChrome(
		"Access & Security Policies",
		"Issue scoped QR tokens (TPN / self-hosted) or manage Admin PIN",
		accessDialogWidth, 16, 10,
		tabs, nil, closeDialog,
	)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func (w *Window) promptAdminPINDialog(parent fyne.Window, onVerified func(pin string)) {
	pinEntry := widget.NewPasswordEntry()
	pinEntry.SetPlaceHolder("Enter Admin PIN")
	errLbl := canvas.NewText("", color.NRGBA{R: 239, G: 68, B: 68, A: 255})
	errLbl.TextSize = 10
	errLbl.Hide()

	var pop *widget.PopUp
	submit := func() {
		if w.token == nil {
			return
		}
		if !w.token.VerifyAdminPIN(pinEntry.Text) {
			errLbl.Text = "Incorrect Admin PIN"
			errLbl.Show()
			errLbl.Refresh()
			return
		}
		pin := pinEntry.Text
		if pop != nil {
			pop.Hide()
		}
		onVerified(pin)
	}
	pinEntry.OnSubmitted = func(string) { submit() }

	okBtn := newIconActionButton("Confirm", nil, submit)
	okBtn.Compact = true
	okBtn.Accent = true
	cancelBtn := newIconActionButton("Cancel", nil, func() {
		if pop != nil {
			pop.Hide()
		}
	})
	cancelBtn.Compact = true

	body := container.NewVBox(
		pinEntry,
		errLbl,
		spacerSize(1, 8),
		container.NewCenter(container.New(&tightHBoxLayout{gap: 8}, okBtn, cancelBtn)),
	)
	panel := newBrandedDialogPanelChrome("Admin PIN Required", "Verify administrator PIN to modify access policies", 320, 16, 8, body, nil, func() {
		if pop != nil {
			pop.Hide()
		}
	})
	pop = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func (w *Window) showScopedTokenQR(parent fyne.Window, label, token string) {
	internalHost, tailscaleHost, protocol, hwID := w.quickConnectTargets()
	link := buildQuickConnectLink(internalHost, tailscaleHost, w.tailnetDeviceHost(), token, protocol, hwID)

	qrImg := canvas.NewImageFromResource(nil)
	qrImg.FillMode = canvas.ImageFillContain
	qrImg.SetMinSize(fyne.NewSize(168, 168))
	if pngBytes, err := qrcode.Encode(link, qrcode.Medium, 280); err == nil {
		qrImg.Resource = fyne.NewStaticResource("scoped-token-qr.png", pngBytes)
	}

	linkEntry := newReadOnlyEntry()
	linkEntry.SetFrozen(link)
	linkField := wrapTokenTextField(linkEntry)

	copyBtn := newIconActionButton(loc().CopyLink, theme.ContentCopyIcon(), func() {
		if link != "" {
			parent.Clipboard().SetContent(link)
		}
	})
	copyBtn.Compact = true
	copyBtn.Accent = true

	var pop *widget.PopUp
	closeFn := func() {
		if pop != nil {
			pop.Hide()
		}
	}
	body := container.NewVBox(
		container.NewCenter(qrImg),
		spacerSize(1, 8),
		linkField,
		spacerSize(1, 8),
		container.NewCenter(copyBtn),
	)
	panel := newBrandedDialogPanelChrome("Scoped QR: "+label, "Self-hosted token (no cloud required)", 380, 16, 8, body, nil, closeFn)
	pop = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func (w *Window) buildScopedAccessTab(parent fyne.Window, withAdminPIN func(func(string))) fyne.CanvasObject {
	listBox := container.NewVBox()

	var refreshList func()
	refreshList = func() {
		listBox.Objects = nil
		if w.token == nil {
			listBox.Refresh()
			return
		}
		shares := w.token.ListAccessTokens()
		activeMap := map[string]bool{}
		for _, s := range w.token.ActiveRestrictedSessions() {
			activeMap[s.ShareID] = true
		}
		now := time.Now()
		if len(shares) == 0 {
			empty := canvas.NewText("No scoped access tokens yet. Click '+ New Access Token' below.", design.ColorMutedOlive)
			empty.TextSize = 11
			listBox.Add(empty)
		}
		for _, sh := range shares {
			sh := sh
			status := "Active"
			if !sh.Live(now) {
				status = "Revoked/Expired"
			} else if activeMap[sh.ID] {
				status = "CONNECTED"
			}
			header := canvas.NewText(fmt.Sprintf("%s  [%s]", sh.DisplayName(), status), design.ColorTextLight)
			header.TextSize = 11
			header.TextStyle.Bold = true

			pol := sh.EffectivePolicy()
			summary := canvas.NewText(formatPolicySummary(pol), design.ColorMutedOlive)
			summary.TextSize = 9

			btns := container.New(&tightHBoxLayout{gap: 4})
			if sh.Local && sh.Live(now) {
				qrBtn := newIconActionButton("QR", nil, func() {
					w.showScopedTokenQR(parent, sh.DisplayName(), sh.LocalToken())
				})
				qrBtn.Compact = true
				btns.Add(qrBtn)
			}
			if sh.Live(now) {
				editBtn := newIconActionButton("Edit", nil, func() {
					withAdminPIN(func(pin string) {
						w.showPolicyMatrixEditor(parent, &sh, pin, refreshList)
					})
				})
				editBtn.Compact = true
				btns.Add(editBtn)
			}
			if activeMap[sh.ID] {
				kickBtn := newIconActionButton("Kick", nil, func() {
					withAdminPIN(func(pin string) {
						if err := w.token.KickAccessSession(pin, sh.ID); err != nil {
							showErrorDialog(err, parent)
							return
						}
						refreshList()
					})
				})
				kickBtn.Compact = true
				btns.Add(kickBtn)
			}
			if sh.Live(now) {
				revBtn := newIconActionButton("Revoke", nil, func() {
					withAdminPIN(func(pin string) {
						if err := w.token.RevokeAccessToken(pin, sh.ID); err != nil {
							showErrorDialog(err, parent)
							return
						}
						refreshList()
					})
				})
				revBtn.Compact = true
				revBtn.Danger = true
				btns.Add(revBtn)
			}

			rowBg := canvas.NewRectangle(design.ColorGray950)
			rowBg.CornerRadius = 6
			rowBg.StrokeColor = design.ColorBorder
			rowBg.StrokeWidth = 1
			rowInner := container.NewVBox(
				container.NewBorder(nil, nil, nil, btns, header),
				summary,
			)
			listBox.Add(container.NewStack(rowBg, newExactInset(rowInner, 8, 8, 6, 6)))
			listBox.Add(spacerSize(1, 4))
		}
		listBox.Refresh()
	}

	newBtn := newIconActionButton("+ New Access Token", nil, func() {
		withAdminPIN(func(pin string) {
			w.showPolicyMatrixEditor(parent, nil, pin, refreshList)
		})
	})
	newBtn.Compact = true
	newBtn.Accent = true

	refreshList()
	scroll := container.NewVScroll(listBox)
	scroll.SetMinSize(fyne.NewSize(420, 260))

	return container.NewVBox(
		container.New(&flushEndsLayout{}, canvas.NewText("Local & Guest Access Tokens", design.ColorSectionTitle), newBtn),
		spacerSize(1, 8),
		scroll,
	)
}

func formatPolicySummary(pol guestauth.Policy) string {
	var tags []string
	if pol.Has(guestauth.CapView) {
		tags = append(tags, "Screen")
	}
	if pol.Has(guestauth.CapInput) {
		tags = append(tags, "Input")
	}
	if pol.Has(guestauth.CapPenTablet) {
		tags = append(tags, "Wacom")
	}
	if pol.Has(guestauth.CapClipToHost) || pol.Has(guestauth.CapClipFromHost) {
		tags = append(tags, "Clip")
	} else {
		tags = append(tags, "No-Clip")
	}
	if pol.Has(guestauth.CapFileTransfer) {
		tags = append(tags, "Files")
	} else {
		tags = append(tags, "No-Files")
	}
	if pol.USB.AllowMassStorage {
		tags = append(tags, "USB-Storage")
	} else {
		tags = append(tags, "No-Flash")
	}
	return strings.Join(tags, " · ")
}

func (w *Window) showPolicyMatrixEditor(parent fyne.Window, existing *guestauth.Share, adminPIN string, onSaved func()) {
	pol := guestauth.PresetVFXTPN()
	labelVal := ""
	consentVal := false
	if existing != nil {
		pol = existing.EffectivePolicy()
		labelVal = existing.DisplayName()
		consentVal = existing.RequireConsent
	}

	labelEntry := widget.NewEntry()
	labelEntry.SetPlaceHolder("Client name (e.g. vfx-artist-01)")
	labelEntry.SetText(labelVal)

	// Checkbox Matrix
	cbView := newStyledCheck("View Screen", pol.Has(guestauth.CapView), nil)
	cbAudio := newStyledCheck("Audio", pol.Has(guestauth.CapAudio), nil)
	cbInput := newStyledCheck("Mouse & Keyboard", pol.Has(guestauth.CapInput), nil)
	cbPen := newStyledCheck("Wacom / Pen (RawHID)", pol.Has(guestauth.CapPenTablet), nil)
	cbGamepad := newStyledCheck("Gamepads", pol.Has(guestauth.CapGamepad), nil)

	cbClipTo := newStyledCheck("Clipboard: Client → Host", pol.Has(guestauth.CapClipToHost), nil)
	cbClipFrom := newStyledCheck("Clipboard: Host → Client", pol.Has(guestauth.CapClipFromHost), nil)
	cbFiles := newStyledCheck("File Transfer / Drag&Drop", pol.Has(guestauth.CapFileTransfer), nil)

	cbUSBHID := newStyledCheck("HID (Kbd/Mouse/SpaceMouse)", pol.USB.AllowHID, nil)
	cbUSBPen := newStyledCheck("Pen Tablet / Wacom", pol.USB.AllowPenTablet, nil)
	cbUSBAudio := newStyledCheck("USB Audio", pol.USB.AllowAudio, nil)
	cbUSBStorage := newStyledCheck("Mass Storage (Flash)", pol.USB.AllowMassStorage, nil)
	cbUSBOther := newStyledCheck("Other USB Classes", pol.USB.AllowOther, nil)

	vidPidEntry := widget.NewEntry()
	vidPidEntry.SetPlaceHolder("VID:PID (e.g. 0529:0001)")
	vidPidEntry.SetText(strings.Join(pol.USB.AllowedVIDPIDs, ", "))

	cbDisplay := newStyledCheck("Displays / Virtual Mon.", pol.Has(guestauth.CapDisplay), nil)
	cbVideoSet := newStyledCheck("Video / Codec Settings", pol.Has(guestauth.CapVideoSettings), nil)
	cbConsent := newStyledCheck("Require Host Consent", consentVal, nil)

	applyPreset := func(p guestauth.Policy) {
		cbView.SetChecked(p.Has(guestauth.CapView))
		cbAudio.SetChecked(p.Has(guestauth.CapAudio))
		cbInput.SetChecked(p.Has(guestauth.CapInput))
		cbPen.SetChecked(p.Has(guestauth.CapPenTablet))
		cbGamepad.SetChecked(p.Has(guestauth.CapGamepad))
		cbClipTo.SetChecked(p.Has(guestauth.CapClipToHost))
		cbClipFrom.SetChecked(p.Has(guestauth.CapClipFromHost))
		cbFiles.SetChecked(p.Has(guestauth.CapFileTransfer))
		cbUSBHID.SetChecked(p.USB.AllowHID)
		cbUSBPen.SetChecked(p.USB.AllowPenTablet)
		cbUSBAudio.SetChecked(p.USB.AllowAudio)
		cbUSBStorage.SetChecked(p.USB.AllowMassStorage)
		cbUSBOther.SetChecked(p.USB.AllowOther)
		cbDisplay.SetChecked(p.Has(guestauth.CapDisplay))
		cbVideoSet.SetChecked(p.Has(guestauth.CapVideoSettings))
	}

	readPolicy := func() guestauth.Policy {
		var caps guestauth.Cap
		if cbView.Checked {
			caps |= guestauth.CapView
		}
		if cbAudio.Checked {
			caps |= guestauth.CapAudio
		}
		if cbInput.Checked {
			caps |= guestauth.CapInput
		}
		if cbPen.Checked {
			caps |= guestauth.CapPenTablet
		}
		if cbGamepad.Checked {
			caps |= guestauth.CapGamepad
		}
		if cbClipTo.Checked {
			caps |= guestauth.CapClipToHost
		}
		if cbClipFrom.Checked {
			caps |= guestauth.CapClipFromHost
		}
		if cbFiles.Checked {
			caps |= guestauth.CapFileTransfer
		}
		if cbUSBHID.Checked || cbUSBPen.Checked || cbUSBAudio.Checked || cbUSBOther.Checked || strings.TrimSpace(vidPidEntry.Text) != "" {
			caps |= guestauth.CapUSB
		}
		if cbUSBStorage.Checked {
			caps |= guestauth.CapDisks | guestauth.CapUSB
		}
		if cbDisplay.Checked {
			caps |= guestauth.CapDisplay
		}
		if cbVideoSet.Checked {
			caps |= guestauth.CapVideoSettings
		}
		var vids []string
		for _, part := range strings.Split(vidPidEntry.Text, ",") {
			if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
				vids = append(vids, p)
			}
		}
		return guestauth.Policy{
			Caps: caps,
			USB: guestauth.USBPolicy{
				AllowHID:         cbUSBHID.Checked,
				AllowPenTablet:   cbUSBPen.Checked,
				AllowAudio:       cbUSBAudio.Checked,
				AllowMassStorage: cbUSBStorage.Checked,
				AllowOther:       cbUSBOther.Checked,
				AllowedVIDPIDs:   vids,
			},
		}
	}

	pVFX := newIconActionButton("VFX / TPN", nil, func() { applyPreset(guestauth.PresetVFXTPN()) })
	pVFX.Compact = true
	pVFX.Accent = true
	pView := newIconActionButton("Viewer", nil, func() { applyPreset(guestauth.PresetViewer()) })
	pView.Compact = true
	pOp := newIconActionButton("Operator", nil, func() { applyPreset(guestauth.PresetOperator()) })
	pOp.Compact = true
	pTech := newIconActionButton("Tech", nil, func() { applyPreset(guestauth.PresetTechnician()) })
	pTech.Compact = true

	expiryLabels := []string{"No expiry", "1 hour", "8 hours", "24 hours", "7 days", "30 days"}
	expirySecs := map[string]int64{
		"No expiry": 0,
		"1 hour":    3600,
		"8 hours":   8 * 3600,
		"24 hours":  24 * 3600,
		"7 days":    7 * 24 * 3600,
		"30 days":   30 * 24 * 3600,
	}
	selectedExpiry := "No expiry"
	expiryDrop := newDialogDropdown(expiryLabels, selectedExpiry, func(s string) {
		selectedExpiry = s
	})

	var pop *widget.PopUp
	closeFn := func() {
		if pop != nil {
			pop.Hide()
		}
	}

	saveBtn := newIconActionButton("Save & Generate QR", nil, func() {
		if w.token == nil {
			return
		}
		lbl := strings.TrimSpace(labelEntry.Text)
		if lbl == "" {
			showErrorDialog(fmt.Errorf("please enter a client name"), parent)
			return
		}
		p := readPolicy()
		if existing != nil {
			exp := existing.ExpiresAt
			if sec := expirySecs[selectedExpiry]; sec > 0 {
				exp = time.Now().Add(time.Duration(sec) * time.Second).Unix()
			}
			if _, err := w.token.UpdateAccessPolicy(adminPIN, existing.ID, lbl, p, cbConsent.Checked, exp); err != nil {
				showErrorDialog(err, parent)
				return
			}
			closeFn()
			if onSaved != nil {
				onSaved()
			}
			return
		}
		sh, tok, err := w.token.CreateLocalToken(adminPIN, lbl, p, expirySecs[selectedExpiry], cbConsent.Checked)
		if err != nil {
			showErrorDialog(err, parent)
			return
		}
		closeFn()
		if onSaved != nil {
			onSaved()
		}
		w.showScopedTokenQR(parent, sh.DisplayName(), tok)
	})
	saveBtn.Compact = true
	saveBtn.Accent = true
	if existing != nil {
		saveBtn.Text = "Save Policy"
	}

	sectionCard := func(title string, items ...fyne.CanvasObject) fyne.CanvasObject {
		t := canvas.NewText(title, design.ColorSectionTitle)
		t.TextSize = 10
		t.TextStyle.Bold = true
		col := container.New(&tightVBoxLayout{gap: 3}, append([]fyne.CanvasObject{t, spacerSize(1, 2)}, items...)...)
		cardBg := canvas.NewRectangle(design.ColorGray950)
		cardBg.CornerRadius = 6
		cardBg.StrokeColor = design.ColorBorder
		cardBg.StrokeWidth = 1
		return container.NewStack(cardBg, newExactInset(col, 8, 8, 7, 7))
	}

	cardScreen := sectionCard("SCREEN & INPUT",
		cbView, cbAudio, cbInput, cbPen, cbGamepad,
	)
	cardDLP := sectionCard("DLP (TPN) & HOST",
		cbClipTo, cbClipFrom, cbFiles,
		cbDisplay, cbVideoSet, cbConsent,
	)
	cardUSB := sectionCard("USB WHITELIST",
		cbUSBHID, cbUSBPen, cbUSBAudio, cbUSBStorage, cbUSBOther,
		spacerSize(1, 2),
		vidPidEntry,
	)

	topRow := container.NewBorder(nil, nil, nil,
		container.New(&tightHBoxLayout{gap: 6}, expiryDrop, saveBtn),
		labelEntry,
	)
	presetLbl := canvas.NewText("Presets:", design.ColorMutedOlive)
	presetLbl.TextSize = 10
	presetRow := container.New(&tightHBoxLayout{gap: 6}, presetLbl, pVFX, pView, pOp, pTech)

	grid := container.NewGridWithColumns(3, cardScreen, cardDLP, cardUSB)

	title := "New Scoped Access Token"
	if existing != nil {
		title = "Edit Policy: " + existing.DisplayName()
	}
	body := container.NewVBox(
		topRow,
		spacerSize(1, 6),
		presetRow,
		spacerSize(1, 6),
		grid,
	)
	panel := newBrandedDialogPanelChrome(title, "Configure granular TPN permissions", 740, 14, 6, body, nil, closeFn)
	pop = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func (w *Window) buildSecurityTab(parent fyne.Window, withAdminPIN func(func(string))) fyne.CanvasObject {
	configured, alwaysReq, disableMaster := false, false, false
	if w.token != nil {
		configured, alwaysReq, disableMaster = w.token.AdminPINStatus()
	}

	statusLbl := canvas.NewText("", design.ColorAddress)
	statusLbl.TextSize = 11
	refreshStatus := func() {
		if w.token != nil {
			configured, alwaysReq, disableMaster = w.token.AdminPINStatus()
		}
		if configured {
			statusLbl.Text = "Admin PIN: Configured (Argon2id)"
		} else {
			statusLbl.Text = "Admin PIN: Not set"
		}
		statusLbl.Refresh()
	}
	refreshStatus()

	curPIN := widget.NewPasswordEntry()
	curPIN.SetPlaceHolder("Current PIN (if set)")
	newPIN := widget.NewPasswordEntry()
	newPIN.SetPlaceHolder("New PIN (min 4 chars; blank to clear)")

	savePINBtn := newIconActionButton("Update Admin PIN", nil, func() {
		if w.token == nil {
			return
		}
		if err := w.token.SetAdminPIN(curPIN.Text, newPIN.Text); err != nil {
			showErrorDialog(err, parent)
			return
		}
		w.cachedAdminPIN = newPIN.Text
		curPIN.SetText("")
		newPIN.SetText("")
		refreshStatus()
	})
	savePINBtn.Compact = true
	savePINBtn.Accent = true

	var alwaysCheck *styledCheck
	alwaysCheck = newStyledCheck("Always require Admin PIN to change policies/tokens", alwaysReq, func(on bool) {
		withAdminPIN(func(pin string) {
			_, _, curDisable := w.token.AdminPINStatus()
			if err := w.token.SetSecurityOptions(pin, on, curDisable); err != nil {
				showErrorDialog(err, parent)
				alwaysCheck.SetChecked(!on)
			}
		})
	})

	var disableMasterCheck *styledCheck
	disableMasterCheck = newStyledCheck("Disable remote Master Key (TPN strict mode)", disableMaster, func(on bool) {
		withAdminPIN(func(pin string) {
			_, curAlways, _ := w.token.AdminPINStatus()
			if err := w.token.SetSecurityOptions(pin, curAlways, on); err != nil {
				showErrorDialog(err, parent)
				disableMasterCheck.SetChecked(!on)
			}
		})
	})

	return container.NewVBox(
		statusLbl,
		spacerSize(1, 6),
		curPIN,
		newPIN,
		spacerSize(1, 6),
		container.NewCenter(savePINBtn),
		spacerSize(1, 12),
		alwaysCheck,
		disableMasterCheck,
	)
}

func (w *Window) buildMasterKeyTab(parent fyne.Window, withAdminPIN func(func(string))) fyne.CanvasObject {
	warn := canvas.NewText("WARNING: Master Key grants unrestricted ROOT access.", color.NRGBA{R: 245, G: 158, B: 11, A: 255})
	warn.TextSize = 10
	warn.Alignment = fyne.TextAlignCenter

	qrImage := canvas.NewImageFromResource(nil)
	qrImage.FillMode = canvas.ImageFillContain
	qrImage.SetMinSize(fyne.NewSize(156, 156))

	linkEntry := newReadOnlyEntry()
	linkField := wrapTokenTextField(linkEntry)

	refreshRoot := func() {
		masterKey := strings.TrimSpace(w.cfg.MasterKey)
		internalHost, tailscaleHost, protocol, hwID := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, w.tailnetDeviceHost(), masterKey, protocol, hwID)
		linkEntry.SetFrozen(link)
		if pngBytes, err := qrcode.Encode(link, qrcode.Medium, 260); err == nil {
			qrImage.Resource = fyne.NewStaticResource("master-qr.png", pngBytes)
			qrImage.Refresh()
		}
	}

	copyBtn := newIconActionButton(loc().CopyLink, theme.ContentCopyIcon(), func() {
		if linkEntry.Text != "" {
			parent.Clipboard().SetContent(linkEntry.Text)
		}
	})
	copyBtn.Compact = true

	regenBtn := newIconActionButton(loc().RegenerateKey, theme.ViewRefreshIcon(), func() {
		withAdminPIN(func(_ string) {
			if w.token == nil {
				return
			}
			cfg, err := w.token.RegenerateMasterKey()
			if err != nil {
				showErrorDialog(err, parent)
				return
			}
			w.cfg = cfg
			refreshRoot()
		})
	})
	regenBtn.Compact = true
	regenBtn.Danger = true

	refreshRoot()
	return container.NewVBox(
		warn,
		spacerSize(1, 6),
		container.NewCenter(qrImage),
		spacerSize(1, 6),
		linkField,
		spacerSize(1, 8),
		container.NewCenter(container.New(&tightHBoxLayout{gap: 8}, copyBtn, regenBtn)),
	)
}

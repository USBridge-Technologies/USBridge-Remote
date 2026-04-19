package controller

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image/color"
	"net/url"
	"strings"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

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
)

const (
	connectionDialogNameLabel                  = "name"
	connectionDialogInternalHostLabel          = "internal ip address"
	connectionDialogTailscaleHostLabel         = "tailscale address"
	connectionDialogTokenLabel                 = "device token"
	qrScanSuccessText                          = "\u2713 qr code successfully scanned"
	connectionDialogButtonsGap         float32 = 12
)

type connectionDialogSpec struct {
	title              string
	connectLabel       string
	connectIcon        fyne.Resource
	saveLabel          string
	deleteLabel        string
	nameValue          string
	internalHostValue  string
	tailscaleHostValue string
	tokenValue         string
	tailscaleRegisterValue bool
	feedbackText       string
	feedbackColor      color.Color
	onConnect          func(name, internalHost, tailscaleHost, token string, tailscaleRegister bool) bool
	onSave             func(name, internalHost, tailscaleHost, token string, tailscaleRegister bool) bool
	onDelete           func(close func())
}

type connectionDialogSecondaryButton struct {
	widget.BaseWidget

	labelText      string
	onTapped       func()
	hovered        bool
	fillColor      color.Color
	borderColor    color.Color
	textColor      color.Color
	hoverFillColor color.Color
	hoverTextColor color.Color
	iconRes        fyne.Resource
	hoverIconRes   fyne.Resource
	bg             *canvas.Rectangle
	border         *canvas.Rectangle
	label          *canvas.Text
	icon           *canvas.Image
}

type connectionDialogPrimaryButton struct {
	widget.BaseWidget

	labelText      string
	onTapped       func()
	hovered        bool
	fillColor      color.Color
	hoverFillColor color.Color
	textColor      color.Color
	iconRes        fyne.Resource
	bg             *canvas.Rectangle
	label          *canvas.Text
	icon           *canvas.Image
}

type connectionDialogEntry struct {
	widget.Entry

	onFocusChanged func(bool)
	OnChanged      func(string)
}

func (cm *ConnectionManager) setLanguage(langCode string) {
	cm.app.Preferences().SetString("language", langCode)
	i18n.SetLanguage(langCode)
	logrus.Infof("Language changed to: %s", langCode)
	if cm.onLanguageChange != nil {
		cm.onLanguageChange()
	}
}

func (e *connectionDialogEntry) FocusGained() {
	e.Entry.FocusGained()
	if e.onFocusChanged != nil {
		e.onFocusChanged(true)
	}
}

func (e *connectionDialogEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.onFocusChanged != nil {
		e.onFocusChanged(false)
	}
}

func (e *connectionDialogEntry) TypedRune(r rune) {
	e.Entry.TypedRune(r)
	if e.OnChanged != nil {
		e.OnChanged(e.Text)
	}
}

func (e *connectionDialogEntry) TypedKey(k *fyne.KeyEvent) {
	e.Entry.TypedKey(k)
	if e.OnChanged != nil {
		e.OnChanged(e.Text)
	}
}

func (e *connectionDialogEntry) SetText(text string) {
	e.Entry.SetText(text)
	if e.OnChanged != nil {
		e.OnChanged(e.Text)
	}
}

func newConnectionDialogLabel(text string) fyne.CanvasObject {
	label := canvas.NewText(text, design.ColorTextMuted)
	label.TextSize = 12
	return label
}

func newConnectionDialogSecondaryButton(label string, onTapped func()) *connectionDialogSecondaryButton {
	btn := &connectionDialogSecondaryButton{
		labelText:      label,
		onTapped:       onTapped,
		fillColor:      color.Transparent,
		borderColor:    design.ColorAccent,
		textColor:      design.ColorAccent,
		hoverFillColor: design.ColorAccent,
		hoverTextColor: design.ColorBackground,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newConnectionDialogDangerSecondaryButton(label string, icon fyne.Resource, onTapped func()) *connectionDialogSecondaryButton {
	btn := &connectionDialogSecondaryButton{
		labelText:      label,
		onTapped:       onTapped,
		fillColor:      design.ColorSurfaceLight,
		borderColor:    color.Transparent,
		textColor:      design.ColorTextLight,
		hoverFillColor: design.ColorBorder,
		hoverTextColor: design.ColorTextLight,
		iconRes:        theme.NewThemedResource(icon),
		hoverIconRes:   theme.NewThemedResource(icon),
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newConnectionDialogPrimaryButton(label string, icon fyne.Resource, onTapped func()) *connectionDialogPrimaryButton {
	btn := &connectionDialogPrimaryButton{
		labelText:      label,
		onTapped:       onTapped,
		fillColor:      design.ColorSurfaceLight,
		hoverFillColor: design.ColorBorder,
		textColor:      design.ColorTextLight,
		iconRes:        icon,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newConnectionDialogAccentButton(label string, icon fyne.Resource, onTapped func()) *connectionDialogPrimaryButton {
	btn := &connectionDialogPrimaryButton{
		labelText:      label,
		onTapped:       onTapped,
		fillColor:      design.ColorAccent,
		hoverFillColor: design.ColorAccentHover,
		textColor:      design.ColorBackground,
		iconRes:        icon,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *connectionDialogSecondaryButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeColor = b.borderColor
	b.border.StrokeWidth = 1

	b.label = canvas.NewText(b.labelText, b.textColor)
	b.label.TextSize = 16
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.icon = canvas.NewImageFromResource(b.iconRes)
	b.icon.FillMode = canvas.ImageFillContain
	if b.iconRes == nil {
		b.icon.Hide()
	} else {
		b.icon.SetMinSize(fyne.NewSize(18, 18))
	}

	b.refreshVisuals()
	content := container.NewCenter(container.NewHBox(
		b.icon,
		view.NewInset(b.label, 6, 0, 0, 0),
	))
	if b.iconRes == nil {
		content = container.NewCenter(b.label)
	}
	return widget.NewSimpleRenderer(container.NewMax(b.bg, content, b.border))
}

func (b *connectionDialogSecondaryButton) MinSize() fyne.Size {
	return fyne.NewSize(0, 36)
}

func (b *connectionDialogSecondaryButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *connectionDialogSecondaryButton) TappedSecondary(*fyne.PointEvent) {}

func (b *connectionDialogSecondaryButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *connectionDialogSecondaryButton) MouseMoved(*desktop.MouseEvent) {}

func (b *connectionDialogSecondaryButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *connectionDialogSecondaryButton) refreshVisuals() {
	if b.bg == nil || b.border == nil || b.label == nil || b.icon == nil {
		return
	}

	b.bg.FillColor = b.fillColor
	b.border.StrokeColor = b.borderColor
	b.border.StrokeWidth = 0
	b.label.Color = b.textColor
	b.icon.Resource = b.iconRes
	if b.hovered {
		b.bg.FillColor = b.hoverFillColor
		b.label.Color = b.hoverTextColor
		if b.hoverIconRes != nil {
			b.icon.Resource = b.hoverIconRes
		}
	}
	if b.borderColor != nil && b.borderColor != color.Transparent {
		b.border.StrokeWidth = 1
	}

	b.bg.Refresh()
	b.border.Refresh()
	b.label.Refresh()
	b.icon.Refresh()
}

func (b *connectionDialogPrimaryButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.fillColor)
	b.bg.CornerRadius = design.RadiusMD

	b.label = canvas.NewText(b.labelText, b.textColor)
	b.label.TextSize = 16
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.icon = canvas.NewImageFromResource(b.iconRes)
	b.icon.FillMode = canvas.ImageFillContain
	if b.iconRes == nil {
		b.icon.Hide()
	} else {
		b.icon.SetMinSize(fyne.NewSize(18, 18))
	}

	content := container.NewCenter(container.NewHBox(
		b.icon,
		view.NewInset(b.label, 10, 0, 0, 0),
	))
	if b.iconRes == nil {
		content = container.NewCenter(b.label)
	}

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, content))
}

func (b *connectionDialogPrimaryButton) MinSize() fyne.Size {
	return fyne.NewSize(0, 36)
}

func (b *connectionDialogPrimaryButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *connectionDialogPrimaryButton) TappedSecondary(*fyne.PointEvent) {}

func (b *connectionDialogPrimaryButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *connectionDialogPrimaryButton) MouseMoved(*desktop.MouseEvent) {}

func (b *connectionDialogPrimaryButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *connectionDialogPrimaryButton) refreshVisuals() {
	if b.bg == nil || b.label == nil || b.icon == nil {
		return
	}

	b.bg.FillColor = b.fillColor
	b.label.Color = b.textColor
	b.icon.Resource = b.iconRes
	if b.hovered {
		b.bg.FillColor = b.hoverFillColor
	}

	b.bg.Refresh()
	b.label.Refresh()
	b.icon.Refresh()
}

func newConnectionDialogField(label string, field fyne.CanvasObject) fyne.CanvasObject {
	labelInsetTop := float32(10)
	if fyne.CurrentDevice().IsMobile() {
		labelInsetTop = 2 // Very compact on mobile
	}
	return container.NewVBox(
		view.NewInset(newConnectionDialogLabel(label), labelInsetTop, 0, 0, 0),
		view.NewInset(field, 0, 0, 0, 2),
	)
}

func newConnectionDialogFieldWithActions(label string, field fyne.CanvasObject, actions fyne.CanvasObject) fyne.CanvasObject {
	labelInsetTop := float32(10)
	if fyne.CurrentDevice().IsMobile() {
		labelInsetTop = 2 // Very compact on mobile
	}
	labelRow := container.NewHBox(
		newConnectionDialogLabel(label),
		layout.NewSpacer(),
		actions,
	)
	return container.NewVBox(
		view.NewInset(labelRow, labelInsetTop, 0, 0, 0),
		view.NewInset(field, 0, 0, 0, 2),
	)
}

func createTokenField(tokenEntry *connectionDialogEntry) fyne.CanvasObject {
	tokenEntry.ActionItem = nil
	tokenEntry.Refresh()
	return tokenEntry
}

func newTokenActionItem(tokenEntry *connectionDialogEntry, internalHostEntry, tailscaleHostEntry *connectionDialogEntry, window fyne.Window) fyne.CanvasObject {
	copyBtn := newCompactConnectionDialogIconButton(theme.ContentCopyIcon(), func() {
		txt := tokenEntry.Text
		if txt != "" && window != nil {
			window.Clipboard().SetContent(txt)
		}
	})
	qrBtn := newCompactConnectionDialogIconButton(theme.VisibilityIcon(), func() {
		token := strings.TrimSpace(tokenEntry.Text)
		if token == "" {
			generated, err := generateQuickToken()
			if err != nil {
				logrus.Errorf("failed to generate quick token: %v", err)
				return
			}
			token = generated
			tokenEntry.SetText(token)
		}
		internalHost := strings.TrimSpace(internalHostEntry.Text)
		tailscaleHost := strings.TrimSpace(tailscaleHostEntry.Text)
		if internalHost == "" && tailscaleHost == "" {
			logrus.Warn("cannot show quick QR: both internal and tailscale addresses are empty")
			return
		}
		showQuickConnectQRCode(window, internalHost, tailscaleHost, token)
	})
	return container.NewHBox(copyBtn, qrBtn)
}

func buildConnectionDialogForm(nameEntry, internalHostEntry, tailscaleHostEntry, tokenEntry *connectionDialogEntry, tailscaleRegisterCheck *widget.Check, window fyne.Window) fyne.CanvasObject {
	var formContainer *fyne.Container

	updateCheckVisibility := func() {
		internalHost := strings.TrimSpace(internalHostEntry.Text)
		tailscaleHost := strings.TrimSpace(tailscaleHostEntry.Text)
		if internalHost != "" && tailscaleHost == "" {
			tailscaleRegisterCheck.Show()
		} else {
			tailscaleRegisterCheck.Hide()
		}
		if formContainer != nil {
			formContainer.Refresh()
		}
	}

	internalHostEntry.OnChanged = func(s string) {
		updateCheckVisibility()
	}
	tailscaleHostEntry.OnChanged = func(s string) {
		updateCheckVisibility()
	}

	formContainer = container.NewVBox(
		newConnectionDialogField(connectionDialogNameLabel, nameEntry),
		newConnectionDialogField(connectionDialogInternalHostLabel, internalHostEntry),
		newConnectionDialogField(connectionDialogTailscaleHostLabel, tailscaleHostEntry),
		tailscaleRegisterCheck,
		newConnectionDialogFieldWithActions(connectionDialogTokenLabel, createTokenField(tokenEntry), newTokenActionItem(tokenEntry, internalHostEntry, tailscaleHostEntry, window)),
	)

	updateCheckVisibility()

	return formContainer
}

func newConnectionDialogFeedback(text string, fill color.Color) fyne.CanvasObject {
	label := canvas.NewText(text, fill)
	label.TextSize = 11
	label.TextStyle.Bold = true
	return label
}

func showAdaptiveConnectionDialog(parent fyne.Window, dialogTitle string, feedback fyne.CanvasObject, form fyne.CanvasObject, connectBtn, saveBtn, deleteBtn fyne.CanvasObject) *widget.PopUp {
	title := view.NewBrandText(dialogTitle, 19, design.ColorTextLight, true)
	title.Alignment = fyne.TextAlignCenter

	closeBtn := newConnectionDialogIconButton(theme.CancelIcon(), nil)
	titleBar := container.New(&connectionDialogTitleLayout{}, title, closeBtn)

	bodyObjects := []fyne.CanvasObject{titleBar}
	if feedback != nil {
		bodyObjects = append(bodyObjects, container.NewCenter(feedback))
	}

	buttonItems := make([]fyne.CanvasObject, 0, 4)
	if deleteBtn != nil && saveBtn != nil && connectBtn == nil {
		buttonItems = append(buttonItems, deleteBtn, saveBtn)
	} else if connectBtn != nil && saveBtn != nil && deleteBtn == nil {
		buttonItems = append(buttonItems, connectBtn, saveBtn)
	} else {
		if connectBtn != nil {
			buttonItems = append(buttonItems, connectBtn)
		}
		if saveBtn != nil {
			buttonItems = append(buttonItems, saveBtn)
		}
		if deleteBtn != nil {
			buttonItems = append(buttonItems, deleteBtn)
		}
	}

	columns := len(buttonItems)
	if columns > 3 {
		columns = 2
	}
	if columns == 0 {
		columns = 1
	}
	var buttons fyne.CanvasObject
	if columns == 2 && len(buttonItems) == 2 {
		buttons = container.New(&connectionDialogButtonsLayout{gap: connectionDialogButtonsGap}, buttonItems...)
	} else {
		buttons = container.NewGridWithColumns(columns, buttonItems...)
	}

	bodyObjects = append(
		bodyObjects,
		view.NewInset(form, 0, 0, 16, 14),
		buttons,
	)
	body := container.NewVBox(bodyObjects...)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD

	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(
		bg,
		view.NewInset(body, 18, 18, 16, 16),
		border,
	)

	popup := view.ShowOverlayPopup(parent, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.Transparent, // No more gray/dim background
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			return connectionDialogPanelSize(panel, canvasSize)
		},
		PanelPos: func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position {
			topMargin := clampFloat32(canvasSize.Height*0.025, 12, 22)
			if fyne.CurrentDevice().IsMobile() {
				return fyne.NewPos((canvasSize.Width-panelSize.Width)/2, topMargin)
			}
			centerY := (canvasSize.Height - panelSize.Height) / 2
			return fyne.NewPos((canvasSize.Width-panelSize.Width)/2, centerY)
		},
	})
	closeBtn.onTapped = func() {
		popup.Hide()
	}
	return popup
}

func showConnectionEditorDialog(parent fyne.Window, window fyne.Window, spec connectionDialogSpec) *widget.PopUp {
	nameEntry := newConnectionNameEntry(spec.nameValue, nil)
	internalHostEntry := newConnectionHostEntry(spec.internalHostValue, nil)
	tailscaleHostEntry := newConnectionTailscaleEntry(spec.tailscaleHostValue, nil)
	tokenEntry := newConnectionTokenEntry(spec.tokenValue, nil)
	tailscaleRegisterCheck := widget.NewCheck(i18n.Current.TailscaleRegisterLabel, nil)
	tailscaleRegisterCheck.Checked = spec.tailscaleRegisterValue
	form := buildConnectionDialogForm(nameEntry, internalHostEntry, tailscaleHostEntry, tokenEntry, tailscaleRegisterCheck, window)

	var feedback fyne.CanvasObject
	if spec.feedbackText != "" {
		fill := spec.feedbackColor
		if fill == nil {
			fill = design.ColorAccent
		}
		feedback = newConnectionDialogFeedback(spec.feedbackText, fill)
	}

	saveLabel := spec.saveLabel
	if saveLabel == "" {
		saveLabel = i18n.Current.DeepLinkSave
	}

	deleteLabel := spec.deleteLabel
	if deleteLabel == "" {
		deleteLabel = i18n.Current.DeleteButton
	}

	var d *widget.PopUp
	var connectBtn fyne.CanvasObject
	var deleteBtn fyne.CanvasObject
	var saveBtn fyne.CanvasObject
	if spec.onConnect != nil {
		connectLabel := spec.connectLabel
		if connectLabel == "" {
			connectLabel = i18n.Current.DeepLinkConnect
		}
		btn := newConnectionDialogPrimaryButton(connectLabel, spec.connectIcon, func() {
			if spec.onConnect != nil && !spec.onConnect(nameEntry.Text, internalHostEntry.Text, tailscaleHostEntry.Text, tokenEntry.Text, tailscaleRegisterCheck.Checked) {
				return
			}
			if d != nil {
				d.Hide()
			}
		})
		connectBtn = btn
	}

	saveBtn = widget.NewButton(saveLabel, func() {
		if spec.onSave != nil && !spec.onSave(nameEntry.Text, internalHostEntry.Text, tailscaleHostEntry.Text, tokenEntry.Text, tailscaleRegisterCheck.Checked) {
			return
		}
		if d != nil {
			d.Hide()
		}
	})
	savePrimaryBtn := saveBtn.(*widget.Button)
	savePrimaryBtn.Importance = widget.HighImportance

	if spec.onConnect != nil && spec.onDelete == nil {
		saveBtn = newConnectionDialogAccentButton(saveLabel, nil, func() {
			if spec.onSave != nil && !spec.onSave(nameEntry.Text, internalHostEntry.Text, tailscaleHostEntry.Text, tokenEntry.Text, tailscaleRegisterCheck.Checked) {
				return
			}
			if d != nil {
				d.Hide()
			}
		})
	}

	if spec.onDelete != nil {
		btn := newConnectionDialogDangerSecondaryButton(deleteLabel, theme.DeleteIcon(), func() {
			spec.onDelete(func() {
				if d != nil {
					d.Hide()
				}
			})
		})
		deleteBtn = btn
	}

	d = showAdaptiveConnectionDialog(parent, spec.title, feedback, form, connectBtn, saveBtn, deleteBtn)
	return d
}

func newConnectionNameEntry(value string, onFocusChanged func(bool)) *connectionDialogEntry {
	entry := &connectionDialogEntry{onFocusChanged: onFocusChanged}
	entry.ExtendBaseWidget(entry)
	entry.Text = value
	entry.SetPlaceHolder("Enter device name...")
	return entry
}

func newConnectionHostEntry(value string, onFocusChanged func(bool)) *connectionDialogEntry {
	entry := &connectionDialogEntry{onFocusChanged: onFocusChanged}
	entry.ExtendBaseWidget(entry)
	entry.Text = value
	entry.SetPlaceHolder("xxx.xxx.x.x")
	return entry
}

func newConnectionTailscaleEntry(value string, onFocusChanged func(bool)) *connectionDialogEntry {
	entry := &connectionDialogEntry{onFocusChanged: onFocusChanged}
	entry.ExtendBaseWidget(entry)
	entry.Text = value
	entry.SetPlaceHolder("device.tailnet.ts.net")
	return entry
}

func newConnectionTokenEntry(value string, onFocusChanged func(bool)) *connectionDialogEntry {
	entry := &connectionDialogEntry{onFocusChanged: onFocusChanged}
	entry.ExtendBaseWidget(entry)
	entry.Text = value
	entry.SetPlaceHolder("")
	entry.Password = false
	return entry
}

func generateQuickToken() (string, error) {
	randomBytes := make([]byte, 24)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(randomBytes)
	if len(token) != 32 {
		return "", fmt.Errorf("unexpected token length: %d", len(token))
	}
	return token, nil
}

func showQuickConnectQRCode(window fyne.Window, internalHost, tailscaleHost, token string) {
	if window == nil {
		return
	}
	qrURL := buildServiceQRFormat(internalHost, tailscaleHost, token)
	pngBytes, err := qrcode.Encode(qrURL, qrcode.Medium, 280)
	if err != nil {
		logrus.Errorf("failed to render quick QR: %v", err)
		return
	}

	resource := fyne.NewStaticResource("quick-connect-qr.png", pngBytes)
	image := canvas.NewImageFromResource(resource)
	image.FillMode = canvas.ImageFillContain
	image.SetMinSize(fyne.NewSize(280, 280))

	linkEntry := widget.NewEntry()
	linkEntry.SetText(qrURL)
	linkEntry.Disable()

	copyBtn := widget.NewButton("Copy Link", func() {
		window.Clipboard().SetContent(qrURL)
	})
	content := container.NewVBox(
		widget.NewLabelWithStyle("Quick Connect QR", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		container.NewCenter(image),
		linkEntry,
		copyBtn,
	)
	d := dialog.NewCustom("Quick Connect", "Close", content, window)
	d.Resize(fyne.NewSize(360, 460))
	d.Show()
}

func buildServiceQRFormat(internalHost, tailscaleHost, token string) string {
	values := url.Values{}
	if strings.TrimSpace(internalHost) != "" {
		values.Set("internal_host", strings.TrimSpace(internalHost))
	}
	if strings.TrimSpace(tailscaleHost) != "" {
		values.Set("tailscale_host", strings.TrimSpace(tailscaleHost))
	}
	values.Set("token", strings.TrimSpace(token))
	if strings.TrimSpace(tailscaleHost) != "" {
		values.Set("protocol", models.ConnectionProtocolTailscale)
	} else if strings.TrimSpace(internalHost) != "" {
		values.Set("protocol", models.ConnectionProtocolQUIC)
	}
	return fmt.Sprintf("usbridge://connect?%s", values.Encode())
}

func resolveHostForDialog(protocol, internalHost, tailscaleHost string) string {
	switch normalizeConnectionProtocol(protocol) {
	case models.ConnectionProtocolTailscale:
		return fallbackText(tailscaleHost, internalHost)
	default:
		return fallbackText(internalHost, tailscaleHost)
	}
}

func (cm *ConnectionManager) showEditDialog(idx int) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	conn := cm.connections[idx]

	showConnectionEditorDialog(cm.window, cm.window, connectionDialogSpec{
		title:                  i18n.Current.EditConnectionTitle,
		saveLabel:              i18n.Current.DeepLinkSave,
		deleteLabel:            i18n.Current.DeleteButton,
		nameValue:              conn.Name,
		internalHostValue:      conn.InternalHost,
		tailscaleHostValue:     conn.TailscaleHost,
		tokenValue:             conn.Token,
		tailscaleRegisterValue: conn.TailscaleRegister,
		onSave: func(name, internalHost, tailscaleHost, token string, tailscaleRegister bool) bool {
			internalHost = strings.TrimSpace(internalHost)
			tailscaleHost = strings.TrimSpace(tailscaleHost)
			if name == "" || (internalHost == "" && tailscaleHost == "") {
				logrus.Warn("name and at least one address are required")
				return false
			}

			cm.connections[idx] = SavedConnection{
				Name:              name,
				InternalHost:      internalHost,
				TailscaleHost:     tailscaleHost,
				Host:              fallbackText(internalHost, tailscaleHost),
				Token:             strings.TrimSpace(token),
				Protocol:          conn.Protocol,
				WireGuardInvite:   conn.WireGuardInvite,
				TailscaleRegister: tailscaleRegister,
			}
			cm.selectedIndex = idx
			cm.saveConnections()
			fyne.Do(func() {
				cm.SelectConnection(idx)
				cm.refreshConnectionsList()
			})
			logrus.Infof("Updated connection: %s", name)
			return true
		},
		onDelete: func(close func()) {
			cm.handleDeleteConnection(idx, close)
		},
	})
}

func (cm *ConnectionManager) showAddDialog() {
	internalHost, tailscaleHost := splitHostByType(cm.hostEntry.Text)
	if selected := normalizeConnectionProtocol(cm.protocolSelect.Selected); selected == models.ConnectionProtocolTailscale {
		internalHost, tailscaleHost = "", strings.TrimSpace(cm.hostEntry.Text)
	}
	cm.showPrefilledAddDialog("", internalHost, tailscaleHost, cm.tokenEntry.Text, "", "", false)
}

func (cm *ConnectionManager) showPrefilledAddDialog(name, internalHost, tailscaleHost, token, protocol, wireGuardInvite string, scanned bool) {
	feedbackText := ""
	if scanned {
		feedbackText = qrScanSuccessText
	}
	token = strings.TrimSpace(token)
	if token == "" {
		generated, err := generateQuickToken()
		if err == nil {
			token = generated
		} else {
			logrus.WithError(err).Warn("failed to generate quick token")
		}
	}

	logrus.Infof("Opening add connection dialog: internal=%s tailscale=%s scanned=%v", internalHost, tailscaleHost, scanned)

	showConnectionEditorDialog(cm.window, cm.window, connectionDialogSpec{
		title:                  i18n.Current.AddConnectionTitle,
		connectLabel:           i18n.Current.DeepLinkConnect,
		connectIcon:            nil,
		saveLabel:              i18n.Current.DeepLinkSave,
		nameValue:              name,
		internalHostValue:      internalHost,
		tailscaleHostValue:     tailscaleHost,
		tokenValue:             token,
		tailscaleRegisterValue: false,
		feedbackText:           feedbackText,
		feedbackColor:          design.ColorAccent,
		onConnect: func(name, internalHost, tailscaleHost, token string, tailscaleRegister bool) bool {
			internalHost = strings.TrimSpace(internalHost)
			tailscaleHost = strings.TrimSpace(tailscaleHost)
			if internalHost == "" && tailscaleHost == "" {
				logrus.Warn("at least one address is required")
				return false
			}

			selectedProtocol := protocol
			if selectedProtocol == "" && cm.protocolSelect != nil {
				selectedProtocol = cm.protocolSelect.Selected
			}
			host := resolveHostForDialog(selectedProtocol, internalHost, tailscaleHost)

			fyne.Do(func() {
				cm.ClearSelection()
				cm.applyConnectionToForm(host, token, selectedProtocol)
			})
			if cm.onConnect != nil {
				cm.onConnect(host, token, selectedProtocol, wireGuardInvite, tailscaleRegister)
			}
			return true
		},
		onSave: func(name, internalHost, tailscaleHost, token string, tailscaleRegister bool) bool {
			internalHost = strings.TrimSpace(internalHost)
			tailscaleHost = strings.TrimSpace(tailscaleHost)
			if internalHost == "" && tailscaleHost == "" {
				logrus.Warn("at least one address is required")
				return false
			}

			selectedProtocol := protocol
			if selectedProtocol == "" && cm.protocolSelect != nil {
				selectedProtocol = cm.protocolSelect.Selected
			}
			host := resolveHostForDialog(selectedProtocol, internalHost, tailscaleHost)

			cm.SaveConnection(name, internalHost, tailscaleHost, token, selectedProtocol, wireGuardInvite, tailscaleRegister)
			fyne.Do(func() {
				cm.applyConnectionToForm(host, token, selectedProtocol)
				cm.refreshConnectionsList()
			})
			return true
		},
	})
}

type connectionDialogIconButton struct {
	widget.BaseWidget

	resource   fyne.Resource
	onTapped   func()
	hovered    bool
	buttonSize fyne.Size
	iconSize   fyne.Size

	bg   *canvas.Rectangle
	icon *canvas.Image
}

func newConnectionDialogIconButton(resource fyne.Resource, onTapped func()) *connectionDialogIconButton {
	btn := &connectionDialogIconButton{
		resource:   resource,
		onTapped:   onTapped,
		buttonSize: fyne.NewSize(28, 28),
		iconSize:   fyne.NewSize(18, 18),
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newCompactConnectionDialogIconButton(resource fyne.Resource, onTapped func()) *connectionDialogIconButton {
	btn := newConnectionDialogIconButton(resource, onTapped)
	btn.buttonSize = fyne.NewSize(24, 24)
	btn.iconSize = fyne.NewSize(15, 15)
	return btn
}

func (b *connectionDialogIconButton) SetResource(resource fyne.Resource) {
	b.resource = resource
	b.refreshVisuals()
}

func (b *connectionDialogIconButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *connectionDialogIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *connectionDialogIconButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *connectionDialogIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *connectionDialogIconButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *connectionDialogIconButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *connectionDialogIconButton) MinSize() fyne.Size {
	return b.buttonSize
}

func (b *connectionDialogIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 6

	b.icon = canvas.NewImageFromResource(b.resource)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.ScaleMode = canvas.ImageScaleSmooth
	b.icon.SetMinSize(b.iconSize)

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.icon)))
}

func (b *connectionDialogIconButton) refreshVisuals() {
	if b.bg == nil || b.icon == nil {
		return
	}

	b.bg.FillColor = color.Transparent
	b.icon.Resource = b.resource
	b.icon.Translucency = 0.32
	if b.hovered {
		b.bg.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10}
		b.icon.Translucency = 0.08
	}

	b.bg.Refresh()
	b.icon.Refresh()
}

type connectionDialogButtonsLayout struct {
	gap float32
}

func (l *connectionDialogButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	left := objects[0]
	right := objects[1]
	width := (size.Width - l.gap) / 2
	if width < 0 {
		width = 0
	}

	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(width, size.Height))

	right.Move(fyne.NewPos(width+l.gap, 0))
	right.Resize(fyne.NewSize(width, size.Height))
}

func (l *connectionDialogButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	leftMin := objects[0].MinSize()
	rightMin := objects[1].MinSize()
	height := maxFloat32(leftMin.Height, rightMin.Height)
	return fyne.NewSize(leftMin.Width+rightMin.Width+l.gap, height)
}

type connectionDialogTitleLayout struct{}

func (l *connectionDialogTitleLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	title := objects[0]
	closeBtn := objects[1]
	titleMin := title.MinSize()
	closeMin := closeBtn.MinSize()

	title.Move(fyne.NewPos(maxFloat32(0, (size.Width-titleMin.Width)/2), maxFloat32(0, (size.Height-titleMin.Height)/2)))
	title.Resize(titleMin)

	closeBtn.Move(fyne.NewPos(maxFloat32(0, size.Width-closeMin.Width), maxFloat32(0, (size.Height-closeMin.Height)/2)))
	closeBtn.Resize(closeMin)
}

func (l *connectionDialogTitleLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	titleMin := objects[0].MinSize()
	closeMin := objects[1].MinSize()
	width := maxFloat32(titleMin.Width, closeMin.Width*2) + closeMin.Width
	height := maxFloat32(titleMin.Height, closeMin.Height)
	return fyne.NewSize(width, height)
}

func connectionDialogPanelSize(panel fyne.CanvasObject, canvasSize fyne.Size) fyne.Size {
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
	maxWidth := canvasSize.Width - margin*2
	maxHeight := canvasSize.Height - margin*2

	// We don't dynamically change maxHeight on mobile anymore to avoid resizing jumps.
	// The compact fields and top positioning handle the layout.

	if maxWidth <= 0 {
		maxWidth = canvasSize.Width
	}
	if maxHeight <= 0 {
		maxHeight = canvasSize.Height
	}

	panelMin := panel.MinSize()
	panelWidth := minFloat32(408, maxWidth)
	if panelWidth < 0 {
		panelWidth = 0
	}

	panelHeight := panelMin.Height
	if panelHeight > maxHeight {
		panelHeight = maxHeight
	}

	return fyne.NewSize(panelWidth, panelHeight)
}

func (cm *ConnectionManager) handleDeleteConnection(idx int, afterDelete func()) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	deletedName := cm.connections[idx].Name

	view.ShowConfirmYesLeft(
		i18n.Current.DeleteConnectionTitle,
		fmt.Sprintf(i18n.Current.DeleteConnectionConfirm, deletedName),
		func(confirmed bool) {
			if !confirmed {
				return
			}
			fyne.Do(func() {
				cm.window.Canvas().Focus(nil)
			})
			cm.connections = append(cm.connections[:idx], cm.connections[idx+1:]...)
			cm.selectedIndex = -1
			cm.saveConnections()
			fyne.Do(func() {
				cm.applyConnectionToForm("", "", "")
				cm.refreshConnectionsList()
			})
			if afterDelete != nil {
				afterDelete()
			}
			logrus.Infof("Deleted connection: %s", deletedName)
		},
		cm.window,
	)
}

func (cm *ConnectionManager) handleQRScan() {
	logrus.Info("Opening QR scanner")
	cm.qrScanner.ShowCameraScanner(cm.window)
}

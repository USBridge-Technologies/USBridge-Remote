package controller

import (
	"fmt"
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const (
	connectionDialogNameLabel          = "name"
	connectionDialogHostLabel          = "ip address"
	connectionDialogTokenLabel         = "auth token"
	qrScanSuccessText                  = "\u2713 qr code successfully scanned"
	connectionDialogButtonsGap float32 = 12
)

type connectionDialogSpec struct {
	title         string
	connectLabel  string
	connectIcon   fyne.Resource
	saveLabel     string
	deleteLabel   string
	nameValue     string
	hostValue     string
	tokenValue    string
	feedbackText  string
	feedbackColor color.Color
	onConnect     func(name, host, token string) bool
	onSave        func(name, host, token string) bool
	onDelete      func(close func())
}

type connectionDialogSecondaryButton struct {
	widget.BaseWidget

	labelText      string
	onTapped       func()
	hovered        bool
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

func (cm *ConnectionManager) setLanguage(langCode string) {
	cm.app.Preferences().SetString("language", langCode)
	i18n.SetLanguage(langCode)
	logrus.Infof("Language changed to: %s", langCode)
	if cm.onLanguageChange != nil {
		cm.onLanguageChange()
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
		borderColor:    color.NRGBA{R: 0xff, G: 0x43, B: 0x36, A: 0xff},
		textColor:      color.NRGBA{R: 0xff, G: 0x43, B: 0x36, A: 0xff},
		hoverFillColor: color.NRGBA{R: 0xff, G: 0x43, B: 0x36, A: 0xff},
		hoverTextColor: design.ColorBackground,
		iconRes:        theme.NewErrorThemedResource(icon),
		hoverIconRes:   theme.NewThemedResource(icon),
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
		view.NewInset(b.label, 10, 0, 0, 0),
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

	b.bg.FillColor = color.Transparent
	b.border.StrokeColor = b.borderColor
	b.label.Color = b.textColor
	b.icon.Resource = b.iconRes
	if b.hovered {
		b.bg.FillColor = b.hoverFillColor
		b.label.Color = b.hoverTextColor
		if b.hoverIconRes != nil {
			b.icon.Resource = b.hoverIconRes
		}
	}

	b.bg.Refresh()
	b.border.Refresh()
	b.label.Refresh()
	b.icon.Refresh()
}

func newConnectionDialogField(label string, field fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(
		view.NewInset(newConnectionDialogLabel(label), 10, 0, 0, 0),
		view.NewInset(field, 0, 0, 0, 2),
	)
}

func createTokenFieldWithButtons(tokenEntry *widget.Entry, window fyne.Window) fyne.CanvasObject {
	reserve := canvas.NewRectangle(color.Transparent)
	reserve.SetMinSize(fyne.NewSize(70, 1))
	tokenEntry.ActionItem = reserve
	tokenEntry.Refresh()

	actions := newTokenActionItem(tokenEntry, window)
	return container.New(&tokenFieldOverlayLayout{rightInset: 10}, tokenEntry, actions)
}

func newTokenActionItem(tokenEntry *widget.Entry, window fyne.Window) fyne.CanvasObject {
	copyBtn := newConnectionDialogIconButton(theme.ContentCopyIcon(), func() {
		txt := tokenEntry.Text
		if txt != "" && window != nil {
			window.Clipboard().SetContent(txt)
		}
	})

	visibilityIcon := theme.VisibilityOffIcon()
	if !tokenEntry.Password {
		visibilityIcon = theme.VisibilityIcon()
	}

	visibilityBtn := newConnectionDialogIconButton(visibilityIcon, nil)
	visibilityBtn.onTapped = func() {
		tokenEntry.Password = !tokenEntry.Password
		if tokenEntry.Password {
			visibilityBtn.SetResource(theme.VisibilityOffIcon())
		} else {
			visibilityBtn.SetResource(theme.VisibilityIcon())
		}
		tokenEntry.Refresh()
	}

	actions := container.NewHBox(
		copyBtn,
		view.NewInset(visibilityBtn, 1, 0, 0, 0),
	)
	return container.NewGridWrap(fyne.NewSize(54, 28), container.NewCenter(actions))
}

func buildConnectionDialogForm(nameEntry, hostEntry, tokenEntry *widget.Entry, window fyne.Window) fyne.CanvasObject {
	return container.NewVBox(
		newConnectionDialogField(connectionDialogNameLabel, nameEntry),
		newConnectionDialogField(connectionDialogHostLabel, hostEntry),
		newConnectionDialogField(connectionDialogTokenLabel, createTokenFieldWithButtons(tokenEntry, window)),
	)
}

func newConnectionDialogFeedback(text string, fill color.Color) fyne.CanvasObject {
	label := canvas.NewText(text, fill)
	label.TextSize = 11
	label.TextStyle.Bold = true
	return label
}

func showConnectionDialog(parent fyne.Window, dialogTitle string, feedback fyne.CanvasObject, form fyne.CanvasObject, connectBtn, saveBtn, deleteBtn fyne.CanvasObject) *widget.PopUp {
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
		buttonItems = append(buttonItems, saveBtn, connectBtn)
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
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			return connectionDialogPanelSize(panel, canvasSize)
		},
	})
	closeBtn.onTapped = func() {
		popup.Hide()
	}
	return popup
}

func showConnectionEditorDialog(parent fyne.Window, window fyne.Window, spec connectionDialogSpec) *widget.PopUp {
	nameEntry := newConnectionNameEntry(spec.nameValue)
	hostEntry := newConnectionHostEntry(spec.hostValue)
	tokenEntry := newConnectionTokenEntry(spec.tokenValue)
	form := buildConnectionDialogForm(nameEntry, hostEntry, tokenEntry, window)

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
		btn := widget.NewButton(connectLabel, func() {
			if spec.onConnect != nil && !spec.onConnect(nameEntry.Text, hostEntry.Text, tokenEntry.Text) {
				return
			}
			if d != nil {
				d.Hide()
			}
		})
		btn.Importance = widget.HighImportance
		if spec.connectIcon != nil {
			btn.SetIcon(spec.connectIcon)
		}
		connectBtn = btn
	}

	saveBtn = widget.NewButton(saveLabel, func() {
		if spec.onSave != nil && !spec.onSave(nameEntry.Text, hostEntry.Text, tokenEntry.Text) {
			return
		}
		if d != nil {
			d.Hide()
		}
	})
	savePrimaryBtn := saveBtn.(*widget.Button)
	savePrimaryBtn.Importance = widget.HighImportance

	if spec.onConnect != nil && spec.onDelete == nil {
		saveBtn = newConnectionDialogSecondaryButton(saveLabel, func() {
			if spec.onSave != nil && !spec.onSave(nameEntry.Text, hostEntry.Text, tokenEntry.Text) {
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

	d = showConnectionDialog(parent, spec.title, feedback, form, connectBtn, saveBtn, deleteBtn)
	return d
}

func newConnectionNameEntry(value string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(value)
	entry.SetPlaceHolder("Enter device name...")
	return entry
}

func newConnectionHostEntry(value string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(value)
	entry.SetPlaceHolder("xxx.xxx.x.x")
	return entry
}

func newConnectionTokenEntry(value string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(value)
	entry.SetPlaceHolder("")
	entry.Password = true
	return entry
}

func (cm *ConnectionManager) showEditDialog(idx int) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	conn := cm.connections[idx]

	showConnectionEditorDialog(cm.window, cm.window, connectionDialogSpec{
		title:       i18n.Current.EditConnectionTitle,
		saveLabel:   i18n.Current.DeepLinkSave,
		deleteLabel: i18n.Current.DeleteButton,
		nameValue:   conn.Name,
		hostValue:   conn.Host,
		tokenValue:  conn.Token,
		onSave: func(name, host, token string) bool {
			if name == "" || host == "" {
				logrus.Warn("name and address are required")
				return false
			}

			cm.connections[idx] = SavedConnection{
				Name:            name,
				Host:            host,
				Token:           token,
				Protocol:        conn.Protocol,
				WireGuardInvite: conn.WireGuardInvite,
			}
			cm.selectedIndex = idx
			cm.saveConnections()
			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && conn.Protocol != "" {
					cm.protocolSelect.SetSelected(conn.Protocol)
				}
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
	cm.showPrefilledAddDialog("", cm.hostEntry.Text, cm.tokenEntry.Text, "", "", false)
}

func (cm *ConnectionManager) showPrefilledAddDialog(name, host, token, protocol, wireGuardInvite string, scanned bool) {
	feedbackText := ""
	if scanned {
		feedbackText = qrScanSuccessText
	}

	logrus.Infof("Opening add connection dialog: host=%s scanned=%v", host, scanned)

	showConnectionEditorDialog(cm.window, cm.window, connectionDialogSpec{
		title:         i18n.Current.AddConnectionTitle,
		connectLabel:  i18n.Current.DeepLinkConnect,
		connectIcon:   nil,
		saveLabel:     i18n.Current.DeepLinkSave,
		nameValue:     name,
		hostValue:     host,
		tokenValue:    token,
		feedbackText:  feedbackText,
		feedbackColor: design.ColorAccent,
		onConnect: func(name, host, token string) bool {
			if host == "" {
				logrus.Warn("host is required")
				return false
			}

			selectedProtocol := protocol
			if selectedProtocol == "" && cm.protocolSelect != nil {
				selectedProtocol = cm.protocolSelect.Selected
			}

			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && selectedProtocol != "" {
					cm.protocolSelect.SetSelected(selectedProtocol)
				}
			})
			if cm.onConnect != nil {
				cm.onConnect(host, token, selectedProtocol, wireGuardInvite)
			}
			return true
		},
		onSave: func(name, host, token string) bool {
			if host == "" {
				logrus.Warn("host is required")
				return false
			}

			selectedProtocol := protocol
			if selectedProtocol == "" && cm.protocolSelect != nil {
				selectedProtocol = cm.protocolSelect.Selected
			}

			cm.SaveConnection(name, host, token, selectedProtocol, wireGuardInvite)
			fyne.Do(func() {
				cm.hostEntry.SetText(host)
				cm.tokenEntry.SetText(token)
				if cm.protocolSelect != nil && selectedProtocol != "" {
					cm.protocolSelect.SetSelected(selectedProtocol)
				}
				cm.refreshConnectionsList()
			})
			return true
		},
	})
}

type connectionDialogIconButton struct {
	widget.BaseWidget

	resource fyne.Resource
	onTapped func()
	hovered  bool

	bg   *canvas.Rectangle
	icon *canvas.Image
}

func newConnectionDialogIconButton(resource fyne.Resource, onTapped func()) *connectionDialogIconButton {
	btn := &connectionDialogIconButton{
		resource: resource,
		onTapped: onTapped,
	}
	btn.ExtendBaseWidget(btn)
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
	return fyne.NewSize(28, 28)
}

func (b *connectionDialogIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 6

	b.icon = canvas.NewImageFromResource(b.resource)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.ScaleMode = canvas.ImageScaleSmooth
	b.icon.SetMinSize(fyne.NewSize(18, 18))

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

type tokenFieldOverlayLayout struct {
	rightInset float32
}

func (l *tokenFieldOverlayLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	entry := objects[0]
	actions := objects[1]

	entry.Move(fyne.NewPos(0, 0))
	entry.Resize(size)

	actionsMin := actions.MinSize()
	x := size.Width - actionsMin.Width - l.rightInset
	if x < 0 {
		x = 0
	}
	y := (size.Height - actionsMin.Height) / 2
	if y < 0 {
		y = 0
	}
	actions.Move(fyne.NewPos(x, y))
	actions.Resize(actionsMin)
}

func (l *tokenFieldOverlayLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
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
				cm.hostEntry.SetText("")
				cm.tokenEntry.SetText("")
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

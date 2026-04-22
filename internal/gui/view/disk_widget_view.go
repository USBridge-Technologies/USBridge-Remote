package view

import (
	"image/color"
	"sync"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

type DiskWidgetUI struct {
	Container   *fyne.Container
	DevicesList *DevicesListView
}

type DevicesListView struct {
	Scroll     *container.Scroll
	content    *fyne.Container
	buildIntro func() fyne.CanvasObject
	buildRows  func() []fyne.CanvasObject
	mu         sync.Mutex
}

type DiskRowWidgets struct {
	Checkbox        *widget.Check
	CaptureSelector *CaptureSelector
	PrefixIcon      *canvas.Image
	NameLabel       *adaptiveNameText
	ModeIcon        *canvas.Text
	ModeTitleLabel  *canvas.Text
	StatusLabel     *widget.Label
	StatusDot       *canvas.Circle
	RORWButton      *widget.Button
	ModeSelect      *HeaderDropdown
	UploadButton    *iconChromeButton
	DeleteButton    *iconChromeButton
	SettingsButton  *DeviceActionButton
}

var diskRowRegistry sync.Map

const (
	deviceControlHeight    = 36
	deviceControlUnitWidth = 40
	deviceControlGap       = 8
	deviceWideControlWidth = (deviceControlUnitWidth * 2) + deviceControlGap
)

type adaptiveNameText struct {
	widget.BaseWidget

	fullText string
	textSize float32
	style    fyne.TextStyle
	color    color.Color
	label    *canvas.Text
}

type DeviceActionButton struct {
	widget.BaseWidget

	labelText         string
	iconRes           fyne.Resource
	onTapped          func()
	disabled          bool
	hovered           bool
	normalFillColor   color.Color
	hoverFillColor    color.Color
	textColor         color.Color
	disabledTextColor color.Color
	bg                *canvas.Rectangle
	icon              *canvas.Image
	label             *canvas.Text
}

type CaptureSelector struct {
	widget.BaseWidget

	selected bool
	disabled bool
	onTapped func()

	outer *canvas.Circle
	inner *canvas.Circle
}

type diskCheckboxTheme struct {
	base fyne.Theme
}

func newAdaptiveNameText() *adaptiveNameText {
	t := &adaptiveNameText{
		textSize: theme.TextSize(),
		color:    design.ColorTextLight,
	}
	t.ExtendBaseWidget(t)
	return t
}

func NewDeviceActionButton(label string, icon fyne.Resource, onTapped func()) *DeviceActionButton {
	b := &DeviceActionButton{
		labelText:         label,
		iconRes:           icon,
		onTapped:          onTapped,
		normalFillColor:   design.ColorSurfaceLight,
		hoverFillColor:    design.ColorBorder,
		textColor:         design.ColorTextLight,
		disabledTextColor: design.ColorTextMuted,
	}
	b.ExtendBaseWidget(b)
	return b
}

func NewCaptureSelector(onTapped func()) *CaptureSelector {
	s := &CaptureSelector{onTapped: onTapped}
	s.ExtendBaseWidget(s)
	return s
}

func (t *adaptiveNameText) SetText(text string) {
	t.fullText = text
	t.Refresh()
}

func (t *adaptiveNameText) SetColor(col color.Color) {
	t.color = col
	t.Refresh()
}

func (t *adaptiveNameText) CreateRenderer() fyne.WidgetRenderer {
	t.label = canvas.NewText("", t.color)
	t.label.TextSize = t.textSize
	t.label.TextStyle = t.style
	return widget.NewSimpleRenderer(t.label)
}

func (t *adaptiveNameText) MinSize() fyne.Size {
	return fyne.MeasureText("...", t.textSize, t.style)
}

func (t *adaptiveNameText) Refresh() {
	fyne.Do(func() {
		if t.label != nil {
			t.label.Color = t.color
			t.label.TextStyle = t.style
			t.label.TextSize = t.textSize
			t.label.Text = t.truncatedForWidth(t.Size().Width)
			t.label.Refresh()
		}
		t.BaseWidget.Refresh()
	})
}

func (t *adaptiveNameText) Resize(size fyne.Size) {
	t.BaseWidget.Resize(size)
	fyne.Do(func() {
		if t.label != nil {
			t.label.Text = t.truncatedForWidth(size.Width)
			t.label.Refresh()
		}
	})
}

func (t *adaptiveNameText) truncatedForWidth(width float32) string {
	text := t.fullText
	if text == "" {
		return ""
	}
	if width <= 0 {
		return "..."
	}
	if fyne.MeasureText(text, t.textSize, t.style).Width <= width {
		return text
	}

	runes := []rune(text)
	if len(runes) <= 3 {
		return text
	}

	best := "..."
	low, high := 0, len(runes)-3
	for low <= high {
		keep := (low + high) / 2
		head := keep / 2
		tail := keep - head
		candidate := string(runes[:head]) + "..." + string(runes[len(runes)-tail:])
		if fyne.MeasureText(candidate, t.textSize, t.style).Width <= width {
			best = candidate
			low = keep + 1
		} else {
			high = keep - 1
		}
	}
	return best
}

func (b *DeviceActionButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.normalFillColor)
	b.bg.CornerRadius = design.RadiusMD

	if b.iconRes != nil {
		b.icon = canvas.NewImageFromResource(b.iconRes)
		b.icon.FillMode = canvas.ImageFillContain
		b.icon.SetMinSize(fyne.NewSize(14, 14))
	}

	b.label = canvas.NewText(b.labelText, b.textColor)
	b.label.TextSize = 12
	b.label.TextStyle.Bold = true

	var content fyne.CanvasObject
	if b.icon != nil {
		content = container.NewHBox(b.icon, b.label)
	} else {
		content = container.NewCenter(b.label)
	}
	renderer := widget.NewSimpleRenderer(container.NewMax(
		b.bg,
		NewInset(container.NewCenter(content), 8, 8, 0, 0),
	))
	b.refreshVisuals()
	return renderer
}

func (s *CaptureSelector) CreateRenderer() fyne.WidgetRenderer {
	s.outer = canvas.NewCircle(color.Transparent)
	s.outer.StrokeWidth = 2
	s.inner = canvas.NewCircle(design.ColorAccent)
	return &captureSelectorRenderer{selector: s}
}

func (s *CaptureSelector) MinSize() fyne.Size {
	return fyne.NewSize(18, 18)
}

func (s *CaptureSelector) Tapped(*fyne.PointEvent) {
	if s.disabled || s.onTapped == nil {
		return
	}
	s.onTapped()
}

func (s *CaptureSelector) SetSelected(selected bool) {
	s.selected = selected
	s.Refresh()
}

func (s *CaptureSelector) SetDisabled(disabled bool) {
	s.disabled = disabled
	s.Refresh()
}

func (s *CaptureSelector) SetOnTapped(onTapped func()) {
	s.onTapped = onTapped
}

type captureSelectorRenderer struct {
	selector *CaptureSelector
}

func (r *captureSelectorRenderer) Layout(size fyne.Size) {
	outerSize := fyne.NewSize(18, 18)
	innerSize := fyne.NewSize(8, 8)
	offsetX := float32(-3)
	r.selector.outer.Move(fyne.NewPos((size.Width-outerSize.Width)/2+offsetX, (size.Height-outerSize.Height)/2))
	r.selector.outer.Resize(outerSize)
	r.selector.inner.Move(fyne.NewPos((size.Width-innerSize.Width)/2+offsetX, (size.Height-innerSize.Height)/2))
	r.selector.inner.Resize(innerSize)
}

func (r *captureSelectorRenderer) MinSize() fyne.Size {
	return r.selector.MinSize()
}

func (r *captureSelectorRenderer) Refresh() {
	stroke := design.ColorTextMuted
	dot := design.ColorAccent
	if r.selector.disabled {
		stroke = design.ColorBorder
		dot = design.ColorBorder
	}
	r.selector.outer.StrokeColor = stroke
	r.selector.inner.FillColor = dot
	if r.selector.selected {
		r.selector.inner.Show()
	} else {
		r.selector.inner.Hide()
	}
	r.selector.outer.Refresh()
	r.selector.inner.Refresh()
}

func (r *captureSelectorRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.selector.outer, r.selector.inner}
}

func (r *captureSelectorRenderer) Destroy() {}

func (b *DeviceActionButton) MinSize() fyne.Size {
	textWidth := fyne.MeasureText(b.labelText, 13, fyne.TextStyle{Bold: true}).Width
	width := float32(10 + textWidth + 10)
	if b.iconRes != nil {
		width += 15 + 6
	}
	if width < deviceWideControlWidth {
		width = deviceWideControlWidth
	}
	return fyne.NewSize(width, deviceControlHeight)
}

func (b *DeviceActionButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTapped == nil {
		return
	}
	b.onTapped()
}

func (b *DeviceActionButton) TappedSecondary(*fyne.PointEvent) {}

func (b *DeviceActionButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *DeviceActionButton) MouseMoved(*desktop.MouseEvent) {}

func (b *DeviceActionButton) MouseOut() {
	if b.disabled {
		return
	}
	b.hovered = false
	b.refreshVisuals()
}

func (b *DeviceActionButton) SetOnTapped(onTapped func()) {
	b.onTapped = onTapped
}

func (b *DeviceActionButton) SetLabel(label string) {
	if b.labelText == label {
		return
	}
	b.labelText = label
	if b.label != nil {
		b.label.Text = label
		b.label.Refresh()
	}
	b.Refresh()
}

func (b *DeviceActionButton) SetIcon(icon fyne.Resource) {
	b.iconRes = icon
	b.Refresh()
}

func (b *DeviceActionButton) SetColors(normalFill, hoverFill, textColor, disabledTextColor color.Color) {
	b.normalFillColor = normalFill
	b.hoverFillColor = hoverFill
	b.textColor = textColor
	b.disabledTextColor = disabledTextColor
	b.Refresh()
}

func (b *DeviceActionButton) Enable() {
	b.disabled = false
	b.Refresh()
}

func (b *DeviceActionButton) Disable() {
	b.disabled = true
	b.hovered = false
	b.Refresh()
}

func (b *DeviceActionButton) Refresh() {
	b.refreshVisuals()
	b.BaseWidget.Refresh()
}

func (b *DeviceActionButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}
	b.bg.FillColor = b.normalFillColor
	b.label.Text = b.labelText
	b.label.Color = b.textColor
	if b.icon != nil {
		b.icon.Resource = b.iconRes
		b.icon.Translucency = 0
	}
	if b.disabled {
		b.label.Color = b.disabledTextColor
		if b.icon != nil {
			b.icon.Translucency = 0.25
		}
	} else if b.hovered {
		b.bg.FillColor = b.hoverFillColor
	}
	b.bg.Refresh()
	if b.icon != nil {
		b.icon.Refresh()
	}
	b.label.Refresh()
}

func (t *diskCheckboxTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameFocus, theme.ColorNameSelection, theme.ColorNameHover:
		return color.Transparent
	default:
		return t.base.Color(name, variant)
	}
}

func (t *diskCheckboxTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *diskCheckboxTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *diskCheckboxTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}

type deviceRowLayout struct {
	gap float32
}

type deviceNameRowLayout struct {
	gap float32
}

func (l *deviceRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	left := objects[0]
	center := objects[1]
	right := objects[2]

	leftSize := left.MinSize()
	rightSize := right.MinSize()

	leftY := (size.Height - leftSize.Height) / 2
	if leftY < 0 {
		leftY = 0
	}
	left.Move(fyne.NewPos(0, leftY))
	left.Resize(leftSize)

	rightY := (size.Height - rightSize.Height) / 2
	if rightY < 0 {
		rightY = 0
	}
	rightX := size.Width - rightSize.Width
	if rightX < leftSize.Width+l.gap {
		rightX = leftSize.Width + l.gap
	}
	right.Move(fyne.NewPos(rightX, rightY))
	right.Resize(rightSize)

	centerX := leftSize.Width + l.gap
	centerWidth := rightX - centerX - l.gap
	if centerWidth < 0 {
		centerWidth = 0
	}
	center.Move(fyne.NewPos(centerX, 0))
	center.Resize(fyne.NewSize(centerWidth, size.Height))
}

func (l *deviceRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}

	left := objects[0].MinSize()
	center := objects[1].MinSize()
	right := objects[2].MinSize()

	width := left.Width + center.Width + right.Width + (l.gap * 2)
	height := left.Height
	if center.Height > height {
		height = center.Height
	}
	if right.Height > height {
		height = right.Height
	}
	return fyne.NewSize(width, height)
}

func (l *deviceNameRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	left := objects[0]
	center := objects[1]
	right := objects[2]

	leftWidth := float32(0)
	rightWidth := float32(0)
	if left.Visible() {
		leftSize := left.MinSize()
		leftWidth = leftSize.Width
		leftY := (size.Height-leftSize.Height)/2 + 2
		if leftY < 0 {
			leftY = 0
		}
		left.Move(fyne.NewPos(0, leftY))
		left.Resize(leftSize)
	}
	if right.Visible() {
		rightSize := right.MinSize()
		rightWidth = rightSize.Width
		right.Move(fyne.NewPos(size.Width-rightWidth, (size.Height-rightSize.Height)/2))
		right.Resize(rightSize)
	}

	centerX := float32(0)
	if left.Visible() {
		centerX += leftWidth + l.gap
	}
	centerRight := size.Width
	if right.Visible() {
		centerRight -= rightWidth + l.gap
	}
	centerWidth := centerRight - centerX
	if centerWidth < 0 {
		centerWidth = 0
	}
	center.Move(fyne.NewPos(centerX, 2))
	center.Resize(fyne.NewSize(centerWidth, size.Height))
}

func (l *deviceNameRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}

	left := objects[0]
	center := objects[1]
	right := objects[2]

	width := center.MinSize().Width
	height := center.MinSize().Height
	visibleSideCount := 0

	if left.Visible() {
		size := left.MinSize()
		width += size.Width
		if size.Height > height {
			height = size.Height
		}
		visibleSideCount++
	}
	if right.Visible() {
		size := right.MinSize()
		width += size.Width
		if size.Height > height {
			height = size.Height
		}
		visibleSideCount++
	}
	if visibleSideCount > 0 {
		width += l.gap * float32(visibleSideCount)
	}
	return fyne.NewSize(width, height)
}

type deviceRowControlsLayout struct {
	gap float32
}

type sectionHeaderInlineLayout struct {
	gap float32
}

type sectionCardWithAttachmentLayout struct {
	bottomInset float32
	rightInset  float32
}

type deviceRowContentLayout struct{}

func (l *deviceRowContentLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	child := objects[0]
	childSize := child.MinSize()
	y := (size.Height - childSize.Height) / 2
	if y < 0 {
		y = 0
	}
	child.Move(fyne.NewPos(0, y))
	child.Resize(fyne.NewSize(size.Width, childSize.Height))
}

func (l *deviceRowContentLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

func (l *deviceRowControlsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		childSize := obj.MinSize()
		y := (size.Height - childSize.Height) / 2
		if y < 0 {
			y = 0
		}
		obj.Move(fyne.NewPos(x, y))
		obj.Resize(childSize)
		x += childSize.Width + l.gap
	}
}

func (l *deviceRowControlsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width := float32(0)
	height := float32(0)
	visibleCount := 0
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		childSize := obj.MinSize()
		width += childSize.Width
		if childSize.Height > height {
			height = childSize.Height
		}
		visibleCount++
	}
	if visibleCount > 1 {
		width += l.gap * float32(visibleCount-1)
	}
	return fyne.NewSize(width, height)
}

func (l *sectionHeaderInlineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	x := float32(0)
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		childSize := obj.MinSize()
		y := (size.Height - childSize.Height) / 2
		if y < 0 {
			y = 0
		}
		obj.Move(fyne.NewPos(x, y))
		obj.Resize(childSize)
		x += childSize.Width + l.gap
	}
}

func (l *sectionHeaderInlineLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	width := float32(0)
	height := float32(0)
	visibleCount := 0
	for _, obj := range objects {
		if obj == nil || !obj.Visible() {
			continue
		}
		childSize := obj.MinSize()
		width += childSize.Width
		if childSize.Height > height {
			height = childSize.Height
		}
		visibleCount++
	}
	if visibleCount > 1 {
		width += l.gap * float32(visibleCount-1)
	}
	return fyne.NewSize(width, height)
}

func (l *sectionCardWithAttachmentLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	card := objects[0]
	cardHeight := size.Height
	card.Move(fyne.NewPos(0, 0))
	card.Resize(fyne.NewSize(size.Width, cardHeight))

	if len(objects) < 2 {
		return
	}

	action := objects[1]
	actionMin := action.MinSize()
	actionX := size.Width - actionMin.Width - l.rightInset
	if actionX < 0 {
		actionX = 0
	}
	actionY := cardHeight - actionMin.Height - l.bottomInset
	if actionY < 0 {
		actionY = 0
	}
	action.Move(fyne.NewPos(actionX, actionY))
	action.Resize(actionMin)
}

func (l *sectionCardWithAttachmentLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	cardMin := objects[0].MinSize()
	if len(objects) < 2 {
		return cardMin
	}

	width := cardMin.Width
	return fyne.NewSize(width, cardMin.Height)
}

func NewDevicesListView(buildIntro func() fyne.CanvasObject, buildRows func() []fyne.CanvasObject) *DevicesListView {
	content := container.NewVBox()
	scroll := container.NewVScroll(NewInset(content, 0, 6, 0, 0))
	return &DevicesListView{
		Scroll:     scroll,
		content:    content,
		buildIntro: buildIntro,
		buildRows:  buildRows,
	}
}

func (v *DevicesListView) Refresh() {
	if v == nil || v.buildRows == nil {
		return
	}

	v.mu.Lock()
	rows := v.buildRows()

	var objects []fyne.CanvasObject
	if v.buildIntro != nil {
		if intro := v.buildIntro(); intro != nil {
			objects = append([]fyne.CanvasObject{intro}, rows...)
		} else {
			objects = rows
		}
	} else {
		objects = rows
	}
	v.mu.Unlock()

	forgetDiskRowWidgets(v.content.Objects)
	v.content.Objects = objects

	v.content.Refresh()
	v.Scroll.Refresh()
}

func (v *DevicesListView) RefreshItem(_ int) {
	v.Refresh()
}

func NewDiskWidgetUI(buildIntro func() fyne.CanvasObject, buildRows func() []fyne.CanvasObject, topRightOverlay fyne.CanvasObject) *DiskWidgetUI {
	devicesList := NewDevicesListView(buildIntro, buildRows)
	root := NewInset(devicesList.Scroll, 8, 0, 6, 0)
	if topRightOverlay != nil {
		fixedOverlay := container.NewGridWrap(topRightOverlay.MinSize(), topRightOverlay)
		overlay := container.NewBorder(
			nil,
			nil,
			nil,
			nil,
			NewInset(container.NewHBox(layout.NewSpacer(), fixedOverlay), 10, 22, 6, 0),
		)
		root = container.NewStack(root, overlay)
	}

	return &DiskWidgetUI{
		Container:   root,
		DevicesList: devicesList,
	}
}

func NewDeviceSectionAddButton(onTapped func()) fyne.CanvasObject {
	btn := widget.NewButton("+", onTapped)
	btn.Importance = widget.LowImportance
	return newCompactActionWrap(connectionCompactActionSize, btn)
}

func newSectionCardHeader(titleText fyne.CanvasObject, leadingAction fyne.CanvasObject, trailingAction fyne.CanvasObject, leadingGap float32) fyne.CanvasObject {
	var headerLeft fyne.CanvasObject = titleText
	if leadingAction != nil {
		headerLeft = container.New(&sectionHeaderInlineLayout{gap: leadingGap},
			titleText,
			leadingAction,
		)
	}

	headerRowItems := []fyne.CanvasObject{headerLeft, layout.NewSpacer()}
	if trailingAction != nil {
		headerRowItems = append(headerRowItems, trailingAction)
	}

	return NewInset(container.NewHBox(headerRowItems...), 4, 4, 0, 0)
}

func NewDiskRowTemplate() fyne.CanvasObject {
	checkbox := widget.NewCheck("", nil)
	checkboxWrap := container.NewThemeOverride(checkbox, &diskCheckboxTheme{base: design.NewBrandTheme()})
	captureSelector := NewCaptureSelector(nil)
	captureSelector.Hide()

	prefixIcon := canvas.NewImageFromResource(assets.DiscIcon)
	prefixIcon.FillMode = canvas.ImageFillContain
	prefixIcon.SetMinSize(fyne.NewSize(18, 18))

	nameLabel := newAdaptiveNameText()
	nameLabel.style.Bold = false
	nameLabel.SetText(i18n.Current.DeviceRowTemplateName)

	modeIcon := canvas.NewText("PTR", theme.Color(theme.ColorNameForeground))
	modeIcon.TextSize = theme.TextSize()
	modeIcon.Hide()
	modeTitleLabel := canvas.NewText(i18n.Current.DeviceMouse, design.ColorTextLight)
	modeTitleLabel.TextSize = theme.TextSize()
	modeTitleLabel.Hide()
	modeWrap := container.NewHBox(modeIcon, modeTitleLabel)

	nameRow := container.New(&deviceNameRowLayout{gap: 6}, prefixIcon, nameLabel, layout.NewSpacer())
	centerContent := container.NewVBox(nameRow, modeWrap)
	center := container.New(&deviceRowContentLayout{}, centerContent)

	statusLabel := widget.NewLabel(i18n.Current.DeviceRowTemplateStatus)
	statusLabel.Alignment = fyne.TextAlignTrailing
	statusLabel.Hide()
	statusDot := canvas.NewCircle(design.ColorTextMuted)
	statusDotWrap := container.NewGridWrap(fyne.NewSize(12, 12), statusDot)
	statusWrap := container.NewGridWrap(fyne.NewSize(18, 18), container.NewCenter(statusDotWrap))
	statusInfo := container.New(&sectionHeaderInlineLayout{gap: 6}, statusLabel, statusWrap)

	roRwBtn := widget.NewButton("RO", nil)
	roRwBtn.Hide()

	modeSelect := NewHeaderDropdown([]string{i18n.Current.DeviceTouchPad, i18n.Current.DeviceAbsolute}, i18n.Current.DeviceTouchPad, nil)
	modeSelect.Hide()

	uploadBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   design.ColorSurfaceLight,
		HoverFill:    design.ColorBorder,
		NormalIcon:   assets.UploadIcon,
		HoverIcon:    assets.UploadIcon,
		DisabledIcon: assets.UploadIconMuted,
		IconSize:     fyne.NewSize(18, 18),
		ButtonSize:   fyne.NewSize(deviceControlUnitWidth, deviceControlHeight),
	})
	uploadBtn.Hide()

	deleteBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:   design.ColorSurfaceLight,
		HoverFill:    color.NRGBA{R: 0x3e, G: 0x1f, B: 0x1f, A: 0xff},
		NormalIcon:   theme.NewThemedResource(theme.DeleteIcon()),
		HoverIcon:    theme.NewErrorThemedResource(theme.DeleteIcon()),
		DisabledIcon: theme.NewDisabledResource(theme.DeleteIcon()),
		IconSize:     fyne.NewSize(18, 18),
		ButtonSize:   fyne.NewSize(deviceControlUnitWidth, deviceControlHeight),
	})
	deleteBtn.Hide()

	settingsBtn := NewDeviceActionButton("Config", assets.ConfigVerticalIcon, nil)
	settingsBtn.Hide()

	right := container.New(&deviceRowControlsLayout{gap: deviceControlGap}, roRwBtn, modeSelect, uploadBtn, deleteBtn, settingsBtn, statusInfo)
	left := container.NewMax(container.NewCenter(checkboxWrap), container.NewCenter(captureSelector))

	root := container.New(&deviceRowLayout{gap: 6}, left, center, right)
	diskRowRegistry.Store(root, &DiskRowWidgets{
		Checkbox:        checkbox,
		CaptureSelector: captureSelector,
		PrefixIcon:      prefixIcon,
		NameLabel:       nameLabel,
		ModeIcon:        modeIcon,
		ModeTitleLabel:  modeTitleLabel,
		StatusLabel:     statusLabel,
		StatusDot:       statusDot,
		RORWButton:      roRwBtn,
		ModeSelect:      modeSelect,
		UploadButton:    uploadBtn,
		DeleteButton:    deleteBtn,
		SettingsButton:  settingsBtn,
	})
	return root
}

func ResolveDiskRowWidgets(obj fyne.CanvasObject) *DiskRowWidgets {
	if widgets, ok := diskRowRegistry.Load(obj); ok {
		if row, ok := widgets.(*DiskRowWidgets); ok {
			return row
		}
	}
	return nil
}

func forgetDiskRowWidgets(objects []fyne.CanvasObject) {
	for _, obj := range objects {
		forgetDiskRowWidgetTree(obj)
	}
}

func forgetDiskRowWidgetTree(obj fyne.CanvasObject) {
	if obj == nil {
		return
	}
	diskRowRegistry.Delete(obj)
	if containerObj, ok := obj.(*fyne.Container); ok {
		for _, child := range containerObj.Objects {
			forgetDiskRowWidgetTree(child)
		}
	}
}

func NewDeviceSectionCard(
	eyebrow string,
	title string,
	description string,
	count string,
	fill color.Color,
	border color.Color,
	badge color.Color,
	rows []fyne.CanvasObject,
	action fyne.CanvasObject,
	trailingAction fyne.CanvasObject,
) fyne.CanvasObject {
	_ = title
	_ = description
	_ = count
	_ = badge

	eyebrowText := NewBrandText(eyebrow, 11, design.ColorTextMuted, true)
	header := newSectionCardHeader(eyebrowText, action, trailingAction, 6)

	var bodyContent fyne.CanvasObject
	if len(rows) == 0 {
		spacer := canvas.NewRectangle(color.Transparent)
		spacer.SetMinSize(fyne.NewSize(1, 12))
		bodyContent = NewInset(spacer, 6, 6, 1, 1)
	} else {
		bodyContent = NewInset(container.NewVBox(rows...), 4, 4, 1, 1)
	}

	cardContent := NewInset(bodyContent, 0, 0, 1, 1)

	card := NewInset(NewCompactSurfacePanel(cardContent, fill, design.RadiusMD+2), 0, 0, 0, 3)
	return container.NewVBox(header, card)
}

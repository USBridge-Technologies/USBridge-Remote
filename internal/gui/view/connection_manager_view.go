package view

import (
	"image/color"
	"strings"
	"sync"
	"time"

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

type ConnectionManagerUI struct {
	Container         *fyne.Container
	ConnectionsScroll *container.Scroll
	ConnectionsBox    *fyne.Container
	QRBtn             fyne.CanvasObject
	AddBtn            fyne.CanvasObject

	contentArea *fyne.Container
	topActions  *fyne.Container
	topHelpBtn  fyne.CanvasObject
	tsStatus    *widget.Label
	tsAccount   *widget.Label
	tsAddress   *widget.Label
	tsCard      fyne.CanvasObject
	tsAuthBtn   *outlinedActionButton

	topQRBtn     *iconChromeButton
	topAddBtn    *outlinedActionButton
	centerQRBtn  *iconChromeButton
	centerAddBtn *onboardingPrimaryButton
}

type ConnectionRowData struct {
	Name            string
	AddressSummary  string
	ProtocolBadge   string
	ProtocolOptions []string
}

type ConnectionRowState struct {
	Disabled bool
	Loading  bool
}

type ConnectionRowActions struct {
	OnSelect         func()
	OnUse            func()
	OnEdit           func()
	OnProtocolChange func(string)
}

const (
	onboardingImageAspectRatio    float32 = 2000.0 / 1072.0
	onboardingImageMaxWidth       float32 = 500
	onboardingImageMaxHeight      float32 = 268
	emptyStateMaxWidth            float32 = 1440
	emptyStateMinWidth            float32 = 280
	emptyStateTitleMaxWidth       float32 = 760
	onboardingCarouselMinWidth    float32 = 260
	onboardingCarouselMinHeight   float32 = 188
	onboardingCarouselMaxHeight   float32 = 500
	onboardingCaptionMaxWidth     float32 = 760
	onboardingStageMinHeight      float32 = 132
	onboardingDotsTopSpacing      float32 = 8
	onboardingCaptionBottomGap    float32 = 4
	onboardingArrowGap            float32 = 4
	onboardingArrowEdgeMinInset   float32 = 4
	onboardingArrowEdgeMaxInset   float32 = 22
	onboardingActionGap           float32 = 4
	onboardingActionMinPrimaryW   float32 = 120
	onboardingActionMaxPrimaryW   float32 = 160
	onboardingActionStackMinWidth float32 = 140
	connectionCompactActionSize   float32 = 30
	connectionCompactActionGap    float32 = 2
	connectionNameEditGap         float32 = 10
)

var (
	onboardingIndicatorInactive = color.NRGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xff}
	onboardingIndicatorActive   = color.NRGBA{R: 0x65, G: 0x65, B: 0x65, A: 0xff}
	connectionActionBlockedFill = design.ColorGray900
)

func NewConnectionManagerUI(onQR func(), onAdd func(), onHelp func(), onTSAuth func()) *ConnectionManagerUI {
	topQRButton := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:  color.Transparent,
		HoverFill:   design.ColorSurfaceLight,
		Stroke:      color.Transparent,
		StrokeWidth: 0,
		NormalIcon:  assets.QRCodeLight,
		HoverIcon:   assets.QRCodeLight,
		IconSize:    fyne.NewSize(15, 15),
		ButtonSize:  fyne.NewSize(connectionCompactActionSize, connectionCompactActionSize),
		OnTapped:    onQR,
	})
	topQRBtn := newCompactActionWrap(connectionCompactActionSize, topQRButton)

	topAddButton := newOutlinedActionButton(compactAddActionLabel(i18n.Current.AddConnectionTitle), onAdd)
	topAddBtn := newCompactActionWrap(connectionCompactActionSize, topAddButton)

	centerQRButton := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:  color.Transparent,
		HoverFill:   design.ColorAccent,
		Stroke:      design.ColorAccent,
		StrokeWidth: 1.5,
		NormalIcon:  assets.QRCodeAccent,
		HoverIcon:   assets.QRCodeBoldBlack,
		IconSize:    fyne.NewSize(18, 18),
		ButtonSize:  fyne.NewSize(42, 42),
		OnTapped:    onQR,
	})
	centerQRBtn := centerQRButton

	centerAddButton := newOnboardingPrimaryButton(onboardingAddActionLabel(i18n.Current.AddConnectionTitle), onAdd)
	centerAddBtn := centerAddButton

	connectionsBox := container.NewVBox()
	connectionsScroll := container.NewScroll(connectionsBox)
	connectionsScroll.SetMinSize(fyne.NewSize(0, 0))

	topActions := container.NewHBox(topAddBtn, centerSpacer(connectionCompactActionGap), topQRBtn)
	var topHelpBtn fyne.CanvasObject
	if onHelp != nil {
		topHelpBtn = NewFooterIconButton(
			assets.QuestionIconDim,
			assets.QuestionIcon,
			fyne.NewSize(13, 13),
			onHelp,
		)
	}
	contentArea := container.NewMax()
	tsStatus := widget.NewLabel("Tailscale: checking")
	tsStatus.Wrapping = fyne.TextWrapWord
	tsAccount := widget.NewLabel("Google: not connected")
	tsAccount.Wrapping = fyne.TextWrapWord
	tsAddress := widget.NewLabel("Address: unavailable")
	tsAddress.Wrapping = fyne.TextWrapWord

	tsAuthBtn := newOutlinedActionButton("Sign In With Google", onTSAuth)
	tsButtons := container.NewGridWithColumns(1, tsAuthBtn)
	tsCard := newConnectionsSectionCard(
		"tailscale",
		nil,
		nil,
		container.NewVBox(
			tsStatus,
			tsAccount,
			tsAddress,
			tsButtons,
		),
	)

	mainContent := NewInset(contentArea, 16, 16, 10, 16)

	bg := canvas.NewRectangle(design.ColorGray950)
	root := container.NewStack(bg, mainContent)

	ui := &ConnectionManagerUI{
		Container:         root,
		ConnectionsScroll: connectionsScroll,
		ConnectionsBox:    connectionsBox,
		QRBtn:             centerQRBtn,
		AddBtn:            centerAddBtn,
		contentArea:       contentArea,
		topActions:        topActions,
		topHelpBtn:        topHelpBtn,
		tsStatus:          tsStatus,
		tsAccount:         tsAccount,
		tsAddress:         tsAddress,
		tsCard:            tsCard,
		tsAuthBtn:         tsAuthBtn,
		topQRBtn:          topQRButton,
		topAddBtn:         topAddButton,
		centerQRBtn:       centerQRButton,
		centerAddBtn:      centerAddButton,
	}
	ui.contentArea.Objects = []fyne.CanvasObject{
		container.NewVBox(tsCard, layout.NewSpacer()),
	}

	return ui
}

func (ui *ConnectionManagerUI) SetEmptyState() {
	stopCanvasAnimations(ui.ConnectionsBox)
	ui.ConnectionsBox.RemoveAll()

	slides := []onboardingSlide{
		{Image: assets.OnboardingStep01, Text: i18n.Current.OnboardingStepConnect},
		{Image: assets.OnboardingStep02, Text: i18n.Current.OnboardingStepIP},
		{Image: assets.OnboardingStep03, Text: i18n.Current.OnboardingStepScan},
	}

	title := NewBrandText(i18n.Current.AddNewDeviceTitle, 26, design.ColorTextLight, true)
	title.Alignment = fyne.TextAlignCenter

	actions := container.New(newOnboardingActionsLayout(onboardingActionGap), ui.AddBtn, ui.QRBtn)
	emptyBlock := container.New(
		newEmptyStateLayout(),
		title,
		newOnboardingCarousel(slides),
		actions,
	)

	ui.contentArea.Objects = []fyne.CanvasObject{
		container.NewVBox(
			ui.tsCard,
			newConnectionsSectionCard(i18n.Current.SavedConnections, ui.topActions, ui.topHelpBtn, emptyBlock),
			layout.NewSpacer(),
		),
	}
	ui.contentArea.Refresh()
}

func (ui *ConnectionManagerUI) SetRows(rows []*fyne.Container) {
	stopCanvasAnimations(ui.ConnectionsBox)
	ui.ConnectionsBox.RemoveAll()
	for _, row := range rows {
		ui.ConnectionsBox.Add(row)
	}
	ui.ConnectionsBox.Refresh()
	listMin := ui.ConnectionsBox.MinSize()
	ui.ConnectionsScroll.SetMinSize(fyne.NewSize(0, listMin.Height))
	ui.contentArea.Objects = []fyne.CanvasObject{
		container.NewVBox(
			ui.tsCard,
			newConnectionsSectionCard(i18n.Current.SavedConnections, ui.topActions, ui.topHelpBtn, ui.ConnectionsScroll),
			layout.NewSpacer(),
		),
	}
	ui.ConnectionsScroll.Refresh()
	ui.contentArea.Refresh()
}

func (ui *ConnectionManagerUI) SetTailscaleState(status, account, address, authLabel string) {
	if ui.tsStatus != nil {
		ui.tsStatus.SetText(status)
	}
	if ui.tsAccount != nil {
		ui.tsAccount.SetText(account)
	}
	if ui.tsAddress != nil {
		ui.tsAddress.SetText(address)
	}
	if ui.tsAuthBtn != nil {
		if strings.TrimSpace(authLabel) != "" {
			ui.tsAuthBtn.SetLabel(authLabel)
		}
		ui.tsAuthBtn.SetDisabled(false)
	}
}

func (ui *ConnectionManagerUI) SetActionButtonsDisabled(disabled bool) {
	if ui.topQRBtn != nil {
		ui.topQRBtn.SetDisabled(disabled)
	}
	if ui.topAddBtn != nil {
		ui.topAddBtn.SetDisabled(disabled)
	}
	if ui.centerQRBtn != nil {
		ui.centerQRBtn.SetDisabled(disabled)
	}
	if ui.centerAddBtn != nil {
		ui.centerAddBtn.SetDisabled(disabled)
	}
}

func newConnectionsSectionCard(title string, leadingAction fyne.CanvasObject, trailingAction fyne.CanvasObject, body fyne.CanvasObject) fyne.CanvasObject {
	titleText := NewBrandText(strings.ToUpper(strings.TrimSpace(title)), 11, design.ColorTextMuted, true)
	header := newSectionCardHeader(titleText, leadingAction, trailingAction, 6)

	card := NewInset(
		NewCompactSurfacePanel(body, design.ColorGray900, design.RadiusMD+2),
		0, 0, 0, 3,
	)

	return container.NewVBox(header, card)
}

type onboardingSlide struct {
	Image fyne.Resource
	Text  string
}

func newOnboardingCarousel(slides []onboardingSlide) fyne.CanvasObject {
	if len(slides) == 0 {
		return canvas.NewRectangle(color.Transparent)
	}

	currentSlide := 0
	image := canvas.NewImageFromResource(slides[currentSlide].Image)
	image.FillMode = canvas.ImageFillContain

	captionLabel := widget.NewLabel(slides[currentSlide].Text)
	captionLabel.Alignment = fyne.TextAlignCenter
	captionLabel.Wrapping = fyne.TextWrapWord
	caption := container.NewThemeOverride(captionLabel, newForegroundOverrideTheme(design.NewBrandTheme(), design.ColorTextMuted))

	dots := make([]*canvas.Circle, len(slides))
	dotItems := make([]fyne.CanvasObject, 0, len(slides)*2)
	for idx := range slides {
		dot := canvas.NewCircle(onboardingIndicatorInactive)
		dots[idx] = dot
		dotItems = append(dotItems, container.NewGridWrap(fyne.NewSize(10, 10), dot))
		if idx < len(slides)-1 {
			dotItems = append(dotItems, centerSpacer(8))
		}
	}

	prevBtn := newArrowButton(assets.ArrowLeftGray, assets.ArrowLeftWhite, nil)
	nextBtn := newArrowButton(assets.ArrowRightGray, assets.ArrowRightWhite, nil)

	applySlide := func() {
		image.Resource = slides[currentSlide].Image
		image.Refresh()
		captionLabel.SetText(slides[currentSlide].Text)

		for idx, dot := range dots {
			if idx == currentSlide {
				dot.FillColor = onboardingIndicatorActive
			} else {
				dot.FillColor = onboardingIndicatorInactive
			}
			dot.Refresh()
		}
	}

	updateControls := func() {
		prevBtn.SetDisabled(currentSlide == 0)
		nextBtn.SetDisabled(currentSlide == len(slides)-1)
	}

	animateTo := func(nextSlide int) {
		if nextSlide < 0 || nextSlide >= len(slides) || nextSlide == currentSlide {
			return
		}
		currentSlide = nextSlide
		applySlide()
		updateControls()
	}

	prevBtn.onTapped = func() {
		animateTo(currentSlide - 1)
	}

	nextBtn.onTapped = func() {
		animateTo(currentSlide + 1)
	}

	applySlide()
	updateControls()

	stage := newOnboardingStage(image, prevBtn, nextBtn, func(direction int) {
		animateTo(currentSlide + direction)
	})

	return container.New(
		newOnboardingCarouselLayout(),
		caption,
		stage,
		container.NewCenter(container.NewHBox(dotItems...)),
	)
}

type onboardingStage struct {
	widget.BaseWidget

	image       fyne.CanvasObject
	prev        fyne.CanvasObject
	next        fyne.CanvasObject
	onSwipe     func(int)
	dragOffsetX float32
}

func newOnboardingStage(image fyne.CanvasObject, prev fyne.CanvasObject, next fyne.CanvasObject, onSwipe func(int)) *onboardingStage {
	stage := &onboardingStage{
		image:   image,
		prev:    prev,
		next:    next,
		onSwipe: onSwipe,
	}
	stage.ExtendBaseWidget(stage)
	return stage
}

func (s *onboardingStage) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(container.New(newCarouselStageLayout(onboardingImageAspectRatio), s.image, s.prev, s.next))
}

func (s *onboardingStage) MinSize() fyne.Size {
	return fyne.NewSize(onboardingCarouselMinWidth, onboardingStageMinHeight)
}

func (s *onboardingStage) Dragged(event *fyne.DragEvent) {
	s.dragOffsetX += event.Dragged.DX
}

func (s *onboardingStage) DragEnd() {
	threshold := clampFloat32(s.Size().Width*0.12, 28, 72)
	if s.onSwipe != nil {
		switch {
		case s.dragOffsetX >= threshold:
			s.onSwipe(-1)
		case s.dragOffsetX <= -threshold:
			s.onSwipe(1)
		}
	}
	s.dragOffsetX = 0
}

type arrowButton struct {
	widget.BaseWidget

	onTapped    func()
	hovered     bool
	disabled    bool
	normalIcon  fyne.Resource
	hoveredIcon fyne.Resource
	icon        *canvas.Image
}

func newArrowButton(normalIcon fyne.Resource, hoveredIcon fyne.Resource, onTapped func()) *arrowButton {
	btn := &arrowButton{
		onTapped:    onTapped,
		normalIcon:  normalIcon,
		hoveredIcon: hoveredIcon,
		icon:        canvas.NewImageFromResource(normalIcon),
	}
	btn.icon.FillMode = canvas.ImageFillContain
	btn.icon.SetMinSize(fyne.NewSize(22, 22))
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *arrowButton) CreateRenderer() fyne.WidgetRenderer {
	hitArea := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(container.NewMax(hitArea, container.NewCenter(b.icon)))
}

func (b *arrowButton) MinSize() fyne.Size {
	return fyne.NewSize(28, 28)
}

func (b *arrowButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshIcon()
}

func (b *arrowButton) MouseMoved(*desktop.MouseEvent) {}

func (b *arrowButton) MouseOut() {
	b.hovered = false
	b.refreshIcon()
}

func (b *arrowButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTapped == nil {
		return
	}
	b.onTapped()
}

func (b *arrowButton) TappedSecondary(*fyne.PointEvent) {}

func (b *arrowButton) SetDisabled(disabled bool) {
	b.disabled = disabled
	if disabled {
		b.hovered = false
	}
	b.refreshIcon()
}

func (b *arrowButton) refreshIcon() {
	if b.icon == nil {
		return
	}

	if b.disabled {
		b.icon.Hide()
		b.icon.Refresh()
		return
	}

	b.icon.Show()
	resource := b.normalIcon
	if b.hovered {
		resource = b.hoveredIcon
	}
	b.icon.Resource = resource
	b.icon.Refresh()
}

type foregroundOverrideTheme struct {
	base       fyne.Theme
	foreground color.Color
}

func newForegroundOverrideTheme(base fyne.Theme, foreground color.Color) fyne.Theme {
	return &foregroundOverrideTheme{
		base:       base,
		foreground: foreground,
	}
}

func (t *foregroundOverrideTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground {
		return t.foreground
	}
	return t.base.Color(name, variant)
}

func (t *foregroundOverrideTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t *foregroundOverrideTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t *foregroundOverrideTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.base.Size(name)
}

var (
	_ fyne.Tappable     = (*arrowButton)(nil)
	_ desktop.Hoverable = (*arrowButton)(nil)
	_ fyne.Widget       = (*arrowButton)(nil)
	_ fyne.Tappable     = (*onboardingPrimaryButton)(nil)
	_ desktop.Hoverable = (*onboardingPrimaryButton)(nil)
	_ fyne.Widget       = (*onboardingPrimaryButton)(nil)
	_ fyne.Draggable    = (*onboardingStage)(nil)
	_ fyne.Widget       = (*onboardingStage)(nil)
	_ fyne.Tappable     = (*iconChromeButton)(nil)
	_ desktop.Hoverable = (*iconChromeButton)(nil)
	_ fyne.Widget       = (*iconChromeButton)(nil)
	_ fyne.Tappable     = (*connectionPrimaryButton)(nil)
	_ desktop.Hoverable = (*connectionPrimaryButton)(nil)
	_ fyne.Widget       = (*connectionPrimaryButton)(nil)
)

func NewConnectionRow(data ConnectionRowData, state ConnectionRowState, actions ConnectionRowActions) *fyne.Container {
	nameBlock := newConnectionNameButton(data.Name, data.AddressSummary, actions.OnEdit)
	nameBlock.SetDisabled(state.Disabled)

	protocolBtn := NewHeaderDropdown(data.ProtocolOptions, data.ProtocolBadge, func(value string) {
		if actions.OnProtocolChange != nil {
			actions.OnProtocolChange(value)
		}
	})
	protocolBtn.Compact = true
	protocolBtn.SetSelected(data.ProtocolBadge)
	protocolBtn.SetDisabled(state.Disabled)

	useBtn := newConnectionActionIconButton(actions.OnUse)
	useBtn.SetDisabled(state.Disabled)
	useBtn.SetLoading(state.Loading)

	left := canvas.NewRectangle(color.Transparent)
	left.SetMinSize(fyne.NewSize(1, 1))
	center := container.New(&connectionCompactContentLayout{}, nameBlock)
	right := container.New(&deviceRowControlsLayout{gap: deviceControlGap}, protocolBtn, useBtn)
	row := container.New(&deviceRowLayout{gap: 6}, left, center, right)
	return NewInset(row, 0, 4, 4, 4)
}

func inlineSpacer(width float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(width, 1))
	return spacer
}

type connectionNameButton struct {
	widget.BaseWidget

	title    string
	subtitle string
	onTapped func()
	disabled bool
	hovered  bool

	bg       *canvas.Rectangle
	titleTxt *adaptiveNameText
	subTxt   *canvas.Text
	icon     *canvas.Image
}

func newConnectionNameButton(title, subtitle string, onTapped func()) *connectionNameButton {
	b := &connectionNameButton{
		title:    title,
		subtitle: subtitle,
		onTapped: onTapped,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *connectionNameButton) SetDisabled(disabled bool) {
	b.disabled = disabled
	if disabled {
		b.hovered = false
	}
	b.refreshVisuals()
}

func (b *connectionNameButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTapped == nil {
		return
	}
	b.onTapped()
}

func (b *connectionNameButton) TappedSecondary(*fyne.PointEvent) {}

func (b *connectionNameButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *connectionNameButton) MouseMoved(*desktop.MouseEvent) {}

func (b *connectionNameButton) MouseOut() {
	if !b.hovered {
		return
	}
	b.hovered = false
	b.refreshVisuals()
}

func (b *connectionNameButton) MinSize() fyne.Size {
	title := fyne.MeasureText("Conn...ion", 14, fyne.TextStyle{Bold: true})
	sub := fyne.MeasureText(b.subtitle, 11, fyne.TextStyle{})
	width := maxFloat32(title.Width, sub.Width) + 34
	height := title.Height + sub.Height + 8
	return fyne.NewSize(width, height)
}

func (b *connectionNameButton) preferredWidth() float32 {
	title := fyne.MeasureText(b.title, 14, fyne.TextStyle{Bold: true})
	sub := fyne.MeasureText(b.subtitle, 11, fyne.TextStyle{})
	return maxFloat32(title.Width, sub.Width) + 34
}

func (b *connectionNameButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.titleTxt = newAdaptiveNameText()
	b.titleTxt.textSize = 14
	b.titleTxt.style = fyne.TextStyle{Bold: true}
	b.titleTxt.SetColor(design.ColorTextLight)
	b.titleTxt.SetText(b.title)
	b.subTxt = NewBrandText(b.subtitle, 11, design.ColorTextMuted, false)
	b.icon = canvas.NewImageFromResource(theme.DocumentCreateIcon())
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(13, 13))

	r := &connectionNameButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.bg, b.titleTxt, b.subTxt, b.icon},
	}
	r.Refresh()
	return r
}

func (b *connectionNameButton) refreshVisuals() {
	if b.bg == nil || b.titleTxt == nil || b.subTxt == nil || b.icon == nil {
		return
	}

	b.bg.FillColor = color.Transparent
	b.titleTxt.SetText(b.title)
	b.titleTxt.SetColor(design.ColorTextLight)
	b.subTxt.Color = design.ColorTextMuted
	b.icon.Translucency = 0

	if b.disabled {
		b.titleTxt.SetColor(design.ColorBorder)
		b.subTxt.Color = design.ColorBorder
		b.icon.Translucency = 0.35
	} else if b.hovered {
		b.bg.FillColor = design.ColorSurfaceLight
	}

	b.bg.Refresh()
	b.subTxt.Refresh()
	b.icon.Refresh()
}

type connectionNameButtonRenderer struct {
	button  *connectionNameButton
	objects []fyne.CanvasObject
}

func (r *connectionNameButtonRenderer) Layout(size fyne.Size) {
	r.button.bg.Resize(size)

	titleWidth := maxFloat32(0, size.Width-34)
	r.button.titleTxt.Move(fyne.NewPos(8, 3))
	r.button.titleTxt.Resize(fyne.NewSize(titleWidth, r.button.titleTxt.MinSize().Height))

	r.button.subTxt.Move(fyne.NewPos(8, 19))
	r.button.subTxt.Resize(fyne.NewSize(maxFloat32(0, size.Width-16), r.button.subTxt.MinSize().Height))

	iconSize := fyne.NewSize(13, 13)
	r.button.icon.Resize(iconSize)
	r.button.icon.Move(fyne.NewPos(maxFloat32(0, size.Width-iconSize.Width-8), 5))
}

func (r *connectionNameButtonRenderer) MinSize() fyne.Size {
	return r.button.MinSize()
}

func (r *connectionNameButtonRenderer) Refresh() {
	r.button.refreshVisuals()
	r.Layout(r.button.Size())
}

func (r *connectionNameButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *connectionNameButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *connectionNameButtonRenderer) Destroy() {}

type connectionCompactContentLayout struct{}

func (l *connectionCompactContentLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	child := objects[0]
	min := child.MinSize()
	width := min.Width
	if btn, ok := child.(*connectionNameButton); ok {
		width = btn.preferredWidth()
	}
	width = minFloat32(size.Width, width)
	if width < 0 {
		width = 0
	}
	y := (size.Height - min.Height) / 2
	if y < 0 {
		y = 0
	}
	child.Move(fyne.NewPos(0, y))
	child.Resize(fyne.NewSize(width, min.Height))
}

func (l *connectionCompactContentLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

type connectionNameLayout struct{}

func newConnectionNameLayout() fyne.Layout {
	return &connectionNameLayout{}
}

func (l *connectionNameLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	textBlock := objects[0]
	editBtn := objects[1]
	editMin := editBtn.MinSize()
	textMin := textBlock.MinSize()
	textWidth := minFloat32(textMin.Width, size.Width-editMin.Width-connectionNameEditGap)
	if textWidth < 0 {
		textWidth = 0
	}

	textBlock.Move(fyne.NewPos(0, 0))
	textBlock.Resize(fyne.NewSize(textWidth, size.Height))

	editY := float32(0)
	if size.Height > editMin.Height {
		editY = maxFloat32(0, (size.Height-editMin.Height)/2-8)
	}
	editBtn.Move(fyne.NewPos(textWidth+connectionNameEditGap, editY))
	editBtn.Resize(editMin)
}

func (l *connectionNameLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	textMin := objects[0].MinSize()
	editMin := objects[1].MinSize()
	return fyne.NewSize(textMin.Width+connectionNameEditGap+editMin.Width, maxFloat32(textMin.Height, editMin.Height))
}

func newConnectionSquareIconButton(icon fyne.Resource, onTapped func(), disabled bool) fyne.CanvasObject {
	return newIconChromeButton(iconChromeButtonSpec{
		Disabled:     disabled,
		DisabledFill: connectionActionBlockedFill,
		NormalFill:   design.ColorGray900,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       design.ColorBorder,
		StrokeWidth:  1,
		NormalIcon:   icon,
		HoverIcon:    icon,
		DisabledIcon: icon,
		IconSize:     fyne.NewSize(18, 18),
		ButtonSize:   fyne.NewSize(40, 40),
		OnTapped:     onTapped,
	})
}

func newConnectionInlineIconButton(icon fyne.Resource, onTapped func(), disabled bool) fyne.CanvasObject {
	return newIconChromeButton(iconChromeButtonSpec{
		Disabled:     disabled,
		NormalFill:   color.Transparent,
		HoverFill:    design.ColorSurfaceLight,
		Stroke:       color.Transparent,
		StrokeWidth:  0,
		NormalIcon:   icon,
		HoverIcon:    icon,
		DisabledIcon: theme.NewDisabledResource(icon),
		IconSize:     fyne.NewSize(15, 15),
		ButtonSize:   fyne.NewSize(connectionCompactActionSize, connectionCompactActionSize),
		OnTapped:     onTapped,
	})
}

type connectionActionIconButton struct {
	widget.BaseWidget

	onTapped    func()
	disabled    bool
	loading     bool
	hovered     bool
	bg          *canvas.Rectangle
	icon        *canvas.Image
	spinnerMu   sync.Mutex
	spinnerStop chan struct{}
	spinnerStep int
}

func newConnectionActionIconButton(onTapped func()) *connectionActionIconButton {
	btn := &connectionActionIconButton{onTapped: onTapped}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *connectionActionIconButton) SetDisabled(disabled bool) {
	b.disabled = disabled
	if disabled {
		b.hovered = false
	}
	b.refreshVisuals()
}

func (b *connectionActionIconButton) SetLoading(loading bool) {
	b.loading = loading
	if loading {
		b.hovered = false
	}
	b.refreshVisuals()
}

func (b *connectionActionIconButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.loading || b.onTapped == nil {
		return
	}
	b.onTapped()
}

func (b *connectionActionIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *connectionActionIconButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled || b.loading {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *connectionActionIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *connectionActionIconButton) MouseOut() {
	if !b.hovered {
		return
	}
	b.hovered = false
	b.refreshVisuals()
}

func (b *connectionActionIconButton) MinSize() fyne.Size {
	return fyne.NewSize(deviceControlUnitWidth, deviceControlHeight)
}

func (b *connectionActionIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorSurfaceLight)
	b.bg.CornerRadius = design.RadiusMD

	b.icon = canvas.NewImageFromResource(assets.ConnectIcon)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(18, 18))

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.icon)))
}

func (b *connectionActionIconButton) refreshVisuals() {
	if b.bg == nil || b.icon == nil {
		return
	}

	fill := design.ColorSurfaceLight
	var resource fyne.Resource = assets.ConnectIcon
	translucency := float64(0)

	switch {
	case b.loading:
		resource = assets.LoadingGrayFrames[0]
	case b.disabled:
		fill = connectionActionBlockedFill
		resource = assets.ConnectIconMuted
		translucency = 0.18
	case b.hovered:
		fill = design.ColorBorder
	}

	b.bg.FillColor = fill
	b.bg.Refresh()
	b.icon.Resource = resource
	b.icon.Translucency = translucency
	b.icon.Refresh()

	if b.loading {
		b.startSpinner()
		return
	}
	b.stopSpinner()
}

func (b *connectionActionIconButton) startSpinner() {
	if len(assets.LoadingGrayFrames) == 0 || b.icon == nil {
		return
	}

	b.stopSpinner()
	stop := make(chan struct{})

	b.spinnerMu.Lock()
	b.spinnerStop = stop
	b.spinnerStep = 0
	b.spinnerMu.Unlock()

	b.icon.Resource = assets.LoadingGrayFrames[0]
	b.icon.Refresh()

	go func() {
		ticker := time.NewTicker(140 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fyne.Do(func() {
					b.spinnerMu.Lock()
					active := b.spinnerStop == stop
					if active {
						b.spinnerStep = (b.spinnerStep + 1) % len(assets.LoadingGrayFrames)
					}
					step := b.spinnerStep
					b.spinnerMu.Unlock()
					if !active || b.icon == nil {
						return
					}
					b.icon.Resource = assets.LoadingGrayFrames[step]
					b.icon.Refresh()
				})
			case <-stop:
				return
			}
		}
	}()
}

func (b *connectionActionIconButton) stopSpinner() {
	b.spinnerMu.Lock()
	stop := b.spinnerStop
	b.spinnerStop = nil
	b.spinnerMu.Unlock()

	if stop != nil {
		close(stop)
	}
}

func (b *connectionActionIconButton) StopAnimations() {
	b.stopSpinner()
}

func compactAddActionLabel(label string) string {
	return "+"
}

func onboardingAddActionLabel(label string) string {
	return "+ " + label
}

type outlinedActionButton struct {
	widget.BaseWidget

	labelText string
	onTapped  func()
	hovered   bool
	disabled  bool
	bg        *canvas.Rectangle
	border    *canvas.Rectangle
	label     *canvas.Text
}

func newOutlinedActionButton(label string, onTapped func()) *outlinedActionButton {
	btn := &outlinedActionButton{
		labelText: label,
		onTapped:  onTapped,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *outlinedActionButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeColor = color.Transparent
	b.border.StrokeWidth = 0

	b.label = canvas.NewText(b.labelText, design.ColorTextMuted)
	b.label.TextSize = 18
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.label), b.border))
}

func (b *outlinedActionButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.labelText, design.ColorTextMuted)
	measure.TextSize = 18
	measure.TextStyle.Bold = true
	labelSize := measure.MinSize()
	width := labelSize.Width + 14
	if width < connectionCompactActionSize {
		width = connectionCompactActionSize
	}
	return fyne.NewSize(width, connectionCompactActionSize)
}

func (b *outlinedActionButton) Tapped(*fyne.PointEvent) {
	if b.disabled {
		return
	}
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *outlinedActionButton) TappedSecondary(*fyne.PointEvent) {}

func (b *outlinedActionButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *outlinedActionButton) MouseMoved(*desktop.MouseEvent) {}

func (b *outlinedActionButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *outlinedActionButton) SetDisabled(disabled bool) {
	b.disabled = disabled
	b.hovered = false
	b.refreshVisuals()
}

func (b *outlinedActionButton) SetLabel(label string) {
	b.labelText = label
	if b.label != nil {
		b.label.Text = label
		b.label.Refresh()
	}
	b.Refresh()
}

func (b *outlinedActionButton) refreshVisuals() {
	if b.bg == nil || b.border == nil || b.label == nil {
		return
	}

	b.bg.FillColor = color.Transparent
	b.label.Color = design.ColorTextMuted
	if b.disabled {
		b.label.Color = design.ColorBorder
	} else if b.hovered {
		b.bg.FillColor = design.ColorSurfaceLight
		b.label.Color = design.ColorTextMuted
	}

	b.bg.Refresh()
	b.border.Refresh()
	b.label.Refresh()
}

func newCompactActionWrap(size float32, child fyne.CanvasObject) fyne.CanvasObject {
	return container.NewCenter(container.NewGridWrap(fyne.NewSize(size, size), child))
}

type transparentTapOverlay struct {
	widget.BaseWidget

	onTapped func()
	disabled bool
}

func newTransparentTapOverlay(onTapped func()) *transparentTapOverlay {
	overlay := &transparentTapOverlay{onTapped: onTapped}
	overlay.ExtendBaseWidget(overlay)
	return overlay
}

func (o *transparentTapOverlay) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(canvas.NewRectangle(color.Transparent))
}

func (o *transparentTapOverlay) Tapped(*fyne.PointEvent) {
	if o.disabled || o.onTapped == nil {
		return
	}

	o.onTapped()
}

func (o *transparentTapOverlay) TappedSecondary(*fyne.PointEvent) {}

func (o *transparentTapOverlay) SetDisabled(disabled bool) {
	o.disabled = disabled
}

type animationStopper interface {
	StopAnimations()
}

func stopCanvasAnimations(obj fyne.CanvasObject) {
	if obj == nil {
		return
	}

	if stopper, ok := obj.(animationStopper); ok {
		stopper.StopAnimations()
	}

	if containerObj, ok := obj.(*fyne.Container); ok {
		for _, child := range containerObj.Objects {
			stopCanvasAnimations(child)
		}
	}
}

type connectionPrimaryButton struct {
	widget.BaseWidget

	labelText string
	onTapped  func()
	disabled  bool
	loading   bool
	hovered   bool

	bg          *canvas.Rectangle
	label       *canvas.Text
	icon        *canvas.Image
	spinnerMu   sync.Mutex
	spinnerStop chan struct{}
	spinnerStep int
}

func newConnectionPrimaryButton(label string, onTapped func()) *connectionPrimaryButton {
	btn := &connectionPrimaryButton{
		labelText: label,
		onTapped:  onTapped,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *connectionPrimaryButton) SetDisabled(disabled bool) {
	b.disabled = disabled
	if disabled {
		b.hovered = false
	}
	b.refreshVisuals()
}

func (b *connectionPrimaryButton) SetLoading(loading bool) {
	b.loading = loading
	if loading {
		b.hovered = false
	}
	b.refreshVisuals()
}

func (b *connectionPrimaryButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.loading || b.onTapped == nil {
		return
	}

	b.onTapped()
}

func (b *connectionPrimaryButton) TappedSecondary(*fyne.PointEvent) {}

func (b *connectionPrimaryButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled || b.loading {
		return
	}

	b.hovered = true
	b.refreshVisuals()
}

func (b *connectionPrimaryButton) MouseMoved(*desktop.MouseEvent) {}

func (b *connectionPrimaryButton) MouseOut() {
	if !b.hovered {
		return
	}

	b.hovered = false
	b.refreshVisuals()
}

func (b *connectionPrimaryButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.labelText, design.ColorBackground)
	measure.TextSize = 14
	measure.TextStyle.Bold = true
	labelSize := measure.MinSize()
	width := labelSize.Width + 28
	if width < 104 {
		width = 104
	}
	return fyne.NewSize(width, 40)
}

func (b *connectionPrimaryButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorAccent)
	b.bg.CornerRadius = design.RadiusMD

	b.label = canvas.NewText(b.labelText, design.ColorBackground)
	b.label.TextSize = 14
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.icon = canvas.NewImageFromResource(nil)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(fyne.NewSize(18, 18))

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(container.NewStack(b.icon, b.label))))
}

func (b *connectionPrimaryButton) refreshVisuals() {
	if b.bg == nil || b.label == nil || b.icon == nil {
		return
	}

	fill := design.ColorAccent
	labelColor := design.ColorBackground
	if b.loading {
		fill = design.ColorAccent
		labelColor = design.ColorBackground
	} else if b.disabled {
		fill = connectionActionBlockedFill
		labelColor = design.ColorBorder
	} else if b.hovered {
		fill = design.ColorAccentHover
	}

	b.bg.FillColor = fill
	b.bg.Refresh()

	b.label.Color = labelColor
	b.label.Refresh()

	if b.loading {
		b.label.Hide()
		b.icon.Show()
		b.startSpinner()
		return
	}

	b.stopSpinner()
	b.icon.Hide()
	b.label.Show()
}

func (b *connectionPrimaryButton) startSpinner() {
	if len(assets.LoadingGrayFrames) == 0 || b.icon == nil {
		return
	}

	b.stopSpinner()

	stop := make(chan struct{})

	b.spinnerMu.Lock()
	b.spinnerStop = stop
	b.spinnerStep = 0
	b.spinnerMu.Unlock()

	b.icon.Resource = assets.LoadingGrayFrames[0]
	b.icon.Refresh()

	go func() {
		ticker := time.NewTicker(140 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				fyne.Do(func() {
					b.spinnerMu.Lock()
					active := b.spinnerStop == stop
					if active {
						b.spinnerStep = (b.spinnerStep + 1) % len(assets.LoadingGrayFrames)
					}
					step := b.spinnerStep
					b.spinnerMu.Unlock()
					if !active || b.icon == nil {
						return
					}
					b.icon.Resource = assets.LoadingGrayFrames[step]
					b.icon.Refresh()
				})
			case <-stop:
				return
			}
		}
	}()
}

func (b *connectionPrimaryButton) stopSpinner() {
	b.spinnerMu.Lock()
	stop := b.spinnerStop
	b.spinnerStop = nil
	b.spinnerMu.Unlock()

	if stop != nil {
		close(stop)
	}
}

func (b *connectionPrimaryButton) StopAnimations() {
	b.stopSpinner()
}

func centerSpacer(width float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(design.ColorGray950)
	spacer.SetMinSize(fyne.NewSize(width, 1))
	return spacer
}

type onboardingPrimaryButton struct {
	widget.BaseWidget

	labelText string
	onTapped  func()
	hovered   bool
	disabled  bool
	bg        *canvas.Rectangle
	label     *canvas.Text
}

func newOnboardingPrimaryButton(label string, onTapped func()) *onboardingPrimaryButton {
	btn := &onboardingPrimaryButton{
		labelText: label,
		onTapped:  onTapped,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *onboardingPrimaryButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorAccent)
	b.bg.CornerRadius = design.RadiusMD

	b.label = canvas.NewText(b.labelText, design.ColorBackground)
	b.label.TextSize = 14
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.label)))
}

func (b *onboardingPrimaryButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.labelText, design.ColorBackground)
	measure.TextSize = 14
	measure.TextStyle.Bold = true
	labelSize := measure.MinSize()
	return fyne.NewSize(labelSize.Width+12, 42)
}

func (b *onboardingPrimaryButton) Tapped(*fyne.PointEvent) {
	if b.disabled {
		return
	}
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *onboardingPrimaryButton) TappedSecondary(*fyne.PointEvent) {}

func (b *onboardingPrimaryButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *onboardingPrimaryButton) MouseMoved(*desktop.MouseEvent) {}

func (b *onboardingPrimaryButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *onboardingPrimaryButton) SetDisabled(disabled bool) {
	b.disabled = disabled
	b.hovered = false
	b.refreshVisuals()
}

func (b *onboardingPrimaryButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}

	if b.disabled {
		b.bg.FillColor = connectionActionBlockedFill
		b.label.Color = design.ColorBorder
	} else if b.hovered {
		b.bg.FillColor = design.ColorAccentHover
		b.label.Color = design.ColorBackground
	} else {
		b.bg.FillColor = design.ColorAccent
		b.label.Color = design.ColorBackground
	}
	b.bg.Refresh()
	b.label.Refresh()
}

type iconChromeButtonSpec struct {
	Disabled     bool
	DisabledFill color.Color
	NormalFill   color.Color
	HoverFill    color.Color
	Stroke       color.Color
	StrokeWidth  float32
	NormalIcon   fyne.Resource
	HoverIcon    fyne.Resource
	DisabledIcon fyne.Resource
	IconSize     fyne.Size
	ButtonSize   fyne.Size
	OnTapped     func()
}

type iconChromeButton struct {
	widget.BaseWidget

	spec    iconChromeButtonSpec
	hovered bool
	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	icon    *canvas.Image
	label   *canvas.Text
	text    string
}

func newIconChromeButton(spec iconChromeButtonSpec) *iconChromeButton {
	btn := &iconChromeButton{spec: spec}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *iconChromeButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(b.spec.NormalFill)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeColor = b.spec.Stroke
	b.border.StrokeWidth = b.spec.StrokeWidth

	b.icon = canvas.NewImageFromResource(b.spec.NormalIcon)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.SetMinSize(b.spec.IconSize)

	b.label = canvas.NewText("", design.ColorTextLight)
	b.label.TextSize = 12
	b.label.TextStyle = fyne.TextStyle{Bold: true}
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.icon), container.NewCenter(b.label), b.border))
}

func (b *iconChromeButton) MinSize() fyne.Size {
	if b.spec.ButtonSize.Width > 0 && b.spec.ButtonSize.Height > 0 {
		return b.spec.ButtonSize
	}
	return fyne.NewSize(48, 48)
}

func (b *iconChromeButton) Tapped(*fyne.PointEvent) {
	if b.spec.Disabled {
		return
	}

	if b.spec.OnTapped != nil {
		b.spec.OnTapped()
	}
}

func (b *iconChromeButton) TappedSecondary(*fyne.PointEvent) {}

func (b *iconChromeButton) SetDisabled(disabled bool) {
	b.spec.Disabled = disabled
	b.hovered = false
	b.refreshVisuals()
}

func (b *iconChromeButton) SetOnTapped(onTapped func()) {
	b.spec.OnTapped = onTapped
}

func (b *iconChromeButton) SetIcons(normalIcon fyne.Resource, hoverIcon fyne.Resource, disabledIcon fyne.Resource) {
	b.spec.NormalIcon = normalIcon
	b.spec.HoverIcon = hoverIcon
	b.spec.DisabledIcon = disabledIcon
	b.refreshVisuals()
}

func (b *iconChromeButton) SetText(text string) {
	b.text = text
	b.refreshVisuals()
}

func (b *iconChromeButton) MouseIn(*desktop.MouseEvent) {
	if b.spec.Disabled {
		return
	}

	b.hovered = true
	b.refreshVisuals()
}

func (b *iconChromeButton) MouseMoved(*desktop.MouseEvent) {}

func (b *iconChromeButton) MouseOut() {
	if b.spec.Disabled {
		return
	}

	b.hovered = false
	b.refreshVisuals()
}

func (b *iconChromeButton) refreshVisuals() {
	if b.bg == nil || b.border == nil || b.icon == nil || b.label == nil {
		return
	}

	b.bg.FillColor = b.spec.NormalFill
	b.icon.Resource = b.spec.NormalIcon
	b.icon.Translucency = 0
	b.label.Text = b.text
	b.label.Color = design.ColorTextLight
	if b.spec.Disabled {
		if b.spec.DisabledFill != nil {
			b.bg.FillColor = b.spec.DisabledFill
		}
		if b.spec.DisabledIcon != nil {
			b.icon.Resource = b.spec.DisabledIcon
		}
		b.icon.Translucency = 0.18
		b.label.Color = design.ColorTextMuted
	} else if b.hovered {
		b.bg.FillColor = b.spec.HoverFill
		if b.spec.HoverIcon != nil {
			b.icon.Resource = b.spec.HoverIcon
		}
	}

	if b.text != "" {
		b.icon.Hide()
		b.label.Show()
	} else {
		b.icon.Show()
		b.label.Hide()
	}

	b.bg.Refresh()
	b.border.Refresh()
	b.icon.Refresh()
	b.label.Refresh()
}

func NewFooterIconButton(normalIcon fyne.Resource, hoverIcon fyne.Resource, iconSize fyne.Size, onTapped func()) fyne.CanvasObject {
	return newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  design.ColorSurfaceLight,
		NormalIcon: normalIcon,
		HoverIcon:  hoverIcon,
		IconSize:   iconSize,
		ButtonSize: fyne.NewSize(28, 28),
		OnTapped:   onTapped,
	})
}

type carouselLayout struct {
	edgeInset float32
}

func newCarouselLayout(edgeInset float32) fyne.Layout {
	return &carouselLayout{edgeInset: edgeInset}
}

func (l *carouselLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	image := objects[0]
	prev := objects[1]
	next := objects[2]

	imageMin := image.MinSize()
	imageX := (size.Width - imageMin.Width) / 2
	imageY := (size.Height - imageMin.Height) / 2
	if imageX < 0 {
		imageX = 0
	}
	if imageY < 0 {
		imageY = 0
	}
	image.Move(fyne.NewPos(imageX, imageY))
	image.Resize(imageMin)

	prevMin := prev.MinSize()
	prev.Move(fyne.NewPos(l.edgeInset, (size.Height-prevMin.Height)/2))
	prev.Resize(prevMin)

	nextMin := next.MinSize()
	nextX := size.Width - l.edgeInset - nextMin.Width
	if nextX < 0 {
		nextX = 0
	}
	next.Move(fyne.NewPos(nextX, (size.Height-nextMin.Height)/2))
	next.Resize(nextMin)
}

func (l *carouselLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	imageMin := objects[0].MinSize()
	height := imageMin.Height
	if len(objects) > 1 {
		if prevHeight := objects[1].MinSize().Height; prevHeight > height {
			height = prevHeight
		}
	}
	if len(objects) > 2 {
		if nextHeight := objects[2].MinSize().Height; nextHeight > height {
			height = nextHeight
		}
	}

	return fyne.NewSize(imageMin.Width+l.edgeInset*2, height)
}

type emptyStateLayout struct{}

func newEmptyStateLayout() fyne.Layout {
	return &emptyStateLayout{}
}

func (l *emptyStateLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	title := objects[0]
	carousel := objects[1]
	actions := objects[2]

	contentWidth := clampFloat32(size.Width*0.82, emptyStateMinWidth, emptyStateMaxWidth)
	carouselWidth := contentWidth
	if size.Width <= 720 {
		carouselWidth = clampFloat32(size.Width, emptyStateMinWidth, emptyStateMaxWidth)
	}
	titleWidth := minFloat32(contentWidth, emptyStateTitleMaxWidth)
	titleMin := title.MinSize()
	titleHeight := titleMin.Height

	actionsWidth := minFloat32(contentWidth, onboardingActionMaxPrimaryW+56+onboardingActionGap)
	actionsHeight := onboardingActionsHeight(actions, actionsWidth)

	topInset := clampFloat32(size.Height*0.008, 0, 12)
	titleToCarouselGap := clampFloat32(size.Height*0.035, 22, 40)
	carouselToActionsGap := clampFloat32(size.Height*0.08, 44, 84)
	bottomInset := clampFloat32(size.Height*0.05, 22, 40)

	titleY := topInset
	actionsY := size.Height - bottomInset - actionsHeight
	if actionsY < titleY+titleHeight+titleToCarouselGap {
		actionsY = titleY + titleHeight + titleToCarouselGap
	}

	carouselY := titleY + titleHeight + titleToCarouselGap
	carouselHeight := actionsY - carouselY - carouselToActionsGap
	carouselHeight = clampFloat32(carouselHeight, onboardingCarouselMinHeight, onboardingCarouselMaxHeight)
	if maxCarouselHeight := actionsY - carouselY - carouselToActionsGap; maxCarouselHeight < carouselHeight {
		carouselHeight = maxFloat32(0, maxCarouselHeight)
	}

	title.Move(fyne.NewPos((size.Width-titleWidth)/2, titleY))
	title.Resize(fyne.NewSize(titleWidth, titleHeight))

	carousel.Move(fyne.NewPos((size.Width-carouselWidth)/2, carouselY))
	carousel.Resize(fyne.NewSize(carouselWidth, carouselHeight))

	actions.Move(fyne.NewPos((size.Width-actionsWidth)/2, actionsY))
	actions.Resize(fyne.NewSize(actionsWidth, actionsHeight))
}

func (l *emptyStateLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}

	titleMin := objects[0].MinSize()
	carouselMin := objects[1].MinSize()
	actionsMin := objects[2].MinSize()

	width := maxFloat32(titleMin.Width, maxFloat32(carouselMin.Width, actionsMin.Width))
	height := titleMin.Height + 12 + carouselMin.Height + 16 + actionsMin.Height
	return fyne.NewSize(width, height)
}

type onboardingCarouselLayout struct{}

func newOnboardingCarouselLayout() fyne.Layout {
	return &onboardingCarouselLayout{}
}

func (l *onboardingCarouselLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	caption := objects[0]
	stage := objects[1]
	dots := objects[2]

	captionWidth := minFloat32(size.Width, onboardingCaptionMaxWidth)
	captionHeight := caption.MinSize().Height
	caption.Move(fyne.NewPos((size.Width-captionWidth)/2, 0))
	caption.Resize(fyne.NewSize(captionWidth, captionHeight))

	dotsMin := dots.MinSize()
	dotsY := size.Height - dotsMin.Height
	if dotsY < captionHeight+onboardingCaptionBottomGap {
		dotsY = captionHeight + onboardingCaptionBottomGap
	}
	dots.Move(fyne.NewPos((size.Width-dotsMin.Width)/2, dotsY))
	dots.Resize(dotsMin)

	stageY := captionHeight + onboardingCaptionBottomGap
	stageHeight := dotsY - stageY - onboardingDotsTopSpacing
	stageHeight = maxFloat32(stageHeight, onboardingStageMinHeight)
	if maxStageHeight := size.Height - stageY - dotsMin.Height - onboardingDotsTopSpacing; maxStageHeight < stageHeight {
		stageHeight = maxFloat32(0, maxStageHeight)
	}
	stage.Move(fyne.NewPos(0, stageY))
	stage.Resize(fyne.NewSize(size.Width, stageHeight))
}

func (l *onboardingCarouselLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 3 {
		return fyne.NewSize(0, 0)
	}

	captionMin := objects[0].MinSize()
	stageMin := objects[1].MinSize()
	dotsMin := objects[2].MinSize()
	width := maxFloat32(captionMin.Width, maxFloat32(stageMin.Width, dotsMin.Width))
	height := captionMin.Height + onboardingCaptionBottomGap + stageMin.Height + onboardingDotsTopSpacing + dotsMin.Height
	return fyne.NewSize(maxFloat32(width, onboardingCarouselMinWidth), maxFloat32(height, onboardingCarouselMinHeight))
}

type carouselStageLayout struct {
	aspectRatio float32
}

func newCarouselStageLayout(aspectRatio float32) fyne.Layout {
	return &carouselStageLayout{aspectRatio: aspectRatio}
}

func (l *carouselStageLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 3 {
		return
	}

	image := objects[0]
	prev := objects[1]
	next := objects[2]

	arrowSize := clampFloat32(minFloat32(size.Width*0.07, size.Height*0.2), 24, 36)
	edgeInset := clampFloat32(size.Width*0.02, onboardingArrowEdgeMinInset, onboardingArrowEdgeMaxInset)
	if size.Width <= 720 {
		edgeInset = 0
	}

	prev.Move(fyne.NewPos(edgeInset, (size.Height-arrowSize)/2))
	prev.Resize(fyne.NewSize(arrowSize, arrowSize))

	nextX := size.Width - edgeInset - arrowSize
	if nextX < edgeInset {
		nextX = edgeInset
	}
	next.Move(fyne.NewPos(nextX, (size.Height-arrowSize)/2))
	next.Resize(fyne.NewSize(arrowSize, arrowSize))

	availableWidth := size.Width - (arrowSize+edgeInset)*2 - onboardingArrowGap*2
	availableHeight := size.Height
	if availableWidth < 0 {
		availableWidth = size.Width
	}
	if availableHeight < 0 {
		availableHeight = 0
	}

	imageWidth := availableWidth
	if l.aspectRatio <= 0 {
		l.aspectRatio = onboardingImageAspectRatio
	}
	if imageWidth > onboardingImageMaxWidth {
		imageWidth = onboardingImageMaxWidth
	}
	imageHeight := imageWidth / l.aspectRatio
	if imageHeight > availableHeight || imageHeight > onboardingImageMaxHeight {
		imageHeight = minFloat32(availableHeight, onboardingImageMaxHeight)
		imageWidth = imageHeight * l.aspectRatio
	}
	if imageWidth < 0 {
		imageWidth = 0
	}
	if imageHeight < 0 {
		imageHeight = 0
	}

	image.Move(fyne.NewPos((size.Width-imageWidth)/2, (size.Height-imageHeight)/2))
	image.Resize(fyne.NewSize(imageWidth, imageHeight))
}

func (l *carouselStageLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(onboardingCarouselMinWidth, onboardingStageMinHeight)
}

type onboardingActionsLayout struct {
	gap float32
}

func newOnboardingActionsLayout(gap float32) fyne.Layout {
	return &onboardingActionsLayout{gap: gap}
}

func (l *onboardingActionsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	primary := objects[0]
	secondary := objects[1]
	primaryMin := primary.MinSize()
	secondaryMin := secondary.MinSize()

	actionHeight := clampFloat32(maxFloat32(primaryMin.Height, secondaryMin.Height), 44, 56)
	secondarySize := actionHeight
	availablePrimaryWidth := size.Width - secondarySize - l.gap
	primaryWidth := minFloat32(availablePrimaryWidth, onboardingActionMaxPrimaryW)

	if primaryWidth >= onboardingActionMinPrimaryW && primaryWidth+secondarySize+l.gap <= size.Width {
		rowWidth := primaryWidth + secondarySize + l.gap
		startX := (size.Width - rowWidth) / 2
		primary.Move(fyne.NewPos(startX, maxFloat32(0, (size.Height-actionHeight)/2)))
		primary.Resize(fyne.NewSize(primaryWidth, actionHeight))

		secondary.Move(fyne.NewPos(startX+primaryWidth+l.gap, maxFloat32(0, (size.Height-actionHeight)/2)))
		secondary.Resize(fyne.NewSize(secondarySize, secondarySize))
		return
	}

	stackWidth := minFloat32(size.Width, maxFloat32(onboardingActionMinPrimaryW, onboardingActionStackMinWidth))
	totalHeight := actionHeight + l.gap + secondarySize
	startY := maxFloat32(0, (size.Height-totalHeight)/2)

	primary.Move(fyne.NewPos((size.Width-stackWidth)/2, startY))
	primary.Resize(fyne.NewSize(stackWidth, actionHeight))

	secondary.Move(fyne.NewPos((size.Width-secondarySize)/2, startY+actionHeight+l.gap))
	secondary.Resize(fyne.NewSize(secondarySize, secondarySize))
}

func (l *onboardingActionsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	primaryMin := objects[0].MinSize()
	secondaryMin := objects[1].MinSize()
	actionHeight := clampFloat32(maxFloat32(primaryMin.Height, secondaryMin.Height), 44, 56)
	secondarySize := actionHeight
	width := onboardingActionMaxPrimaryW + secondarySize + l.gap
	height := actionHeight
	return fyne.NewSize(width, height)
}

type centerOffsetLayout struct {
	offsetY float32
}

func newCenterOffsetLayout(offsetY float32) fyne.Layout {
	return &centerOffsetLayout{offsetY: offsetY}
}

func (l *centerOffsetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	child := objects[0]
	minSize := child.MinSize()
	y := (size.Height-minSize.Height)/2 + l.offsetY
	if y < 0 {
		y = 0
	}

	child.Move(fyne.NewPos(0, y))
	child.Resize(fyne.NewSize(size.Width, minSize.Height))
}

func (l *centerOffsetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	return objects[0].MinSize()
}

func minFloat32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func maxFloat32(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func clampFloat32(value, minValue, maxValue float32) float32 {
	if maxValue < minValue {
		maxValue = minValue
	}
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}

func onboardingActionsHeight(actions fyne.CanvasObject, width float32) float32 {
	container, ok := actions.(*fyne.Container)
	if !ok || len(container.Objects) < 2 {
		return actions.MinSize().Height
	}

	primaryMin := container.Objects[0].MinSize()
	secondaryMin := container.Objects[1].MinSize()
	actionHeight := clampFloat32(maxFloat32(primaryMin.Height, secondaryMin.Height), 44, 56)
	secondarySize := actionHeight
	if width >= onboardingActionMinPrimaryW+secondarySize+onboardingActionGap {
		return actionHeight
	}

	return actionHeight + onboardingActionGap + secondarySize
}

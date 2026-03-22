package view

import (
	"image/color"

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
	headerArea  *fyne.Container
	topActions  *fyne.Container
}

type ConnectionRowData struct {
	Name          string
	Host          string
	ProtocolBadge string
	EditLabel     string
}

type ConnectionRowActions struct {
	OnSelect       func()
	OnUse          func()
	OnEdit         func()
	OnDelete       func()
	OnProtocolMenu func(*widget.Button)
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
	onboardingActionGap           float32 = 12
	onboardingActionMinPrimaryW   float32 = 150
	onboardingActionMaxPrimaryW   float32 = 190
	onboardingActionStackMinWidth float32 = 160
)

var (
	onboardingIndicatorInactive = color.NRGBA{R: 0x35, G: 0x35, B: 0x35, A: 0xff}
	onboardingIndicatorActive   = color.NRGBA{R: 0x65, G: 0x65, B: 0x65, A: 0xff}
)

func NewConnectionManagerUI(onQR func(), onAdd func()) *ConnectionManagerUI {
	topQRBtn := widget.NewButtonWithIcon(i18n.Current.QRScannerButton, theme.SearchIcon(), onQR)
	topQRBtn.Importance = widget.MediumImportance

	topAddBtn := widget.NewButtonWithIcon(i18n.Current.AddConnectionTitle, theme.ContentAddIcon(), onAdd)
	topAddBtn.Importance = widget.HighImportance

	centerQRBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill:  color.Transparent,
		HoverFill:   design.ColorAccentHover,
		Stroke:      design.ColorAccentHover,
		StrokeWidth: 1,
		NormalIcon:  assets.QRCodeAccent,
		HoverIcon:   assets.QRCodeBoldBlack,
		IconSize:    fyne.NewSize(24, 24),
		OnTapped:    onQR,
	})

	centerAddBtn := newOnboardingPrimaryButton("+  "+i18n.Current.AddConnectionTitle, onAdd)

	connectionsBox := container.NewVBox()
	connectionsScroll := container.NewScroll(connectionsBox)
	connectionsScroll.SetMinSize(fyne.NewSize(0, 420))

	topActions := container.NewHBox(topQRBtn, centerSpacer(10), topAddBtn)
	headerArea := container.NewMax()
	contentArea := container.NewMax()

	mainContent := container.NewBorder(
		NewInset(headerArea, 16, 8, 8, 4),
		nil,
		nil,
		nil,
		NewInset(contentArea, 16, 16, 0, 16),
	)

	bg := canvas.NewRectangle(design.ColorGray950)
	root := container.NewStack(bg, mainContent)

	ui := &ConnectionManagerUI{
		Container:         root,
		ConnectionsScroll: connectionsScroll,
		ConnectionsBox:    connectionsBox,
		QRBtn:             centerQRBtn,
		AddBtn:            centerAddBtn,
		contentArea:       contentArea,
		headerArea:        headerArea,
		topActions:        topActions,
	}

	ui.setHeader("", nil)

	return ui
}

func (ui *ConnectionManagerUI) SetEmptyState() {
	ui.ConnectionsBox.RemoveAll()
	ui.setHeader("", nil)

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
		emptyBlock,
	}
	ui.contentArea.Refresh()
}

func (ui *ConnectionManagerUI) SetRows(rows []*fyne.Container) {
	ui.ConnectionsBox.RemoveAll()
	for _, row := range rows {
		ui.ConnectionsBox.Add(row)
	}
	ui.setHeader(i18n.Current.SavedConnections, ui.topActions)
	ui.contentArea.Objects = []fyne.CanvasObject{ui.ConnectionsScroll}
	ui.ConnectionsBox.Refresh()
	ui.contentArea.Refresh()
}

func (ui *ConnectionManagerUI) setHeader(title string, right fyne.CanvasObject) {
	var left fyne.CanvasObject
	if title != "" {
		left = NewBrandText(title, 20, design.ColorTextLight, true)
	}
	ui.headerArea.Objects = []fyne.CanvasObject{
		container.NewBorder(nil, nil, left, right, nil),
	}
	ui.headerArea.Refresh()
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
)

func NewConnectionRow(data ConnectionRowData, actions ConnectionRowActions) *fyne.Container {
	nameText := canvas.NewText(data.Name, design.ColorTextLight)
	nameText.TextSize = theme.TextSubHeadingSize() + 2
	nameText.TextStyle.Bold = true

	nameSelectBtn := widget.NewButton("", actions.OnSelect)
	nameSelectBtn.Importance = widget.LowImportance
	nameRow := container.NewStack(nameText, container.NewMax(nameSelectBtn))

	hostLabel := canvas.NewText(data.Host, design.ColorTextMuted)
	hostLabel.TextSize = 14

	hostSelectBtn := widget.NewButton("", actions.OnSelect)
	hostSelectBtn.Importance = widget.LowImportance
	hostLabelWithClick := container.NewStack(container.NewMax(hostLabel), container.NewMax(hostSelectBtn))

	protocolBtn := widget.NewButton(data.ProtocolBadge, nil)
	protocolBtn.Importance = widget.MediumImportance
	protocolBtn.OnTapped = func() {
		actions.OnProtocolMenu(protocolBtn)
	}

	useBtn := widget.NewButtonWithIcon(i18n.Current.ConnectButton, theme.LoginIcon(), actions.OnUse)
	useBtn.Importance = widget.HighImportance

	editBtn := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), actions.OnEdit)
	editBtn.Importance = widget.LowImportance

	deleteBtn := widget.NewButtonWithIcon("", theme.DeleteIcon(), actions.OnDelete)
	deleteBtn.Importance = widget.LowImportance

	centerSelectBtn := widget.NewButton("", actions.OnSelect)
	centerSelectBtn.Importance = widget.LowImportance
	centerArea := container.NewStack(layout.NewSpacer(), container.NewMax(centerSelectBtn))

	topRow := container.NewBorder(nil, nil, nameRow, protocolBtn, nil)
	bottomRow := container.NewBorder(nil, nil,
		hostLabelWithClick,
		container.NewHBox(useBtn, editBtn, deleteBtn),
		centerArea,
	)

	accentLine := canvas.NewRectangle(design.ColorAlphaAccent22)
	accentLine.SetMinSize(fyne.NewSize(0, 3))

	card := NewCompactSurfacePanel(
		container.NewBorder(
			accentLine,
			nil,
			nil,
			nil,
			container.NewVBox(topRow, bottomRow),
		),
		design.ColorSurface,
		design.RadiusMD,
	)

	return container.NewPadded(card)
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
	b.label.TextSize = 15
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.label)))
}

func (b *onboardingPrimaryButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.labelText, design.ColorBackground)
	measure.TextSize = 15
	measure.TextStyle.Bold = true
	labelSize := measure.MinSize()
	return fyne.NewSize(labelSize.Width+24, 46)
}

func (b *onboardingPrimaryButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *onboardingPrimaryButton) TappedSecondary(*fyne.PointEvent) {}

func (b *onboardingPrimaryButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *onboardingPrimaryButton) MouseMoved(*desktop.MouseEvent) {}

func (b *onboardingPrimaryButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *onboardingPrimaryButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}

	if b.hovered {
		b.bg.FillColor = design.ColorAccentHover
	} else {
		b.bg.FillColor = design.ColorAccent
	}
	b.bg.Refresh()
	b.label.Refresh()
}

type iconChromeButtonSpec struct {
	NormalFill  color.Color
	HoverFill   color.Color
	Stroke      color.Color
	StrokeWidth float32
	NormalIcon  fyne.Resource
	HoverIcon   fyne.Resource
	IconSize    fyne.Size
	ButtonSize  fyne.Size
	OnTapped    func()
}

type iconChromeButton struct {
	widget.BaseWidget

	spec    iconChromeButtonSpec
	hovered bool
	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	icon    *canvas.Image
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

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.icon), b.border))
}

func (b *iconChromeButton) MinSize() fyne.Size {
	if b.spec.ButtonSize.Width > 0 && b.spec.ButtonSize.Height > 0 {
		return b.spec.ButtonSize
	}
	return fyne.NewSize(48, 48)
}

func (b *iconChromeButton) Tapped(*fyne.PointEvent) {
	if b.spec.OnTapped != nil {
		b.spec.OnTapped()
	}
}

func (b *iconChromeButton) TappedSecondary(*fyne.PointEvent) {}

func (b *iconChromeButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *iconChromeButton) MouseMoved(*desktop.MouseEvent) {}

func (b *iconChromeButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *iconChromeButton) refreshVisuals() {
	if b.bg == nil || b.border == nil || b.icon == nil {
		return
	}

	b.bg.FillColor = b.spec.NormalFill
	b.icon.Resource = b.spec.NormalIcon
	if b.hovered {
		b.bg.FillColor = b.spec.HoverFill
		if b.spec.HoverIcon != nil {
			b.icon.Resource = b.spec.HoverIcon
		}
	}

	b.bg.Refresh()
	b.border.Refresh()
	b.icon.Refresh()
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

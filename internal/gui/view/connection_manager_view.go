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
	helpBtn     fyne.CanvasObject
	langBtn     fyne.CanvasObject
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

func NewConnectionManagerUI(onLanguageMenu func(fyne.CanvasObject), onHelp func(), onQR func(), onAdd func()) *ConnectionManagerUI {
	var langBtn fyne.CanvasObject
	langBtn = newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  color.Transparent,
		NormalIcon: assets.LanguageIconMuted,
		HoverIcon:  assets.LanguageIcon,
		IconSize:   fyne.NewSize(22, 22),
		OnTapped: func() {
			onLanguageMenu(langBtn)
		},
	})

	helpBtn := newIconChromeButton(iconChromeButtonSpec{
		NormalFill: color.Transparent,
		HoverFill:  color.Transparent,
		NormalIcon: assets.QuestionIconMuted,
		HoverIcon:  assets.QuestionIcon,
		IconSize:   fyne.NewSize(18, 18),
		OnTapped:   onHelp,
	})

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

	centerAddBtn := widget.NewButtonWithIcon(i18n.Current.AddConnectionTitle, theme.ContentAddIcon(), onAdd)
	centerAddBtn.Importance = widget.HighImportance

	connectionsBox := container.NewVBox()
	connectionsScroll := container.NewScroll(connectionsBox)
	connectionsScroll.SetMinSize(fyne.NewSize(0, 420))

	topActions := container.NewHBox(topQRBtn, centerSpacer(10), topAddBtn)
	headerArea := container.NewMax()
	contentArea := container.NewMax()

	mainContent := container.NewVBox(
		NewInset(headerArea, 16, 8, 8, 4),
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
		helpBtn:           helpBtn,
		langBtn:           langBtn,
		topActions:        topActions,
	}

	ui.setHeader("", ui.headerTools())

	return ui
}

func (ui *ConnectionManagerUI) SetEmptyState() {
	ui.ConnectionsBox.RemoveAll()
	ui.setHeader("", ui.headerTools())

	slides := []onboardingSlide{
		{Image: assets.OnboardingStep01, Text: i18n.Current.OnboardingStepConnect},
		{Image: assets.OnboardingStep02, Text: i18n.Current.OnboardingStepIP},
		{Image: assets.OnboardingStep03, Text: i18n.Current.OnboardingStepScan},
	}

	actions := container.NewHBox(
		container.NewGridWrap(fyne.NewSize(208, 48), ui.AddBtn),
		centerSpacer(12),
		container.NewGridWrap(fyne.NewSize(48, 48), ui.QRBtn),
	)
	emptyBlock := container.NewVBox(
		container.NewCenter(NewBrandText(i18n.Current.AddNewDeviceTitle, 26, design.ColorTextLight, true)),
		NewInset(newOnboardingCarousel(slides), 0, 0, 8, 8),
		NewInset(container.NewCenter(actions), 0, 0, 12, 0),
	)

	ui.contentArea.Objects = []fyne.CanvasObject{
		container.New(newCenterOffsetLayout(-42), emptyBlock),
	}
	ui.contentArea.Refresh()
}

func (ui *ConnectionManagerUI) SetRows(rows []*fyne.Container) {
	ui.ConnectionsBox.RemoveAll()
	for _, row := range rows {
		ui.ConnectionsBox.Add(row)
	}
	ui.setHeader(i18n.Current.SavedConnections, container.NewHBox(ui.topActions, centerSpacer(10), ui.headerTools()))
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

func (ui *ConnectionManagerUI) headerTools() fyne.CanvasObject {
	return container.NewHBox(ui.helpBtn, centerSpacer(4), ui.langBtn)
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
	image.SetMinSize(fyne.NewSize(520, 248))

	caption := widget.NewLabel(slides[currentSlide].Text)
	caption.Alignment = fyne.TextAlignCenter
	caption.Wrapping = fyne.TextWrapWord

	dots := make([]*canvas.Circle, len(slides))
	dotItems := make([]fyne.CanvasObject, 0, len(slides)*2)
	for idx := range slides {
		dot := canvas.NewCircle(color.NRGBA{R: 0x76, G: 0x76, B: 0x76, A: 0xff})
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
		caption.SetText(slides[currentSlide].Text)

		for idx, dot := range dots {
			if idx == currentSlide {
				dot.FillColor = color.NRGBA{R: 0x9b, G: 0x9b, B: 0x9b, A: 0xff}
			} else {
				dot.FillColor = color.NRGBA{R: 0x76, G: 0x76, B: 0x76, A: 0xff}
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

	return container.NewVBox(
		NewInset(container.NewCenter(container.NewGridWrap(fyne.NewSize(420, 40), caption)), 0, 0, 0, 4),
		container.New(newCarouselLayout(28), container.NewGridWrap(fyne.NewSize(560, 280), image), prevBtn, nextBtn),
		NewInset(container.NewCenter(container.NewHBox(dotItems...)), 0, 0, 14, 0),
	)
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
	b.refreshIcon()
}

func (b *arrowButton) refreshIcon() {
	resource := b.normalIcon
	if !b.disabled && b.hovered {
		resource = b.hoveredIcon
	}
	b.icon.Resource = resource
	b.icon.Refresh()
}

var (
	_ fyne.Tappable     = (*arrowButton)(nil)
	_ desktop.Hoverable = (*arrowButton)(nil)
	_ fyne.Widget       = (*arrowButton)(nil)
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

type iconChromeButtonSpec struct {
	NormalFill  color.Color
	HoverFill   color.Color
	Stroke      color.Color
	StrokeWidth float32
	NormalIcon  fyne.Resource
	HoverIcon   fyne.Resource
	IconSize    fyne.Size
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

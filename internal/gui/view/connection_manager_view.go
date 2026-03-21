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
	QRBtn             *widget.Button
	AddBtn            *widget.Button

	contentArea *fyne.Container
	headerArea  *fyne.Container
	langBtn     *widget.Button
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

func NewConnectionManagerUI(onLanguageMenu func(*widget.Button), onQR func(), onAdd func()) *ConnectionManagerUI {
	langBtn := widget.NewButton("🌐", nil)
	langBtn.Importance = widget.LowImportance
	langBtn.OnTapped = func() {
		onLanguageMenu(langBtn)
	}

	topQRBtn := widget.NewButtonWithIcon(i18n.Current.QRScannerButton, theme.SearchIcon(), onQR)
	topQRBtn.Importance = widget.MediumImportance

	topAddBtn := widget.NewButtonWithIcon(i18n.Current.AddConnectionTitle, theme.ContentAddIcon(), onAdd)
	topAddBtn.Importance = widget.HighImportance

	centerQRBtn := widget.NewButtonWithIcon(i18n.Current.QRScannerButton, theme.SearchIcon(), onQR)
	centerQRBtn.Importance = widget.MediumImportance

	centerAddBtn := widget.NewButtonWithIcon(i18n.Current.AddConnectionTitle, theme.ContentAddIcon(), onAdd)
	centerAddBtn.Importance = widget.HighImportance

	connectionsBox := container.NewVBox()
	connectionsScroll := container.NewScroll(connectionsBox)
	connectionsScroll.SetMinSize(fyne.NewSize(0, 420))

	topActions := container.NewHBox(topQRBtn, centerSpacer(10), topAddBtn)
	headerArea := container.NewMax()

	contentArea := container.NewMax()

	mainContent := container.NewVBox(
		NewInset(headerArea, 16, 16, 0, 8),
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
		langBtn:           langBtn,
		topActions:        topActions,
	}

	ui.setHeader("", langBtn)

	return ui
}

func (ui *ConnectionManagerUI) SetEmptyState() {
	ui.ConnectionsBox.RemoveAll()
	ui.setHeader("", ui.langBtn)

	slides := []onboardingSlide{
		{Image: assets.OnboardingStep01, Text: i18n.Current.OnboardingStepConnect},
		{Image: assets.OnboardingStep02, Text: i18n.Current.OnboardingStepIP},
		{Image: assets.OnboardingStep03, Text: i18n.Current.OnboardingStepScan},
	}

	emptyTitle := NewBrandText(i18n.Current.AddNewDeviceTitle, 26, design.ColorTextLight, true)
	actions := container.NewHBox(ui.AddBtn, centerSpacer(12), ui.QRBtn)
	emptyBlock := container.NewVBox(
		container.NewCenter(emptyTitle),
		NewInset(newOnboardingCarousel(slides), 0, 0, 20, 8),
		NewInset(container.NewCenter(actions), 0, 0, 18, 0),
	)

	ui.contentArea.Objects = []fyne.CanvasObject{
		container.NewVBox(
			layout.NewSpacer(),
			container.NewCenter(emptyBlock),
			layout.NewSpacer(),
		),
	}
	ui.contentArea.Refresh()
}

func (ui *ConnectionManagerUI) SetRows(rows []*fyne.Container) {
	ui.ConnectionsBox.RemoveAll()
	for _, row := range rows {
		ui.ConnectionsBox.Add(row)
	}
	ui.setHeader(i18n.Current.SavedConnections, container.NewHBox(ui.topActions, centerSpacer(10), ui.langBtn))
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
	image.SetMinSize(fyne.NewSize(520, 248))

	caption := widget.NewLabel(slides[currentSlide].Text)
	caption.Alignment = fyne.TextAlignCenter
	caption.Wrapping = fyne.TextWrapWord

	dots := make([]*canvas.Circle, len(slides))
	dotItems := make([]fyne.CanvasObject, 0, len(slides)*2)
	for idx := range slides {
		dot := canvas.NewCircle(design.ColorBorder)
		dots[idx] = dot
		dotItems = append(dotItems, container.NewGridWrap(fyne.NewSize(10, 10), dot))
		if idx < len(slides)-1 {
			dotItems = append(dotItems, centerSpacer(8))
		}
	}

	prevBtn := newArrowButton("‹", nil)
	nextBtn := newArrowButton("›", nil)

	updateSlide := func() {
		image.Resource = slides[currentSlide].Image
		image.Refresh()
		caption.SetText(slides[currentSlide].Text)

		for idx, dot := range dots {
			if idx == currentSlide {
				dot.FillColor = design.ColorAccent
			} else {
				dot.FillColor = design.ColorBorder
			}
			dot.Refresh()
		}

		if currentSlide == 0 {
			prevBtn.SetDisabled(true)
		} else {
			prevBtn.SetDisabled(false)
		}
		if currentSlide == len(slides)-1 {
			nextBtn.SetDisabled(true)
		} else {
			nextBtn.SetDisabled(false)
		}
	}

	prevBtn.onTapped = func() {
		if currentSlide == 0 {
			return
		}
		currentSlide--
		updateSlide()
	}

	nextBtn.onTapped = func() {
		if currentSlide >= len(slides)-1 {
			return
		}
		currentSlide++
		updateSlide()
	}

	updateSlide()

	return container.NewVBox(
		NewInset(container.NewCenter(container.NewGridWrap(fyne.NewSize(420, 40), caption)), 0, 0, 0, 12),
		container.NewBorder(
			nil,
			nil,
			container.NewCenter(container.NewGridWrap(fyne.NewSize(40, 40), prevBtn)),
			container.NewCenter(container.NewGridWrap(fyne.NewSize(40, 40), nextBtn)),
			container.NewCenter(container.NewGridWrap(fyne.NewSize(560, 280), image)),
		),
		NewInset(container.NewCenter(container.NewHBox(dotItems...)), 0, 0, 14, 0),
	)
}

type arrowButton struct {
	widget.BaseWidget

	symbol   string
	onTapped func()
	hovered  bool
	disabled bool
	label    *canvas.Text
}

func newArrowButton(symbol string, onTapped func()) *arrowButton {
	btn := &arrowButton{
		symbol:   symbol,
		onTapped: onTapped,
		label:    canvas.NewText(symbol, design.ColorTextLight),
	}
	btn.label.TextSize = 30
	btn.label.TextStyle.Bold = true
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *arrowButton) CreateRenderer() fyne.WidgetRenderer {
	hitArea := canvas.NewRectangle(color.Transparent)
	return widget.NewSimpleRenderer(container.NewMax(
		hitArea,
		container.NewCenter(b.label),
	))
}

func (b *arrowButton) MinSize() fyne.Size {
	return fyne.NewSize(40, 40)
}

func (b *arrowButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshColor()
}

func (b *arrowButton) MouseMoved(*desktop.MouseEvent) {}

func (b *arrowButton) MouseOut() {
	b.hovered = false
	b.refreshColor()
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
	b.refreshColor()
}

func (b *arrowButton) refreshColor() {
	switch {
	case b.disabled:
		b.label.Color = design.ColorBorder
	case b.hovered:
		b.label.Color = design.ColorAccent
	default:
		b.label.Color = design.ColorTextLight
	}
	b.label.Refresh()
}

var (
	_ fyne.Tappable     = (*arrowButton)(nil)
	_ desktop.Hoverable = (*arrowButton)(nil)
	_ fyne.Widget       = (*arrowButton)(nil)
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

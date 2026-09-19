package ui

import (
	"image/color"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// overlayPopupSpec is the client's ShowOverlayPopup shape: a full-window
// dim rectangle plus a centered card, so the backdrop covers the canvas
// (including after a window resize) instead of only the InnerPadding ring
// around widget.NewModalPopUp's content.
type overlayPopupSpec struct {
	Panel        fyne.CanvasObject
	DimColor     color.Color
	PanelSize    func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	PanelPos     func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position
	OnOutsideTap func()
}

type overlayPopupLayout struct {
	panelSize func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size
	panelPos  func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position
}

func (l *overlayPopupLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	dim := objects[0]
	panel := objects[1]

	// widget.PopUp insets its content by theme.InnerPadding() on each side.
	// Stretch the dim past that inset so the popup widget's own background
	// never shows as a bright ring around the card.
	innerPad := theme.InnerPadding()
	dim.Move(fyne.NewPos(-innerPad/2, -innerPad/2))
	dim.Resize(fyne.NewSize(size.Width+innerPad, size.Height+innerPad))

	panelSize := overlayDefaultPanelSize(size, panel)
	if l.panelSize != nil {
		panelSize = l.panelSize(size, panel)
	}
	if panelSize.Width > size.Width {
		panelSize.Width = size.Width
	}
	if panelSize.Height > size.Height {
		panelSize.Height = size.Height
	}
	if panelSize.Width < 0 {
		panelSize.Width = 0
	}
	if panelSize.Height < 0 {
		panelSize.Height = 0
	}
	pos := fyne.NewPos((size.Width-panelSize.Width)/2, (size.Height-panelSize.Height)/2)
	if l.panelPos != nil {
		pos = l.panelPos(size, panelSize)
	}
	if pos.X < 0 {
		pos.X = 0
	}
	if pos.Y < 0 {
		pos.Y = 0
	}
	if maxX := size.Width - panelSize.Width; pos.X > maxX {
		pos.X = maxX
	}
	if maxY := size.Height - panelSize.Height; pos.Y > maxY {
		pos.Y = maxY
	}
	panel.Move(pos)
	panel.Resize(panelSize)
}

func (l *overlayPopupLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

func overlayDefaultPanelSize(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
	if panel == nil {
		return fyne.NewSize(0, 0)
	}
	panelMin := panel.MinSize()
	if panelMin.Width > canvasSize.Width {
		panelMin.Width = canvasSize.Width
	}
	if panelMin.Height > canvasSize.Height {
		panelMin.Height = canvasSize.Height
	}
	return panelMin
}

func showOverlayPopup(parent fyne.Window, spec overlayPopupSpec) *widget.PopUp {
	if parent == nil || spec.Panel == nil {
		return nil
	}
	dimColor := spec.DimColor
	if dimColor == nil {
		dimColor = design.ColorOverlayDim
	}
	dim := canvas.NewRectangle(dimColor)
	themedPanel := container.NewThemeOverride(spec.Panel, design.NewBrandTheme())
	// Swallow taps so widget.NewPopUp doesn't dismiss itself (it is not
	// modal). The dim fills the window, so there is no true "outside".
	content := container.New(&overlayPopupLayout{panelSize: spec.PanelSize, panelPos: spec.PanelPos},
		newOverlayTapCatcher(dim, spec.OnOutsideTap),
		newOverlayTapCatcher(themedPanel, nil),
	)
	popup := widget.NewPopUp(content, parent.Canvas())
	popup.Move(fyne.NewPos(0, 0))
	popup.Resize(parent.Canvas().Size())
	beginOverlay()
	watchOverlayPopup(parent, popup)
	popup.Show()
	return popup
}

func watchOverlayPopup(parent fyne.Window, popup *widget.PopUp) {
	if parent == nil || popup == nil {
		return
	}
	go func() {
		var lastSize fyne.Size
		syncDone := make(chan struct{})
		fyne.Do(func() {
			if parent.Canvas() != nil {
				lastSize = parent.Canvas().Size()
			}
			close(syncDone)
		})
		<-syncDone

		wasShown := false
		for {
			var currentVisible bool
			var currentSize fyne.Size
			var hasCanvas bool

			syncDone = make(chan struct{})
			fyne.Do(func() {
				if popup != nil {
					currentVisible = popup.Visible()
				}
				if parent != nil && parent.Canvas() != nil {
					currentSize = parent.Canvas().Size()
					hasCanvas = true
				}
				close(syncDone)
			})
			<-syncDone

			if currentVisible {
				wasShown = true
			} else if wasShown {
				fyne.Do(endOverlay)
				return
			}

			if hasCanvas && currentSize != lastSize {
				lastSize = currentSize
				fyne.Do(func() {
					if popup == nil || !popup.Visible() {
						return
					}
					popup.Move(fyne.NewPos(0, 0))
					popup.Resize(currentSize)
				})
			}
			time.Sleep(120 * time.Millisecond)
		}
	}()
}

// overlayTapCatcher is a full-size tappable wrap so a dimmed overlay can
// consume clicks (and optionally run onTap) instead of letting widget.PopUp
// dismiss itself.
type overlayTapCatcher struct {
	widget.BaseWidget
	inner fyne.CanvasObject
	onTap func()
}

func newOverlayTapCatcher(inner fyne.CanvasObject, onTap func()) *overlayTapCatcher {
	c := &overlayTapCatcher{inner: inner, onTap: onTap}
	c.ExtendBaseWidget(c)
	return c
}

func (c *overlayTapCatcher) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.inner)
}

func (c *overlayTapCatcher) Tapped(*fyne.PointEvent) {
	if c.onTap != nil {
		c.onTap()
	}
}

func (c *overlayTapCatcher) TappedSecondary(*fyne.PointEvent) {}

func (c *overlayTapCatcher) MouseIn(*desktop.MouseEvent) {
	clearCardHovers()
}

func (c *overlayTapCatcher) MouseMoved(*desktop.MouseEvent) {}

func (c *overlayTapCatcher) MouseOut() {}

func (c *overlayTapCatcher) Cursor() desktop.Cursor { return desktop.DefaultCursor }

var (
	_ fyne.Tappable     = (*overlayTapCatcher)(nil)
	_ desktop.Hoverable = (*overlayTapCatcher)(nil)
)

// newBrandedDialogPanel is the Token/Account card chrome: accent hairline,
// title + corner X, full-bleed header/footer separators, Gray900 fill.
// Footer is optional; pass nil to skip the bottom bar.
func newBrandedDialogPanel(title string, width float32, body, footer fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	return newBrandedDialogPanelInsets(title, width, 24, 12, body, footer, onClose)
}

func newBrandedDialogPanelInsets(title string, width, padX, bodyPadT float32, body, footer fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	return newBrandedDialogPanelChrome(title, "", width, padX, bodyPadT, body, footer, onClose)
}

func newBrandedDialogPanelChrome(title, subtitle string, width, padX, bodyPadT float32, body, footer fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	return newBrandedDialogPanelChromeExtra(title, subtitle, nil, width, padX, bodyPadT, 10, 16, body, footer, onClose)
}

func newBrandedDialogPanelChromeExtra(title, subtitle string, titleExtra fyne.CanvasObject, width, padX, bodyPadT, footerPadT, footerPadB float32, body, footer fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	titleText := canvas.NewText(title, design.ColorTextLight)
	titleText.TextSize = 13
	titleText.TextStyle.Bold = true

	var titleRow fyne.CanvasObject = titleText
	if titleExtra != nil {
		titleRow = container.New(&tightHBoxLayout{gap: 8}, titleText, titleExtra)
	}

	var headerInner fyne.CanvasObject = titleRow
	headerBandH := float32(45)
	headerPadT, headerPadB := float32(12), float32(17)
	if strings.TrimSpace(subtitle) != "" {
		sub := canvas.NewText(subtitle, design.ColorMutedOlive)
		sub.TextSize = 8
		headerInner = container.New(&tightVBoxLayout{gap: 4}, titleRow, sub)
		headerBandH = 61
		headerPadT, headerPadB = 10, 17
	}

	headerSep := canvas.NewRectangle(design.ColorDialogSep)
	headerSep.SetMinSize(fyne.NewSize(0, 1))
	headerBand := canvas.NewRectangle(color.Transparent)
	headerBand.SetMinSize(fyne.NewSize(0, headerBandH))
	header := container.New(&tightVBoxLayout{gap: 0},
		newDialogTopAccentBar(),
		container.NewStack(headerBand, newExactInset(headerInner, 21, 44, headerPadT, headerPadB)),
		headerSep,
	)

	widthLock := canvas.NewRectangle(color.Transparent)
	widthLock.SetMinSize(fyne.NewSize(width, 1))

	bodyPadB := float32(16)
	if footer != nil {
		bodyPadB = 8
	}
	center := container.NewBorder(
		widthLock, nil, nil, nil,
		newExactInset(body, padX, padX, bodyPadT, bodyPadB),
	)

	var footerBlock fyne.CanvasObject
	if footer != nil {
		footerSep := canvas.NewRectangle(design.ColorDialogSep)
		footerSep.SetMinSize(fyne.NewSize(0, 1))
		footerBlock = container.NewVBox(
			footerSep,
			newExactInset(footer, padX, padX, footerPadT, footerPadB),
		)
	}

	inner := container.NewBorder(header, footerBlock, nil, nil, center)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	closeBtn := newAccountDialogIconButton(accountDialogCloseIcon, onClose)
	return container.NewStack(
		bg,
		inner,
		container.New(&accountDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn),
		border,
	)
}

func wrapDialogField(inner fyne.CanvasObject) fyne.CanvasObject {
	return wrapDialogFieldPad(inner, 8, 8, 4, 4)
}

func wrapDialogFieldCompact(inner fyne.CanvasObject) fyne.CanvasObject {
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(0, dialogDropdownHeight))
	themed := container.NewThemeOverride(inner, &dialogFieldTheme{Theme: design.NewBrandTheme()})
	return container.NewStack(lock, themed)
}

func wrapDialogFieldPad(inner fyne.CanvasObject, l, r, t, b float32) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	themed := container.NewThemeOverride(inner, &dialogFieldTheme{Theme: design.NewBrandTheme()})
	return container.NewStack(bg, newExactInset(themed, l, r, t, b))
}

func wrapDialogValueBox(inner fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	return container.NewStack(bg, newExactInset(inner, 8, 8, 4, 4))
}

func dialogFieldCaption(text string) *canvas.Text {
	t := canvas.NewText(text, design.ColorMutedOlive)
	t.TextSize = 10
	return t
}

func dialogNoteText(text string) *canvas.Text {
	t := canvas.NewText(text, design.ColorMutedOlive)
	t.TextSize = 10
	return t
}

func dialogErrorText() *canvas.Text {
	t := canvas.NewText("", design.ColorAlert)
	t.TextSize = 11
	t.Alignment = fyne.TextAlignCenter
	t.Hide()
	return t
}

func setDialogError(t *canvas.Text, msg string) {
	if t == nil {
		return
	}
	t.Text = msg
	if msg == "" {
		t.Hide()
	} else {
		t.Show()
	}
	t.Refresh()
}

func newDialogCTA(label string, tapped func()) *iconActionButton {
	btn := newIconActionButton(label, nil, tapped)
	btn.CTA = true
	btn.Compact = true
	return btn
}

type dialogFieldTheme struct {
	fyne.Theme
}

func (t *dialogFieldTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground, theme.ColorNameDisabledButton:
		return design.ColorGray950
	case theme.ColorNameInputBorder:
		return design.ColorTailscaleChipBorder
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return design.ColorTeal
	case theme.ColorNamePlaceHolder:
		return design.ColorEmptyHint
	case theme.ColorNameForeground:
		return design.ColorTextLight
	case theme.ColorNameSelection:
		return design.ColorAccentSoft
	}
	return t.Theme.Color(name, v)
}

func (t *dialogFieldTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 0
	case theme.SizeNameInnerPadding:
		return 5
	case theme.SizeNameInputBorder:
		// Fyne sizes the caret to this; 0 made the cursor invisible.
		return 1
	case theme.SizeNameInputRadius:
		return 6
	case theme.SizeNameText, theme.SizeNameCaptionText:
		return 10
	}
	return t.Theme.Size(name)
}

func newDialogFormRow(label string, labelW float32, field fyne.CanvasObject) fyne.CanvasObject {
	return container.New(&dialogFormRowLayout{gap: 10, labelW: labelW}, dialogFieldCaption(label), field)
}

func dialogFormLabelWidth(labels ...string) float32 {
	var w float32
	for _, s := range labels {
		m := fyne.MeasureText(s, 10, fyne.TextStyle{})
		if m.Width > w {
			w = m.Width
		}
	}
	return w
}

type dialogFormRowLayout struct {
	gap, labelW float32
}

func (l *dialogFormRowLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	label := objects[0]
	field := objects[1]
	lw := l.labelW
	if lw <= 0 {
		lw = label.MinSize().Width
	}
	lh := label.MinSize().Height
	label.Move(fyne.NewPos(0, (size.Height-lh)/2))
	label.Resize(fyne.NewSize(lw, lh))
	fw := size.Width - lw - l.gap
	if fw < 0 {
		fw = 0
	}
	fh := field.MinSize().Height
	if fh > size.Height {
		fh = size.Height
	}
	field.Move(fyne.NewPos(lw+l.gap, (size.Height-fh)/2))
	field.Resize(fyne.NewSize(fw, fh))
}

func (l *dialogFormRowLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	lm := objects[0].MinSize()
	fm := objects[1].MinSize()
	h := lm.Height
	if fm.Height > h {
		h = fm.Height
	}
	lw := l.labelW
	if lw <= 0 {
		lw = lm.Width
	}
	return fyne.NewSize(lw+l.gap+fm.Width, h)
}

package controller

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"sync"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/driver/mobile"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"
)

const (
	pcpanelLedPollInterval = 5 * time.Second
	addressBarButtonSize   = 36 // Square buttons: width = height = line height
)

var (
	pcpanelIndicatorIdle  = color.NRGBA{R: 0x16, G: 0x16, B: 0x16, A: 0x38}
	pcpanelIndicatorAlert = design.ColorProtocolQUIC
	pcpanelPowerColor     = color.NRGBA{R: 0xff, G: 0x5a, B: 0x52, A: 0xff}
	pcpanelResetColor     = color.NRGBA{R: 0xe9, G: 0x8a, B: 0x2b, A: 0xff}
	pcpanelHoldHoverFill  = color.NRGBA{R: 0x45, G: 0x45, B: 0x45, A: 0xff}
)

// pcpanelFixedWidthLayout fixes content width (min=max) so that the dialog doesn't shrink or stretch
type pcpanelFixedWidthLayout struct {
	width float32
}

func (l *pcpanelFixedWidthLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

func (l *pcpanelFixedWidthLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	min := fyne.NewSize(0, 0)
	for _, o := range objects {
		childMin := o.MinSize()
		if childMin.Height > min.Height {
			min.Height = childMin.Height
		}
	}
	if l.width > 0 {
		min.Width = l.width
	}
	return min
}

type pcpanelActionButton struct {
	widget.BaseWidget

	onTapped   func()
	hovered    bool
	blinking   bool
	blinkPhase bool

	bg        *canvas.Rectangle
	indicator *canvas.Circle
	icon      *canvas.Image

	blinkMu   sync.Mutex
	blinkStop chan struct{}
}

type pcpanelModeButton struct {
	widget.BaseWidget

	text    string
	active  bool
	hovered bool
	onTap   func()

	bg    *canvas.Rectangle
	label *canvas.Text
}

type pcpanelHoldButton struct {
	widget.BaseWidget

	labelText    string
	onConfirmed  func()
	holdDuration time.Duration
	activeColor  color.Color
	hovered      bool
	pressing     bool
	progress     float64
	progressMu   sync.Mutex
	progressStop chan struct{}
	bg           *canvas.Rectangle
	fill         *canvas.Rectangle
	border       *canvas.Rectangle
	label        *canvas.Text
	track        *canvas.Rectangle
}

var (
	_ fyne.Tappable     = (*pcpanelHoldButton)(nil)
	_ desktop.Mouseable = (*pcpanelHoldButton)(nil)
	_ mobile.Touchable  = (*pcpanelHoldButton)(nil)
	_ fyne.Draggable    = (*pcpanelHoldButton)(nil)
)

type pcpanelIconButton struct {
	widget.BaseWidget

	icon     fyne.Resource
	onTap    func()
	hovered  bool
	bg       *canvas.Rectangle
	border   *canvas.Rectangle
	iconView *canvas.Image
}

type pcpanelDialogButtonsLayout struct {
	gap float32
}

type pcpanelModeButtonsLayout struct {
	gap float32
}

type pcpanelHoldButtonRenderer struct {
	button  *pcpanelHoldButton
	objects []fyne.CanvasObject
}

func newPCPanelActionButton(onTapped func()) *pcpanelActionButton {
	btn := &pcpanelActionButton{onTapped: onTapped}
	btn.ExtendBaseWidget(btn)
	return btn
}

func newPCPanelDialogCloseButton(onTap func()) *pcpanelIconButton {
	btn := &pcpanelIconButton{
		icon:  theme.CancelIcon(),
		onTap: onTap,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *pcpanelActionButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorAccent)
	b.bg.CornerRadius = design.RadiusMD

	b.indicator = canvas.NewCircle(pcpanelIndicatorIdle)
	b.icon = canvas.NewImageFromResource(assets.PowerOffIconActive)
	b.icon.FillMode = canvas.ImageFillContain

	r := &pcpanelActionButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.bg, b.indicator, b.icon},
	}
	r.Refresh()
	return r
}

func (b *pcpanelIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD

	b.iconView = canvas.NewImageFromResource(b.icon)
	b.iconView.FillMode = canvas.ImageFillContain
	b.iconView.SetMinSize(fyne.NewSize(14, 14))

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(
		b.bg,
		container.NewCenter(b.iconView),
		b.border,
	))
}

func (b *pcpanelIconButton) MinSize() fyne.Size {
	return fyne.NewSize(28, 28)
}

func (b *pcpanelIconButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *pcpanelIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pcpanelIconButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *pcpanelIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *pcpanelIconButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *pcpanelIconButton) refreshVisuals() {
	if b.bg == nil || b.iconView == nil {
		return
	}

	b.bg.FillColor = color.Transparent
	if b.hovered {
		b.bg.FillColor = design.ColorSurfaceLight
	}

	b.bg.Refresh()
	b.iconView.Refresh()
}

func (b *pcpanelActionButton) MinSize() fyne.Size {
	return fyne.NewSize(addressBarButtonSize, addressBarButtonSize)
}

func (b *pcpanelActionButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *pcpanelActionButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pcpanelActionButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *pcpanelActionButton) MouseMoved(*desktop.MouseEvent) {}

func (b *pcpanelActionButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *pcpanelActionButton) SetBlinking(blinking bool) {
	b.blinkMu.Lock()
	if b.blinking == blinking {
		b.blinkMu.Unlock()
		return
	}
	b.blinking = blinking

	stop := b.blinkStop
	b.blinkStop = nil
	b.blinkPhase = false
	b.blinkMu.Unlock()

	if stop != nil {
		close(stop)
	}

	if !blinking {
		fyne.Do(func() {
			b.Refresh()
		})
		return
	}

	newStop := make(chan struct{})
	b.blinkMu.Lock()
	b.blinkStop = newStop
	b.blinkMu.Unlock()

	go func(stop <-chan struct{}) {
		ticker := time.NewTicker(450 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(func() {
					b.blinkMu.Lock()
					if b.blinkStop != stop {
						b.blinkMu.Unlock()
						return
					}
					b.blinkPhase = !b.blinkPhase
					b.blinkMu.Unlock()
					b.Refresh()
				})
			}
		}
	}(newStop)

	b.Refresh()
}

func (b *pcpanelActionButton) refreshVisuals() {
	if b.bg == nil || b.indicator == nil || b.icon == nil {
		return
	}

	fill := design.ColorAccent
	if b.hovered {
		fill = design.ColorAccentHover
	}

	indicatorFill := pcpanelIndicatorIdle
	b.blinkMu.Lock()
	if b.blinking && b.blinkPhase {
		indicatorFill = pcpanelIndicatorAlert
	}
	b.blinkMu.Unlock()

	b.bg.FillColor = fill
	b.indicator.FillColor = indicatorFill
	b.icon.Resource = assets.PowerOffIconActive

	b.bg.Refresh()
	b.indicator.Refresh()
	b.icon.Refresh()
}

type pcpanelActionButtonRenderer struct {
	button  *pcpanelActionButton
	objects []fyne.CanvasObject
}

func (r *pcpanelActionButtonRenderer) Layout(size fyne.Size) {
	r.button.bg.Resize(size)

	indicatorSize := fyne.NewSize(24, 24)
	r.button.indicator.Resize(indicatorSize)
	r.button.indicator.Move(fyne.NewPos((size.Width-indicatorSize.Width)/2, (size.Height-indicatorSize.Height)/2))

	iconSize := fyne.NewSize(18, 18)
	r.button.icon.Resize(iconSize)
	r.button.icon.Move(fyne.NewPos((size.Width-iconSize.Width)/2, (size.Height-iconSize.Height)/2))
}

func (r *pcpanelActionButtonRenderer) MinSize() fyne.Size {
	return r.button.MinSize()
}

func (r *pcpanelActionButtonRenderer) Refresh() {
	r.button.refreshVisuals()
	r.Layout(r.button.Size())
	canvas.Refresh(r.button)
}

func (r *pcpanelActionButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *pcpanelActionButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *pcpanelActionButtonRenderer) Destroy() {}

func newPCPanelHoldButton(label string, holdDuration time.Duration, activeColor color.Color, onConfirmed func()) *pcpanelHoldButton {
	btn := &pcpanelHoldButton{
		labelText:    label,
		onConfirmed:  onConfirmed,
		holdDuration: holdDuration,
		activeColor:  activeColor,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *pcpanelHoldButton) CreateRenderer() fyne.WidgetRenderer {
	b.track = canvas.NewRectangle(design.ColorSurfaceLight)
	b.track.CornerRadius = design.RadiusMD

	b.fill = canvas.NewRectangle(b.activeColor)
	b.fill.CornerRadius = design.RadiusMD

	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeColor = design.ColorBorder
	b.border.StrokeWidth = 1

	b.label = canvas.NewText(b.labelText, design.ColorTextLight)
	b.label.TextSize = 15
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return &pcpanelHoldButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.track, b.bg, b.fill, b.label, b.border},
	}
}

func (b *pcpanelHoldButton) MinSize() fyne.Size {
	return fyne.NewSize(150, 40)
}

func (b *pcpanelHoldButton) Tapped(*fyne.PointEvent) {}

func (b *pcpanelHoldButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pcpanelHoldButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *pcpanelHoldButton) MouseMoved(*desktop.MouseEvent) {}

func (b *pcpanelHoldButton) MouseOut() {
	b.hovered = false
	if !b.pressing {
		b.refreshVisuals()
	}
}

func (b *pcpanelHoldButton) MouseDown(*desktop.MouseEvent) {
	b.startHold()
}

func (b *pcpanelHoldButton) MouseUp(*desktop.MouseEvent) {
	b.cancelHold()
}

func (b *pcpanelHoldButton) TouchDown(*mobile.TouchEvent) {
	b.startHold()
}

func (b *pcpanelHoldButton) TouchUp(*mobile.TouchEvent) {
	b.cancelHold()
}

func (b *pcpanelHoldButton) TouchCancel(*mobile.TouchEvent) {
	b.cancelHold()
}

func (b *pcpanelHoldButton) Dragged(*fyne.DragEvent) {
	b.startHold()
}

func (b *pcpanelHoldButton) DragEnd() {
	b.cancelHold()
}
func (b *pcpanelHoldButton) startHold() {
	if b.holdDuration <= 0 {
		b.holdDuration = 2 * time.Second
	}

	b.progressMu.Lock()
	if b.pressing {
		b.progressMu.Unlock()
		return
	}
	if b.progressStop != nil {
		close(b.progressStop)
	}
	stop := make(chan struct{})
	b.progressStop = stop
	b.pressing = true
	b.progress = 0
	b.progressMu.Unlock()

	b.Refresh()

	go func(stop <-chan struct{}) {
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		startedAt := time.Now()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				elapsed := time.Since(startedAt)
				progress := float64(elapsed) / float64(b.holdDuration)
				if progress > 1 {
					progress = 1
				}

				fyne.Do(func() {
					b.progressMu.Lock()
					if b.progressStop != stop {
						b.progressMu.Unlock()
						return
					}
					b.progress = progress
					done := progress >= 1
					b.progressMu.Unlock()
					b.Refresh()

					if done {
						b.progressMu.Lock()
						if b.progressStop == stop {
							b.progressStop = nil
							b.pressing = false
						}
						b.progressMu.Unlock()
						if b.onConfirmed != nil {
							b.onConfirmed()
						}
					}
				})

				if progress >= 1 {
					return
				}
			}
		}
	}(stop)
}

func (b *pcpanelHoldButton) cancelHold() {
	b.progressMu.Lock()
	stop := b.progressStop
	b.progressStop = nil
	b.pressing = false
	b.progress = 0
	b.progressMu.Unlock()
	if stop != nil {
		close(stop)
	}
	b.Refresh()
}

func (b *pcpanelHoldButton) refreshVisuals() {
	if b.track == nil || b.fill == nil || b.border == nil || b.label == nil {
		return
	}

	b.track.FillColor = design.ColorSurfaceLight
	b.fill.FillColor = color.Transparent
	b.bg.FillColor = color.Transparent
	b.border.StrokeColor = color.Transparent
	b.border.StrokeWidth = 0
	b.label.Color = design.ColorTextLight
	if b.progress > 0 {
		b.fill.FillColor = b.activeColor
	}
	if b.hovered {
		b.bg.FillColor = pcpanelHoldHoverFill
	}

	b.track.Refresh()
	b.fill.Refresh()
	b.bg.Refresh()
	b.border.Refresh()
	b.label.Refresh()
}

func (r *pcpanelHoldButtonRenderer) Layout(size fyne.Size) {
	r.button.track.Resize(size)

	progressWidth := float32(float64(size.Width) * r.button.progress)
	if progressWidth < 0 {
		progressWidth = 0
	}
	if progressWidth < 1 {
		progressWidth = 0
	}
	r.button.fill.Move(fyne.NewPos(0, 0))
	r.button.fill.Resize(fyne.NewSize(progressWidth, size.Height))
	fillRadius := design.RadiusMD
	if maxRadius := progressWidth / 2; maxRadius < fillRadius {
		fillRadius = maxRadius
	}
	if maxRadius := size.Height / 2; maxRadius < fillRadius {
		fillRadius = maxRadius
	}
	if fillRadius < 0 {
		fillRadius = 0
	}
	r.button.fill.CornerRadius = fillRadius

	r.button.bg.Resize(size)
	r.button.border.Resize(size)

	labelSize := r.button.label.MinSize()
	r.button.label.Move(fyne.NewPos((size.Width-labelSize.Width)/2, (size.Height-labelSize.Height)/2))
	r.button.label.Resize(labelSize)
}

func (r *pcpanelHoldButtonRenderer) MinSize() fyne.Size {
	return r.button.MinSize()
}

func (r *pcpanelHoldButtonRenderer) Refresh() {
	r.button.refreshVisuals()
	r.Layout(r.button.Size())
	canvas.Refresh(r.button)
}

func (r *pcpanelHoldButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *pcpanelHoldButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *pcpanelHoldButtonRenderer) Destroy() {}

func newPCPanelModeButton(text string, onTap func()) *pcpanelModeButton {
	btn := &pcpanelModeButton{text: text, onTap: onTap}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *pcpanelModeButton) SetActive(active bool) {
	if b.active == active {
		return
	}
	b.active = active
	b.refreshVisuals()
}

func (b *pcpanelModeButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *pcpanelModeButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pcpanelModeButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *pcpanelModeButton) MouseMoved(*desktop.MouseEvent) {}

func (b *pcpanelModeButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *pcpanelModeButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *pcpanelModeButton) MinSize() fyne.Size {
	return fyne.NewSize(90, 36)
}

func (b *pcpanelModeButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorSurfaceLight)
	b.bg.CornerRadius = design.RadiusMD
	b.bg.StrokeWidth = 1

	b.label = canvas.NewText(b.text, design.ColorTextLight)
	b.label.TextSize = 13
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.label)))
}

func (b *pcpanelModeButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}

	activeColor := pcpanelPowerColor
	if strings.EqualFold(strings.TrimSpace(b.text), "Reset") {
		activeColor = pcpanelResetColor
	}

	if b.active {
		b.bg.FillColor = activeColor
		b.bg.StrokeColor = activeColor
		b.label.Color = design.ColorBackground
	} else {
		b.bg.FillColor = design.ColorSurfaceLight
		b.bg.StrokeColor = activeColor
		b.label.Color = activeColor
		if b.hovered {
			b.bg.FillColor = design.ColorGray900
		}
	}

	b.bg.Refresh()
	b.label.Refresh()
}

func (l *pcpanelDialogButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	left := objects[0]
	right := objects[1]

	leftWidth := (size.Width - l.gap) / 2
	if fyne.CurrentDevice().IsMobile() || size.Width < 360 {
		leftWidth = (size.Width - l.gap) * 0.36
	}
	if leftWidth < 0 {
		leftWidth = 0
	}
	rightWidth := size.Width - leftWidth - l.gap
	if rightWidth < 0 {
		rightWidth = 0
	}

	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(leftWidth, size.Height))
	right.Move(fyne.NewPos(leftWidth+l.gap, 0))
	right.Resize(fyne.NewSize(rightWidth, size.Height))
}

func (l *pcpanelDialogButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	left := objects[0].MinSize()
	right := objects[1].MinSize()
	height := left.Height
	if right.Height > height {
		height = right.Height
	}
	return fyne.NewSize(left.Width+right.Width+l.gap, height)
}

func (l *pcpanelModeButtonsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	totalGap := l.gap * float32(len(objects)-1)
	itemWidth := (size.Width - totalGap) / float32(len(objects))
	if itemWidth < 0 {
		itemWidth = 0
	}
	x := float32(0)
	for _, object := range objects {
		object.Move(fyne.NewPos(x, 0))
		object.Resize(fyne.NewSize(itemWidth, size.Height))
		x += itemWidth + l.gap
	}
}

func (l *pcpanelModeButtonsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	width := float32(0)
	height := float32(0)
	for i, object := range objects {
		min := object.MinSize()
		width += min.Width
		if i > 0 {
			width += l.gap
		}
		if min.Height > height {
			height = min.Height
		}
	}
	return fyne.NewSize(width, height)
}

// PCPanelWidget is a power button with activity indicator in the address bar
type PCPanelWidget struct {
	actionBtn      *pcpanelActionButton
	container      *fyne.Container
	usbClient      *api.USBClient
	stopPoll       chan struct{}
	pollMu         sync.Mutex
	pollCtxCancel  context.CancelFunc // cancels any in-flight poll HTTP request
	powerOn        bool
	hddOn          bool
	window         fyne.Window
}

// NewPCPanelWidget creates a widget with a combined Power/Reset button.
func NewPCPanelWidget(w fyne.Window) *PCPanelWidget {
	p := &PCPanelWidget{
		window: w,
	}
	p.actionBtn = newPCPanelActionButton(p.onActionClick)
	p.container = container.NewHBox(p.actionBtn)
	p.container.Hide()
	return p
}

// GetContainer returns the container for placement in the address bar
func (p *PCPanelWidget) GetContainer() *fyne.Container {
	return p.container
}

// SetClient sets the USB client and starts polling for LEDs
func (p *PCPanelWidget) SetClient(c *api.USBClient) {
	p.pollMu.Lock()
	if p.stopPoll != nil {
		close(p.stopPoll)
		p.stopPoll = nil
	}
	// Cancel any in-flight HTTP request on the old client so it doesn't race
	// with Disconnect() being called in the background cleanup goroutine.
	if p.pollCtxCancel != nil {
		p.pollCtxCancel()
		p.pollCtxCancel = nil
	}
	p.usbClient = c
	p.pollMu.Unlock()

	if c == nil {
		p.container.Hide()
		p.updateLEDIcons(false, false)
		return
	}

	p.container.Show()
	p.updateLEDIcons(false, false)
	p.pollLeds()
	// Initial poll immediately
	go func() {
		resp, err := c.GetPCPanelLeds()
		if err != nil {
			logrus.Debugf("PCPanel LEDs initial poll: %v", err)
			return
		}
		fyne.Do(func() {
			p.updateLEDIcons(resp.Data.Power, resp.Data.HDD)
		})
	}()
}

// pollLeds periodically polls LEDs state
func (p *PCPanelWidget) pollLeds() {
	p.pollMu.Lock()
	if p.stopPoll != nil {
		close(p.stopPoll)
	}
	p.stopPoll = make(chan struct{})
	stop := p.stopPoll
	p.pollMu.Unlock()

	go func() {
		ticker := time.NewTicker(pcpanelLedPollInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				p.pollMu.Lock()
				c := p.usbClient
				p.pollMu.Unlock()
				if c == nil {
					return
				}
				// Create a per-request context so SetClient can cancel it if
				// Disconnect() is called while this request is in flight.
				ctx, cancel := context.WithCancel(context.Background())
				p.pollMu.Lock()
				p.pollCtxCancel = cancel
				p.pollMu.Unlock()

				resp, err := c.GetPCPanelLedsWithContext(ctx)
				cancel() // always release resources

				p.pollMu.Lock()
				p.pollCtxCancel = nil
				p.pollMu.Unlock()

				if err != nil {
					logrus.Debugf("PCPanel LEDs poll error: %v", err)
					continue
				}
				fyne.Do(func() {
					p.updateLEDIcons(resp.Data.Power, resp.Data.HDD)
				})
			}
		}
	}()
}

// updateLEDIcons updates indicators based on target machine's LED state.
func (p *PCPanelWidget) updateLEDIcons(powerOn, hddOn bool) {
	p.powerOn = powerOn
	p.hddOn = hddOn

	if p.actionBtn != nil {
		p.actionBtn.SetBlinking(hddOn)
		p.actionBtn.Refresh()
	}
}

func (p *PCPanelWidget) onActionClick() {
	if p.usbClient == nil {
		return
	}
	p.showPowerActionDialog()
}

func (p *PCPanelWidget) showPowerActionDialog() {
	if p.window == nil {
		return
	}

	actionTitles := map[string]string{
		"power": "Power Off",
		"reset": "Reset",
	}

	titleText := view.NewBrandText("Power controls", 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter

	holdSlider := widget.NewSlider(0, 10)
	holdSlider.Step = 1
	holdSlider.Value = 0

	shortLabel := canvas.NewText("Short (0s)", design.ColorTextMuted)
	shortLabel.TextSize = 11
	longLabel := canvas.NewText("Long (10s)", design.ColorTextMuted)
	longLabel.TextSize = 11
	durationHints := view.NewInset(
		container.NewBorder(nil, nil, shortLabel, longLabel, canvas.NewRectangle(color.Transparent)),
		12, 12, 0, 0,
	)
	durationTitle := widget.NewLabel("Button Press Duration (0s)")

	powerOptions := container.NewVBox(
		durationTitle,
		holdSlider,
		durationHints,
	)

	detailsContainer := container.NewVBox()
	actionInfoLabel := widget.NewLabel("")
	actionInfoLabel.Alignment = fyne.TextAlignCenter
	actionInfoLabel.Wrapping = fyne.TextWrapWord

	powerBtn := newPCPanelModeButton("Power Off", nil)
	resetBtn := newPCPanelModeButton("Reset", nil)
	modeButtons := container.New(&pcpanelModeButtonsLayout{gap: 10}, powerBtn, resetBtn)

	var currentAction string
	var holdButton *pcpanelHoldButton
	updateDetails := func(action string) {
		currentAction = action
		actionInfoLabel.SetText(i18n.Current.PCPanelActionConfirm)

		if action == "power" {
			detailsContainer.Objects = []fyne.CanvasObject{powerOptions}
			powerBtn.SetActive(true)
			resetBtn.SetActive(false)
			if holdButton != nil {
				holdButton.activeColor = pcpanelPowerColor
				holdButton.Refresh()
			}
		} else {
			detailsContainer.Objects = nil
			powerBtn.SetActive(false)
			resetBtn.SetActive(true)
			if holdButton != nil {
				holdButton.activeColor = pcpanelResetColor
				holdButton.Refresh()
			}
		}
		detailsContainer.Refresh()
		if holdButton != nil {
			holdButton.cancelHold()
		}
	}

	var popup *widget.PopUp
	holdButton = newPCPanelHoldButton("Hold to Confirm", 2*time.Second, pcpanelPowerColor, func() {
		client := p.usbClient
		if client != nil {
			var err error
			switch currentAction {
			case "power":
				err = client.PressPCPanelButton("power", int(holdSlider.Value))
			case "reset":
				err = client.PressPCPanelButton("reset", 0)
			}
			if err != nil {
				logrus.Errorf("PCPanel %s error: %v", actionTitles[currentAction], err)
				view.ShowErrorDialog(err, p.window)
				return
			}
		}
		if popup != nil {
			popup.Hide()
		}
	})

	noBtn := widget.NewButton(i18n.Current.Cancel, func() {
		if popup != nil {
			popup.Hide()
		}
	})

	powerBtn.onTap = func() {
		updateDetails("power")
	}
	resetBtn.onTap = func() {
		updateDetails("reset")
	}

	holdSlider.OnChanged = func(v float64) {
		durationTitle.SetText(fmt.Sprintf("Button Press Duration (%ds)", int(v)))
	}

	closeBtn := newPCPanelDialogCloseButton(func() {
		if popup != nil {
			popup.Hide()
		}
	})
	titleBar := container.NewBorder(nil, nil, nil, closeBtn, container.NewCenter(titleText))

	bodyContent := container.NewVBox(
		titleBar,
		widget.NewLabel("Action"),
		modeButtons,
		detailsContainer,
	)
	footer := container.NewVBox(
		actionInfoLabel,
		view.NewInset(container.New(&pcpanelDialogButtonsLayout{gap: 12}, noBtn, holdButton), 0, 0, 8, 0),
	)
	form := container.NewBorder(nil, footer, nil, nil, bodyContent)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	panel := container.NewStack(
		bg,
		view.NewInset(form, 18, 18, 16, 16),
		border,
	)

	updateDetails("power")
	popup = view.ShowOverlayPopup(p.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
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
			panelWidth := minFloat32(maxFloat32(panelMin.Width, 420), maxWidth)
			panelHeight := minFloat32(maxFloat32(panelMin.Height, 350), maxHeight)
			return fyne.NewSize(panelWidth, panelHeight)
		},
	})
}

func (p *PCPanelWidget) showPowerDialog() {
	label := widget.NewLabel(i18n.Current.PCPanelPowerConfirm)
	label.Wrapping = fyne.TextWrapWord

	holdSlider := widget.NewSlider(0, 10)
	holdSlider.Step = 1
	holdSlider.Value = 0

	holdLabel := widget.NewLabel(i18n.Current.PCPanelPowerShortPress)
	holdLabel.Wrapping = fyne.TextWrapWord
	holdSlider.OnChanged = func(v float64) {
		if v <= 0 {
			holdLabel.SetText(i18n.Current.PCPanelPowerShortPress)
			return
		}
		holdLabel.SetText(fmt.Sprintf(i18n.Current.PCPanelPowerLongPress, int(v)) + " - " + i18n.Current.PCPanelLongPressNotSupported)
	}

	extra := container.NewVBox(
		label,
		widget.NewLabel(i18n.Current.PCPanelPowerHoldTime),
		holdSlider,
		holdLabel,
	)

	p.showProtectedActionDialog(
		i18n.Current.PCPanelPowerTitle,
		`Type "power" to confirm shutdown.`,
		"power",
		extra,
		func() {
			client := p.usbClient
			if client != nil {
				if err := client.PressPCPanelButton("power", int(holdSlider.Value)); err != nil {
					logrus.Errorf("PCPanel Power error: %v", err)
					view.ShowErrorDialog(err, p.window)
				}
			}
		},
	)
}

func (p *PCPanelWidget) showResetDialog() {
	label := widget.NewLabel(i18n.Current.PCPanelResetConfirm)
	label.Wrapping = fyne.TextWrapWord

	p.showProtectedActionDialog(
		i18n.Current.PCPanelResetTitle,
		`Type "reset" to confirm reboot.`,
		"reset",
		label,
		func() {
			client := p.usbClient
			if client != nil {
				if err := client.PressPCPanelButton("reset", 0); err != nil {
					logrus.Errorf("PCPanel Reset error: %v", err)
					view.ShowErrorDialog(err, p.window)
				}
			}
		},
	)
}

func (p *PCPanelWidget) showProtectedActionDialog(title, hint, expectedWord string, extra fyne.CanvasObject, onConfirm func()) {
	if p.window == nil {
		return
	}

	expectedWord = strings.TrimSpace(strings.ToLower(expectedWord))

	titleLabel := widget.NewLabel(title)
	titleLabel.Wrapping = fyne.TextWrapWord

	hintLabel := widget.NewLabel(hint)
	hintLabel.Wrapping = fyne.TextWrapWord

	helpLabel := widget.NewLabel("English only. Confirmation is case-insensitive.")
	helpLabel.Wrapping = fyne.TextWrapWord

	confirmEntry := widget.NewEntry()
	confirmEntry.SetPlaceHolder(expectedWord)

	contentItems := []fyne.CanvasObject{titleLabel, hintLabel, helpLabel}
	if extra != nil {
		contentItems = append(contentItems, widget.NewSeparator(), extra)
	}
	contentItems = append(contentItems, widget.NewSeparator(), confirmEntry)

	inner := container.NewVBox(contentItems...)

	var minW float32 = 360
	if fyne.CurrentDevice().IsMobile() {
		sz := p.window.Canvas().Size()
		minW = sz.Width * 0.85
		if minW < 280 {
			minW = 280
		}
	}

	content := container.New(&pcpanelFixedWidthLayout{width: minW}, inner)

	var d dialog.Dialog
	yesBtn := widget.NewButton(i18n.Current.Yes, func() {
		if onConfirm != nil {
			onConfirm()
		}
		if d != nil {
			d.Hide()
		}
	})
	yesBtn.Importance = widget.DangerImportance
	yesBtn.SetIcon(theme.ConfirmIcon())
	yesBtn.Disable()

	noBtn := widget.NewButton(i18n.Current.No, func() {
		if d != nil {
			d.Hide()
		}
	})
	noBtn.SetIcon(theme.CancelIcon())

	confirmEntry.OnChanged = func(value string) {
		if strings.EqualFold(strings.TrimSpace(value), expectedWord) {
			yesBtn.Enable()
			return
		}
		yesBtn.Disable()
	}

	buttons := container.NewGridWithColumns(2, yesBtn, noBtn)
	inner.Objects = append(inner.Objects, buttons)

	d = dialog.NewCustomWithoutButtons(title, content, p.window)
	d.Show()
}

// ── Starlark syntax highlighter ──────────────────────────────────────────────

var starlarkKw = map[string]bool{
	"and": true, "break": true, "continue": true, "def": true,
	"elif": true, "else": true, "False": true, "for": true,
	"if": true, "in": true, "lambda": true, "load": true,
	"None": true, "not": true, "or": true, "pass": true,
	"return": true, "True": true,
}

var starlarkBuiltin = map[string]bool{
	"abs": true, "all": true, "any": true, "bool": true, "dict": true,
	"dir": true, "enumerate": true, "fail": true, "float": true,
	"getattr": true, "hasattr": true, "hash": true, "int": true,
	"len": true, "list": true, "max": true, "min": true, "print": true,
	"range": true, "repr": true, "reversed": true, "set": true,
	"sorted": true, "str": true, "tuple": true, "type": true, "zip": true,
}

// transparentEntryTheme wraps a theme to make Entry background and text
// transparent, so only the cursor and selection remain visible.
// Used for the syntax-highlighted editor overlay.
type transparentEntryTheme struct {
	fyne.Theme
}

func (t *transparentEntryTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground, theme.ColorNameInputBorder,
		theme.ColorNameForeground, theme.ColorNamePlaceHolder:
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}

func starlarkHighlight(code string) []widget.RichTextSegment {
	lines := strings.Split(code, "\n")
	out := make([]widget.RichTextSegment, 0, len(lines)*4)
	for i, line := range lines {
		if i > 0 {
			out = append(out, &widget.TextSegment{
				Text:  "\n",
				Style: widget.RichTextStyle{Inline: true, TextStyle: fyne.TextStyle{Monospace: true}},
			})
		}
		out = append(out, tokenizeStarlarkLine(line)...)
	}
	return out
}

func codeSeg(text string, cname fyne.ThemeColorName) *widget.TextSegment {
	return &widget.TextSegment{
		Text: text,
		Style: widget.RichTextStyle{
			ColorName: cname,
			Inline:    true,
			TextStyle: fyne.TextStyle{Monospace: true},
		},
	}
}

func tokenizeStarlarkLine(line string) []widget.RichTextSegment {
	out := make([]widget.RichTextSegment, 0, 8)
	n := len(line)
	i := 0

	for i < n {
		c := line[i]

		// comment
		if c == '#' {
			out = append(out, codeSeg(line[i:], design.ColorNameCodeComment))
			return out
		}

		// string literal (single or triple quoted)
		if c == '"' || c == '\'' {
			q := c
			j := i + 1
			if j+1 < n && line[j] == q && line[j+1] == q {
				// triple-quoted
				j += 2
				for j+2 < n {
					if line[j] == q && line[j+1] == q && line[j+2] == q {
						j += 3
						break
					}
					j++
				}
			} else {
				for j < n && line[j] != q {
					if line[j] == '\\' {
						j++
					}
					j++
				}
				if j < n {
					j++
				}
			}
			out = append(out, codeSeg(line[i:j], design.ColorNameCodeString))
			i = j
			continue
		}

		// number
		if c >= '0' && c <= '9' {
			j := i + 1
			if c == '0' && j < n && (line[j] == 'x' || line[j] == 'X') {
				j++
				for j < n && (line[j] >= '0' && line[j] <= '9' ||
					line[j] >= 'a' && line[j] <= 'f' ||
					line[j] >= 'A' && line[j] <= 'F') {
					j++
				}
			} else {
				for j < n && (line[j] >= '0' && line[j] <= '9' || line[j] == '.') {
					j++
				}
			}
			out = append(out, codeSeg(line[i:j], design.ColorNameCodeNumber))
			i = j
			continue
		}

		// identifier / keyword / builtin
		if c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' {
			j := i + 1
			for j < n && (line[j] == '_' || line[j] >= 'a' && line[j] <= 'z' ||
				line[j] >= 'A' && line[j] <= 'Z' || line[j] >= '0' && line[j] <= '9') {
				j++
			}
			word := line[i:j]
			var cname fyne.ThemeColorName
			switch {
			case starlarkKw[word]:
				cname = design.ColorNameCodeKeyword
			case starlarkBuiltin[word]:
				cname = design.ColorNameCodeBuiltin
			default:
				cname = design.ColorNameCodeDefault
			}
			out = append(out, codeSeg(word, cname))
			i = j
			continue
		}

		// operators, punctuation, whitespace — collect until next special boundary
		j := i + 1
		for j < n {
			nc := line[j]
			if nc == '#' || nc == '"' || nc == '\'' ||
				nc >= '0' && nc <= '9' ||
				nc == '_' || nc >= 'a' && nc <= 'z' || nc >= 'A' && nc <= 'Z' {
				break
			}
			j++
		}
		out = append(out, codeSeg(line[i:j], design.ColorNameCodeDefault))
		i = j
	}
	return out
}

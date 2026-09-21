package controller

import (
	"context"
	"fmt"
	"image/color"
	"math"
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
	// addressBarButtonSize sizes this widget's own header button (the power
	// indicator/menu trigger) -- 28, not the original 36, to match
	// headerCompactButtonSize (gui/connection_header.go), the connections
	// screen's own header buttons. At 36 this button alone made the header
	// row (main_window_layout.go's createMainAddressBar) taller than the
	// connections screen's header despite both going through near-identical
	// band padding.
	addressBarButtonSize = 28 // Square buttons: width = height = line height
)

var (
	pcpanelIndicatorIdle    = color.NRGBA{R: 0x16, G: 0x16, B: 0x16, A: 0x38}
	pcpanelIndicatorAlert   = design.ColorAlert
	pcpanelDialogCardBG     = color.NRGBA{R: 0x1e, G: 0x22, B: 0x25, A: 0xff}
	pcpanelDialogBorder     = color.NRGBA{R: 0x33, G: 0x37, B: 0x2f, A: 0xff}
	pcpanelDialogHint       = color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}
	pcpanelDialogLabel      = color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff}
	pcpanelDialogSep        = color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff}
	pcpanelDialogValue      = color.NRGBA{R: 0xeb, G: 0xff, B: 0xbc, A: 0xff}
	pcpanelDialogCancelIcon = fyne.NewStaticResource("pcpanel_dialog_cancel.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#8f9381"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))
)

const (
	pcpanelDialogPanelWidth   = float32(408)
	pcpanelDialogPillTextSize = float32(9.5)
	pcpanelDialogPillHeight   = float32(32)
	pcpanelDialogPillPadX     = float32(15)
	pcpanelDialogHintTextSize = float32(8)
	pcpanelSliderThumbRadius  = float32(6)
	pcpanelSliderGlowRadius   = float32(9)
	pcpanelSliderTrackHeight  = float32(3)
	pcpanelSliderHeight       = float32(20)
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

	text     string
	active   bool
	hovered  bool
	disabled bool
	onTap    func()

	bg    *canvas.Rectangle
	label *canvas.Text
}

type pcpanelHoldButton struct {
	widget.BaseWidget

	labelText    string
	onConfirmed  func()
	holdDuration time.Duration
	hovered      bool
	disabled     bool
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

type pcpanelCancelButton struct {
	widget.BaseWidget

	text     string
	onTap    func()
	hovered  bool
	disabled bool
	label    *canvas.Text
}

type pcpanelDurationSlider struct {
	widget.BaseWidget

	Min, Max, Step float64
	Value          float64
	OnChanged      func(float64)
	disabled       bool

	track *canvas.Rectangle
	glow  *canvas.Circle
	thumb *canvas.Circle
}

type pcpanelCornerButtonLayout struct {
	Top   float32
	Right float32
}

var (
	_ fyne.Tappable      = (*pcpanelCancelButton)(nil)
	_ desktop.Hoverable  = (*pcpanelCancelButton)(nil)
	_ desktop.Cursorable = (*pcpanelCancelButton)(nil)
	_ fyne.Tappable      = (*pcpanelDurationSlider)(nil)
	_ fyne.Draggable     = (*pcpanelDurationSlider)(nil)
	_ desktop.Cursorable = (*pcpanelDurationSlider)(nil)
)

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
		icon:  pcpanelDialogCancelIcon,
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
	b.iconView.SetMinSize(fyne.NewSize(18, 18))

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

	// Scaled down along with addressBarButtonSize's own 36->28 shrink (24/18
	// at 36px left only ~2px of margin around the indicator circle at 28px --
	// visibly cramped next to this same row's other, more breathing-room'd
	// 28px buttons).
	indicatorSize := fyne.NewSize(20, 20)
	r.button.indicator.Resize(indicatorSize)
	r.button.indicator.Move(fyne.NewPos((size.Width-indicatorSize.Width)/2, (size.Height-indicatorSize.Height)/2))

	iconSize := fyne.NewSize(15, 15)
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

func newPCPanelHoldButton(label string, holdDuration time.Duration, onConfirmed func()) *pcpanelHoldButton {
	btn := &pcpanelHoldButton{
		labelText:    label,
		onConfirmed:  onConfirmed,
		holdDuration: holdDuration,
	}
	btn.ExtendBaseWidget(btn)
	return btn
}

func (b *pcpanelHoldButton) CreateRenderer() fyne.WidgetRenderer {
	b.track = canvas.NewRectangle(design.ColorConnectionBadgeText)
	b.track.CornerRadius = design.RadiusMD

	b.fill = canvas.NewRectangle(design.ColorConnectionAddFill)
	b.fill.CornerRadius = design.RadiusMD

	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD

	b.border = canvas.NewRectangle(color.Transparent)
	b.border.CornerRadius = design.RadiusMD
	b.border.StrokeColor = design.ColorBorder
	b.border.StrokeWidth = 1

	b.label = canvas.NewText(b.labelText, design.ColorGray950)
	b.label.TextSize = pcpanelDialogPillTextSize
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return &pcpanelHoldButtonRenderer{
		button:  b,
		objects: []fyne.CanvasObject{b.track, b.bg, b.fill, b.label, b.border},
	}
}

func (b *pcpanelHoldButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.labelText, color.Black)
	measure.TextSize = pcpanelDialogPillTextSize
	measure.TextStyle.Bold = true
	width := measure.MinSize().Width + pcpanelDialogPillPadX*2
	if width < 132 {
		width = 132
	}
	return fyne.NewSize(width, pcpanelDialogPillHeight)
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

// SetDisabled locks the hold-to-confirm button so pressing/dragging it has
// no effect, and renders it in a muted "inactive" style.
func (b *pcpanelHoldButton) SetDisabled(disabled bool) {
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	if disabled {
		b.cancelHold()
	}
	b.refreshVisuals()
}

func (b *pcpanelHoldButton) startHold() {
	if b.disabled {
		return
	}
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

	fill := design.ColorConnectionBadgeText
	text := design.ColorGray950
	if b.disabled {
		fill = color.NRGBA{R: 0x31, G: 0xa6, B: 0x94, A: 0xff}
	} else if b.hovered && b.progress == 0 {
		fill = color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff}
	}

	b.track.FillColor = fill
	b.fill.FillColor = color.Transparent
	if b.progress > 0 {
		b.fill.FillColor = design.ColorConnectionAddFill
	}
	b.bg.FillColor = color.Transparent
	b.border.StrokeColor = color.Transparent
	b.border.StrokeWidth = 0
	b.label.Color = text

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

// SetDisabled locks the button so it can no longer be tapped or hovered,
// and renders it in a muted "inactive" style.
func (b *pcpanelModeButton) SetDisabled(disabled bool) {
	if b.disabled == disabled {
		return
	}
	b.disabled = disabled
	b.refreshVisuals()
}

func (b *pcpanelModeButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTap == nil {
		return
	}
	b.onTap()
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
	return fyne.NewSize(84, 30)
}

func (b *pcpanelModeButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(design.ColorSurfaceLight)
	b.bg.CornerRadius = design.RadiusMD
	b.bg.StrokeWidth = 1

	b.label = canvas.NewText(b.text, design.ColorTextLight)
	b.label.TextSize = pcpanelDialogPillTextSize
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewMax(b.bg, container.NewCenter(b.label)))
}

func (b *pcpanelModeButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}

	if b.disabled {
		b.bg.FillColor = color.Transparent
		b.bg.StrokeColor = design.ColorTailscaleChipBorder
		b.label.Color = pcpanelDialogHint
		b.bg.Refresh()
		b.label.Refresh()
		return
	}

	if b.active {
		b.bg.FillColor = design.ColorConnectionBadgeText
		b.bg.StrokeColor = color.Transparent
		b.label.Color = design.ColorGray950
	} else {
		b.bg.FillColor = color.Transparent
		b.bg.StrokeColor = design.ColorTailscaleChipBorder
		b.label.Color = design.ColorTextLight
		if b.hovered {
			b.bg.FillColor = color.NRGBA{R: 0x26, G: 0x2a, B: 0x2e, A: 0xff}
			b.bg.StrokeColor = design.ColorConnectionBadgeText
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
	if view.IsMobile() || size.Width < 360 {
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

func pcpanelDialogFieldLabel(text string) *canvas.Text {
	label := canvas.NewText(strings.ToUpper(text), pcpanelDialogLabel)
	label.TextSize = 9
	label.TextStyle.Bold = true
	return label
}

func pcpanelDialogVSpace(height float32) fyne.CanvasObject {
	spacer := canvas.NewRectangle(color.Transparent)
	spacer.SetMinSize(fyne.NewSize(0, height))
	return spacer
}

func pcpanelDialogTopAccentBar() fyne.CanvasObject {
	teal := design.ColorConnectionBadgeText
	lime := design.ColorConnectionAddFill
	tealTransparent := color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0}
	limeTransparent := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0}
	accentLeftFade := canvas.NewHorizontalGradient(tealTransparent, teal)
	accentLeftFade.SetMinSize(fyne.NewSize(70, 2))
	accentRightFade := canvas.NewHorizontalGradient(lime, limeTransparent)
	accentRightFade.SetMinSize(fyne.NewSize(70, 2))
	accentMid := canvas.NewHorizontalGradient(teal, lime)
	return container.NewBorder(nil, nil, accentLeftFade, accentRightFade, accentMid)
}

func pcpanelDialogHairline() *canvas.Rectangle {
	line := canvas.NewRectangle(pcpanelDialogSep)
	line.SetMinSize(fyne.NewSize(0, 1))
	return line
}

func pcpanelDialogCard(bgFill color.Color, content fyne.CanvasObject) fyne.CanvasObject {
	bg := canvas.NewRectangle(bgFill)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = pcpanelDialogBorder
	border.StrokeWidth = 1
	return container.NewStack(bg, content, border)
}

func pcpanelDialogValuePill(text *canvas.Text) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = 6
	border.StrokeColor = pcpanelDialogBorder
	border.StrokeWidth = 1
	return container.NewStack(bg, border, view.NewInset(container.NewCenter(text), 10, 10, 2, 2))
}

func pcpanelDialogCanvasPanelWidth(canvasSize fyne.Size) float32 {
	if canvasSize.Width <= 0 {
		return pcpanelDialogPanelWidth
	}
	margin := clampFloat32(minFloat32(canvasSize.Width, canvasSize.Height)*0.04, 20, 28)
	maxWidth := canvasSize.Width - margin*2
	if maxWidth <= 0 {
		maxWidth = canvasSize.Width
	}
	if maxWidth <= 0 {
		return pcpanelDialogPanelWidth
	}
	return minFloat32(pcpanelDialogPanelWidth, maxWidth)
}

func (l *pcpanelCornerButtonLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	obj := objects[0]
	min := obj.MinSize()
	obj.Resize(min)
	obj.Move(fyne.NewPos(size.Width-l.Right-min.Width, l.Top))
}

func (l *pcpanelCornerButtonLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

func newPCPanelCancelButton(text string, onTap func()) *pcpanelCancelButton {
	b := &pcpanelCancelButton{text: text, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *pcpanelCancelButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTap == nil {
		return
	}
	b.onTap()
}

func (b *pcpanelCancelButton) TappedSecondary(*fyne.PointEvent) {}

func (b *pcpanelCancelButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}

func (b *pcpanelCancelButton) MouseMoved(*desktop.MouseEvent) {}

func (b *pcpanelCancelButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}

func (b *pcpanelCancelButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *pcpanelCancelButton) MinSize() fyne.Size {
	measure := canvas.NewText(b.text, color.Black)
	measure.TextSize = pcpanelDialogPillTextSize
	measure.TextStyle.Bold = true
	return fyne.NewSize(measure.MinSize().Width+pcpanelDialogPillPadX*2, pcpanelDialogPillHeight)
}

func (b *pcpanelCancelButton) CreateRenderer() fyne.WidgetRenderer {
	b.label = canvas.NewText(b.text, pcpanelDialogHint)
	b.label.TextSize = pcpanelDialogPillTextSize
	b.label.TextStyle.Bold = true
	b.label.Alignment = fyne.TextAlignCenter
	return widget.NewSimpleRenderer(container.NewCenter(b.label))
}

func (b *pcpanelCancelButton) Refresh() {
	if b.label != nil {
		if b.hovered && !b.disabled {
			b.label.Color = design.ColorTextLight
		} else {
			b.label.Color = pcpanelDialogHint
		}
		b.label.Refresh()
	}
	b.BaseWidget.Refresh()
}

func newPCPanelDurationSlider(min, max, step float64) *pcpanelDurationSlider {
	s := &pcpanelDurationSlider{Min: min, Max: max, Step: step, Value: min}
	s.ExtendBaseWidget(s)
	return s
}

func (s *pcpanelDurationSlider) SetDisabled(disabled bool) {
	s.disabled = disabled
	s.Refresh()
}

func (s *pcpanelDurationSlider) SetValue(value float64) {
	if value < s.Min {
		value = s.Min
	}
	if value > s.Max {
		value = s.Max
	}
	if value == s.Value {
		return
	}
	s.Value = value
	s.Refresh()
	if s.OnChanged != nil {
		s.OnChanged(s.Value)
	}
}

func (s *pcpanelDurationSlider) valueFraction() float32 {
	if s.Max <= s.Min {
		return 0
	}
	f := (s.Value - s.Min) / (s.Max - s.Min)
	if f < 0 {
		f = 0
	}
	if f > 1 {
		f = 1
	}
	return float32(f)
}

func (s *pcpanelDurationSlider) setValueFromX(x float32) {
	if s.disabled {
		return
	}
	usable := s.Size().Width - pcpanelSliderThumbRadius*2
	if usable <= 0 {
		return
	}
	rel := (x - pcpanelSliderThumbRadius) / usable
	if rel < 0 {
		rel = 0
	}
	if rel > 1 {
		rel = 1
	}
	value := s.Min + float64(rel)*(s.Max-s.Min)
	if s.Step > 0 {
		value = math.Round(value/s.Step) * s.Step
	}
	s.SetValue(value)
}

func (s *pcpanelDurationSlider) Tapped(e *fyne.PointEvent) {
	s.setValueFromX(e.Position.X)
}

func (s *pcpanelDurationSlider) TappedSecondary(*fyne.PointEvent) {}

func (s *pcpanelDurationSlider) Dragged(e *fyne.DragEvent) {
	s.setValueFromX(e.Position.X)
}

func (s *pcpanelDurationSlider) DragEnd() {}

func (s *pcpanelDurationSlider) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (s *pcpanelDurationSlider) MinSize() fyne.Size {
	return fyne.NewSize(120, pcpanelSliderHeight)
}

func (s *pcpanelDurationSlider) CreateRenderer() fyne.WidgetRenderer {
	s.track = canvas.NewRectangle(color.NRGBA{R: 0x31, G: 0x35, B: 0x39, A: 0xff})
	s.track.CornerRadius = pcpanelSliderTrackHeight / 2
	s.glow = canvas.NewCircle(color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0x33})
	s.thumb = canvas.NewCircle(design.ColorConnectionBadgeText)
	return &pcpanelDurationSliderRenderer{slider: s}
}

type pcpanelDurationSliderRenderer struct {
	slider *pcpanelDurationSlider
}

func (r *pcpanelDurationSliderRenderer) Layout(size fyne.Size) {
	s := r.slider
	trackY := (size.Height - pcpanelSliderTrackHeight) / 2
	s.track.Move(fyne.NewPos(0, trackY))
	s.track.Resize(fyne.NewSize(size.Width, pcpanelSliderTrackHeight))

	thumbRange := size.Width - pcpanelSliderThumbRadius*2
	if thumbRange < 0 {
		thumbRange = 0
	}
	cx := pcpanelSliderThumbRadius + s.valueFraction()*thumbRange
	cy := size.Height / 2

	s.glow.Move(fyne.NewPos(cx-pcpanelSliderGlowRadius, cy-pcpanelSliderGlowRadius))
	s.glow.Resize(fyne.NewSize(pcpanelSliderGlowRadius*2, pcpanelSliderGlowRadius*2))
	s.thumb.Move(fyne.NewPos(cx-pcpanelSliderThumbRadius, cy-pcpanelSliderThumbRadius))
	s.thumb.Resize(fyne.NewSize(pcpanelSliderThumbRadius*2, pcpanelSliderThumbRadius*2))
}

func (r *pcpanelDurationSliderRenderer) MinSize() fyne.Size {
	return r.slider.MinSize()
}

func (r *pcpanelDurationSliderRenderer) Refresh() {
	s := r.slider
	if s.disabled {
		s.track.FillColor = color.NRGBA{R: 0x31, G: 0x35, B: 0x39, A: 0x88}
		s.thumb.FillColor = pcpanelDialogHint
		s.glow.FillColor = color.Transparent
	} else {
		s.track.FillColor = color.NRGBA{R: 0x31, G: 0x35, B: 0x39, A: 0xff}
		s.thumb.FillColor = design.ColorConnectionBadgeText
		s.glow.FillColor = color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0x33}
	}
	s.track.Refresh()
	s.thumb.Refresh()
	s.glow.Refresh()
	r.Layout(s.Size())
	canvas.Refresh(s)
}

func (r *pcpanelDurationSliderRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

func (r *pcpanelDurationSliderRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.slider.track, r.slider.glow, r.slider.thumb}
}

func (r *pcpanelDurationSliderRenderer) Destroy() {}

// PCPanelWidget is a power button with activity indicator in the address bar
type PCPanelWidget struct {
	actionBtn     *pcpanelActionButton
	container     *fyne.Container
	usbClient     *api.USBClient
	stopPoll      chan struct{}
	pollMu        sync.Mutex
	pollCtxCancel context.CancelFunc // cancels any in-flight poll HTTP request
	powerOn       bool
	hddOn         bool
	window        fyne.Window
	agentOS       string // OS reported by the connected agent (empty/"usbridge" = real hardware); guarded by pollMu
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
	if c == nil {
		p.agentOS = ""
	}
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

	// Fetch the agent OS once so the power/reset popup can be locked down for
	// plain OS agents (Windows/Linux/macOS), for which it doesn't apply.
	go func() {
		info, err := c.GetDeviceInfo()
		if err != nil || info == nil {
			return
		}
		p.pollMu.Lock()
		if p.usbClient == c {
			p.agentOS = info.AgentOS
		}
		p.pollMu.Unlock()
	}()
}

// getAgentOS returns the last known agent OS string, guarded by pollMu.
func (p *PCPanelWidget) getAgentOS() string {
	p.pollMu.Lock()
	defer p.pollMu.Unlock()
	return p.agentOS
}

// SetAgentOS seeds power/reset lockout before the async GetDeviceInfo poll.
func (p *PCPanelWidget) SetAgentOS(osName string) {
	if p == nil {
		return
	}
	p.pollMu.Lock()
	p.agentOS = strings.TrimSpace(osName)
	p.pollMu.Unlock()
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

// ShowPowerMenu opens the same Power controls popup the header's own
// power/reset button used to trigger directly (see p.actionBtn) -- now the
// "Power Reset" entry in the gear-icon settings menu instead
// (gui.newHeaderSettingsMenuButton), since this widget's container no
// longer sits in the header at all. A no-op with nothing connected, exactly
// like the old button was: it just never rendered enabled/visible then.
func (p *PCPanelWidget) ShowPowerMenu() {
	p.onActionClick()
}

func (p *PCPanelWidget) showPowerActionDialog() {
	if p.window == nil {
		return
	}

	// Power/Reset simulate physically pressing the target machine's front
	// panel buttons via USBridge hardware — meaningless for a plain OS agent.
	locked := !isUSBridgeAgentOS(p.getAgentOS())

	actionTitles := map[string]string{
		"power": i18n.Current.PCPanelPowerOff,
		"reset": i18n.Current.PCPanelResetTitle,
	}

	hide := func(popup **widget.PopUp) {
		if popup != nil && *popup != nil {
			(*popup).Hide()
		}
	}
	var popup *widget.PopUp

	holdSlider := newPCPanelDurationSlider(0, 10, 1)
	durationValue := canvas.NewText("0s", pcpanelDialogValue)
	durationValue.TextSize = 10
	durationValue.TextStyle = fyne.TextStyle{Bold: true, Monospace: true}
	holdSlider.OnChanged = func(v float64) {
		durationValue.Text = fmt.Sprintf("%ds", int(v))
		durationValue.Refresh()
	}

	shortLabel := canvas.NewText(i18n.Current.PCPanelShortHold, pcpanelDialogHint)
	shortLabel.TextSize = pcpanelDialogHintTextSize
	longLabel := canvas.NewText(i18n.Current.PCPanelLongHold, pcpanelDialogHint)
	longLabel.TextSize = pcpanelDialogHintTextSize
	durationHints := container.NewBorder(nil, nil, shortLabel, longLabel, nil)
	durationHeader := container.NewBorder(nil, nil, pcpanelDialogFieldLabel(i18n.Current.PCPanelDuration), pcpanelDialogValuePill(durationValue), nil)
	durationCard := pcpanelDialogCard(pcpanelDialogCardBG, view.NewInset(container.NewVBox(durationHeader, holdSlider, durationHints), 14, 14, 10, 10))

	detailsContainer := container.NewVBox()

	powerBtn := newPCPanelModeButton(i18n.Current.PCPanelPowerOff, nil)
	resetBtn := newPCPanelModeButton(i18n.Current.PCPanelResetTitle, nil)
	modeButtons := container.New(&pcpanelModeButtonsLayout{gap: 8}, powerBtn, resetBtn)
	actionCard := pcpanelDialogCard(design.ColorGray950, view.NewInsetExact(modeButtons, 4, 4, 4, 4))

	var currentAction string
	var holdButton *pcpanelHoldButton
	updateDetails := func(action string) {
		currentAction = action

		if action == "power" {
			detailsContainer.Objects = []fyne.CanvasObject{pcpanelDialogVSpace(8), durationCard, pcpanelDialogVSpace(12)}
			powerBtn.SetActive(true)
			resetBtn.SetActive(false)
		} else {
			detailsContainer.Objects = nil
			powerBtn.SetActive(false)
			resetBtn.SetActive(true)
		}
		detailsContainer.Refresh()
		if holdButton != nil {
			holdButton.cancelHold()
		}
		if popup != nil {
			popup.Refresh()
		}
	}

	holdButton = newPCPanelHoldButton(i18n.Current.PCPanelHoldToConfirm, 2*time.Second, func() {
		client := p.usbClient
		if client == nil {
			return
		}
		hide(&popup)
		action := currentAction
		holdVal := int(holdSlider.Value)
		go func() {
			var err error
			switch action {
			case "power":
				err = client.PressPCPanelButton("power", holdVal)
			case "reset":
				err = client.PressPCPanelButton("reset", 0)
			}
			if err != nil {
				logrus.Errorf("PCPanel %s error: %v", actionTitles[action], err)
				fyne.Do(func() { view.ShowErrorDialog(err, p.window) })
			}
		}()
	})

	cancelBtn := newPCPanelCancelButton(i18n.Current.Cancel, func() {
		hide(&popup)
	})

	powerBtn.onTap = func() {
		updateDetails("power")
	}
	resetBtn.onTap = func() {
		updateDetails("reset")
	}

	closeBtn := newPCPanelDialogCloseButton(func() {
		hide(&popup)
	})

	if locked {
		powerBtn.SetDisabled(true)
		resetBtn.SetDisabled(true)
		holdSlider.SetDisabled(true)
		holdButton.SetDisabled(true)
	}

	title := view.NewBrandText(i18n.Current.PCPanelPowerControls, 13, design.ColorTextLight, true)
	subtitleLbl := widget.NewLabel(i18n.Current.PCPanelPowerHardwareOnly)
	subtitleLbl.Wrapping = fyne.TextWrapWord
	subtitleThemed := container.NewThemeOverride(subtitleLbl, &mutedForegroundTheme{design.NewBrandTheme()})
	nudgedSubtitle := container.New(&subtitleLeftNudgeLayout{Amount: 8}, subtitleThemed)
	titleCol := container.New(&tightHeaderVBoxLayout{Gap: -2}, title, nudgedSubtitle)
	headerBlock := container.New(&tightHeaderVBoxLayout{Gap: 0}, pcpanelDialogTopAccentBar(), view.NewInset(titleCol, 21, 44, 9, 4), pcpanelDialogHairline())

	bodyContent := container.NewVBox(
		pcpanelDialogFieldLabel(i18n.Current.PCPanelAction),
		actionCard,
		detailsContainer,
	)

	footerButtons := container.NewBorder(nil, nil, container.NewCenter(cancelBtn), holdButton)
	footerBlock := container.NewVBox(pcpanelDialogHairline(), view.NewInsetExact(footerButtons, 12, 18, 6, 0))
	form := container.NewBorder(headerBlock, footerBlock, nil, nil, view.NewInset(bodyContent, 18, 18, 12, 0))

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	cornerBtn := container.New(&pcpanelCornerButtonLayout{Top: 12, Right: 12}, closeBtn)
	panel := container.NewStack(
		bg,
		view.NewInsetExact(form, 0, 0, 0, 8),
		cornerBtn,
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
			panelWidth := minFloat32(maxFloat32(panelMin.Width, pcpanelDialogCanvasPanelWidth(canvasSize)), maxWidth)
			panelHeight := minFloat32(panelMin.Height, maxHeight)
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
			if client == nil {
				return
			}
			holdVal := int(holdSlider.Value)
			go func() {
				if err := client.PressPCPanelButton("power", holdVal); err != nil {
					logrus.Errorf("PCPanel Power error: %v", err)
					fyne.Do(func() { view.ShowErrorDialog(err, p.window) })
				}
			}()
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
			if client == nil {
				return
			}
			go func() {
				if err := client.PressPCPanelButton("reset", 0); err != nil {
					logrus.Errorf("PCPanel Reset error: %v", err)
					fyne.Do(func() { view.ShowErrorDialog(err, p.window) })
				}
			}()
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
	sz := p.window.Canvas().Size()
	if view.UseCompactLayout(sz.Width) {
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
		theme.ColorNameForeground, theme.ColorNamePlaceHolder,
		theme.ColorNameFocus, theme.ColorNameShadow:
		return color.Transparent
	case theme.ColorNamePrimary:
		return design.ColorConnectionBadgeText
	}
	return t.Theme.Color(name, variant)
}

const scriptEditorTextSize float32 = 11

func (t *transparentEntryTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return scriptEditorTextSize
	case theme.SizeNameInputBorder:
		return 0
	case theme.SizeNameInnerPadding:
		return 2
	case theme.SizeNamePadding:
		return 1
	}
	return t.Theme.Size(name)
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

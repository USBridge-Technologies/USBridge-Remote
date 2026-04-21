package view

import (
	"image/color"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

var (
	tailscaleControlBg          = color.NRGBA{R: 0x3a, G: 0x3a, B: 0x3a, A: 0xff}
	tailscaleControlSelectedBg  = color.NRGBA{R: 0x4b, G: 0x4b, B: 0x4b, A: 0xff}
	tailscaleControlHoverBg     = color.NRGBA{R: 0x46, G: 0x46, B: 0x46, A: 0xff}
	tailscaleControlDisabledBg  = color.NRGBA{R: 0x31, G: 0x31, B: 0x31, A: 0xff}
	tailscaleControlPadding     = float32(2)
	tailscaleControlSegmentGap  = float32(0)
	tailscaleControlTextSize    = float32(10)
	tailscaleControlSegmentPadX = float32(20)
)

type TailscaleModeSwitch struct {
	widget.BaseWidget

	OnChanged func(models.TailscaleMode)
	Selected  models.TailscaleMode
	Disabled  bool

	userspaceBtn *tailscaleModeButton
	systemBtn    *tailscaleModeButton
}

func NewTailscaleModeSwitch(selected models.TailscaleMode, onChanged func(models.TailscaleMode)) *TailscaleModeSwitch {
	s := &TailscaleModeSwitch{
		Selected:  selected,
		OnChanged: onChanged,
	}
	s.ExtendBaseWidget(s)

	s.userspaceBtn = newTailscaleModeButton("Userspace", "left", selected == models.TailscaleModeUserspace, func() {
		s.selectMode(models.TailscaleModeUserspace)
	})
	s.systemBtn = newTailscaleModeButton("System", "right", selected == models.TailscaleModeSystem, func() {
		s.selectMode(models.TailscaleModeSystem)
	})

	return s
}

func (s *TailscaleModeSwitch) selectMode(mode models.TailscaleMode) {
	if s.Disabled || s.Selected == mode {
		return
	}
	s.Selected = mode
	s.userspaceBtn.setSelected(mode == models.TailscaleModeUserspace)
	s.systemBtn.setSelected(mode == models.TailscaleModeSystem)
	if s.OnChanged != nil {
		s.OnChanged(mode)
	}
}

func (s *TailscaleModeSwitch) SetSelected(mode models.TailscaleMode) {
	s.Selected = mode
	if s.userspaceBtn != nil {
		s.userspaceBtn.setSelected(mode == models.TailscaleModeUserspace)
	}
	if s.systemBtn != nil {
		s.systemBtn.setSelected(mode == models.TailscaleModeSystem)
	}
}

func (s *TailscaleModeSwitch) SetDisabled(disabled bool) {
	s.Disabled = disabled
	if s.userspaceBtn != nil {
		s.userspaceBtn.setDisabled(disabled)
	}
	if s.systemBtn != nil {
		s.systemBtn.setDisabled(disabled)
	}
}

func (s *TailscaleModeSwitch) CreateRenderer() fyne.WidgetRenderer {
	content := container.New(&tailscaleModeSwitchLayout{overlap: 2}, s.userspaceBtn, s.systemBtn)
	return widget.NewSimpleRenderer(content)
}

func (s *TailscaleModeSwitch) MinSize() fyne.Size {
	uText := fyne.MeasureText(s.userspaceBtn.text, tailscaleControlTextSize, fyne.TextStyle{})
	sText := fyne.MeasureText(s.systemBtn.text, tailscaleControlTextSize, fyne.TextStyle{})
	segmentWidth := maxFloat32(uText.Width, sText.Width) + tailscaleControlSegmentPadX
	segmentHeight := maxFloat32(s.userspaceBtn.MinSize().Height, s.systemBtn.MinSize().Height)
	return fyne.NewSize(segmentWidth*2-2, segmentHeight)
}

type tailscaleModeSwitchLayout struct {
	overlap float32
}

func (l *tailscaleModeSwitchLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}

	left := objects[0]
	right := objects[1]
	overlap := l.overlap
	if overlap < 0 {
		overlap = 0
	}
	if overlap > size.Width {
		overlap = 0
	}

	width := (size.Width + overlap) / 2
	left.Move(fyne.NewPos(0, 0))
	left.Resize(fyne.NewSize(width, size.Height))

	rightX := size.Width - width
	if rightX < 0 {
		rightX = 0
	}
	right.Move(fyne.NewPos(rightX, 0))
	right.Resize(fyne.NewSize(width, size.Height))
}

func (l *tailscaleModeSwitchLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}

	leftMin := objects[0].MinSize()
	rightMin := objects[1].MinSize()
	width := leftMin.Width + rightMin.Width - l.overlap
	if width < 0 {
		width = 0
	}
	height := maxFloat32(leftMin.Height, rightMin.Height)
	return fyne.NewSize(width, height)
}

type tailscaleModeButton struct {
	widget.BaseWidget
	text     string
	side     string
	selected bool
	disabled bool
	hovered  bool
	onTap    func()

	bg       *canvas.Rectangle
	seamMask *canvas.Rectangle
	label    *canvas.Text
}

func newTailscaleModeButton(text string, side string, selected bool, onTap func()) *tailscaleModeButton {
	b := &tailscaleModeButton{
		text:     text,
		side:     side,
		selected: selected,
		onTap:    onTap,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *tailscaleModeButton) setSelected(selected bool) {
	b.selected = selected
	b.refreshVisuals()
}

func (b *tailscaleModeButton) setDisabled(disabled bool) {
	b.disabled = disabled
	b.refreshVisuals()
}

func (b *tailscaleModeButton) Tapped(*fyne.PointEvent) {
	if b.disabled || b.onTap == nil {
		return
	}
	b.onTap()
}

func (b *tailscaleModeButton) TappedSecondary(*fyne.PointEvent) {}

func (b *tailscaleModeButton) MouseIn(*desktop.MouseEvent) {
	if b.disabled {
		return
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *tailscaleModeButton) MouseMoved(*desktop.MouseEvent) {}

func (b *tailscaleModeButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *tailscaleModeButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(tailscaleControlBg)
	b.bg.CornerRadius = design.RadiusMD

	b.seamMask = canvas.NewRectangle(tailscaleControlBg)

	b.label = canvas.NewText(b.text, design.ColorTextMuted)
	b.label.TextSize = tailscaleControlTextSize
	b.label.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return &tailscaleModeButtonRenderer{button: b}
}

func (b *tailscaleModeButton) MinSize() fyne.Size {
	textMin := fyne.MeasureText(b.text, tailscaleControlTextSize, fyne.TextStyle{})
	return fyne.NewSize(textMin.Width+tailscaleControlSegmentPadX, 24)
}

func (b *tailscaleModeButton) refreshVisuals() {
	if b.bg == nil || b.label == nil || b.seamMask == nil {
		return
	}

	bgColor := color.Color(tailscaleControlBg)
	labelColor := design.ColorTextMuted

	if b.selected {
		bgColor = tailscaleControlSelectedBg
		labelColor = design.ColorTextLight
	} else if b.hovered {
		bgColor = tailscaleControlHoverBg
	}

	if b.disabled {
		if b.selected {
			bgColor = tailscaleControlDisabledBg
		}
		labelColor = design.ColorBorder
	}

	b.bg.FillColor = bgColor
	b.seamMask.FillColor = bgColor
	b.label.Color = labelColor
	b.bg.Refresh()
	b.seamMask.Refresh()
	b.label.Refresh()
}

type tailscaleModeButtonRenderer struct {
	button *tailscaleModeButton
}

func (r *tailscaleModeButtonRenderer) Layout(size fyne.Size) {
	if r.button.bg == nil || r.button.seamMask == nil || r.button.label == nil {
		return
	}

	r.button.bg.Move(fyne.NewPos(0, 0))
	r.button.bg.Resize(size)

	maskWidth := float32(8)
	if maskWidth > size.Width/2 {
		maskWidth = size.Width / 2
	}

	maskX := float32(0)
	if r.button.side == "left" {
		maskX = size.Width - maskWidth
	}
	r.button.seamMask.Move(fyne.NewPos(maskX, 0))
	r.button.seamMask.Resize(fyne.NewSize(maskWidth, size.Height))

	labelSize := r.button.label.MinSize()
	r.button.label.Move(fyne.NewPos((size.Width-labelSize.Width)/2, (size.Height-labelSize.Height)/2))
	r.button.label.Resize(labelSize)
}

func (r *tailscaleModeButtonRenderer) MinSize() fyne.Size {
	return r.button.MinSize()
}

func (r *tailscaleModeButtonRenderer) Refresh() {
	r.button.refreshVisuals()
	r.Layout(r.button.Size())
}

func (r *tailscaleModeButtonRenderer) Destroy() {}

func (r *tailscaleModeButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.button.bg, r.button.seamMask, r.button.label}
}

func (r *tailscaleModeButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

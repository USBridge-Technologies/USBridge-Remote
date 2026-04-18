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

	s.userspaceBtn = newTailscaleModeButton("Userspace", selected == models.TailscaleModeUserspace, func() {
		s.selectMode(models.TailscaleModeUserspace)
	})
	s.systemBtn = newTailscaleModeButton("System", selected == models.TailscaleModeSystem, func() {
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
	content := container.NewGridWithColumns(2, s.userspaceBtn, s.systemBtn)
	
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	
	return widget.NewSimpleRenderer(container.NewStack(bg, content))
}

func (s *TailscaleModeSwitch) MinSize() fyne.Size {
	uMin := s.userspaceBtn.MinSize()
	sMin := s.systemBtn.MinSize()
	return fyne.NewSize(uMin.Width+sMin.Width, maxFloat32(uMin.Height, sMin.Height))
}

type tailscaleModeButton struct {
	widget.BaseWidget
	text     string
	selected bool
	disabled bool
	hovered  bool
	onTap    func()

	bg    *canvas.Rectangle
	label *canvas.Text
}

func newTailscaleModeButton(text string, selected bool, onTap func()) *tailscaleModeButton {
	b := &tailscaleModeButton{
		text:     text,
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
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = design.RadiusMD
	
	b.label = canvas.NewText(b.text, design.ColorTextMuted)
	b.label.TextSize = 10
	b.label.Alignment = fyne.TextAlignCenter
	
	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewStack(b.bg, container.NewCenter(b.label)))
}

func (b *tailscaleModeButton) MinSize() fyne.Size {
	textMin := fyne.MeasureText(b.text, 10, fyne.TextStyle{})
	return fyne.NewSize(textMin.Width+16, 24)
}

func (b *tailscaleModeButton) refreshVisuals() {
	if b.bg == nil || b.label == nil {
		return
	}
	
	bgColor := color.Color(color.Transparent)
	labelColor := design.ColorTextMuted
	
	if b.selected {
		bgColor = design.ColorSurfaceLight
		labelColor = design.ColorTextLight
	} else if b.hovered {
		bgColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x08}
	}
	
	if b.disabled && !b.selected {
		labelColor = design.ColorBorder
	}
	
	b.bg.FillColor = bgColor
	b.label.Color = labelColor
	b.bg.Refresh()
	b.label.Refresh()
}

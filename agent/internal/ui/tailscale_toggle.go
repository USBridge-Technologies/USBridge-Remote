package ui

import (
	"image/color"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

// tailscaleHeaderToggle is the header Tailscale pill: label + switch, and
// when signed in, the Tailscale IP plus a person-count chip that opens the
// details dialog (the former main-window Tailscale card).
type tailscaleHeaderToggle struct {
	widget.BaseWidget

	onTapped func()
	onPeople func()
	// onSecondary: right click / long press -- the control server choice
	// (Tailscale or the owner's own Headscale).
	onSecondary func()
	on          bool
	loading     bool
	disabled    bool
	hovered     bool
	peopleHover bool
	ip          string
	peers       int

	peopleHitX float32
	peopleHitW float32
	switchHitX float32
	switchHitW float32

	bg         *canvas.Rectangle
	border     *canvas.Rectangle
	label      *canvas.Text
	track      *canvas.Rectangle
	thumb      *canvas.Circle
	ipText     *canvas.Text
	peopleBg   *canvas.Rectangle
	peopleIcon *canvas.Image
	peopleNum  *canvas.Text
}

func newTailscaleHeaderToggle(onTapped, onPeople func()) *tailscaleHeaderToggle {
	t := &tailscaleHeaderToggle{onTapped: onTapped, onPeople: onPeople}
	t.ExtendBaseWidget(t)
	return t
}

func (t *tailscaleHeaderToggle) SetOn(on bool) {
	t.on = on
	if !on {
		t.ip = ""
		t.peers = 0
	}
	t.refreshVisuals()
	t.Refresh()
}

func (t *tailscaleHeaderToggle) SetIP(ip string) {
	t.ip = ip
	t.Refresh()
}

func (t *tailscaleHeaderToggle) SetPeerCount(n int) {
	if n < 0 {
		n = 0
	}
	t.peers = n
	t.Refresh()
}

func (t *tailscaleHeaderToggle) SetLoading(loading bool) {
	t.loading = loading
	if loading {
		t.hovered = false
	}
	t.refreshVisuals()
	t.Refresh()
}

func (t *tailscaleHeaderToggle) detailsVisible() bool {
	return t.on && t.ip != ""
}

func (t *tailscaleHeaderToggle) Tapped(e *fyne.PointEvent) {
	if t.disabled || t.loading {
		return
	}
	if e != nil && t.detailsVisible() && t.peopleHitW > 0 &&
		e.Position.X >= t.peopleHitX && e.Position.X < t.peopleHitX+t.peopleHitW {
		if t.onPeople != nil {
			t.onPeople()
		}
		return
	}
	if t.onTapped == nil {
		return
	}
	if e != nil && t.switchHitW > 0 &&
		(e.Position.X < t.switchHitX || e.Position.X >= t.switchHitX+t.switchHitW) {
		return
	}
	t.onTapped()
}

func (t *tailscaleHeaderToggle) TappedSecondary(*fyne.PointEvent) {
	if t.onSecondary != nil && !t.loading {
		t.onSecondary()
	}
}

func (t *tailscaleHeaderToggle) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (t *tailscaleHeaderToggle) MouseMoved(e *desktop.MouseEvent) {
	over := false
	if e != nil && !t.disabled && !t.loading && t.detailsVisible() && t.peopleHitW > 0 {
		over = e.Position.X >= t.peopleHitX && e.Position.X < t.peopleHitX+t.peopleHitW
	}
	if t.peopleHover == over {
		return
	}
	t.peopleHover = over
	t.refreshPeopleHover()
}

func (t *tailscaleHeaderToggle) MouseOut() {
	noteChromeHoverOut()
	if t.peopleHover {
		t.peopleHover = false
		t.refreshPeopleHover()
	}
}

func (t *tailscaleHeaderToggle) Cursor() desktop.Cursor { return desktop.DefaultCursor }

func (t *tailscaleHeaderToggle) MinSize() fyne.Size {
	const h float32 = 24
	if !t.detailsVisible() {
		return fyne.NewSize(92, h)
	}
	ipW := fyne.MeasureText(t.ip, 10, fyne.TextStyle{Monospace: true}).Width
	numW := fyne.MeasureText(strconv.Itoa(t.peers), 10, fyne.TextStyle{Bold: true}).Width
	peopleW := 6 + 12 + 3 + numW + 6
	// 10 + Tailscale 55 + 4 + track 24 + 8 + ip + 6 + people + 8
	w := float32(10+55+4+24+8) + ipW + 6 + peopleW + 8
	return fyne.NewSize(w, h)
}

func (t *tailscaleHeaderToggle) CreateRenderer() fyne.WidgetRenderer {
	t.bg = canvas.NewRectangle(design.ColorGray950)
	t.bg.CornerRadius = 12

	t.border = canvas.NewRectangle(color.Transparent)
	t.border.CornerRadius = 12
	t.border.StrokeColor = design.ColorTailscaleChipBorder
	t.border.StrokeWidth = 1

	t.label = canvas.NewText("Tailscale", design.ColorTailscaleChipLabel)
	t.label.TextSize = 10
	t.label.TextStyle = fyne.TextStyle{Bold: true}
	t.label.Alignment = fyne.TextAlignLeading

	t.track = canvas.NewRectangle(design.ColorGray900)
	t.track.CornerRadius = 7

	t.thumb = canvas.NewCircle(design.ColorGray400)

	t.ipText = canvas.NewText("", design.ColorAddress)
	t.ipText.TextSize = 10
	t.ipText.TextStyle = fyne.TextStyle{Monospace: true}

	t.peopleBg = canvas.NewRectangle(color.Transparent)
	t.peopleBg.CornerRadius = 8
	t.peopleBg.StrokeWidth = 0

	t.peopleIcon = canvas.NewImageFromResource(theme.NewColoredResource(theme.AccountIcon(), design.ColorNameMutedOlive))
	t.peopleIcon.FillMode = canvas.ImageFillStretch
	t.peopleIcon.SetMinSize(fyne.NewSize(12, 12))

	t.peopleNum = canvas.NewText("0", design.ColorMutedOlive)
	t.peopleNum.TextSize = 10
	t.peopleNum.TextStyle = fyne.TextStyle{Bold: true}

	t.refreshVisuals()
	return &tailscaleHeaderToggleRenderer{toggle: t}
}

func (t *tailscaleHeaderToggle) refreshVisuals() {
	if t.bg == nil || t.border == nil || t.label == nil || t.track == nil || t.thumb == nil {
		return
	}

	bgColor := design.ColorGray950
	borderColor := design.ColorTailscaleChipBorder
	labelColor := design.ColorTailscaleChipLabel
	trackColor := design.ColorGray900
	thumbColor := design.ColorGray400

	if t.on {
		trackColor = design.ColorCTA
		thumbColor = design.ColorWhite
	}
	if t.disabled || t.loading {
		labelColor = design.ColorGray400
		trackColor = design.ColorGray950
		borderColor = design.ColorGray900
		thumbColor = design.ColorGray900
	}

	t.bg.FillColor = bgColor
	t.border.StrokeColor = borderColor
	t.border.StrokeWidth = 1
	t.label.Color = labelColor
	t.track.FillColor = trackColor
	t.thumb.FillColor = thumbColor

	t.bg.Refresh()
	t.border.Refresh()
	t.label.Refresh()
	t.track.Refresh()
	t.thumb.Refresh()

	if t.ipText != nil {
		t.ipText.Text = t.ip
		t.ipText.Refresh()
	}
	if t.peopleNum != nil {
		t.peopleNum.Text = strconv.Itoa(t.peers)
		t.refreshPeopleHover()
	}
	show := t.detailsVisible()
	if t.ipText != nil {
		if show {
			t.ipText.Show()
		} else {
			t.ipText.Hide()
		}
	}
	if t.peopleBg != nil {
		if show {
			t.peopleBg.Show()
			t.peopleIcon.Show()
			t.peopleNum.Show()
		} else {
			t.peopleBg.Hide()
			t.peopleIcon.Hide()
			t.peopleNum.Hide()
		}
	}
}

func (t *tailscaleHeaderToggle) refreshPeopleHover() {
	if t.peopleNum == nil || t.peopleIcon == nil {
		return
	}
	tint := design.ColorNameMutedOlive
	num := design.ColorMutedOlive
	if t.peopleHover {
		tint = theme.ColorNameForeground
		num = design.ColorTextLight
	}
	t.peopleNum.Color = num
	t.peopleNum.Refresh()
	t.peopleIcon.Resource = theme.NewColoredResource(theme.AccountIcon(), tint)
	t.peopleIcon.Refresh()
}

type tailscaleHeaderToggleRenderer struct {
	toggle *tailscaleHeaderToggle
}

func (r *tailscaleHeaderToggleRenderer) Layout(size fyne.Size) {
	t := r.toggle
	if t.bg == nil || t.border == nil || t.label == nil || t.track == nil || t.thumb == nil {
		return
	}

	t.bg.Move(fyne.NewPos(0, 0))
	t.bg.Resize(size)
	t.border.Move(fyne.NewPos(0, 0))
	t.border.Resize(size)

	labelH := float32(14)
	t.label.Move(fyne.NewPos(10, (size.Height-labelH)/2))
	t.label.Resize(fyne.NewSize(55, labelH))

	trackSize := fyne.NewSize(24, 14)
	trackX := float32(10 + 55 + 4)
	trackY := (size.Height - trackSize.Height) / 2
	t.track.Move(fyne.NewPos(trackX, trackY))
	t.track.Resize(trackSize)
	t.switchHitX = trackX - 4
	t.switchHitW = trackSize.Width + 8

	thumbSize := float32(10)
	thumbPad := float32(2)
	thumbY := trackY + thumbPad
	thumbX := trackX + thumbPad
	if t.on {
		thumbX = trackX + trackSize.Width - thumbSize - thumbPad
	}
	t.thumb.Move(fyne.NewPos(thumbX, thumbY))
	t.thumb.Resize(fyne.NewSize(thumbSize, thumbSize))

	if t.ipText == nil || t.peopleBg == nil {
		return
	}
	if !t.detailsVisible() {
		t.peopleHitX = 0
		t.peopleHitW = 0
		return
	}

	ipX := trackX + trackSize.Width + 8
	ipW := fyne.MeasureText(t.ip, 10, fyne.TextStyle{Monospace: true}).Width
	t.ipText.Move(fyne.NewPos(ipX, (size.Height-labelH)/2))
	t.ipText.Resize(fyne.NewSize(ipW, labelH))

	numW := fyne.MeasureText(t.peopleNum.Text, 10, fyne.TextStyle{Bold: true}).Width
	peopleH := float32(18)
	peopleW := 6 + 12 + 3 + numW + 6
	peopleX := ipX + ipW + 6
	peopleY := (size.Height - peopleH) / 2
	t.peopleBg.Move(fyne.NewPos(peopleX, peopleY))
	t.peopleBg.Resize(fyne.NewSize(peopleW, peopleH))
	iconY := peopleY + (peopleH-12)/2
	placeSquareIcon(t.peopleIcon, fyne.NewPos(peopleX+6, iconY), 12)
	t.peopleNum.Move(fyne.NewPos(peopleX+6+12+3, (size.Height-labelH)/2))
	t.peopleNum.Resize(fyne.NewSize(numW, labelH))
	t.peopleHitX = peopleX
	t.peopleHitW = peopleW
}

func (r *tailscaleHeaderToggleRenderer) MinSize() fyne.Size { return r.toggle.MinSize() }

func (r *tailscaleHeaderToggleRenderer) Refresh() {
	r.toggle.refreshVisuals()
	r.Layout(r.toggle.Size())
}

func (r *tailscaleHeaderToggleRenderer) Destroy() {}

func (r *tailscaleHeaderToggleRenderer) Objects() []fyne.CanvasObject {
	t := r.toggle
	return []fyne.CanvasObject{
		t.bg, t.label, t.track, t.thumb, t.ipText,
		t.peopleBg, t.peopleIcon, t.peopleNum, t.border,
	}
}

func (r *tailscaleHeaderToggleRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

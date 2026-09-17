package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

const (
	loginAvatarSide       float32 = 24
	loginAvatarHit        float32 = 28
	loginAvatarLetterSize float32 = 11
)

// loginAvatarButton is the header account chip — same circle+letter as the
// client's connection header: gray "U" while logged out, teal + first
// letter of the email while logged in.
type loginAvatarButton struct {
	widget.BaseWidget
	letterText string
	onTapped   func()
	hovered    bool
	loggedIn   bool
	circle     *canvas.Circle
	letter     *canvas.Text
}

func newLoginAvatarButton(onTapped func()) *loginAvatarButton {
	b := &loginAvatarButton{letterText: "U", onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *loginAvatarButton) MinSize() fyne.Size {
	return fyne.NewSize(loginAvatarSide, loginAvatarSide)
}

func (b *loginAvatarButton) CreateRenderer() fyne.WidgetRenderer {
	b.circle = canvas.NewCircle(design.ColorLoginAvatarBg)
	b.circle.StrokeColor = design.ColorChromeOlive
	b.circle.StrokeWidth = 1.5

	b.letter = canvas.NewText(b.letterText, design.ColorLoginAvatarText)
	b.letter.TextSize = loginAvatarLetterSize
	b.letter.TextStyle = fyne.TextStyle{Bold: true}
	b.letter.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return &loginAvatarButtonRenderer{btn: b}
}

func (b *loginAvatarButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *loginAvatarButton) TappedSecondary(*fyne.PointEvent) {}

func (b *loginAvatarButton) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	b.hovered = true
	b.refreshVisuals()
}

func (b *loginAvatarButton) MouseMoved(*desktop.MouseEvent) {}

func (b *loginAvatarButton) MouseOut() {
	noteChromeHoverOut()
	b.hovered = false
	b.refreshVisuals()
}

func (b *loginAvatarButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *loginAvatarButton) SetState(loggedIn bool, email string) {
	b.loggedIn = loggedIn
	letter := "U"
	if loggedIn {
		if trimmed := strings.TrimSpace(email); trimmed != "" {
			letter = strings.ToUpper(string([]rune(trimmed)[0]))
		}
	}
	b.letterText = letter
	if b.letter != nil {
		b.letter.Text = letter
		b.letter.Refresh()
	}
	b.refreshVisuals()
}

func (b *loginAvatarButton) refreshVisuals() {
	if b.circle == nil {
		return
	}
	fill := design.ColorLoginAvatarBg
	letterColor := design.ColorLoginAvatarText
	if b.loggedIn {
		fill = design.ColorCTA
		letterColor = design.ColorCTALabel
		if b.hovered {
			fill = design.ColorCTAHover
		}
	} else if b.hovered {
		fill = design.ColorSurfaceLight
	}
	b.circle.FillColor = fill
	b.circle.Refresh()
	if b.letter != nil {
		b.letter.Color = letterColor
		b.letter.Refresh()
	}
}

type loginAvatarButtonRenderer struct {
	btn *loginAvatarButton
}

func (r *loginAvatarButtonRenderer) Layout(size fyne.Size) {
	if r.btn.circle == nil || r.btn.letter == nil {
		return
	}
	r.btn.circle.Move(fyne.NewPos(0, 0))
	r.btn.circle.Resize(size)

	min := r.btn.letter.MinSize()
	x := (size.Width - min.Width) / 2
	y := (size.Height-min.Height)/2 - 1
	r.btn.letter.Move(fyne.NewPos(x, y))
	r.btn.letter.Resize(min)
}

func (r *loginAvatarButtonRenderer) MinSize() fyne.Size { return r.btn.MinSize() }

func (r *loginAvatarButtonRenderer) Refresh() {
	r.btn.refreshVisuals()
	r.Layout(r.btn.Size())
}

func (r *loginAvatarButtonRenderer) Destroy() {}

func (r *loginAvatarButtonRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.btn.circle, r.btn.letter}
}

func (r *loginAvatarButtonRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

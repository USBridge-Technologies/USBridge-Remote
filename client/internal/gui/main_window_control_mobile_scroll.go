package gui

import (
	"image/color"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// mobileFooterActionScroller is the Control footer action cluster on phones:
// a horizontal scroll of icons with small chevrons that appear when more
// content sits off-screen. Lives to the right of the burger so the menu
// button never scrolls under the icons.
type mobileFooterActionScroller struct {
	widget.BaseWidget
	scroll      *container.Scroll
	leftArrow   *mobileFooterScrollArrow
	rightArrow  *mobileFooterScrollArrow
	content     fyne.CanvasObject
	step        float32
}

func newMobileFooterActionScroller(content fyne.CanvasObject) *mobileFooterActionScroller {
	s := &mobileFooterActionScroller{content: content, step: 72}
	s.ExtendBaseWidget(s)
	s.scroll = container.NewHScroll(content)
	s.scroll.Direction = container.ScrollHorizontalOnly
	s.leftArrow = newMobileFooterScrollArrow(true, func() {
		s.nudge(-s.step)
	})
	s.rightArrow = newMobileFooterScrollArrow(false, func() {
		s.nudge(s.step)
	})
	s.scroll.OnScrolled = func(fyne.Position) {
		s.refreshArrows()
	}
	return s
}

func (s *mobileFooterActionScroller) nudge(delta float32) {
	if s.scroll == nil {
		return
	}
	off := s.scroll.Offset
	off.X += delta
	if off.X < 0 {
		off.X = 0
	}
	s.scroll.Offset = off
	s.scroll.Refresh()
	s.refreshArrows()
}

func (s *mobileFooterActionScroller) refreshArrows() {
	if s.scroll == nil || s.content == nil {
		return
	}
	viewW := s.scroll.Size().Width
	contentW := s.content.MinSize().Width
	if viewW <= 0 {
		viewW = s.Size().Width
	}
	overflow := contentW > viewW+1
	leftOn := overflow && s.scroll.Offset.X > 1
	rightOn := overflow && s.scroll.Offset.X+viewW < contentW-1
	if s.leftArrow != nil {
		if leftOn {
			s.leftArrow.Show()
		} else {
			s.leftArrow.Hide()
		}
		s.leftArrow.Refresh()
	}
	if s.rightArrow != nil {
		if rightOn {
			s.rightArrow.Show()
		} else {
			s.rightArrow.Hide()
		}
		s.rightArrow.Refresh()
	}
}

func (s *mobileFooterActionScroller) CreateRenderer() fyne.WidgetRenderer {
	return &mobileFooterActionScrollerRenderer{s: s}
}

func (s *mobileFooterActionScroller) MinSize() fyne.Size {
	s.ExtendBaseWidget(s)
	h := float32(32)
	if s.content != nil {
		if ch := s.content.MinSize().Height; ch > h {
			h = ch
		}
	}
	// Prefer filling remaining footer width; MinSize width stays small so
	// Border/layout can expand the scroller into leftover space.
	return fyne.NewSize(48, h)
}

type mobileFooterActionScrollerRenderer struct {
	s *mobileFooterActionScroller
}

func (r *mobileFooterActionScrollerRenderer) Layout(size fyne.Size) {
	s := r.s
	arrowW := float32(18)
	contentW := float32(0)
	if s.content != nil {
		contentW = s.content.MinSize().Width
	}
	if contentW > 0 && contentW <= size.Width {
		// Fits: pin the cluster to the right (same as the old footer pack).
		s.scroll.Offset = fyne.NewPos(0, 0)
		s.scroll.Move(fyne.NewPos(size.Width-contentW, 0))
		s.scroll.Resize(fyne.NewSize(contentW, size.Height))
	} else {
		s.scroll.Move(fyne.NewPos(0, 0))
		s.scroll.Resize(size)
	}
	s.leftArrow.Resize(fyne.NewSize(arrowW, size.Height))
	s.leftArrow.Move(fyne.NewPos(0, 0))
	s.rightArrow.Resize(fyne.NewSize(arrowW, size.Height))
	s.rightArrow.Move(fyne.NewPos(size.Width-arrowW, 0))
	s.refreshArrows()
}

func (r *mobileFooterActionScrollerRenderer) MinSize() fyne.Size {
	return r.s.MinSize()
}

func (r *mobileFooterActionScrollerRenderer) Refresh() {
	r.s.refreshArrows()
	canvas.Refresh(r.s)
}

func (r *mobileFooterActionScrollerRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.s.scroll, r.s.leftArrow, r.s.rightArrow}
}

func (r *mobileFooterActionScrollerRenderer) Destroy() {}

type mobileFooterScrollArrow struct {
	widget.BaseWidget
	left     bool
	onTap    func()
	hovered  bool
	label    *canvas.Text
	bg       *canvas.Rectangle
}

func newMobileFooterScrollArrow(left bool, onTap func()) *mobileFooterScrollArrow {
	a := &mobileFooterScrollArrow{left: left, onTap: onTap}
	a.ExtendBaseWidget(a)
	a.Hide()
	return a
}

func (a *mobileFooterScrollArrow) CreateRenderer() fyne.WidgetRenderer {
	glyph := "›"
	if a.left {
		glyph = "‹"
	}
	a.label = canvas.NewText(glyph, design.ColorConnectionBadgeText)
	a.label.TextSize = 16
	a.label.TextStyle = fyne.TextStyle{Bold: true}
	a.label.Alignment = fyne.TextAlignCenter
	a.bg = canvas.NewRectangle(color.NRGBA{R: 0x12, G: 0x14, B: 0x16, A: 0xe6})
	a.bg.CornerRadius = 4
	return widget.NewSimpleRenderer(container.NewStack(a.bg, a.label))
}

func (a *mobileFooterScrollArrow) MinSize() fyne.Size {
	return fyne.NewSize(18, 32)
}

func (a *mobileFooterScrollArrow) Tapped(*fyne.PointEvent) {
	if a.onTap != nil {
		a.onTap()
	}
}

func (a *mobileFooterScrollArrow) TappedSecondary(*fyne.PointEvent) {}

func (a *mobileFooterScrollArrow) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (a *mobileFooterScrollArrow) MouseIn(*desktop.MouseEvent) {
	a.hovered = true
	if a.label != nil {
		a.label.Color = design.ColorTextLight
		a.label.Refresh()
	}
}

func (a *mobileFooterScrollArrow) MouseOut() {
	a.hovered = false
	if a.label != nil {
		a.label.Color = design.ColorConnectionBadgeText
		a.label.Refresh()
	}
}

func (a *mobileFooterScrollArrow) MouseMoved(*desktop.MouseEvent) {}

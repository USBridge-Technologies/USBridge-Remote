package view

import "fyne.io/fyne/v2"

// touchSlop is how far a finger may wander on a control before the touch
// counts as a scroll. Fyne's mobile driver starts a drag past 4dp -- less
// than a fingertip wobbles -- and the list under a toggle scrolled instead
// of the toggle getting its tap.
const touchSlop = 14

// tapSlop gives a control on a phone a fingertip's worth of slop: the
// control is Draggable, so Fyne hands it the drag instead of the list under
// it. Within touchSlop the touch is still the control's tap; past it the
// drag goes on to scroll, as it would have gone without the control.
type tapSlop struct {
	scroll  fyne.Draggable
	moved   fyne.Delta
	passing bool
}

func (s *tapSlop) dragged(e *fyne.DragEvent) {
	if s.passing {
		if s.scroll != nil {
			s.scroll.Dragged(e)
		}
		return
	}
	s.moved.DX += e.Dragged.DX
	s.moved.DY += e.Dragged.DY
	if abs32(s.moved.DX) <= touchSlop && abs32(s.moved.DY) <= touchSlop {
		return
	}
	s.passing = true
	if s.scroll != nil {
		ev := *e
		ev.Dragged = s.moved
		s.scroll.Dragged(&ev)
	}
}

// dragEnd reports whether the touch was a tap.
func (s *tapSlop) dragEnd() bool {
	tap := !s.passing
	if s.passing && s.scroll != nil {
		s.scroll.DragEnd()
	}
	s.moved, s.passing = fyne.Delta{}, false
	return tap
}

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

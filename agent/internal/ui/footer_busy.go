package ui

import (
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/assets"
	"usbridge_agent/internal/ui/design"
)

const footerBusySpinnerSize float32 = 14
const footerBusySpinnerInterval = 140 * time.Millisecond

// footerBusyHint is the agent footer's muted-olive dot spinner + status copy.
type footerBusyHint struct {
	widget.BaseWidget

	hint  string
	label *canvas.Text
	box   *fyne.Container

	mu     sync.Mutex
	stop   chan struct{}
	active bool
	img    *canvas.Image
}

func newFooterBusyHint(hint string) *footerBusyHint {
	s := &footerBusyHint{hint: hint}
	s.ExtendBaseWidget(s)
	s.label = canvas.NewText(hint, design.ColorMutedOlive)
	s.label.TextSize = 9
	s.Hide()
	return s
}

func (s *footerBusyHint) Start() {
	s.mu.Lock()
	if s.active {
		s.mu.Unlock()
		return
	}
	s.active = true
	stop := make(chan struct{})
	s.stop = stop
	s.mu.Unlock()

	s.Show()
	s.Refresh()
	frames := assets.LoadingMutedFrames
	if s.img != nil && len(frames) > 0 {
		s.img.Resource = frames[0]
		s.img.Refresh()
	}

	go func() {
		ticker := time.NewTicker(footerBusySpinnerInterval)
		defer ticker.Stop()
		frame := 0
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if len(frames) == 0 {
					continue
				}
				frame = (frame + 1) % len(frames)
				res := frames[frame]
				fyne.Do(func() {
					s.mu.Lock()
					active := s.active
					s.mu.Unlock()
					if !active || s.img == nil {
						return
					}
					s.img.Resource = res
					s.img.Refresh()
				})
			}
		}
	}()
}

func (s *footerBusyHint) Stop() {
	s.mu.Lock()
	if !s.active {
		s.mu.Unlock()
		return
	}
	s.active = false
	stop := s.stop
	s.stop = nil
	s.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	s.Hide()
	s.Refresh()
}

func (s *footerBusyHint) MinSize() fyne.Size {
	if s.box != nil {
		return s.box.MinSize()
	}
	base := fyne.NewSize(footerBusySpinnerSize, footerBusySpinnerSize)
	textSize := s.label.MinSize()
	return fyne.NewSize(base.Width+6+textSize.Width, fyne.Max(base.Height, textSize.Height))
}

func (s *footerBusyHint) CreateRenderer() fyne.WidgetRenderer {
	s.img = canvas.NewImageFromResource(nil)
	s.img.FillMode = canvas.ImageFillContain
	s.img.SetMinSize(fyne.NewSize(footerBusySpinnerSize, footerBusySpinnerSize))
	if len(assets.LoadingMutedFrames) > 0 {
		s.img.Resource = assets.LoadingMutedFrames[0]
	}
	s.box = container.New(&tightHBoxLayout{gap: 6}, s.img, s.label)
	return widget.NewSimpleRenderer(s.box)
}

func (w *Window) startProtocolBusy() {
	if w.protocolBusy == nil {
		return
	}
	w.protocolBusy.Start()
}

func (w *Window) stopProtocolBusy() {
	if w.protocolBusy == nil {
		return
	}
	w.protocolBusy.Stop()
}

func (w *Window) finishProtocolSwitch() {
	w.stopProtocolBusy()
	if w.token != nil {
		w.syncProtocolPicker(w.token.EntitlementStatus())
	}
	w.performRefresh()
}

// footerPinEndsLayout pins the first child left (if visible) and the last
// child right, so the version tag stays on the right when the spinner hides.
type footerPinEndsLayout struct{}

func (l *footerPinEndsLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	if right := objects[len(objects)-1]; right != nil && right.Visible() {
		rs := right.MinSize()
		rh := rs.Height
		if rh > size.Height {
			rh = size.Height
		}
		right.Resize(fyne.NewSize(rs.Width, rh))
		right.Move(fyne.NewPos(size.Width-rs.Width, (size.Height-rh)/2))
	}
	if len(objects) < 2 {
		return
	}
	if left := objects[0]; left != nil && left.Visible() {
		ls := left.MinSize()
		lh := ls.Height
		if lh > size.Height {
			lh = size.Height
		}
		left.Resize(fyne.NewSize(ls.Width, lh))
		left.Move(fyne.NewPos(0, (size.Height-lh)/2))
	}
}

func (l *footerPinEndsLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var w, h float32
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		w += min.Width
		if min.Height > h {
			h = min.Height
		}
	}
	return fyne.NewSize(w, h)
}

type footerTextButton struct {
	widget.BaseWidget
	label   string
	hovered bool
	onTap   func()
	text    *canvas.Text
}

func newFooterTextButton(label string, onTap func()) *footerTextButton {
	b := &footerTextButton{label: label, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *footerTextButton) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *footerTextButton) TappedSecondary(*fyne.PointEvent) {}

func (b *footerTextButton) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	b.hovered = true
	b.Refresh()
}

func (b *footerTextButton) MouseOut() {
	noteChromeHoverOut()
	b.hovered = false
	b.Refresh()
}

func (b *footerTextButton) MouseMoved(*desktop.MouseEvent) {}

func (b *footerTextButton) Cursor() desktop.Cursor { return desktop.PointerCursor }

func (b *footerTextButton) MinSize() fyne.Size {
	t := canvas.NewText(b.label, design.ColorMutedOlive)
	t.TextSize = 9
	return t.MinSize()
}

func (b *footerTextButton) CreateRenderer() fyne.WidgetRenderer {
	b.text = canvas.NewText(b.label, design.ColorMutedOlive)
	b.text.TextSize = 9
	return &footerTextButtonRenderer{btn: b, objects: []fyne.CanvasObject{b.text}}
}

type footerTextButtonRenderer struct {
	btn     *footerTextButton
	objects []fyne.CanvasObject
}

func (r *footerTextButtonRenderer) Layout(size fyne.Size) {
	if r.btn.text == nil {
		return
	}
	ts := r.btn.text.MinSize()
	r.btn.text.Resize(ts)
	r.btn.text.Move(fyne.NewPos(0, (size.Height-ts.Height)/2))
}

func (r *footerTextButtonRenderer) MinSize() fyne.Size { return r.btn.MinSize() }

func (r *footerTextButtonRenderer) Refresh() {
	if r.btn.text == nil {
		return
	}
	r.btn.text.Text = r.btn.label
	r.btn.text.Color = design.ColorMutedOlive
	if r.btn.hovered {
		r.btn.text.Color = design.ColorTextLight
	}
	r.btn.text.Refresh()
	r.Layout(r.btn.Size())
}

func (r *footerTextButtonRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *footerTextButtonRenderer) Destroy()                     {}

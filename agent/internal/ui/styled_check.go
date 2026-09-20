package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
)

var (
	checkGlyphLime = fyne.NewStaticResource("check-lime.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c4e77a" stroke-width="3.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12L10 18L20 6"/></svg>`))
	checkGlyphOnTeal = fyne.NewStaticResource("check-on-teal.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#111111" stroke-width="3.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12L10 18L20 6"/></svg>`))
	crossGlyphRed = fyne.NewStaticResource("cross-red.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#fda4af" stroke-width="3.6" stroke-linecap="round" stroke-linejoin="round"><path d="M6 6L18 18M18 6L6 18"/></svg>`))
	checkGlyphMuted = fyne.NewStaticResource("check-muted.svg", []byte(
		`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#6e7168" stroke-width="3.6" stroke-linecap="round" stroke-linejoin="round"><path d="M4 12L10 18L20 6"/></svg>`))
)

func newCheckImage(res fyne.Resource) *canvas.Image {
	img := canvas.NewImageFromResource(res)
	img.FillMode = canvas.ImageFillContain
	img.SetMinSize(fyne.NewSize(11, 11))
	return img
}

// styledCheck is the teal tick used in Protocol and for Permissions
// toggles (Autostart / GPU clocks / WebRTC). Accessibility and Screen
// Capture are display-only — see permStatusChip.
type styledCheck struct {
	widget.DisableableWidget
	Label     string
	Checked   bool
	OnChanged func(bool)
	OnTap     func()
	hovered   bool
}

func newStyledCheck(label string, checked bool, onChanged func(bool)) *styledCheck {
	c := &styledCheck{Label: label, Checked: checked, OnChanged: onChanged}
	c.ExtendBaseWidget(c)
	registerChromeWidget(c)
	return c
}

func (c *styledCheck) SetChecked(on bool) {
	if c.Checked == on {
		return
	}
	c.Checked = on
	c.Refresh()
}

func (c *styledCheck) CreateRenderer() fyne.WidgetRenderer {
	box := canvas.NewRectangle(color.Transparent)
	box.CornerRadius = 3
	box.StrokeWidth = 1
	mark := newCheckImage(checkGlyphOnTeal)
	if !c.Checked {
		mark.Hide()
	}
	text := canvas.NewText(c.Label, design.ColorSectionTitle)
	text.TextSize = 11
	r := &styledCheckRenderer{check: c, box: box, mark: mark, text: text, objects: []fyne.CanvasObject{box, mark, text}}
	r.Refresh()
	return r
}

func (c *styledCheck) MinSize() fyne.Size {
	if strings.TrimSpace(c.Label) == "" {
		return fyne.NewSize(16, 18)
	}
	t := canvas.NewText(c.Label, design.ColorSectionTitle)
	t.TextSize = 11
	return fyne.NewSize(t.MinSize().Width+24, 20)
}

func (c *styledCheck) Tapped(*fyne.PointEvent) {
	if c.Disabled() {
		return
	}
	if c.OnChanged != nil {
		c.SetChecked(!c.Checked)
		c.OnChanged(c.Checked)
		return
	}
	if c.OnTap != nil {
		c.OnTap()
	}
}

func (c *styledCheck) TappedSecondary(*fyne.PointEvent) {}

func (c *styledCheck) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	c.hovered = true
	c.Refresh()
}

func (c *styledCheck) MouseOut() {
	noteChromeHoverOut()
	c.hovered = false
	c.Refresh()
}

func (c *styledCheck) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (c *styledCheck) Cursor() desktop.Cursor { return desktop.PointerCursor }

type styledCheckRenderer struct {
	check   *styledCheck
	box     *canvas.Rectangle
	mark    *canvas.Image
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *styledCheckRenderer) Layout(size fyne.Size) {
	const boxSide float32 = 14
	const markSide float32 = 11
	r.box.Resize(fyne.NewSize(boxSide, boxSide))
	r.box.Move(fyne.NewPos(0, (size.Height-boxSide)/2))
	placeSquareIcon(r.mark, fyne.NewPos((boxSide-markSide)/2, (size.Height-markSide)/2-0.5), markSide)
	if strings.TrimSpace(r.check.Label) == "" {
		r.text.Hide()
		return
	}
	r.text.Show()
	ts := r.text.MinSize()
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos(20, (size.Height-ts.Height)/2))
}

func (r *styledCheckRenderer) MinSize() fyne.Size { return r.check.MinSize() }

func (r *styledCheckRenderer) Refresh() {
	r.text.Text = r.check.Label
	ch := currentChrome()
	if r.check.Checked {
		r.box.FillColor = ch.Accent
		r.box.StrokeColor = ch.Accent
		r.mark.Show()
		r.text.Color = ch.Accent
	} else {
		r.box.FillColor = color.Transparent
		r.box.StrokeColor = design.ColorChromeOlive
		r.mark.Hide()
		r.text.Color = design.ColorSectionTitle
	}
	if r.check.Disabled() {
		r.box.StrokeColor = design.ColorBorder
		r.text.Color = design.ColorBorder
	} else if r.check.hovered && !r.check.Checked {
		r.box.StrokeColor = ch.Accent
	}
	r.box.Refresh()
	r.mark.Refresh()
	r.text.Refresh()
	r.Layout(r.check.Size())
}

func (r *styledCheckRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *styledCheckRenderer) Destroy()                     {}

// permStatusChip is the Accessibility / Screen Capture indicator: the same
// 10px label as Tailscale "Account", with a lime tick once granted. When
// onRequest is non-nil (this platform/capture-mode actually has something to
// request -- see the showAccessButton/showScreenCaptureButton gating in
// ShowAndRun, mirroring the old dedicated "Request" buttons), the whole chip
// is tappable and its label turns teal as a click hint; not-granted with a
// nil onRequest (nothing actionable here, e.g. Windows/X11) stays plain
// muted text with no pointer cursor, same as before this was made tappable.
type permStatusChip struct {
	widget.BaseWidget
	root      *fyne.Container
	mark      *canvas.Image
	labelT    *canvas.Text
	btn       *iconActionButton
	baseLabel string
	onRequest func()
	granted   bool
	busy      bool
}

// newPermStatusChip builds one Permissions line: [✓/✗] Label ...... [button].
// The button is a "Grant" request while not granted and an inactive "Granted"
// once it is; with a nil onRequest (nothing actionable on this platform) the
// button is omitted.
func newPermStatusChip(label string, onRequest func()) *permStatusChip {
	mark := newCheckImage(checkGlyphLime)
	mark.SetMinSize(fyne.NewSize(11, 11))
	labelT := canvas.NewText(label, design.ColorSectionTitle)
	labelT.TextSize = 11
	c := &permStatusChip{mark: mark, labelT: labelT, baseLabel: label, onRequest: onRequest}
	left := container.New(&tightHBoxLayout{gap: 6}, container.New(&checkNudgeLayout{dy: -1}, mark), labelT)
	var right fyne.CanvasObject
	if onRequest != nil {
		c.btn = newIconActionButton(loc().PermGrant, nil, func() {
			if c.busy {
				return
			}
			c.busy = true
			c.onRequest()
		})
		c.btn.Tiny = true
		right = c.btn
	}
	lock := canvas.NewRectangle(color.Transparent)
	lock.SetMinSize(fyne.NewSize(0, tinyActionSize))
	if right != nil {
		c.root = container.NewMax(lock, container.New(&flushEndsLayout{}, left, right))
	} else {
		c.root = container.NewMax(lock, container.New(&flushEndsLayout{}, left))
	}
	c.ExtendBaseWidget(c)
	c.SetChecked(false)
	return c
}

func (c *permStatusChip) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(c.root)
}

func (c *permStatusChip) SetBaseLabel(label string) {
	if c == nil {
		return
	}
	c.baseLabel = label
	c.refreshVisuals()
}

func (c *permStatusChip) SetChecked(on bool) {
	if c == nil {
		return
	}
	c.granted = on
	if on {
		c.mark.Resource = checkGlyphLime
	} else {
		c.mark.Resource = crossGlyphRed
	}
	c.mark.Refresh()
	c.refreshVisuals()
}

func (c *permStatusChip) refreshVisuals() {
	if c.labelT == nil {
		return
	}
	c.labelT.Text = c.baseLabel
	c.labelT.Refresh()
	if c.btn != nil {
		if c.granted {
			c.btn.SetText(loc().PermGranted)
			c.btn.Disable()
		} else {
			c.btn.SetText(loc().PermGrant)
			c.btn.Enable()
		}
	}
}

// requestDone lets the caller clear the busy flag once its (async) request
// finishes -- SetChecked alone doesn't imply that, since a request can
// legitimately end without changing the granted state (denied, or already
// granted before the call).
func (c *permStatusChip) requestDone() {
	if c == nil {
		return
	}
	c.busy = false
}

type checkNudgeLayout struct{ dx, dy float32 }

func (l *checkNudgeLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		o.Resize(size)
		o.Move(fyne.NewPos(l.dx, l.dy))
	}
}

func (l *checkNudgeLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var s fyne.Size
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		if min.Width > s.Width {
			s.Width = min.Width
		}
		if min.Height > s.Height {
			s.Height = min.Height
		}
	}
	return s
}

// permToggleRow is a full-width Permissions line (label left, tick right).
// The whole row is the hit target — same as protocolPickRow — so hovering
// or clicking the tick toggles the item, not just the label.
type permToggleRow struct {
	widget.BaseWidget
	label *canvas.Text
	hint  *canvas.Text
	check *styledCheck
	inner *fyne.Container
}

func newPermToggleRowWidget(label string, check *styledCheck) *permToggleRow {
	t := canvas.NewText(label, design.ColorSectionTitle)
	t.TextSize = 11
	hint := canvas.NewText("", design.ColorEmptyHint)
	hint.TextSize = 8
	hint.Hide()
	r := &permToggleRow{
		label: t,
		hint:  hint,
		check: check,
		inner: container.New(&flushEndsLayout{},
			container.New(&tightHBoxLayout{gap: 4}, t, hint),
			check),
	}
	r.ExtendBaseWidget(r)
	return r
}

func (r *permToggleRow) SetLabel(label string) {
	if r == nil || r.label == nil {
		return
	}
	r.label.Text = label
	r.label.Refresh()
}

func (r *permToggleRow) SetHint(hint string) {
	if r == nil || r.hint == nil {
		return
	}
	r.hint.Text = hint
	if strings.TrimSpace(hint) == "" {
		r.hint.Hide()
	} else {
		r.hint.Show()
	}
	r.hint.Refresh()
	r.Refresh()
}

func (r *permToggleRow) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(r.inner)
}

func (r *permToggleRow) MinSize() fyne.Size {
	s := fyne.NewSize(0, tinyActionSize)
	if r.inner != nil {
		min := r.inner.MinSize()
		if min.Width > s.Width {
			s.Width = min.Width
		}
		if min.Height > s.Height {
			s.Height = min.Height
		}
	}
	return s
}

func (r *permToggleRow) Tapped(*fyne.PointEvent) {
	if r.check == nil || r.check.Disabled() {
		return
	}
	r.check.Tapped(nil)
}

func (r *permToggleRow) TappedSecondary(*fyne.PointEvent) {}

func (r *permToggleRow) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	if r.check == nil {
		return
	}
	r.check.hovered = true
	r.check.Refresh()
}

func (r *permToggleRow) MouseOut() {
	noteChromeHoverOut()
	if r.check == nil {
		return
	}
	r.check.hovered = false
	r.check.Refresh()
}

func (r *permToggleRow) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (r *permToggleRow) Cursor() desktop.Cursor {
	if r.check != nil && r.check.Disabled() {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

package view

import (
	"image/color"
	"reflect"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
)

// usableCanvasObject is true when obj can safely be asked Visible()/MinSize()
// -- a typed-nil pointer stored in a fyne.CanvasObject interface is not
// == nil, and calling Visible() on it panics (see BackupWidgetUI.rebuildFooter
// passing a still-nil connectingHint).
func usableCanvasObject(obj fyne.CanvasObject) bool {
	if obj == nil {
		return false
	}
	v := reflect.ValueOf(obj)
	switch v.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		return !v.IsNil()
	}
	return true
}

const (
	headerBandHorizontalInset float32 = 10
	headerBandTitleTopInset   float32 = 10
	headerBandBodyTopInset    float32 = 6
	headerBandBodyBottomInset float32 = 5
)

// insetLayout is NewInsetExact's own layout: a plain fyne.Layout that insets
// its one child by exact left/right/top/bottom pixels, with none of
// container.NewBorder's own hidden extra (see NewInset's own doc comment for
// what that extra is). Only for the handful of call sites that actually
// need that exactness -- NewHeaderBand's own body/title wrapping and the
// Control header's status-indicator strip (main_window_status_indicator_bar.go)
// and connection row (connection_header.go) -- since those are what this
// exactness was diagnosed and fixed for. Every other NewInset call site in
// the app (there are dozens: cards, tables, dialogs, menus...) was visually
// tuned against NewInset's own +theme.Padding()-per-side behavior, so
// switching them to this too would detune every one of them at once -- use
// plain NewInset there.
type insetLayout struct {
	left, right, top, bottom float32
}

func (l *insetLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	content := objects[0]
	w := size.Width - l.left - l.right
	h := size.Height - l.top - l.bottom
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	content.Move(fyne.NewPos(l.left, l.top))
	content.Resize(fyne.NewSize(w, h))
}

func (l *insetLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}
	min := objects[0].MinSize()
	return fyne.NewSize(min.Width+l.left+l.right, min.Height+l.top+l.bottom)
}

// NewInsetExact insets content by the exact left/right/top/bottom pixels
// given, no more -- see insetLayout's own doc comment for which few call
// sites should actually use this instead of plain NewInset.
func NewInsetExact(content fyne.CanvasObject, left, right, top, bottom float32) *fyne.Container {
	return container.New(&insetLayout{left: left, right: right, top: top, bottom: bottom}, content)
}

// NewInset insets content via container.NewBorder -- a transparent spacer
// rectangle per non-zero side, passed as Border's own top/bottom/left/right.
// Fyne's own border layout (layout/borderlayout.go) silently adds
// theme.Padding() (4px by default) *again* on top of each spacer's own size
// for every side that's non-nil -- so NewInset(x, 10, 10, 3, 3) actually
// renders as 3+4=7px top/bottom, 10+4=14px left/right, not the exact 3/10
// asked for. That's a genuine Fyne quirk, not a deliberate design -- but
// every one of this app's dozens of NewInset call sites (cards, tables,
// dialogs, menus, the account panel...) was visually tuned with that extra
// padding already baked in, so this stays as-is rather than "fixed": doing
// so once shrank every one of those elements' padding at once (found only
// after the fact, from the Control header's own status strip and
// NewHeaderBand needing pixel-exact math -- see NewInsetExact instead for
// those specific, narrow cases).
func NewInset(content fyne.CanvasObject, left, right, top, bottom float32) *fyne.Container {
	var topSpacer, bottomSpacer, leftSpacer, rightSpacer fyne.CanvasObject
	if top > 0 {
		s := canvas.NewRectangle(color.Transparent)
		s.SetMinSize(fyne.NewSize(0, top))
		topSpacer = s
	}
	if bottom > 0 {
		s := canvas.NewRectangle(color.Transparent)
		s.SetMinSize(fyne.NewSize(0, bottom))
		bottomSpacer = s
	}
	if left > 0 {
		s := canvas.NewRectangle(color.Transparent)
		s.SetMinSize(fyne.NewSize(left, 0))
		leftSpacer = s
	}
	if right > 0 {
		s := canvas.NewRectangle(color.Transparent)
		s.SetMinSize(fyne.NewSize(right, 0))
		rightSpacer = s
	}
	return container.NewBorder(topSpacer, bottomSpacer, leftSpacer, rightSpacer, content)
}

// bottomLineLayout is NewBottomLine's own layout -- content on top, a thin
// line (its own MinSize().Height, typically <1px) directly under it, with
// zero added padding. Same reasoning as insetLayout: the previous
// `container.NewBorder(nil, line, nil, nil, content)` pattern silently adds
// theme.Padding() (4px) under the line whenever the bottom slot is non-nil,
// with nothing matching it on top (top is nil) -- a real, if small,
// top/bottom asymmetry (see NewInset's own doc comment for the general
// mechanism).
type bottomLineLayout struct{}

func (l *bottomLineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	content, line := objects[0], objects[1]
	lineHeight := line.MinSize().Height
	contentHeight := size.Height - lineHeight
	if contentHeight < 0 {
		contentHeight = 0
	}
	content.Move(fyne.NewPos(0, 0))
	content.Resize(fyne.NewSize(size.Width, contentHeight))
	line.Move(fyne.NewPos(0, contentHeight))
	line.Resize(fyne.NewSize(size.Width, lineHeight))
}

func (l *bottomLineLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	contentMin := objects[0].MinSize()
	lineMin := objects[1].MinSize()
	width := contentMin.Width
	if lineMin.Width > width {
		width = lineMin.Width
	}
	return fyne.NewSize(width, contentMin.Height+lineMin.Height)
}

// NewBottomLine stacks content above a thin line (e.g. a header's accent
// line) with no padding between them, pixel-exact -- see bottomLineLayout's
// own doc comment for why this isn't just container.NewBorder(nil, line,
// nil, nil, content).
func NewBottomLine(content, line fyne.CanvasObject) *fyne.Container {
	return container.New(&bottomLineLayout{}, content, line)
}

// topLineLayout is NewTopLine's own layout -- a thin line (its own
// MinSize().Height) directly above content, with zero added padding.
// Mirrors bottomLineLayout so the Devices/Scripts/Control footer can
// wear the same hairline the app header has under it, without the
// theme.Padding() gap container.NewBorder would insert.
type topLineLayout struct{}

func (l *topLineLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 2 {
		return
	}
	content, line := objects[0], objects[1]
	lineHeight := line.MinSize().Height
	contentHeight := size.Height - lineHeight
	if contentHeight < 0 {
		contentHeight = 0
	}
	line.Move(fyne.NewPos(0, 0))
	line.Resize(fyne.NewSize(size.Width, lineHeight))
	content.Move(fyne.NewPos(0, lineHeight))
	content.Resize(fyne.NewSize(size.Width, contentHeight))
}

func (l *topLineLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 2 {
		return fyne.NewSize(0, 0)
	}
	contentMin := objects[0].MinSize()
	lineMin := objects[1].MinSize()
	width := contentMin.Width
	if lineMin.Width > width {
		width = lineMin.Width
	}
	return fyne.NewSize(width, contentMin.Height+lineMin.Height)
}

// NewTopLine stacks a thin line above content with no padding between
// them, pixel-exact -- the footer counterpart to NewBottomLine.
func NewTopLine(content, line fyne.CanvasObject) *fyne.Container {
	return container.New(&topLineLayout{}, content, line)
}

// edgeStackLayout is NewEdgeStack's own layout: top pinned to its own
// MinSize height, bottom pinned to its own MinSize height, content filling
// whatever's left between them -- zero gap anywhere, same reasoning as
// insetLayout/bottomLineLayout (see insetLayout's own doc comment for the
// general container.NewBorder pitfall this avoids). This is what
// createMainAddressBar/createConnectionAddressBar's own header band used to
// sit in via plain container.NewBorder(header, footer, nil, nil, content) --
// which silently left a theme.Padding() (4px) gap of raw background color
// between the header's own bottom accent line and the tab content under it,
// visible as a thin grey strip whenever that content itself was black
// (e.g. Control's video area with no stream yet).
type edgeStackLayout struct {
	hasTop, hasBottom bool
}

func (l *edgeStackLayout) split(objects []fyne.CanvasObject) (top, bottom, content fyne.CanvasObject) {
	idx := 0
	if l.hasTop {
		top = objects[idx]
		idx++
	}
	if l.hasBottom {
		bottom = objects[idx]
		idx++
	}
	content = objects[idx]
	return
}

func (l *edgeStackLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	top, bottom, content := l.split(objects)

	var topHeight, bottomHeight float32
	if top != nil {
		topHeight = top.MinSize().Height
		top.Move(fyne.NewPos(0, 0))
		top.Resize(fyne.NewSize(size.Width, topHeight))
	}
	if bottom != nil {
		bottomHeight = bottom.MinSize().Height
		bottom.Move(fyne.NewPos(0, size.Height-bottomHeight))
		bottom.Resize(fyne.NewSize(size.Width, bottomHeight))
	}

	contentHeight := size.Height - topHeight - bottomHeight
	if contentHeight < 0 {
		contentHeight = 0
	}
	content.Move(fyne.NewPos(0, topHeight))
	content.Resize(fyne.NewSize(size.Width, contentHeight))
}

func (l *edgeStackLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	top, bottom, content := l.split(objects)

	contentMin := content.MinSize()
	width, height := contentMin.Width, contentMin.Height
	if top != nil {
		topMin := top.MinSize()
		height += topMin.Height
		if topMin.Width > width {
			width = topMin.Width
		}
	}
	if bottom != nil {
		bottomMin := bottom.MinSize()
		height += bottomMin.Height
		if bottomMin.Width > width {
			width = bottomMin.Width
		}
	}
	return fyne.NewSize(width, height)
}

// NewEdgeStack stacks an optional top (its own MinSize height), an optional
// bottom (its own MinSize height), and content filling the rest, with no
// gap between any of them -- top/bottom may be nil to omit that edge.
func NewEdgeStack(top, bottom, content fyne.CanvasObject) *fyne.Container {
	l := &edgeStackLayout{hasTop: top != nil, hasBottom: bottom != nil}
	objects := make([]fyne.CanvasObject, 0, 3)
	if top != nil {
		objects = append(objects, top)
	}
	if bottom != nil {
		objects = append(objects, bottom)
	}
	objects = append(objects, content)
	return container.New(l, objects...)
}

func NewSurfacePanel(content fyne.CanvasObject, fill color.Color, radius float32) *fyne.Container {
	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius
	bg.StrokeColor = design.ColorBorder
	bg.StrokeWidth = 1

	return container.NewStack(
		bg,
		container.NewPadded(container.NewPadded(content)),
	)
}

func NewCompactSurfacePanel(content fyne.CanvasObject, fill color.Color, radius float32) *fyne.Container {
	shadowSoft := canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x12})
	shadowSoft.CornerRadius = radius + 2

	shadowTight := canvas.NewRectangle(color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x18})
	shadowTight.CornerRadius = radius + 1

	bg := canvas.NewRectangle(fill)
	bg.CornerRadius = radius

	panelContent := NewInset(content, 4, 4, 2, 2)

	return container.New(
		&compactSurfacePanelLayout{},
		shadowSoft,
		shadowTight,
		bg,
		panelContent,
	)
}

type compactSurfacePanelLayout struct{}

func (l *compactSurfacePanelLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 4 {
		return
	}

	shadowSoft := objects[0]
	shadowTight := objects[1]
	bg := objects[2]
	content := objects[3]

	panelHeight := size.Height - 8
	if panelHeight < 0 {
		panelHeight = 0
	}

	shadowSoft.Move(fyne.NewPos(6, 6))
	shadowSoft.Resize(fyne.NewSize(maxFloat32(0, size.Width-12), panelHeight))

	shadowTight.Move(fyne.NewPos(3, 3))
	shadowTight.Resize(fyne.NewSize(maxFloat32(0, size.Width-6), panelHeight))

	bg.Move(fyne.NewPos(0, 0))
	bg.Resize(fyne.NewSize(size.Width, panelHeight))

	content.Move(fyne.NewPos(0, 0))
	content.Resize(fyne.NewSize(size.Width, panelHeight))
}

func (l *compactSurfacePanelLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 4 {
		return fyne.NewSize(0, 0)
	}

	contentMin := objects[3].MinSize()
	return fyne.NewSize(contentMin.Width, contentMin.Height+8)
}

func NewBrandBadge(text string, size fyne.Size) *fyne.Container {
	bg := canvas.NewRectangle(design.ColorAccent)
	bg.CornerRadius = design.RadiusMD

	label := canvas.NewText(text, design.ColorBackground)
	label.TextSize = 16
	label.TextStyle.Bold = true

	return container.NewGridWrap(
		size,
		container.NewStack(bg, container.NewCenter(label)),
	)
}

func NewBrandText(text string, size float32, col color.Color, bold bool) *canvas.Text {
	txt := canvas.NewText(text, col)
	txt.TextSize = size
	txt.TextStyle.Bold = bold
	return txt
}

func NewOutlinedControl(content fyne.CanvasObject, width, height float32) *fyne.Container {
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	panel := container.NewStack(bg, content, border)
	if width > 0 && height > 0 {
		return container.NewGridWrap(fyne.NewSize(width, height), panel)
	}
	if height > 0 {
		return NewFixedHeight(panel, height)
	}
	return panel
}

func NewFixedHeight(content fyne.CanvasObject, height float32) *fyne.Container {
	if height <= 0 {
		return container.NewStack(content)
	}
	return container.New(&fixedHeightLayout{height: height}, content)
}

type fixedHeightLayout struct {
	height float32
}

func (l *fixedHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}

	child := objects[0]
	targetHeight := l.height
	if targetHeight <= 0 {
		targetHeight = size.Height
	}
	if targetHeight > size.Height {
		targetHeight = size.Height
	}

	y := (size.Height - targetHeight) / 2
	if y < 0 {
		y = 0
	}

	child.Move(fyne.NewPos(0, y))
	child.Resize(fyne.NewSize(size.Width, targetHeight))
}

func (l *fixedHeightLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) == 0 {
		return fyne.NewSize(0, 0)
	}

	childMin := objects[0].MinSize()
	return fyne.NewSize(childMin.Width, l.height)
}

func NewHeaderBand(title string, content fyne.CanvasObject) *fyne.Container {
	bg := canvas.NewRectangle(design.ColorGray900)

	body := NewInsetExact(content, headerBandHorizontalInset, headerBandHorizontalInset, headerBandBodyTopInset, headerBandBodyBottomInset)
	if strings.TrimSpace(title) != "" {
		titleText := NewBrandText(strings.ToUpper(strings.TrimSpace(title)), 12, design.ColorTextMuted, true)
		titleWrap := NewInsetExact(titleText, headerBandHorizontalInset, headerBandHorizontalInset, headerBandTitleTopInset, 4)
		body = container.NewVBox(titleWrap, NewInsetExact(content, headerBandHorizontalInset, headerBandHorizontalInset, 8, headerBandBodyBottomInset))
	}

	// Same hairline accent under the band as the connections screen's own
	// header (connection_header.go's newConnectionHeader) -- this used to be
	// missing here, the one visible difference once Control's header
	// (createMainAddressBar, this function's only caller) started reusing
	// that header's own accessory menu.
	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))

	return container.NewStack(bg, NewBottomLine(body, accentLine))
}

// MobileFooterBottomInset adds padding to the bottom of the container on mobile devices
// to keep it above the system navigation bar (Android).
func MobileFooterBottomInset(base float32) float32 {
	if !IsMobile() {
		return base
	}

	// In Edge-to-Edge mode we need very little padding.
	return base + 4
}

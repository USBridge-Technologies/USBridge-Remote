package view

import (
	"image/color"
	"strings"

	"usbridge-client/internal/gui/design"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
)

// AppFooterRowHeight is the locked content height of the shared window
// footer (Connections, Control, Devices, Snapshots, Scripts). Spinner and
// script chips are 14px; without the lock the strip jumps when a hint
// appears or disappears.
const AppFooterRowHeight = float32(14)

// Compact mobile pad under version/chips (Control and other tabs). Android
// reports bottom inset=0 so this sits near the screen edge.
const appFooterMobileBottomPad = float32(2)

// Connections keeps a taller footer on mobile so the version clears the
// gesture/nav buttons without enabling Fyne's system bottom safe-inset
// (which inflated every screen's chrome).
const connectionsMobileBottomPad = float32(26)

func appFooterVPads() (top, bottom float32) {
	top = 4
	bottom = 6
	if IsMobile() {
		bottom = appFooterMobileBottomPad
	}
	return
}

func appFooterLineHeight() float32 {
	return 0.5
}

// AppFooterOuterHeight is NewAppFooter's full strip height (row + pads +
// hairline). Native video overlays subtract this from canvasH − containerH
// when AbsolutePosition has not settled yet.
func AppFooterOuterHeight() float32 {
	top, bottom := appFooterVPads()
	return AppFooterRowHeight + top + bottom + appFooterLineHeight()
}

// NewAppFooter is the one bottom strip used on every main screen: optional
// left chips (busy spinner, script status), optional right action
// (Disconnect All), and the build version. A ColorHeaderAccentLine
// hairline sits on top -- the same stroke the app header wears underneath.
// rightBtn, spinner, and extraLeft may be nil.
func NewAppFooter(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	top, bottom := appFooterVPads()
	return newAppFooter(version, true, top, bottom, rightBtn, spinner, extraLeft...)
}

// NewConnectionsAppFooter is NewAppFooter with a taller mobile bottom pad so
// the Connections version sits above the nav/gesture area without turning
// on the system bottom safe-inset for the whole app.
func NewConnectionsAppFooter(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	top, bottom := appFooterVPads()
	if IsMobile() {
		bottom = connectionsMobileBottomPad
	}
	return newAppFooter(version, true, top, bottom, rightBtn, spinner, extraLeft...)
}

// NewAppFooterNoLine is NewAppFooter without the top hairline -- used when
// this strip sits directly under another bar that already has its own line
// (mobile Control tab footer + version).
func NewAppFooterNoLine(version string, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	top, bottom := appFooterVPads()
	return newAppFooter(version, false, top, bottom, rightBtn, spinner, extraLeft...)
}

func newAppFooter(version string, withLine bool, top, bottom float32, rightBtn, spinner fyne.CanvasObject, extraLeft ...fyne.CanvasObject) fyne.CanvasObject {
	leftParts := make([]fyne.CanvasObject, 0, 1+len(extraLeft))
	if usableCanvasObject(spinner) {
		leftParts = append(leftParts, spinner)
	}
	for _, extra := range extraLeft {
		if usableCanvasObject(extra) {
			leftParts = append(leftParts, extra)
		}
	}
	var left fyne.CanvasObject
	if len(leftParts) > 0 {
		left = container.New(&DeviceRowControlsLayout{Gap: 10}, leftParts...)
	}
	var rightParts []fyne.CanvasObject
	if rightBtn != nil {
		rightParts = append(rightParts, rightBtn)
	}
	if v := strings.TrimSpace(version); v != "" {
		if !strings.HasPrefix(strings.ToLower(v), "v") {
			v = "v" + v
		}
		rightParts = append(rightParts, newFooterVersionButton(v))
	}
	var right fyne.CanvasObject
	if len(rightParts) > 0 {
		right = container.New(&DeviceRowControlsLayout{Gap: 12}, rightParts...)
	}
	var row fyne.CanvasObject
	if left == nil && right == nil {
		row = canvas.NewRectangle(color.Transparent)
	} else {
		row = container.NewBorder(nil, nil, left, right)
	}
	heightLock := canvas.NewRectangle(color.Transparent)
	heightLock.SetMinSize(fyne.NewSize(0, AppFooterRowHeight))
	return newAppFooterStrip(NewInsetExact(container.NewMax(heightLock, row), 18, 18, top, bottom), withLine)
}

func newAppFooterStrip(inner fyne.CanvasObject, withLine bool) fyne.CanvasObject {
	// Square fill to the window's own bottom edge -- without this the
	// footer is just a hairline + inset content, and a maximized Win11
	// window shows leftover rounded "ears" of whatever sits behind it.
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 0
	if !withLine {
		return container.NewStack(bg, inner)
	}
	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, appFooterLineHeight()))
	return container.NewStack(bg, NewTopLine(inner, accentLine))
}

const (
	whatsNewUnseenDotSize float32 = 6
	whatsNewUnseenDotHang float32 = 4
)

func newFooterVersionButton(version string) fyne.CanvasObject {
	b := &whatsNewFooterVersion{text: version}
	b.ExtendBaseWidget(b)
	return b
}

// NewFooterVersionButton is the shared tappable "vX.Y.Z" chip used in
// Connections / connected chrome footers (opens What's new).
func NewFooterVersionButton(version string) fyne.CanvasObject {
	v := strings.TrimSpace(version)
	if v == "" {
		return nil
	}
	if !strings.HasPrefix(strings.ToLower(v), "v") {
		v = "v" + v
	}
	return newFooterVersionButton(v)
}

// footerVersionDigitsVisible hides the version numerals without changing
// MinSize -- Control tab keeps the same footer height as Devices/Snapshots
// /Scripts, it just does not paint the digits (or the What's-new pip).
var footerVersionDigitsVisible = true

// SetFooterVersionDigitsVisible paints or un-paints every live version
// chip. Hidden digits still occupy their layout slot.
func SetFooterVersionDigitsVisible(visible bool) {
	if footerVersionDigitsVisible == visible {
		refreshWhatsNewFooterUnseen()
		return
	}
	footerVersionDigitsVisible = visible
	refreshWhatsNewFooterUnseen()
}

var whatsNewFooterVersions []*whatsNewFooterVersion

func registerWhatsNewFooterVersion(w *whatsNewFooterVersion) {
	for _, existing := range whatsNewFooterVersions {
		if existing == w {
			return
		}
	}
	whatsNewFooterVersions = append(whatsNewFooterVersions, w)
}

func unregisterWhatsNewFooterVersion(w *whatsNewFooterVersion) {
	out := whatsNewFooterVersions[:0]
	for _, existing := range whatsNewFooterVersions {
		if existing != w {
			out = append(out, existing)
		}
	}
	whatsNewFooterVersions = out
}

func refreshWhatsNewFooterUnseen() {
	for _, w := range whatsNewFooterVersions {
		if w != nil {
			w.syncUnseen()
		}
	}
}

// whatsNewFooterVersion is the desktop footer build tag with a teal pip
// at the top-right while the current What's new catalog is unseen.
type whatsNewFooterVersion struct {
	widget.BaseWidget
	text    string
	hovered bool
	lbl     *canvas.Text
	dot     *canvas.Circle
}

func (b *whatsNewFooterVersion) Tapped(*fyne.PointEvent) {
	if !footerVersionDigitsVisible {
		return
	}
	ShowWhatsNewDialog(whatsNewParentWindow())
}

func (b *whatsNewFooterVersion) TappedSecondary(*fyne.PointEvent) {}

func (b *whatsNewFooterVersion) Cursor() desktop.Cursor {
	if !footerVersionDigitsVisible {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

func (b *whatsNewFooterVersion) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshLabel()
}

func (b *whatsNewFooterVersion) MouseMoved(*desktop.MouseEvent) {}

func (b *whatsNewFooterVersion) MouseOut() {
	b.hovered = false
	b.refreshLabel()
}

func (b *whatsNewFooterVersion) refreshLabel() {
	if b.lbl == nil {
		return
	}
	if !footerVersionDigitsVisible {
		b.lbl.Color = color.Transparent
	} else if b.hovered {
		b.lbl.Color = design.ColorTextLight
	} else {
		b.lbl.Color = design.ColorTextMuted
	}
	b.lbl.Refresh()
}

func (b *whatsNewFooterVersion) syncUnseen() {
	b.applyUnseenDot()
	b.Refresh()
}

func (b *whatsNewFooterVersion) applyUnseenDot() {
	if b.dot == nil {
		return
	}
	if footerVersionDigitsVisible && whatsNewHasUnseen() {
		b.dot.Show()
	} else {
		b.dot.Hide()
	}
}

func (b *whatsNewFooterVersion) CreateRenderer() fyne.WidgetRenderer {
	b.lbl = canvas.NewText(b.text, design.ColorTextMuted)
	b.lbl.TextSize = 9
	b.refreshLabel()
	b.dot = canvas.NewCircle(design.ColorConnectionBadgeText)
	b.applyUnseenDot()
	registerWhatsNewFooterVersion(b)
	return &whatsNewFooterVersionRenderer{w: b, objects: []fyne.CanvasObject{b.lbl, b.dot}}
}

type whatsNewFooterVersionRenderer struct {
	w       *whatsNewFooterVersion
	objects []fyne.CanvasObject
}

func (r *whatsNewFooterVersionRenderer) Destroy() {
	unregisterWhatsNewFooterVersion(r.w)
}

func (r *whatsNewFooterVersionRenderer) Objects() []fyne.CanvasObject { return r.objects }

func (r *whatsNewFooterVersionRenderer) MinSize() fyne.Size {
	if r.w.lbl == nil {
		return fyne.NewSize(0, 0)
	}
	m := r.w.lbl.MinSize()
	// Always reserve the unseen-pip hang so showing/hiding the dot does not
	// reflow footer width. Height must stay within AppFooterRowHeight —
	// videoChromeBelow() and the footer's heightLock assume a 14dp row;
	// inflating MinSize (e.g. for a phone tap target) made the real strip
	// taller than the overlay chrome budget and video covered the footer.
	m.Width += whatsNewUnseenDotHang
	if IsMobile() && m.Width < 40 {
		m.Width = 40
	}
	if m.Height > AppFooterRowHeight {
		m.Height = AppFooterRowHeight
	}
	return m
}

func (r *whatsNewFooterVersionRenderer) Layout(size fyne.Size) {
	if r.w.lbl == nil {
		return
	}
	ts := r.w.lbl.MinSize()
	r.w.lbl.Resize(ts)
	r.w.lbl.Move(fyne.NewPos(0, (size.Height-ts.Height)/2))
	if r.w.dot == nil {
		return
	}
	d := whatsNewUnseenDotSize
	r.w.dot.Resize(fyne.NewSize(d, d))
	x := size.Width - d
	if x < ts.Width-d/2 {
		x = ts.Width - d/2
	}
	if x < 0 {
		x = 0
	}
	r.w.dot.Move(fyne.NewPos(x, 0))
}

func (r *whatsNewFooterVersionRenderer) Refresh() {
	r.w.refreshLabel()
	r.w.applyUnseenDot()
	if r.w.dot != nil {
		r.w.dot.FillColor = design.ColorConnectionBadgeText
		r.w.dot.Refresh()
	}
	r.Layout(r.w.Size())
}

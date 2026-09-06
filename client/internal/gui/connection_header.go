package gui

import (
	"image/color"
	"runtime"
	"strings"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// connectionHeaderActions are the events the connection screen's header can
// report -- what the user tapped, never what should happen as a result. The
// composition root (createConnectionAddressBar) wires these to the actual
// controller calls.
type connectionHeaderActions struct {
	// OnShowLanguageMenu is called with the language button itself as the
	// anchor, so the popup menu can position itself against it.
	OnShowLanguageMenu func(anchor fyne.CanvasObject)
	OnOpenCommunity    func()
	OnOpenInfo         func()
	OnToggleTailscale  func()
}

// ConnectionHeaderHandle lets the controller push live Tailscale status into
// an already-built connection header, without owning (or even seeing the
// type of) any of its widgets. nil-safe: a nil accessory (wasm builds, see
// newConnectionHeader) yields a handle whose SetTailscaleState is a no-op.
type ConnectionHeaderHandle struct {
	toggle *tailscaleHeaderToggle
}

// SetTailscaleState updates the header's Tailscale toggle from the same raw
// status/auth-label strings the tsnet polling loop already produces.
func (h *ConnectionHeaderHandle) SetTailscaleState(status, authLabel string) {
	if h == nil || h.toggle == nil {
		return
	}
	active, loading := summarizeTailscaleState(status, authLabel)
	h.toggle.SetOn(active)
	h.toggle.SetLoading(loading)
	h.toggle.SetDisabled(loading) // Block button during transition
}

// newConnectionHeader builds the top bar shown on the connections screen
// (before a device is connected): logo + wordmark on the left, and on the
// right the Tailscale toggle, info, community and language buttons. The
// returned handle is how the controller later pushes Tailscale status into
// the toggle it just built.
//
// This is the desktop-only design for now -- there is no mobile variant of
// this component yet. When one exists, the choice between them belongs in
// the caller (createConnectionAddressBar), not inside this component.
func newConnectionHeader(actions connectionHeaderActions) (*fyne.Container, *ConnectionHeaderHandle) {
	logo := canvas.NewImageFromResource(assets.LogoUSBridgeIcon)
	logo.SetMinSize(fyne.NewSize(24, 24))
	logo.FillMode = canvas.ImageFillContain

	title := canvas.NewText("USBridge", design.ColorLogoWordmark)
	title.TextSize = 14
	title.TextStyle = fyne.TextStyle{Bold: true}

	leftRow := container.New(&centeredInlineLayout{gap: 8, minGap: 4}, logo, title)

	var langBtn *headerStatusBadgeButton
	langBtn = newHeaderStatusBadgeButton(assets.LanguageIconHeader, func() {
		if actions.OnShowLanguageMenu != nil {
			actions.OnShowLanguageMenu(langBtn)
		}
	})
	langBtn.SetBadgeText("")
	langBtn.SetIconSize(fyne.NewSize(16, 16))

	communityBtn := newHeaderStatusBadgeButton(assets.DiscordIconHeader, func() {
		if actions.OnOpenCommunity != nil {
			actions.OnOpenCommunity()
		}
	})
	communityBtn.SetBadgeText("")
	communityBtn.SetIconSize(fyne.NewSize(16, 16))

	infoBtn := newHeaderStatusBadgeButton(assets.QuestionIconHeader, func() {
		if actions.OnOpenInfo != nil {
			actions.OnOpenInfo()
		}
	})
	infoBtn.SetBadgeText("")
	infoBtn.SetIconSize(fyne.NewSize(16, 16))

	var tailscaleAccessory fyne.CanvasObject
	handle := &ConnectionHeaderHandle{}
	if runtime.GOOS == "js" {
		// No embedded tsnet in a browser tab (tailscale_service_wasm.go is a
		// stub) -- the "Sign In With Google" toggle has nothing to do here,
		// so don't show it at all rather than show a button that can't
		// function. handle.toggle stays nil, so SetTailscaleState is a no-op.
		tailscaleAccessory = canvas.NewRectangle(color.Transparent)
	} else {
		toggle := newTailscaleHeaderToggle(actions.OnToggleTailscale)
		handle.toggle = toggle
		tailscaleAccessory = toggle
	}

	rightRow := container.New(&centeredInlineLayout{gap: 8, minGap: 4}, tailscaleAccessory, infoBtn, communityBtn, langBtn)

	row := container.NewHBox(leftRow, layout.NewSpacer(), rightRow)

	bg := canvas.NewRectangle(design.ColorGray900)
	paddedRow := view.NewInset(row, 16, 16, 2, 2)

	return container.NewStack(bg, paddedRow), handle
}

// summarizeTailscaleState turns the tsnet polling loop's free-form status
// text into the toggle's two boolean visual states (on, loading). authLabel
// is currently unused (kept for signature symmetry with the raw status
// strings the polling loop already has on hand).
func summarizeTailscaleState(status, _ string) (bool, bool) {
	raw := strings.ToLower(strings.TrimSpace(status))

	switch {
	case strings.Contains(raw, "signed out"), strings.Contains(raw, "not connected"), strings.Contains(raw, "needslogin"), strings.Contains(raw, "loggedout"):
		return false, false
	case strings.Contains(raw, "starting"), strings.Contains(raw, "signing"), strings.Contains(raw, "browser opened"), strings.Contains(raw, "auth url"), strings.Contains(raw, "checking"):
		return false, true
	case strings.Contains(raw, "stopped"), strings.Contains(raw, "no state"), strings.Contains(raw, "login failed"):
		return false, false
	case strings.Contains(raw, "running"), strings.Contains(raw, "connected"), strings.Contains(raw, "active"):
		return true, false
	case strings.Contains(raw, "tailscale:"):
		return false, false
	default:
		return false, false
	}
}

// tailscaleHeaderToggle is the small pill switch in the connection header
// that shows/toggles Tailscale sign-in state (see ConnectionHeaderHandle for
// how the controller drives it).
type tailscaleHeaderToggle struct {
	widget.BaseWidget

	onTapped func()
	on       bool
	loading  bool
	disabled bool
	hovered  bool

	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	label   *canvas.Text
	track   *canvas.Rectangle
	thumb   *canvas.Circle
	spinner *canvas.Image

	anim view.SpinnerAnimator
}

func newTailscaleHeaderToggle(onTapped func()) *tailscaleHeaderToggle {
	toggle := &tailscaleHeaderToggle{onTapped: onTapped}
	toggle.ExtendBaseWidget(toggle)
	return toggle
}

func (t *tailscaleHeaderToggle) SetOn(on bool) {
	t.on = on
	t.refreshVisuals()
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

func (t *tailscaleHeaderToggle) SetDisabled(disabled bool) {
	t.disabled = disabled
	if disabled {
		t.hovered = false
	}
	t.refreshVisuals()
	t.Refresh()
}

func (t *tailscaleHeaderToggle) Tapped(e *fyne.PointEvent) {
	if t.disabled || t.loading || t.onTapped == nil {
		return
	}
	if e.Position.X < t.Size().Width-36 {
		return
	}
	t.onTapped()
}

func (t *tailscaleHeaderToggle) TappedSecondary(*fyne.PointEvent) {}

func (t *tailscaleHeaderToggle) MouseIn(e *desktop.MouseEvent) {
	if t.disabled || t.loading {
		return
	}
	t.hovered = true
	t.refreshVisuals()
}

func (t *tailscaleHeaderToggle) MouseMoved(e *desktop.MouseEvent) {
	hover := false
	if !t.disabled && !t.loading && e.Position.X >= t.Size().Width-36 {
		hover = true
	}
	if t.hovered != hover {
		t.hovered = hover
		t.refreshVisuals()
	}
}

func (t *tailscaleHeaderToggle) MouseOut() {
	if !t.hovered {
		return
	}
	t.hovered = false
	t.refreshVisuals()
}

func (t *tailscaleHeaderToggle) MinSize() fyne.Size {
	return fyne.NewSize(92, 24)
}

func (t *tailscaleHeaderToggle) CreateRenderer() fyne.WidgetRenderer {
	t.bg = canvas.NewRectangle(design.ColorSurfaceLight)
	t.bg.CornerRadius = 12

	t.border = canvas.NewRectangle(color.Transparent)
	t.border.CornerRadius = 12
	t.border.StrokeColor = design.ColorAccent
	t.border.StrokeWidth = 1

	t.label = canvas.NewText("Tailscale", design.ColorTextMuted)
	t.label.TextSize = 10
	t.label.TextStyle = fyne.TextStyle{Bold: true}
	t.label.Alignment = fyne.TextAlignLeading

	t.track = canvas.NewRectangle(design.ColorSurfaceLight)
	t.track.CornerRadius = 7

	t.thumb = canvas.NewCircle(design.ColorGray400)

	t.spinner = canvas.NewImageFromResource(assets.LoadingGrayFrames[0])
	t.spinner.FillMode = canvas.ImageFillContain
	t.spinner.Hidden = true

	t.refreshVisuals()
	return &tailscaleHeaderToggleRenderer{toggle: t}
}

func (t *tailscaleHeaderToggle) refreshVisuals() {
	if t.bg == nil || t.border == nil || t.label == nil || t.track == nil || t.thumb == nil || t.spinner == nil {
		return
	}

	bgColor := design.ColorGray950
	borderColor := design.ColorTailscaleChipBorder
	labelColor := design.ColorTailscaleChipLabel

	trackColor := design.ColorGray900
	thumbColor := design.ColorGray400

	if t.on {
		trackColor = design.ColorAccent
		thumbColor = design.ColorWhite
	}
	if t.disabled {
		labelColor = design.ColorGray400
		trackColor = design.ColorGray950
		borderColor = design.ColorGray900
	}

	t.bg.FillColor = bgColor
	t.border.StrokeColor = borderColor
	t.border.StrokeWidth = 1
	t.label.Color = labelColor

	t.track.Hidden = t.loading
	t.thumb.Hidden = t.loading
	t.track.FillColor = trackColor
	t.thumb.FillColor = thumbColor

	if t.disabled {
		t.thumb.FillColor = design.ColorGray900
	}

	t.bg.Refresh()
	t.border.Refresh()
	t.label.Refresh()
	t.track.Refresh()
	t.thumb.Refresh()

	t.spinner.Hidden = !t.loading
	t.spinner.Refresh()
	switch {
	case t.loading && !t.anim.IsRunning():
		// Unlike the other spinner-driven buttons, don't restart from frame 0
		// on every refresh -- refreshVisuals runs on every SetOn/SetDisabled
		// too, which fire far more often than the animation's own frame tick.
		t.anim.Start(assets.LoadingGrayFrames, func(frame fyne.Resource) {
			if t.spinner == nil {
				return
			}
			t.spinner.Resource = frame
			t.spinner.Refresh()
		})
	case !t.loading:
		t.anim.Stop()
	}
}

type tailscaleHeaderToggleRenderer struct {
	toggle *tailscaleHeaderToggle
}

func (r *tailscaleHeaderToggleRenderer) Layout(size fyne.Size) {
	if r.toggle.bg == nil || r.toggle.border == nil || r.toggle.label == nil || r.toggle.track == nil || r.toggle.thumb == nil || r.toggle.spinner == nil {
		return
	}

	r.toggle.bg.Move(fyne.NewPos(0, 0))
	r.toggle.bg.Resize(size)
	r.toggle.border.Move(fyne.NewPos(0, 0))
	r.toggle.border.Resize(size)

	r.toggle.label.Move(fyne.NewPos(10, (size.Height-14)/2))
	r.toggle.label.Resize(fyne.NewSize(55, 14))

	trackSize := fyne.NewSize(24, 14)
	trackX := size.Width - trackSize.Width - 6
	trackY := (size.Height - trackSize.Height) / 2
	r.toggle.track.Move(fyne.NewPos(trackX, trackY))
	r.toggle.track.Resize(trackSize)

	thumbSize := float32(10)
	thumbY := trackY + 2
	thumbX := trackX + 2
	if r.toggle.on {
		thumbX = trackX + trackSize.Width - thumbSize - 2
	}
	r.toggle.thumb.Move(fyne.NewPos(thumbX, thumbY))
	r.toggle.thumb.Resize(fyne.NewSize(thumbSize, thumbSize))

	spinnerSize := float32(14)
	r.toggle.spinner.Move(fyne.NewPos(trackX+(trackSize.Width-spinnerSize)/2, trackY))
	r.toggle.spinner.Resize(fyne.NewSize(spinnerSize, spinnerSize))
}

func (r *tailscaleHeaderToggleRenderer) MinSize() fyne.Size {
	return r.toggle.MinSize()
}

func (r *tailscaleHeaderToggleRenderer) Refresh() {
	r.toggle.refreshVisuals()
	r.Layout(r.toggle.Size())
}

func (r *tailscaleHeaderToggleRenderer) Destroy() {
	r.toggle.anim.Stop()
}

func (r *tailscaleHeaderToggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.toggle.bg, r.toggle.label, r.toggle.track, r.toggle.thumb, r.toggle.spinner, r.toggle.border}
}

func (r *tailscaleHeaderToggleRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

var (
	_ fyne.Tappable     = (*tailscaleHeaderToggle)(nil)
	_ desktop.Hoverable = (*tailscaleHeaderToggle)(nil)
	_ fyne.Widget       = (*tailscaleHeaderToggle)(nil)
)

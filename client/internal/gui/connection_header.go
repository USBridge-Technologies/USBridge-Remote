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
	// OnOpenAccount opens the account login/sync dialog (see
	// MainWindow.showAccountDialog) -- fired by the login avatar button.
	OnOpenAccount func()
}

// ConnectionHeaderHandle lets the controller push live Tailscale status and
// account/login state into an already-built connection header, without
// owning (or even seeing the type of) any of its widgets. nil-safe: a nil
// accessory (wasm builds, see newConnectionHeader) yields a handle whose
// SetTailscaleState is a no-op.
type ConnectionHeaderHandle struct {
	toggle *tailscaleHeaderToggle
	avatar *loginAvatarButton
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

// SetAccountState updates the login avatar button from the account manager's
// login state -- teal background with the account email's first letter while
// logged in, the plain gray "U" placeholder otherwise (see
// loginAvatarButton.SetState).
func (h *ConnectionHeaderHandle) SetAccountState(loggedIn bool, email string) {
	if h == nil || h.avatar == nil {
		return
	}
	h.avatar.SetState(loggedIn, email)
}

// headerCompactButtonSize is how big the info/community/language buttons
// render in this header -- smaller and closer together than
// headerStatusBadgeButton's own 36x36 default (used as-is by the connected
// screen's video/audio status buttons), which is why each one here is
// wrapped in a GridWrap rather than changing that shared default.
var headerCompactButtonSize = fyne.NewSize(28, 28)

// gearIconHeader is a plain Material "settings" gear glyph, muted to match
// this header's other icon buttons (info/community/language -- see
// assets.LanguageIconHeader's own #c3c6b4) -- inline rather than a new
// assets.go:embed entry, the same way this package already inlines other
// small one-off SVGs (e.g. connection_list_table.go's edit/delete icons).
var gearIconHeader = fyne.NewStaticResource("gear-header.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#c3c6b4"><path d="M19.14,12.94c0.04-0.3,0.06-0.61,0.06-0.94c0-0.32-0.02-0.64-0.07-0.94l2.03-1.58c0.18-0.14,0.23-0.41,0.12-0.61l-1.92-3.32c-0.12-0.22-0.37-0.29-0.59-0.22l-2.39,0.96c-0.5-0.38-1.03-0.7-1.62-0.94L14.4,2.81c-0.04-0.24-0.24-0.41-0.48-0.41h-3.84c-0.24,0-0.43,0.17-0.47,0.41L9.25,5.35C8.66,5.59,8.12,5.92,7.63,6.29L5.24,5.33c-0.22-0.08-0.47,0-0.59,0.22L2.74,8.87C2.62,9.08,2.66,9.34,2.86,9.48l2.03,1.58C4.84,11.36,4.8,11.69,4.8,12s0.02,0.64,0.07,0.94l-2.03,1.58c-0.18,0.14-0.23,0.41-0.12,0.61l1.92,3.32c0.12,0.22,0.37,0.29,0.59,0.22l2.39-0.96c0.5,0.38,1.03,0.7,1.62,0.94l0.36,2.54c0.05,0.24,0.24,0.41,0.48,0.41h3.84c0.24,0,0.44-0.17,0.47-0.41l0.36-2.54c0.59-0.24,1.13-0.56,1.62-0.94l2.39,0.96c0.22,0.08,0.47,0,0.59-0.22l1.92-3.32c0.12-0.22,0.07-0.47-0.12-0.61L19.14,12.94z M12,15.6c-1.98,0-3.6-1.62-3.6-3.6s1.62-3.6,3.6-3.6s3.6,1.62,3.6,3.6S13.98,15.6,12,15.6z"/></svg>`))

// headerSettingsMenuActions is connectionHeaderActions minus
// OnToggleTailscale (newHeaderSettingsMenuButton's caller -- Control's own
// header, see createMainAddressBar -- has no Tailscale toggle to wire,
// unlike the connections screen's full newConnectionHeader) plus
// OnPowerReset, Control's own addition: the PC-panel power/reset button
// (controller.PCPanelWidget) used to sit in the header as its own icon;
// it's now this menu's first row instead (see PCPanelWidget.ShowPowerMenu),
// freeing that space for the Control/Devices/Snapshots/Scripts selector
// (mainWindowLayout's own tabHeaderButtons).
type headerSettingsMenuActions struct {
	OnPowerReset       func()
	OnShowLanguageMenu func(anchor fyne.CanvasObject)
	OnOpenCommunity    func()
	OnOpenInfo         func()
	OnOpenAccount      func()
}

// newHeaderSettingsMenuButton builds a single gear-icon button that opens a
// ShowStyledMenuTeal dropdown (the same teal/10px look as the language
// menu's own popup -- see connection_manager_ui.go's showLanguageMenu)
// listing Power Reset/Info/Community/Language/Account -- Control's own
// reuse of newConnectionHeader's accessory row (see createMainAddressBar),
// collapsed into one button instead of several separate ones so they don't
// compete for space with Control's own tab-selector/status-icon row.
// Language's own row just forwards to actions.OnShowLanguageMenu, opening
// that same language popup anchored to this gear button.
func newHeaderSettingsMenuButton(actions headerSettingsMenuActions) fyne.CanvasObject {
	var btn *headerStatusBadgeButton
	btn = newHeaderStatusBadgeButton(gearIconHeader, func() {
		view.ShowStyledMenuTeal(btn, []view.StyledMenuItem{
			{Label: "Power Reset", OnTap: func() {
				if actions.OnPowerReset != nil {
					actions.OnPowerReset()
				}
			}},
			{Label: "Info", OnTap: func() {
				if actions.OnOpenInfo != nil {
					actions.OnOpenInfo()
				}
			}},
			{Label: "Community", OnTap: func() {
				if actions.OnOpenCommunity != nil {
					actions.OnOpenCommunity()
				}
			}},
			{Label: "Language", OnTap: func() {
				if actions.OnShowLanguageMenu != nil {
					actions.OnShowLanguageMenu(btn)
				}
			}},
			{Label: "Account", OnTap: func() {
				if actions.OnOpenAccount != nil {
					actions.OnOpenAccount()
				}
			}},
		})
	})
	btn.SetBadgeText("")
	btn.SetIconSize(fyne.NewSize(15, 15))
	return container.NewGridWrap(headerCompactButtonSize, btn)
}

// newConnectionHeader builds the top bar shown on the connections screen
// (before a device is connected): logo+wordmark lockup on the left, and on
// the right the Tailscale toggle, info, community and language buttons. The
// returned handle is how the controller later pushes Tailscale status into
// the toggle it just built.
//
// This is the desktop-only design for now -- there is no mobile variant of
// this component yet. When one exists, the choice between them belongs in
// the caller (createConnectionAddressBar), not inside this component.
func newConnectionHeader(actions connectionHeaderActions) (*fyne.Container, *ConnectionHeaderHandle) {
	logoLockup := canvas.NewImageFromResource(assets.LogoUSBridgeLockup)
	logoLockup.FillMode = canvas.ImageFillContain
	const logoAspectRatio = 951.0 / 236.0
	const logoHeight = 28
	logoLockup.SetMinSize(fyne.NewSize(logoHeight*logoAspectRatio, logoHeight))

	var langBtn *headerStatusBadgeButton
	langBtn = newHeaderStatusBadgeButton(assets.LanguageIconHeader, func() {
		if actions.OnShowLanguageMenu != nil {
			actions.OnShowLanguageMenu(langBtn)
		}
	})
	langBtn.SetBadgeText("")
	langBtn.SetIconSize(fyne.NewSize(15, 15))

	communityBtn := newHeaderStatusBadgeButton(assets.DiscordIconHeader, func() {
		if actions.OnOpenCommunity != nil {
			actions.OnOpenCommunity()
		}
	})
	communityBtn.SetBadgeText("")
	communityBtn.SetIconSize(fyne.NewSize(15, 15))

	infoBtn := newHeaderStatusBadgeButton(assets.QuestionIconHeader, func() {
		if actions.OnOpenInfo != nil {
			actions.OnOpenInfo()
		}
	})
	infoBtn.SetBadgeText("")
	infoBtn.SetIconSize(fyne.NewSize(15, 15))

	// Account/login avatar -- opens the account login/sync dialog (see
	// connectionHeaderActions.OnOpenAccount, wired by createConnectionAddressBar
	// to MainWindow.showAccountDialog).
	loginBtn := newLoginAvatarButton("U", func() {
		if actions.OnOpenAccount != nil {
			actions.OnOpenAccount()
		}
	})

	var tailscaleAccessory fyne.CanvasObject
	handle := &ConnectionHeaderHandle{avatar: loginBtn}
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

	rightRow := container.New(&centeredInlineLayout{gap: 4, minGap: 2},
		tailscaleAccessory,
		container.NewGridWrap(headerCompactButtonSize, infoBtn),
		container.NewGridWrap(headerCompactButtonSize, communityBtn),
		container.NewGridWrap(headerCompactButtonSize, langBtn),
		container.NewGridWrap(headerCompactButtonSize, loginBtn),
	)

	row := container.NewHBox(logoLockup, layout.NewSpacer(), rightRow)

	bg := canvas.NewRectangle(design.ColorGray900)
	// Bottom inset 0, not 2 -- with the accent line's own 0.5px reserved
	// right below this (see accentLine/content below), a symmetric 2/2
	// left visibly more free space under the row than above it, and the
	// whole band a couple px taller than it needed to be.
	paddedRow := view.NewInset(row, 16, 16, 2, 0)

	accentLine := canvas.NewRectangle(design.ColorHeaderAccentLine)
	accentLine.SetMinSize(fyne.NewSize(1, 0.5))

	content := container.NewBorder(nil, accentLine, nil, nil, paddedRow)

	return container.NewStack(bg, content), handle
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

	bg     *canvas.Rectangle
	border *canvas.Rectangle
	label  *canvas.Text
	track  *canvas.Rectangle
	thumb  *canvas.Circle
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
		trackColor = design.ColorAccent
		thumbColor = design.ColorWhite
	}
	if t.disabled {
		// Loading always sets disabled too (see ConnectionHeaderHandle.
		// SetTailscaleState) -- this is the toggle's only "thinking" cue: no
		// spinner, just the track/thumb dimming and going unclickable.
		labelColor = design.ColorGray400
		trackColor = design.ColorGray950
		borderColor = design.ColorGray900
	}

	t.bg.FillColor = bgColor
	t.border.StrokeColor = borderColor
	t.border.StrokeWidth = 1
	t.label.Color = labelColor

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
}

type tailscaleHeaderToggleRenderer struct {
	toggle *tailscaleHeaderToggle
}

func (r *tailscaleHeaderToggleRenderer) Layout(size fyne.Size) {
	if r.toggle.bg == nil || r.toggle.border == nil || r.toggle.label == nil || r.toggle.track == nil || r.toggle.thumb == nil {
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
}

func (r *tailscaleHeaderToggleRenderer) MinSize() fyne.Size {
	return r.toggle.MinSize()
}

func (r *tailscaleHeaderToggleRenderer) Refresh() {
	r.toggle.refreshVisuals()
	r.Layout(r.toggle.Size())
}

func (r *tailscaleHeaderToggleRenderer) Destroy() {}

func (r *tailscaleHeaderToggleRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.toggle.bg, r.toggle.label, r.toggle.track, r.toggle.thumb, r.toggle.border}
}

func (r *tailscaleHeaderToggleRenderer) BackgroundColor() color.Color {
	return color.Transparent
}

var (
	_ fyne.Tappable     = (*tailscaleHeaderToggle)(nil)
	_ desktop.Hoverable = (*tailscaleHeaderToggle)(nil)
	_ fyne.Widget       = (*tailscaleHeaderToggle)(nil)
	_ fyne.Tappable     = (*loginAvatarButton)(nil)
	_ desktop.Hoverable = (*loginAvatarButton)(nil)
	_ fyne.Widget       = (*loginAvatarButton)(nil)
)

// loginAvatarButton is the connections screen's placeholder account/login
// avatar (circle + initial letter) shown next to the language icon -- no
// functionality yet. A dedicated widget rather than headerStatusBadgeButton
// for two reasons: that widget's hover fill is a rounded *square*, wrong
// behind a circular avatar, and Fyne's SVG renderer doesn't support <text>
// elements at all -- an SVG-baked letter silently never draws, so it has to
// be a real canvas.Text instead.
type loginAvatarButton struct {
	widget.BaseWidget

	letterText string
	onTapped   func()
	hovered    bool
	// loggedIn switches refreshVisuals from the plain gray placeholder look
	// to a teal-filled avatar -- set via SetState, driven by
	// ConnectionHeaderHandle.SetAccountState (ultimately AccountManager's
	// own login state).
	loggedIn bool

	circle *canvas.Circle
	letter *canvas.Text
}

func newLoginAvatarButton(letterText string, onTapped func()) *loginAvatarButton {
	b := &loginAvatarButton{letterText: letterText, onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *loginAvatarButton) MinSize() fyne.Size {
	return fyne.NewSize(24, 24)
}

func (b *loginAvatarButton) CreateRenderer() fyne.WidgetRenderer {
	b.circle = canvas.NewCircle(design.ColorLoginAvatarBg)
	b.circle.StrokeColor = design.ColorHeaderAccentLine
	b.circle.StrokeWidth = 1.5

	b.letter = canvas.NewText(b.letterText, design.ColorLoginAvatarText)
	b.letter.TextSize = 11
	b.letter.TextStyle = fyne.TextStyle{Bold: true}
	b.letter.Alignment = fyne.TextAlignCenter

	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewStack(b.circle, container.NewCenter(b.letter)))
}

func (b *loginAvatarButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *loginAvatarButton) TappedSecondary(*fyne.PointEvent) {}

func (b *loginAvatarButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *loginAvatarButton) MouseMoved(*desktop.MouseEvent) {}

func (b *loginAvatarButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

// SetState reflects the account manager's login state onto the avatar:
// teal background with the email's first letter (uppercased) while logged
// in, or back to the plain gray placeholder letter ("U") when logged out.
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

// refreshVisuals only ever changes the circle's own fill (plus the letter's
// color, since a teal-filled circle needs a dark letter to stay readable) --
// never adds a separate hover shape -- so the hover state stays circular no
// matter what.
func (b *loginAvatarButton) refreshVisuals() {
	if b.circle == nil {
		return
	}
	fill := design.ColorLoginAvatarBg
	letterColor := design.ColorLoginAvatarText
	if b.loggedIn {
		fill = design.ColorConnectionBadgeText
		letterColor = design.ColorGray950
		if b.hovered {
			fill = color.NRGBA{R: 0x61, G: 0xf0, B: 0xd3, A: 0xff} // same hover teal Save/Apply use
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

package gui

import (
	"context"
	"fmt"
	"image/color"
	"strings"
	"time"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"

	_ "embed"
	_ "golang.org/x/image/webp"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

//go:embed assets/google-logo-rounded-google-logo-google-gradient-logo-free-png.webp
var googleLogoBytes []byte

type accountGoogleLoginButton struct {
	widget.BaseWidget
	onTapped func()
	hovered  bool
}

func newAccountGoogleLoginButton(onTapped func()) *accountGoogleLoginButton {
	b := &accountGoogleLoginButton{onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *accountGoogleLoginButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff})
	bg.CornerRadius = 8
	icon := canvas.NewImageFromResource(fyne.NewStaticResource("google.webp", googleLogoBytes))
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(16, 16))
	text := canvas.NewText(i18n.Current.AccountLoginGoogle, color.NRGBA{R: 0x4c, G: 0x68, B: 0x03, A: 0xff})
	text.TextSize = 12
	text.TextStyle.Bold = true
	return &accountGoogleLoginButtonRenderer{
		btn:     b,
		bg:      bg,
		icon:    icon,
		text:    text,
		objects: []fyne.CanvasObject{bg, icon, text},
	}
}

func (b *accountGoogleLoginButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}
func (b *accountGoogleLoginButton) TappedSecondary(*fyne.PointEvent) {}
func (b *accountGoogleLoginButton) Cursor() desktop.Cursor           { return desktop.PointerCursor }
func (b *accountGoogleLoginButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}
func (b *accountGoogleLoginButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}
func (b *accountGoogleLoginButton) MouseMoved(*desktop.MouseEvent) {}

type accountGoogleLoginButtonRenderer struct {
	btn     *accountGoogleLoginButton
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *accountGoogleLoginButtonRenderer) Destroy() {}
func (r *accountGoogleLoginButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *accountGoogleLoginButtonRenderer) MinSize() fyne.Size {
	const (
		padX = float32(18)
		padY = float32(8)
		icon = float32(16)
		gap  = float32(8)
		minH = float32(36)
	)
	ts := r.text.MinSize()
	// canvas.Text MinSize clips descenders (g/y) and the last glyph's
	// side bearing; keep extra room so "Google" is fully visible.
	textW := ts.Width + 4
	textH := ts.Height + 4
	h := textH + padY*2
	if h < minH {
		h = minH
	}
	return fyne.NewSize(padX*2+icon+gap+textW, h)
}

func (r *accountGoogleLoginButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	const iconSize, gap = float32(16), float32(8)
	ts := r.text.MinSize()
	textW := ts.Width + 4
	textH := ts.Height + 4
	contentW := iconSize + gap + textW
	start := (size.Width - contentW) / 2
	if start < 18 {
		start = 18
	}
	r.icon.Resize(fyne.NewSize(iconSize, iconSize))
	r.icon.Move(fyne.NewPos(start, (size.Height-iconSize)/2))
	r.text.Resize(fyne.NewSize(textW, textH))
	r.text.Move(fyne.NewPos(start+iconSize+gap, (size.Height-textH)/2))
}

func (r *accountGoogleLoginButtonRenderer) Refresh() {
	if r.btn.hovered {
		r.bg.FillColor = color.NRGBA{R: 0xb4, G: 0xd8, B: 0x6a, A: 0xff}
	} else {
		r.bg.FillColor = color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0xff}
	}
	r.bg.Refresh()
	r.text.Refresh()
	r.icon.Refresh()
	r.Layout(r.btn.Size())
}

// accountDialogSnapshot is the subset of AccountManager state that actually
// changes what showAccountDialog's body needs to look like -- compared
// tick-to-tick by the background poller (see showAccountDialog) so a
// render only happens on a REAL transition (login started/finished/failed,
// logged out), never unconditionally on every 2s tick. Rebuilding the
// whole body on every tick regardless of whether anything changed is what
// caused the dialog to visibly flicker (body flashing in and out)
// and, worse, wiped out the sync-passphrase Entry's in-progress text on
// every tick -- widget.NewPasswordEntry() started over from empty each
// time body.RemoveAll() ran, so a passphrase could never actually be typed
// in before the next tick erased it.
type accountDialogSnapshot struct {
	loginInProgress bool
	loggedIn        bool
	lastError       string
}

func newAccountDialogSnapshot(am *controller.AccountManager) accountDialogSnapshot {
	return accountDialogSnapshot{
		loginInProgress: am.LoginInProgress(),
		loggedIn:        am.LoggedIn(),
		lastError:       am.LastError(),
	}
}

// showAccountDialog is the client's account button's single entry point --
// a small self-re-rendering dialog driven by a status snapshot: who am I
// signed in as, plus the sync passphrase that end-to-end encrypts the
// synced connections list (see internal/syncconn, connection_manager_sync.go).
func (mw *MainWindow) showAccountDialog() {
	if mw.connectionManager == nil || mw.connectionManager.Account == nil {
		return
	}
	cm := mw.connectionManager
	am := cm.Account

	body := container.NewVBox()
	// resettingSyncPassphrase: true while the "Forgot passphrase? Reset
	// it" flow (see accountSyncPassphraseSection) is showing its
	// new-passphrase entry -- a UI-only flag, not part of AccountManager's
	// own state (render() rebuilds the whole body on every call, so
	// anything that must survive across renders lives out here).
	var resettingSyncPassphrase bool

	var scroll *container.Scroll
	footerContainer := container.NewStack()

	var render func()
	render = func() {
		body.RemoveAll()
		footerContainer.Objects = nil

		switch {
		case am.LoginInProgress():
			lbl := widget.NewLabel(i18n.Current.AccountWaitingGoogle)
			lbl.Wrapping = fyne.TextWrapWord
			lbl.Alignment = fyne.TextAlignCenter
			styledLbl := wrapAccountField(lbl, 12, color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
			body.Add(styledLbl)

			prog := widget.NewProgressBarInfinite()
			styledProg := container.NewThemeOverride(prog, &progressTheme{Theme: theme.DefaultTheme()})
			progContainer := container.New(&fixedHeightLayout{height: 5}, styledProg)

			// Add some padding above and below the progress bar
			body.Add(view.NewInset(progContainer, 0, 0, 8, 4))

			cancelBtn := newAccountDialogTextButton(i18n.Current.Cancel, func() {
				am.CancelLogin()
				render()
			})
			body.Add(container.NewCenter(cancelBtn))

		case am.LoggedIn():
			emailText := newAccountEmailText(am.Email())

			var identityHeader fyne.CanvasObject
			if resettingSyncPassphrase {
				// Hide avatar and "Signed in as"
				identityHeader = emailText
			} else {
				trimmed := strings.TrimSpace(am.Email())
				letter := "U"
				if trimmed != "" {
					letter = strings.ToUpper(string([]rune(trimmed)[0]))
				}
				signedInLabel := canvas.NewText(i18n.Current.AccountSignedInAs, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff})
				signedInLabel.TextSize = 10
				identityCopy := view.NewInset(container.NewVBox(signedInLabel, emailText), 10, 0, 0, 0)
				if accountDialogMobile() {
					identityHeader = container.NewBorder(nil, nil, newAccountAvatarBadge(letter), nil, identityCopy)
				} else {
					identityHeader = container.NewHBox(newAccountAvatarBadge(letter), identityCopy)
				}
			}

			var identityBody *fyne.Container
			if resettingSyncPassphrase {
				identityBody = container.New(&tightVBoxLayout{}, identityHeader)
			} else {
				identityBody = container.NewVBox(identityHeader)
			}

			if errMsg := am.LastError(); errMsg != "" {
				errText := canvas.NewText(errMsg, design.ColorAlert)
				errText.TextSize = 11
				identityBody.Add(errText)
			}

			identityBody.Add(newAccountDivider())
			syncContent, syncFooter := accountSyncPassphraseSection(cm, am, &resettingSyncPassphrase, render)
			identityBody.Add(syncContent)

			body.Add(newAccountCard(identityBody))

			var footerLeft fyne.CanvasObject
			if am.HasSyncKey() && !resettingSyncPassphrase {
				footerLeft = newAccountDialogLinkButton(i18n.Current.AccountForgotPassphrase, i18n.Current.AccountResetIt, func() {
					resettingSyncPassphrase = true
					render()
				})
			} else if syncFooter != nil {
				footerLeft = syncFooter
			}

			logoutIconNormal := fyne.NewStaticResource("logout.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#e0e3e7"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg>`))
			logoutIconHover := fyne.NewStaticResource("logout_hover.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#ed6b7f"><path d="M17 7l-1.41 1.41L18.17 11H8v2h10.17l-2.58 2.58L17 17l5-5zM4 5h8V3H4c-1.1 0-2 .9-2 2v14c0 1.1.9 2 2 2h8v-2H4V5z"/></svg>`))

			logoutBtn := newAccountDialogDarkButton(i18n.Current.AccountLogOut, logoutIconNormal, logoutIconHover, func() {
				am.Logout()
				resettingSyncPassphrase = false
				render()
			})

			var footerLeftCentered fyne.CanvasObject
			if footerLeft != nil {
				footerLeftCentered = container.NewCenter(footerLeft)
			}

			footerBar := container.NewBorder(nil, nil, footerLeftCentered, logoutBtn)
			if !am.HasSyncKey() && !resettingSyncPassphrase {
				// Password-entry footer: Log out left, Set passphrase right.
				// The other way around put the primary action on the left
				// and made the row read backwards.
				footerBar = container.NewBorder(nil, nil, logoutBtn, footerLeftCentered)
			}
			fl, fr, ft, fb := accountDialogFooterInset()
			footerArea := container.NewVBox(
				newAccountDivider(),
				view.NewInset(footerBar, fl, fr, ft, fb),
			)
			footerContainer.Objects = []fyne.CanvasObject{footerArea}

		default:
			intro := widget.NewLabel(i18n.Current.AccountLoginIntro)
			intro.Wrapping = fyne.TextWrapWord
			intro.Alignment = fyne.TextAlignCenter
			styledIntro := wrapAccountField(intro, 12, color.NRGBA{R: 0xc5, G: 0xc8, B: 0xb5, A: 0xff})
			body.Add(styledIntro)

			loginBtn := newAccountGoogleLoginButton(func() {
				if err := am.StartLogin(); err == nil {
					render()
				}
			})
			body.Add(container.NewCenter(loginBtn))

			if errMsg := am.LastError(); errMsg != "" {
				errText := canvas.NewText(errMsg, design.ColorAlert)
				errText.TextSize = 11
				body.Add(errText)
			}
		}
		body.Refresh()
		footerContainer.Refresh()

		applyAccountDialogScroll(scroll, body, am.LoggedIn(), am.HasSyncKey(), am.LoginInProgress())
	}
	render()

	// Chrome restyled after the Add Connection dialog's own panel: title +
	// X header with the teal-to-lime top accent hairline, dark
	// design.ColorGray900 background, thin design.ColorBorder outline --
	// instead of this dialog's old plain dialog.NewCustom frame.
	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	title := view.NewBrandText(i18n.Current.AccountTitle, 13, design.ColorTextLight, true)
	closeBtn := newAccountDialogIconButton(accountDialogCloseIcon, closeDialog)
	topAccent := newAccountDialogTopAccentBar()

	sep := canvas.NewRectangle(color.NRGBA{R: 0x30, G: 0x34, B: 0x2e, A: 0xff})
	sep.SetMinSize(fyne.NewSize(0, 1))

	// right=44 on the title's own inset (not closeBtn sharing this row)
	// reserves clearance so the title never runs under closeBtn, which sits
	// on its own layer closer to the panel's actual corner (see cornerBtn
	// below) -- same reasoning as the Add Connection dialog's own header.
	tl, tr, tt, tb := accountDialogTitleInset()
	header := container.NewVBox(topAccent, view.NewInset(title, tl, tr, tt, tb), sep)

	scroll = container.NewVScroll(nil)
	applyAccountDialogScroll(scroll, body, am.LoggedIn(), am.HasSyncKey(), am.LoginInProgress())

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	// closeBtn sits on its own layer, pinned close to the panel's actual
	// top-right corner rather than sharing header's own content margin --
	// same accountDialogCornerButtonLayout treatment (mirroring
	// controller.dialogCornerButtonLayout) the Add Connection dialog and QR
	// scanner popup's own header X use.
	cornerBtn := container.New(&accountDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn)

	panel := container.NewStack(
		bg,
		container.NewBorder(header, footerContainer, nil, nil, scroll),
		cornerBtn,
		border,
	)

	popup = view.ShowOverlayPopup(mw.window, view.OverlayPopupSpec{
		Panel:    panel,
		DimColor: color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
			return accountDialogPanelSize(panel, canvasSize)
		},
		PanelPos: func(canvasSize fyne.Size, panelSize fyne.Size) fyne.Position {
			return accountDialogPanelPos(canvasSize, panelSize)
		},
	})

	// Polls while the dialog is open (2s cadence) so a login completing
	// in the browser is
	// reflected without needing to close and reopen this dialog -- but
	// only actually re-renders (rebuilding every widget, including
	// whatever Entry the human might be mid-typing into) when the
	// snapshot genuinely changed since the last tick. See
	// accountDialogSnapshot's own doc comment for why this matters.
	// Stops itself once popup is no longer visible (X button, tap-outside,
	// or however else it closed) rather than needing an explicit
	// close-hook -- widget.PopUp has no SetOnClosed equivalent.
	go func() {
		last := newAccountDialogSnapshot(am)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if popup == nil || !popup.Visible() {
				return
			}
			next := newAccountDialogSnapshot(am)
			if next == last {
				continue
			}
			last = next
			fyne.Do(render)
		}
	}()
}

// accountDialogCloseIcon is the same muted-olive X glyph the Add Connection
// dialog's own header close button uses (controller package's
// connectionDialogCancelIconRes) -- duplicated here (one line of SVG)
// rather than exported across the package boundary just for this.
var accountDialogCloseIcon = fyne.NewStaticResource("account_dialog_cancel.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#8f9381"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))

// accountDialogIconButton is a minimal transparent-until-hovered icon
// button -- originally a trimmed-down copy of the Add Connection dialog's
// own close button (controller.connectionDialogIconButton), generalized to
// take any icon resource/size (see the header close button).
type accountDialogIconButton struct {
	widget.BaseWidget

	resource fyne.Resource
	onTapped func()
	hovered  bool

	opaqueIcon bool
	iconSize   float32
	buttonSize float32

	bg   *canvas.Rectangle
	bdr  *canvas.Rectangle
	icon *canvas.Image
}

func newAccountDialogIconButton(resource fyne.Resource, onTapped func()) *accountDialogIconButton {
	b := &accountDialogIconButton{
		resource:   resource,
		onTapped:   onTapped,
		iconSize:   18,
		buttonSize: 28,
	}
	b.ExtendBaseWidget(b)
	return b
}

func (b *accountDialogIconButton) MinSize() fyne.Size {
	return fyne.NewSize(b.buttonSize, b.buttonSize)
}

func (b *accountDialogIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 6

	b.bdr = canvas.NewRectangle(color.Transparent)
	b.bdr.CornerRadius = 6
	b.bdr.StrokeWidth = 1

	b.icon = canvas.NewImageFromResource(b.resource)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.ScaleMode = canvas.ImageScaleSmooth
	b.icon.SetMinSize(fyne.NewSize(b.iconSize, b.iconSize))
	if !b.opaqueIcon {
		b.icon.Translucency = 0.32
	}

	return widget.NewSimpleRenderer(container.NewStack(b.bg, b.bdr, container.NewCenter(b.icon)))
}

func (b *accountDialogIconButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}

func (b *accountDialogIconButton) TappedSecondary(*fyne.PointEvent) {}

func (b *accountDialogIconButton) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}

func (b *accountDialogIconButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}

func (b *accountDialogIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *accountDialogIconButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}

func (b *accountDialogIconButton) refreshVisuals() {
	if b.bg == nil {
		return
	}
	if b.hovered {
		b.bg.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10}
		b.bdr.StrokeColor = color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}
		if !b.opaqueIcon {
			b.icon.Translucency = 0.08
		}
	} else {
		b.bg.FillColor = color.Transparent
		b.bdr.StrokeColor = color.Transparent
		if !b.opaqueIcon {
			b.icon.Translucency = 0.32
		}
	}
	b.bg.Refresh()
	b.bdr.Refresh()
	b.icon.Refresh()
}

// newAccountDialogTopAccentBar is the same thin teal-to-lime fade hairline
// the Add Connection dialog and QR scanner popups carry along their top
// edge -- duplicated here rather than exported across the package boundary
// (see controller.newConnectionDialogTopAccentBar, the original).
func newAccountDialogTopAccentBar() fyne.CanvasObject {
	teal := design.ColorConnectionBadgeText
	lime := design.ColorConnectionAddFill
	tealTransparent := color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0}
	limeTransparent := color.NRGBA{R: 0xc4, G: 0xe7, B: 0x7a, A: 0}
	leftFade := canvas.NewHorizontalGradient(tealTransparent, teal)
	leftFade.SetMinSize(fyne.NewSize(70, 2))
	rightFade := canvas.NewHorizontalGradient(lime, limeTransparent)
	rightFade.SetMinSize(fyne.NewSize(70, 2))
	mid := canvas.NewHorizontalGradient(teal, lime)
	return container.NewBorder(nil, nil, leftFade, rightFade, mid)
}

// accountDialogCornerButtonLayout pins its single child at a fixed offset
// from the panel's top-right corner, at the child's own natural size --
// mirrors controller.dialogCornerButtonLayout (used by the Add Connection
// dialog and QR scanner popups for the same "X sits in the corner, decoupled
// from the title's own margin" placement), duplicated here rather than
// exported across the package boundary for one small layout type.
type accountDialogCornerButtonLayout struct {
	Top   float32
	Right float32
}

func (l *accountDialogCornerButtonLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	btn := objects[0]
	btnSize := btn.MinSize()
	btn.Resize(btnSize)
	btn.Move(fyne.NewPos(size.Width-l.Right-btnSize.Width, l.Top))
}

func (l *accountDialogCornerButtonLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

func clampFloat32(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// accountSyncPassphraseSection lets the human set (or, on a second device,
// re-enter the same one they used on the first) the passphrase that
// derives this device's connections-sync encryption key -- see
// internal/syncconn's doc comment for why this is a SEPARATE secret from
// the Google login above, never sent to any server. Also covers the
// "I forgot my passphrase" recovery path, gated by *resetting -- see
// ResetSyncPassphrase's own doc comment for why that's a genuinely
// different operation from the normal set-passphrase one below (it
// deliberately overwrites the account's synced data instead of merging
// with it, since nothing can decrypt the old blob anymore once its
// passphrase is forgotten).
type progressTheme struct {
	fyne.Theme
}

func (t *progressTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return color.NRGBA{R: 0x41, G: 0xe0, B: 0xc3, A: 0xff} // Slider color
	}
	if name == theme.ColorNameInputBackground || name == theme.ColorNameButton || name == theme.ColorNameScrollBarBackground {
		return color.NRGBA{R: 0x2e, G: 0x9e, B: 0x8a, A: 0xff} // Background color
	}
	return t.Theme.Color(name, v)
}

func (t *progressTheme) Size(name fyne.ThemeSizeName) float32 {
	if strings.HasSuffix(string(name), "Radius") {
		return 3 // smaller radius for the 5px bar
	}
	return t.Theme.Size(name)
}

type fixedHeightLayout struct {
	height float32
}

func (l *fixedHeightLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		if size.Width < o.MinSize().Width {
			size.Width = o.MinSize().Width
		}
	}
	size.Height = l.height
	return size
}

func (l *fixedHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		o.Resize(fyne.NewSize(size.Width, l.height))
		o.Move(fyne.NewPos(0, 0))
	}
}

type tightVBoxLayout struct{}

func (t *tightVBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var size fyne.Size
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		if m.Width > size.Width {
			size.Width = m.Width
		}
		size.Height += m.Height
	}
	return size
}

func (t *tightVBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	y := float32(0)
	for _, o := range objects {
		if !o.Visible() {
			continue
		}
		m := o.MinSize()
		o.Move(fyne.NewPos(0, y))
		o.Resize(fyne.NewSize(size.Width, m.Height))
		y += m.Height
	}
}

func accountSyncPassphraseSection(cm *controller.ConnectionManager, am *controller.AccountManager, resetting *bool, render func()) (fyne.CanvasObject, fyne.CanvasObject) {
	titleText := canvas.NewText(i18n.Current.AccountConnectionsSync, design.ColorTextLight)
	titleText.TextSize = 12
	titleText.TextStyle = fyne.TextStyle{Bold: true}

	on := am.HasSyncKey() && !*resetting
	pill := newAccountStatusPill(map[bool]string{true: i18n.Current.AccountSyncOn, false: i18n.Current.AccountSyncOff}[on], on)

	if on {
		desc := newAccountSyncOnDescription()
		autoSync := newAccountAutoSyncRow(cm.AutoSyncNewConnections(), func(checked bool) {
			cm.SetAutoSyncNewConnections(checked)
		})
		titleRow := container.NewBorder(nil, nil, titleText, view.NewInset(pill, 0, 0, 2, 0))
		return view.NewInset(container.New(&tightVBoxLayout{}, titleRow, desc, autoSync), 0, 0, 4, 0), nil
	}

	titleRow := container.NewBorder(nil, nil, titleText, view.NewInset(pill, 0, 0, 2, 0))

	if *resetting {
		warn := widget.NewLabel(i18n.Current.AccountResetWarn)
		warn.Wrapping = fyne.TextWrapWord
		styledWarn := wrapAccountField(warn, 8, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff})

		entry := widget.NewPasswordEntry()
		entry.SetPlaceHolder(i18n.Current.AccountNewPassphrase)
		entry.TextStyle.Monospace = true
		styledEntry := wrapAccountField(entry, 10, color.NRGBA{R: 0xe9, G: 0xfd, B: 0xbb, A: 0xff})

		statusLabel := widget.NewLabel("")
		statusLabel.Wrapping = fyne.TextWrapWord
		styledStatus := wrapAccountField(statusLabel, 12, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff})
		styledStatus.Hide()

		var resetBtn, cancelBtn fyne.CanvasObject

		resetBtn = newAccountDialogDarkButton(i18n.Current.AccountResetOverwrite, nil, nil, func() {
			if entry.Text == "" {
				return
			}
			statusLabel.SetText(i18n.Current.AccountResetting)
			styledStatus.Show()
			styledEntry.Hide()
			resetBtn.Hide()
			cancelBtn.Hide()

			go func() {
				err := cm.ResetSyncPassphrase(context.Background(), entry.Text)
				if err != nil {
					fyne.Do(func() {
						statusLabel.SetText(fmt.Sprintf(i18n.Current.AccountResetFailed, err))
						styledEntry.Show()
						resetBtn.Show()
						cancelBtn.Show()
						// Do NOT call render() here, it would destroy and recreate the view with an empty label!
					})
					return
				}
				*resetting = false
				fyne.Do(render)
			}()
		})

		cancelBtn = newAccountDialogTextButton(i18n.Current.Cancel, func() {
			*resetting = false
			render()
		})

		return container.New(&tightVBoxLayout{}, titleRow, styledWarn, styledEntry, styledStatus), container.NewHBox(cancelBtn, resetBtn)
	}

	label := widget.NewLabel(i18n.Current.AccountSetPassphraseHint)
	label.Wrapping = fyne.TextWrapWord
	styledLabel := wrapAccountField(label, 8, color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff})

	entry := widget.NewPasswordEntry()
	entry.SetPlaceHolder(i18n.Current.AccountPassphrasePlaceholder)
	entry.TextStyle.Monospace = true
	styledEntry := wrapAccountField(entry, 10, color.NRGBA{R: 0xe9, G: 0xfd, B: 0xbb, A: 0xff})

	saveBtn := newAccountDialogDarkButton(i18n.Current.AccountSetPassphrase, nil, nil, func() {
		if entry.Text == "" {
			return
		}
		am.SetSyncPassphrase(entry.Text)
		render()
	})
	return container.New(&tightVBoxLayout{}, titleRow, styledLabel, styledEntry), container.NewHBox(saveBtn)
}

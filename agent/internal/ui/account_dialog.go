package ui

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/assets"
	"usbridge_agent/internal/account"
	"usbridge_agent/internal/ui/design"
)

// openAccount is the header avatar (and Protocol Login) entry: logged-out
// opens the Google login overlay; logged-in opens the account menu.
func (w *Window) openAccount(parent fyne.Window, anchor fyne.CanvasObject) {
	if w.token == nil {
		return
	}
	acc := w.token.AccountStatus()
	if acc.LoggedIn && !acc.LoginInProgress {
		w.showAccountMenu(anchor)
		return
	}
	w.showAccountLoginDialog(parent)
}

func (w *Window) showAccountLoginDialog(parent fyne.Window) {
	if parent == nil || w.token == nil {
		return
	}

	body := container.NewVBox()
	var loginURLFallback string
	var popup *widget.PopUp
	stopPoll := make(chan struct{})
	var stopped bool
	closeDialog := func() {
		if !stopped {
			stopped = true
			close(stopPoll)
		}
		if popup != nil {
			popup.Hide()
		}
	}

	var render func()
	render = func() {
		body.RemoveAll()
		acc := w.token.AccountStatus()
		w.refreshAccountAvatar()

		if acc.LoggedIn && !acc.LoginInProgress {
			closeDialog()
			return
		}

		switch {
		case acc.LoginInProgress:
			wait := widget.NewLabel(loc().WaitingGoogleLogin)
			wait.Wrapping = fyne.TextWrapWord
			wait.Alignment = fyne.TextAlignCenter
			body.Add(wrapAccountField(wait, 12, design.ColorMutedOlive))
			prog := widget.NewProgressBarInfinite()
			styledProg := container.NewThemeOverride(prog, &accountProgressTheme{Theme: design.NewBrandTheme()})
			body.Add(newExactInset(container.New(&fixedHeightLayout{height: 5}, styledProg), 0, 0, 8, 4))
			body.Add(container.NewCenter(newAccountDialogTextButton(loc().Cancel, func() {
				w.token.CancelAccountLogin()
				render()
			})))
			if loginURLFallback != "" {
				hint := widget.NewLabel(loc().CouldntOpenBrowserLogin)
				hint.Wrapping = fyne.TextWrapWord
				body.Add(wrapAccountField(hint, 11, design.ColorMutedOlive))
				if parsed, err := url.Parse(loginURLFallback); err == nil && parsed != nil {
					link := widget.NewHyperlink(loginURLFallback, parsed)
					link.Wrapping = fyne.TextWrapBreak
					body.Add(wrapAccountField(link, 11, design.ColorTeal))
				}
			}

		default:
			intro := widget.NewLabel(loc().LoginIntro)
			intro.Wrapping = fyne.TextWrapWord
			intro.Alignment = fyne.TextAlignCenter
			body.Add(wrapAccountField(intro, 12, design.ColorMutedOlive))
			body.Add(spacerSize(1, 4))
			body.Add(container.NewCenter(newGoogleLoginButton(func() {
				loginURLFallback = ""
				go func() {
					loginURL, err := w.token.StartAccountLogin()
					if err != nil {
						fyne.Do(func() {
							w.refreshAccountAvatar()
							render()
						})
						return
					}
					parsed, parseErr := url.Parse(loginURL)
					openErr := parseErr
					if parseErr == nil && w.app != nil {
						openErr = w.app.OpenURL(parsed)
					}
					fyne.Do(func() {
						if openErr != nil {
							loginURLFallback = loginURL
						}
						w.refreshAccountAvatar()
						render()
					})
				}()
			})))

			if acc.LastError != "" {
				errText := canvas.NewText(acc.LastError, design.ColorAlert)
				errText.TextSize = 11
				body.Add(spacerSize(1, 8))
				body.Add(errText)
			}
		}
		body.Refresh()
	}
	render()

	title := canvas.NewText(loc().AccountTitle, design.ColorTextLight)
	title.TextSize = 13
	title.TextStyle.Bold = true
	sep := canvas.NewRectangle(design.ColorDialogSep)
	sep.SetMinSize(fyne.NewSize(0, 1))
	headerBand := canvas.NewRectangle(color.Transparent)
	headerBand.SetMinSize(fyne.NewSize(0, 40))
	header := container.New(&tightVBoxLayout{gap: 0},
		newDialogTopAccentBar(),
		container.NewStack(headerBand, newExactInset(title, 21, 44, 12, 12)),
		sep,
	)

	widthLock := canvas.NewRectangle(color.Transparent)
	widthLock.SetMinSize(fyne.NewSize(400, 1))
	inner := container.NewBorder(
		header, nil, nil, nil,
		container.NewVBox(widthLock, newExactInset(body, 21, 21, 2, 16)),
	)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	closeBtn := newAccountDialogIconButton(accountDialogCloseIcon, closeDialog)
	panel := container.NewStack(
		bg,
		inner,
		container.New(&accountDialogCornerButtonLayout{Top: 12, Right: 12}, closeBtn),
		border,
	)

	popup = widget.NewModalPopUp(container.NewCenter(panel), parent.Canvas())
	popup.Resize(parent.Canvas().Size())
	popup.Show()

	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		last := newAccountSnapshot(w.token.AccountStatus())
		for {
			select {
			case <-stopPoll:
				return
			case <-ticker.C:
				acc := w.token.AccountStatus()
				snap := newAccountSnapshot(acc)
				if snap == last {
					continue
				}
				last = snap
				fyne.Do(func() {
					w.refreshAccountAvatar()
					render()
				})
			}
		}
	}()
}

func (w *Window) showAccountMenu(anchor fyne.CanvasObject) {
	if anchor == nil || w.token == nil {
		return
	}
	drv := fyne.CurrentApp().Driver()
	c := drv.CanvasForObject(anchor)
	if c == nil {
		return
	}

	acc := w.token.AccountStatus()
	if acc.LoggedIn {
		w.token.RefreshAccountLicenses()
		acc = w.token.AccountStatus()
	}

	signed := canvas.NewText(loc().SignedInAs, design.ColorEmptyHint)
	signed.TextSize = 10
	email := canvas.NewText(strings.TrimSpace(acc.Email), design.ColorTextLight)
	email.TextSize = 12
	email.TextStyle.Bold = true
	if email.Text == "" {
		email.Text = loc().USBridgeAccount
	}

	subLabel, subValue := canvas.NewText(loc().Subscription, design.ColorMutedOlive), canvas.NewText(accountSubscriptionLabel(acc), design.ColorTextLight)
	planLabel, planValue := canvas.NewText(loc().Plan, design.ColorMutedOlive), canvas.NewText(accountPlanLabel(acc), design.ColorTextLight)
	subLabel.TextSize, subValue.TextSize = 10, 10
	planLabel.TextSize, planValue.TextSize = 10, 10
	paintAccountPlan := func(acc account.Status) {
		subValue.Text = accountSubscriptionLabel(acc)
		planValue.Text = accountPlanLabel(acc)
		subValue.Color = design.ColorTextLight
		if accountHasPaidLicense(acc) {
			subValue.Color = design.ColorCTA
		}
		subValue.Refresh()
		planValue.Refresh()
	}
	paintAccountPlan(acc)

	sep1 := canvas.NewRectangle(design.ColorDialogSep)
	sep1.SetMinSize(fyne.NewSize(0, 1))
	sep2 := canvas.NewRectangle(design.ColorDialogSep)
	sep2.SetMinSize(fyne.NewSize(0, 1))

	// licensesBody: lets a customer who bought a license on another machine
	// move it onto this one, same "rebind" call as billing.usbridge.io/manage
	// (see internal/account's doc comment) -- only rendered when there's
	// actually another license on the account to offer, so the common
	// single-device case doesn't grow a menu row it'll never use.
	licensesBody := container.NewVBox()
	var renderLicenses func(acc account.Status)
	renderLicenses = func(acc account.Status) {
		licensesBody.RemoveAll()
		for _, lic := range acc.Licenses {
			if !strings.EqualFold(lic.Status, "licensed") {
				continue
			}
			lic := lic
			tail := lic.Identifier
			if len(tail) > 8 {
				tail = tail[len(tail)-8:]
			}
			name := canvas.NewText(fmt.Sprintf("%s ·%s", accountTierLabel(lic.Tier), tail), design.ColorTextLight)
			name.TextSize = 10
			if acc.RebindInProgress {
				state := canvas.NewText(loc().Moving, design.ColorMutedOlive)
				state.TextSize = 10
				licensesBody.Add(container.New(&flushEndsLayout{}, name, state))
				continue
			}
			useBtn := newAccountDialogTextButton(loc().UseLicenseOnDevice, func() {
				go func() {
					_ = w.token.RebindLicenseToThisDevice(lic.Identifier)
					fyne.Do(func() {
						next := w.token.AccountStatus()
						paintAccountPlan(next)
						renderLicenses(next)
						w.syncProtocolPicker(w.token.EntitlementStatus())
					})
				}()
			})
			licensesBody.Add(container.New(&flushEndsLayout{}, name, useBtn))
		}
		if len(licensesBody.Objects) > 0 {
			hint := canvas.NewText(loc().YourLicenses, design.ColorEmptyHint)
			hint.TextSize = 9
			licensesBody.Objects = append([]fyne.CanvasObject{hint}, licensesBody.Objects...)
		}
		licensesBody.Refresh()
	}
	renderLicenses(acc)

	var popup *tealMenuPopup
	logout := newCardHeaderButton(loc().LogOut, headerLogoutIcon, func() {
		if popup != nil {
			popup.Hide()
		}
		go func() {
			_ = w.token.LogoutAccount()
			fyne.Do(func() {
				w.refreshAccountAvatar()
			})
		}()
	})
	logout.logout = true

	inner := container.New(&tightVBoxLayout{gap: 6},
		signed,
		email,
		sep1,
		container.New(&flushEndsLayout{}, subLabel, subValue),
		container.New(&flushEndsLayout{}, planLabel, planValue),
		licensesBody,
		sep2,
		container.New(&centerHLayout{}, logout),
	)
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1
	content := container.NewStack(bg, newExactInset(inner, 10, 10, 10, 10), border)

	width := float32(220)
	if min := content.MinSize(); min.Width > width {
		width = min.Width
	}
	if ew := email.MinSize().Width + 24; ew > width {
		width = ew
	}
	height := content.MinSize().Height
	popup = newTealMenuPopup(content, c, fyne.NewSize(width, height))
	pos := drv.AbsolutePositionForObject(anchor)
	popupPos := fyne.NewPos(
		pos.X+anchor.Size().Width-width,
		pos.Y+anchor.Size().Height+6,
	)
	canvasSize := c.Size()
	if popupPos.X < 8 {
		popupPos.X = 8
	}
	if popupPos.X+width > canvasSize.Width-8 {
		popupPos.X = canvasSize.Width - width - 8
	}
	if popupPos.Y+height > canvasSize.Height-8 {
		popupPos.Y = canvasSize.Height - height - 8
	}
	if popupPos.Y < 8 {
		popupPos.Y = 8
	}
	popup.ShowAtPosition(popupPos)

	if !acc.LoggedIn {
		return
	}
	go func() {
		ticker := time.NewTicker(400 * time.Millisecond)
		defer ticker.Stop()
		last := newAccountSnapshot(acc)
		for range ticker.C {
			if popup == nil || !popup.Visible() {
				return
			}
			next := w.token.AccountStatus()
			snap := newAccountSnapshot(next)
			if snap == last {
				continue
			}
			last = snap
			acc := next
			fyne.Do(func() {
				if popup == nil || !popup.Visible() {
					return
				}
				paintAccountPlan(acc)
				renderLicenses(acc)
				if w.token != nil {
					w.syncProtocolPicker(w.token.EntitlementStatus())
				}
			})
		}
	}()
}

func accountHasPaidLicense(acc account.Status) bool {
	for _, lic := range acc.Licenses {
		if strings.EqualFold(lic.Status, "licensed") {
			switch strings.ToLower(lic.Tier) {
			case "pro", "enterprise":
				return true
			}
		}
	}
	return false
}

func accountTierLabel(tier string) string {
	switch strings.ToLower(tier) {
	case "pro":
		return "Pro"
	case "enterprise":
		return "Enterprise"
	default:
		return "Free"
	}
}

func accountSubscriptionLabel(acc account.Status) string {
	hasLicensed := false
	hasTrial := false
	for _, lic := range acc.Licenses {
		switch strings.ToLower(lic.Status) {
		case "licensed":
			hasLicensed = true
		case "trial":
			hasTrial = true
		}
	}
	switch {
	case hasLicensed:
		return loc().SubActive
	case hasTrial:
		return loc().SubTrial
	default:
		return loc().SubNone
	}
}

func accountPlanLabel(acc account.Status) string {
	plan := "Opensource/Free"
	for _, lic := range acc.Licenses {
		if !strings.EqualFold(lic.Status, "licensed") {
			continue
		}
		switch strings.ToLower(lic.Tier) {
		case "pro":
			plan = "Pro"
		case "enterprise":
			return "Enterprise"
		}
	}
	return plan
}

func (w *Window) refreshAccountAvatar() {
	if w.loginAvatar == nil {
		return
	}
	if w.token == nil {
		w.loginAvatar.SetState(false, "")
		return
	}
	acc := w.token.AccountStatus()
	w.loginAvatar.SetState(acc.LoggedIn, acc.Email)
}

var accountDialogCloseIcon = fyne.NewStaticResource("account_dialog_cancel.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="#8f9381"><path d="M19 6.41 17.59 5 12 10.59 6.41 5 5 6.41 10.59 12 5 17.59 6.41 19 12 13.41 17.59 19 19 17.59 13.41 12z"/></svg>`))

type accountDialogIconButton struct {
	widget.BaseWidget
	resource fyne.Resource
	onTapped func()
	hovered  bool
	bg       *canvas.Rectangle
	bdr      *canvas.Rectangle
	icon     *canvas.Image
}

func newAccountDialogIconButton(resource fyne.Resource, onTapped func()) *accountDialogIconButton {
	b := &accountDialogIconButton{resource: resource, onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *accountDialogIconButton) MinSize() fyne.Size { return fyne.NewSize(28, 28) }

func (b *accountDialogIconButton) CreateRenderer() fyne.WidgetRenderer {
	b.bg = canvas.NewRectangle(color.Transparent)
	b.bg.CornerRadius = 6
	b.bdr = canvas.NewRectangle(color.Transparent)
	b.bdr.CornerRadius = 6
	b.bdr.StrokeWidth = 1
	b.icon = canvas.NewImageFromResource(b.resource)
	b.icon.FillMode = canvas.ImageFillContain
	b.icon.ScaleMode = canvas.ImageScaleSmooth
	b.icon.SetMinSize(fyne.NewSize(18, 18))
	b.icon.Translucency = 0.32
	b.refreshVisuals()
	return widget.NewSimpleRenderer(container.NewStack(b.bg, b.bdr, container.NewCenter(b.icon)))
}

func (b *accountDialogIconButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}
func (b *accountDialogIconButton) TappedSecondary(*fyne.PointEvent) {}
func (b *accountDialogIconButton) Cursor() desktop.Cursor           { return desktop.PointerCursor }
func (b *accountDialogIconButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.refreshVisuals()
}
func (b *accountDialogIconButton) MouseOut() {
	b.hovered = false
	b.refreshVisuals()
}
func (b *accountDialogIconButton) MouseMoved(*desktop.MouseEvent) {}

func (b *accountDialogIconButton) refreshVisuals() {
	if b.bg == nil {
		return
	}
	if b.hovered {
		b.bg.FillColor = color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0x10}
		b.bdr.StrokeColor = color.NRGBA{R: 0x8f, G: 0x93, B: 0x81, A: 0xff}
		b.icon.Translucency = 0.08
	} else {
		b.bg.FillColor = color.Transparent
		b.bdr.StrokeColor = color.Transparent
		b.icon.Translucency = 0.32
	}
	b.bg.Refresh()
	b.bdr.Refresh()
	b.icon.Refresh()
}

type accountDialogCornerButtonLayout struct{ Top, Right float32 }

func (l *accountDialogCornerButtonLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 || objects[0] == nil {
		return
	}
	o := objects[0]
	min := o.MinSize()
	o.Resize(min)
	o.Move(fyne.NewPos(size.Width-min.Width-l.Right, l.Top))
}

func (l *accountDialogCornerButtonLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

type accountFieldTheme struct {
	fyne.Theme
	textSize  float32
	textColor color.Color
}

func (t *accountFieldTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	if name == theme.ColorNameForeground && t.textColor != nil {
		return t.textColor
	}
	if name == theme.ColorNameShadow {
		return color.Transparent
	}
	return t.Theme.Color(name, variant)
}

func (t *accountFieldTheme) Size(name fyne.ThemeSizeName) float32 {
	if name == theme.SizeNameText {
		return t.textSize
	}
	if name == theme.SizeNamePadding {
		return 2
	}
	return t.Theme.Size(name)
}

func wrapAccountField(obj fyne.CanvasObject, textSize float32, textColor color.Color) fyne.CanvasObject {
	return container.NewThemeOverride(obj, &accountFieldTheme{Theme: design.NewBrandTheme(), textSize: textSize, textColor: textColor})
}

type googleLoginButton struct {
	widget.BaseWidget
	onTapped func()
	hovered  bool
}

func newGoogleLoginButton(tapped func()) *googleLoginButton {
	b := &googleLoginButton{onTapped: tapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *googleLoginButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorCTA)
	bg.CornerRadius = 8
	icon := canvas.NewImageFromResource(assets.GoogleLogo)
	icon.FillMode = canvas.ImageFillContain
	icon.SetMinSize(fyne.NewSize(16, 16))
	text := canvas.NewText(loc().LogInWithGoogle, design.ColorCTALabel)
	text.TextSize = 11
	text.TextStyle.Bold = true
	return &googleLoginButtonRenderer{
		btn:     b,
		bg:      bg,
		icon:    icon,
		text:    text,
		objects: []fyne.CanvasObject{bg, icon, text},
	}
}

func (b *googleLoginButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}
func (b *googleLoginButton) TappedSecondary(*fyne.PointEvent) {}
func (b *googleLoginButton) Cursor() desktop.Cursor           { return desktop.PointerCursor }
func (b *googleLoginButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}
func (b *googleLoginButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}
func (b *googleLoginButton) MouseMoved(*desktop.MouseEvent) {}

type googleLoginButtonRenderer struct {
	btn     *googleLoginButton
	bg      *canvas.Rectangle
	icon    *canvas.Image
	text    *canvas.Text
	objects []fyne.CanvasObject
}

func (r *googleLoginButtonRenderer) Destroy() {}
func (r *googleLoginButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *googleLoginButtonRenderer) MinSize() fyne.Size {
	ts := r.text.MinSize()
	return fyne.NewSize(16+4+ts.Width+24, 28)
}

func (r *googleLoginButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	iconSize := float32(16)
	gap := float32(4)
	ts := r.text.MinSize()
	contentW := iconSize + gap + ts.Width
	start := (size.Width - contentW) / 2
	if start < 12 {
		start = 12
	}
	placeSquareIcon(r.icon, fyne.NewPos(start, (size.Height-iconSize)/2), iconSize)
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos(start+iconSize+gap, (size.Height-ts.Height)/2-0.5))
}

func (r *googleLoginButtonRenderer) Refresh() {
	if r.btn.hovered {
		r.bg.FillColor = design.ColorCTAHover
	} else {
		r.bg.FillColor = design.ColorCTA
	}
	r.bg.Refresh()
	r.text.Refresh()
	r.icon.Refresh()
	r.Layout(r.btn.Size())
}

type accountDialogTextButton struct {
	widget.BaseWidget
	text     string
	onTapped func()
	hovered  bool
}

func newAccountDialogTextButton(text string, onTapped func()) *accountDialogTextButton {
	b := &accountDialogTextButton{text: text, onTapped: onTapped}
	b.ExtendBaseWidget(b)
	return b
}

func (b *accountDialogTextButton) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = 4
	bg.StrokeWidth = 1
	bg.StrokeColor = design.ColorChromeOlive
	lbl := canvas.NewText(b.text, design.ColorMutedOlive)
	lbl.TextSize = 9
	lbl.TextStyle.Bold = true
	return &accountDialogTextButtonRenderer{
		btn:     b,
		bg:      bg,
		lbl:     lbl,
		objects: []fyne.CanvasObject{bg, lbl},
	}
}

func (b *accountDialogTextButton) Tapped(*fyne.PointEvent) {
	if b.onTapped != nil {
		b.onTapped()
	}
}
func (b *accountDialogTextButton) TappedSecondary(*fyne.PointEvent) {}
func (b *accountDialogTextButton) Cursor() desktop.Cursor           { return desktop.PointerCursor }
func (b *accountDialogTextButton) MouseIn(*desktop.MouseEvent) {
	b.hovered = true
	b.Refresh()
}
func (b *accountDialogTextButton) MouseOut() {
	b.hovered = false
	b.Refresh()
}
func (b *accountDialogTextButton) MouseMoved(*desktop.MouseEvent) {}

type accountDialogTextButtonRenderer struct {
	btn     *accountDialogTextButton
	bg      *canvas.Rectangle
	lbl     *canvas.Text
	objects []fyne.CanvasObject
}

func (r *accountDialogTextButtonRenderer) Destroy() {}
func (r *accountDialogTextButtonRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *accountDialogTextButtonRenderer) MinSize() fyne.Size {
	ts := r.lbl.MinSize()
	return fyne.NewSize(ts.Width+16, fyne.Max(ts.Height+8, 18))
}

func (r *accountDialogTextButtonRenderer) Layout(size fyne.Size) {
	r.bg.Resize(size)
	ts := r.lbl.MinSize()
	r.lbl.Resize(ts)
	r.lbl.Move(fyne.NewPos((size.Width-ts.Width)/2, (size.Height-ts.Height)/2-0.5))
}

func (r *accountDialogTextButtonRenderer) Refresh() {
	r.lbl.Text = r.btn.text
	if r.btn.hovered {
		r.bg.FillColor = design.ColorCTA
		r.bg.StrokeColor = design.ColorCTA
		r.lbl.Color = design.ColorCTALabel
	} else {
		r.bg.FillColor = design.ColorGray900
		r.bg.StrokeColor = design.ColorChromeOlive
		r.lbl.Color = design.ColorMutedOlive
	}
	r.bg.Refresh()
	r.lbl.Refresh()
	r.Layout(r.btn.Size())
}

type accountProgressTheme struct{ fyne.Theme }

func (t *accountProgressTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	if name == theme.ColorNamePrimary {
		return design.ColorTeal
	}
	if name == theme.ColorNameInputBackground || name == theme.ColorNameButton || name == theme.ColorNameScrollBarBackground {
		return color.NRGBA{R: 0x2e, G: 0x9e, B: 0x8a, A: 0xff}
	}
	return t.Theme.Color(name, v)
}

func (t *accountProgressTheme) Size(name fyne.ThemeSizeName) float32 {
	if strings.HasSuffix(string(name), "Radius") {
		return 3
	}
	return t.Theme.Size(name)
}

type fixedHeightLayout struct{ height float32 }

func (l *fixedHeightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		o.Resize(fyne.NewSize(size.Width, l.height))
		o.Move(fyne.NewPos(0, (size.Height-l.height)/2))
	}
}

func (l *fixedHeightLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	w := float32(0)
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		if min := o.MinSize(); min.Width > w {
			w = min.Width
		}
	}
	return fyne.NewSize(w, l.height)
}

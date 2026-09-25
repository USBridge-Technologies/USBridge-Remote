package ui

import (
	"fmt"
	"image/color"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/assets"
	"usbridge_agent/internal/account"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/ui/design"
)

const (
	protocolOpensource = "opensource"
	protocolFree       = "free"
	protocolPro        = "pro"
	protocolEnterprise = "enterprise"
)

type protocolOption struct {
	key       string
	label     string
	badge     string
	badgeClr  color.Color
	badgeLine color.Color
	icon      fyne.Resource
}

func protocolBadgeColors(key string) (fg color.Color, line color.Color) {
	switch key {
	case protocolOpensource:
		return design.ColorMutedOlive, design.ColorChromeOlive
	case protocolFree:
		ch := currentChrome()
		if ch.Kind == protocolPro || ch.Kind == protocolEnterprise {
			return design.ColorMutedOlive, design.ColorChromeOlive
		}
		return design.ColorTeal, design.ColorTeal
	case protocolPro, protocolEnterprise:
		return design.ColorProSoft, design.ColorProSoft
	default:
		return design.ColorMutedOlive, design.ColorChromeOlive
	}
}

var protocolOptions = []protocolOption{
	{protocolOpensource, "Sunshine", "Opensource", design.ColorMutedOlive, design.ColorChromeOlive, nil},
	{protocolFree, "USBridge Streamer", "Free", design.ColorTeal, design.ColorTeal, nil},
	{protocolPro, "USBridge", "Pro", design.ColorProSoft, design.ColorProSoft, assets.StarProIcon},
	{protocolEnterprise, "USBridge", "Enterprise", design.ColorProSoft, design.ColorProSoft, assets.StarProIcon},
}

func protocolKeyFromStatus(st entitlement.Status) string {
	return st.Protocol()
}

func protocolNeedsPurchase(pick string, st entitlement.Status, acc account.Status) bool {
	if protocolCoveredByEntitlement(pick, st) || accountLicenseIdentifier(acc, pick) != "" {
		return false
	}
	return pick == protocolPro || pick == protocolEnterprise
}

func protocolCoveredByEntitlement(pick string, st entitlement.Status) bool {
	t := strings.ToLower(st.Tier)
	switch pick {
	case protocolPro:
		return t == "pro" || t == "enterprise"
	case protocolEnterprise:
		return t == "enterprise"
	default:
		return true
	}
}

// accountLicenseIdentifier returns a licensed desktop license this account
// already owns that covers pick, or "" if they still need to buy. Prefers
// a license already bound to this machine, then one sitting on another PC.
func accountLicenseIdentifier(acc account.Status, pick string) string {
	return accountLicenseIdentifierPref(acc, pick, false)
}

func accountLicenseOnThisDevice(acc account.Status, pick string) string {
	return accountLicenseIdentifierPref(acc, pick, true)
}

func accountLicenseIdentifierPref(acc account.Status, pick string, onlyHere bool) string {
	if pick != protocolPro && pick != protocolEnterprise {
		return ""
	}
	var here, other string
	for _, lic := range acc.Licenses {
		if !strings.EqualFold(lic.Status, "licensed") {
			continue
		}
		if onlyHere && !lic.OnThisDevice {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(lic.Tier)) {
		case "enterprise":
			if lic.OnThisDevice {
				return lic.Identifier
			}
			if other == "" {
				other = lic.Identifier
			}
		case "pro":
			if pick != protocolPro {
				continue
			}
			if lic.OnThisDevice {
				here = lic.Identifier
			} else if other == "" {
				other = lic.Identifier
			}
		}
	}
	if here != "" {
		return here
	}
	return other
}

func accountLicenseBoundHere(acc account.Status, identifier string) bool {
	for _, lic := range acc.Licenses {
		if lic.Identifier == identifier {
			return lic.OnThisDevice
		}
	}
	return false
}

func protocolPurchaseTier(pick string) string {
	if pick == protocolEnterprise {
		return "enterprise"
	}
	return "pro"
}

// protocolPaidTier is the highest paid plan actually bound to THIS
// machine (hardware entitlement or an account license flagged OnThisDevice).
// A Pro license parked on another PC does not count: this agent can stay
// on Free until the user rebinds it.
func protocolPaidTier(st entitlement.Status, acc account.Status) string {
	if protocolCoveredByEntitlement(protocolEnterprise, st) || accountLicenseOnThisDevice(acc, protocolEnterprise) != "" {
		return protocolEnterprise
	}
	if protocolCoveredByEntitlement(protocolPro, st) || accountLicenseOnThisDevice(acc, protocolPro) != "" {
		return protocolPro
	}
	return ""
}

// protocolIncludes reports whether applied already covers key, so that
// row is shown with a gray tick and cannot be chosen as a downgrade
// (Pro includes Free; Enterprise includes Pro and Free).
func protocolIncludes(applied, key string) bool {
	switch applied {
	case protocolPro:
		return key == protocolFree
	case protocolEnterprise:
		return key == protocolFree || key == protocolPro
	default:
		return false
	}
}

func protocolNormalizePick(pick, applied, paid string) string {
	if protocolIncludes(applied, pick) {
		return applied
	}
	if pick == protocolFree && (paid == protocolPro || paid == protocolEnterprise) {
		if paid == protocolEnterprise {
			return protocolEnterprise
		}
		return protocolPro
	}
	return pick
}

func protocolRowIncluded(applied, pick, key string) bool {
	if key == "" || key == pick {
		return false
	}
	return protocolIncludes(applied, key) || protocolIncludes(pick, key)
}

func protocolHoverHighlights(hover, paid, key string) bool {
	if paid != protocolPro && paid != protocolEnterprise {
		return false
	}
	if hover != protocolFree && hover != protocolPro {
		return false
	}
	return key == protocolFree || key == protocolPro
}

func (w *Window) newProtocolPanel(parent fyne.Window) fyne.CanvasObject {
	st := entitlement.Status{}
	if w.token != nil {
		st = w.token.EntitlementStatus()
	}
	w.protocolApplied = protocolKeyFromStatus(st)
	w.protocolPick = w.protocolApplied

	w.protocolRows = make([]*protocolPickRow, 0, len(protocolOptions))
	rows := make([]fyne.CanvasObject, 0, len(protocolOptions))
	for _, opt := range protocolOptions {
		opt := opt
		row := newProtocolPickRow(opt, opt.key == w.protocolPick, func() {
			w.selectProtocolPick(opt.key)
		}, func(on bool) {
			w.setProtocolHover(opt.key, on)
		})
		w.protocolRows = append(w.protocolRows, row)
		info := newTinyGlyphButtonColored(theme.InfoIcon(), design.ColorNameMutedOlive, func() {
			w.showTariffPickerDialog(parent, opt.key)
		})
		rows = append(rows, container.New(&flushEndsLayout{}, row, info))
	}

	w.protocolChange = newCardHeaderButton(loc().Change, headerChangeIcon, func() {
		w.applySelectedProtocol(parent)
	})
	w.refreshProtocolPickerVisuals(st.LinkInProgress || st.DownloadInProgress)

	headerBits := []fyne.CanvasObject{}
	if w.supportBtn != nil {
		headerBits = append(headerBits, w.supportBtn)
	}
	headerBits = append(headerBits, w.protocolChange)
	headerBtns := container.New(&tightHBoxLayout{gap: 4}, headerBits...)
	panel := newPanel(panelIconProtocol, loc().Protocol, headerBtns, container.New(&tightVBoxLayout{gap: 4}, rows...))
	if p, ok := panel.(*themedPanel); ok {
		w.protocolPanel = p
	}
	return panel
}

func (w *Window) protocolStatus() (entitlement.Status, account.Status) {
	if w.token == nil {
		return entitlement.Status{}, account.Status{}
	}
	return w.token.EntitlementStatus(), w.token.AccountStatus()
}

// onProtocolBuyClicked: accented Buy Pro / Buy Enterprise goes straight to
// Stripe; the muted chip (Sunshine or Free selected) opens the tariff info
// on the Pro tab.
func (w *Window) onProtocolBuyClicked(parent fyne.Window) {
	pick := w.protocolPick
	if pick == "" {
		st, _ := w.protocolStatus()
		pick = protocolKeyFromStatus(st)
	}
	switch pick {
	case protocolPro, protocolEnterprise:
		w.openTariffCheckout(parent, protocolPurchaseTier(pick))
	default:
		w.showTariffPickerDialog(parent, protocolPro)
	}
}

func (w *Window) selectProtocolPick(key string) {
	st, acc := w.protocolStatus()
	w.protocolPick = protocolNormalizePick(key, w.protocolApplied, protocolPaidTier(st, acc))
	w.refreshProtocolPickerVisuals(false)
}

func (w *Window) setProtocolHover(key string, on bool) {
	prev := w.protocolHover
	if on {
		w.protocolHover = key
	} else if w.protocolHover == key {
		w.protocolHover = ""
	}
	if prev == w.protocolHover {
		return
	}
	st, acc := w.protocolStatus()
	paid := protocolPaidTier(st, acc)
	for _, row := range w.protocolRows {
		if row == nil {
			continue
		}
		row.SetPreview(protocolHoverHighlights(w.protocolHover, paid, row.key))
	}
}

func (w *Window) refreshProtocolPickerVisuals(busy bool) {
	st, acc := w.protocolStatus()
	paid := protocolPaidTier(st, acc)
	for _, row := range w.protocolRows {
		if row == nil {
			continue
		}
		row.SetChecked(row.key == w.protocolPick)
		row.SetIncluded(protocolRowIncluded(w.protocolApplied, w.protocolPick, row.key))
		row.SetPreview(protocolHoverHighlights(w.protocolHover, paid, row.key))
	}
	if acc.RebindInProgress {
		busy = true
	}
	needsBuy := protocolNeedsPurchase(w.protocolPick, st, acc)
	if w.protocolChange != nil {
		pending := !busy && w.protocolPick != "" && w.protocolPick != w.protocolApplied && !needsBuy
		w.protocolChange.SetAccent(pending)
		if busy || needsBuy {
			w.protocolChange.Disable()
		} else {
			w.protocolChange.Enable()
		}
	}
	w.refreshSupportButton(st)
}

func (w *Window) syncProtocolPicker(st entitlement.Status) {
	w.protocolApplied = protocolKeyFromStatus(st)
	if w.protocolPick == "" {
		w.protocolPick = w.protocolApplied
	}
	w.refreshProtocolPickerVisuals(st.LinkInProgress || st.DownloadInProgress)
}

func (w *Window) maybeFinishPendingTierSwitch(st entitlement.Status) {
	if w.token == nil || w.pendingTierSwitch == "" {
		return
	}
	if st.Tier != w.pendingTierSwitch || !st.RustShineStaged || st.ActiveBackend == "rustshine" {
		return
	}
	w.pendingTierSwitch = ""
	w.startProtocolBusy()
	go func() {
		_ = w.token.SetStreamBackend("rustshine")
		fyne.Do(w.finishProtocolSwitch)
	}()
}

func (w *Window) applySelectedProtocol(parent fyne.Window) {
	if w.token == nil || w.protocolPick == "" {
		return
	}
	st := w.token.EntitlementStatus()
	acc := w.token.AccountStatus()
	w.protocolPick = protocolNormalizePick(w.protocolPick, w.protocolApplied, protocolPaidTier(st, acc))
	if w.protocolPick == w.protocolApplied || protocolNeedsPurchase(w.protocolPick, st, acc) {
		w.refreshProtocolPickerVisuals(false)
		return
	}
	key := w.protocolPick

	if key != protocolOpensource && !st.RustShineStaged && parent != nil {
		w.showStreamerConsentDialog(parent, func(confirmed bool) {
			if !confirmed {
				w.protocolPick = w.protocolApplied
				w.refreshProtocolPickerVisuals(false)
				return
			}
			w.proceedProtocolSwitch(parent, key, st, acc)
		})
		return
	}
	w.proceedProtocolSwitch(parent, key, st, acc)
}

func (w *Window) proceedProtocolSwitch(parent fyne.Window, key string, st entitlement.Status, acc account.Status) {
	if w.protocolChange != nil {
		w.protocolChange.Disable()
	}
	done := w.finishProtocolSwitch

	switch key {
	case protocolOpensource:
		w.startProtocolBusy()
		go func() {
			_ = w.token.SetStreamBackend("sunshine")
			fyne.Do(done)
		}()
	case protocolFree:
		if paid := protocolPaidTier(st, acc); paid == protocolPro || paid == protocolEnterprise {
			w.requestPaidTier(parent, st, paid, done)
			return
		}
		w.startRustShineSwitch(st, done)
	case protocolPro:
		w.requestPaidTier(parent, st, "pro", done)
	case protocolEnterprise:
		w.requestPaidTier(parent, st, "enterprise", done)
	}
}

func (w *Window) startRustShineSwitch(st entitlement.Status, done func()) {
	w.startProtocolBusy()
	go func() {
		if !st.RustShineStaged {
			if err := w.token.DownloadRustShine(nil); err != nil {
				fyne.Do(done)
				return
			}
		}
		_ = w.token.SetStreamBackend("rustshine")
		fyne.Do(done)
	}()
}

func (w *Window) requestPaidTier(parent fyne.Window, st entitlement.Status, tier string, done func()) {
	if st.Tier == tier || (tier == "pro" && st.Tier == "enterprise") {
		w.startRustShineSwitch(st, done)
		return
	}
	if w.token != nil {
		pick := protocolPro
		if tier == "enterprise" {
			pick = protocolEnterprise
		}
		acc := w.token.AccountStatus()
		if id := accountLicenseIdentifier(acc, pick); id != "" {
			if accountLicenseBoundHere(acc, id) {
				w.applyAccountLicense(id, tier, done)
				return
			}
			if parent == nil {
				fyne.Do(done)
				return
			}
			showConfirmToast(fmt.Sprintf(loc().RebindLicenseConfirm, tierDisplayName(tier)), func(yes bool) {
				if !yes {
					done()
					return
				}
				w.applyAccountLicense(id, tier, done)
			}, parent)
			return
		}
	}
	if parent == nil {
		fyne.Do(done)
		return
	}
	showConfirmDialog(
		fmt.Sprintf(loc().SubscribeTitle, tierDisplayName(tier)),
		fmt.Sprintf(loc().SubscribeBody, tierDisplayName(tier)),
		func(confirmed bool) {
			if !confirmed {
				w.protocolPick = w.protocolApplied
				done()
				return
			}
			w.pendingTierSwitch = tier
			go func() {
				checkoutURL, err := w.token.StartPurchase(tier)
				if err != nil {
					fyne.Do(done)
					return
				}
				parsed, parseErr := url.Parse(checkoutURL)
				openErr := parseErr
				if parseErr == nil && w.app != nil {
					openErr = w.app.OpenURL(parsed)
				}
				if openErr != nil {
					showInfoDialog(loc().CheckoutTitle,
						loc().CouldntOpenBrowserBuy+"\n"+checkoutURL, parent)
				}
				fyne.Do(done)
			}()
		},
		parent,
	)
}

func (w *Window) applyAccountLicense(identifier, tier string, done func()) {
	w.startProtocolBusy()
	go func() {
		if err := w.token.RebindLicenseToThisDevice(identifier); err != nil {
			fyne.Do(done)
			return
		}
		st := w.token.EntitlementStatus()
		if st.Tier != tier && !(tier == "pro" && st.Tier == "enterprise") {
			fyne.Do(func() {
				w.pendingTierSwitch = tier
				done()
			})
			return
		}
		if !st.RustShineStaged {
			if err := w.token.DownloadRustShine(nil); err != nil {
				fyne.Do(func() {
					w.pendingTierSwitch = tier
					done()
				})
				return
			}
		}
		_ = w.token.SetStreamBackend("rustshine")
		fyne.Do(done)
	}()
}

type protocolPickRow struct {
	widget.BaseWidget
	key       string
	label     string
	badge     string
	badgeClr  color.Color
	badgeLine color.Color
	icon      fyne.Resource
	checked   bool
	included  bool
	preview   bool
	hovered   bool
	onTap     func()
	onHover   func(bool)
}

func newProtocolPickRow(opt protocolOption, checked bool, onTap func(), onHover func(bool)) *protocolPickRow {
	r := &protocolPickRow{
		key:       opt.key,
		label:     opt.label,
		badge:     opt.badge,
		badgeClr:  opt.badgeClr,
		badgeLine: opt.badgeLine,
		icon:      opt.icon,
		checked:   checked,
		onTap:     onTap,
		onHover:   onHover,
	}
	r.ExtendBaseWidget(r)
	registerChromeWidget(r)
	return r
}

func (r *protocolPickRow) SetChecked(on bool) {
	if r.checked == on {
		return
	}
	r.checked = on
	r.Refresh()
}

func (r *protocolPickRow) SetIncluded(on bool) {
	if r.included == on {
		return
	}
	r.included = on
	r.Refresh()
}

func (r *protocolPickRow) SetPreview(on bool) {
	if r.preview == on {
		return
	}
	r.preview = on
	r.Refresh()
}

func (r *protocolPickRow) CreateRenderer() fyne.WidgetRenderer {
	box := canvas.NewRectangle(color.Transparent)
	box.CornerRadius = 3
	box.StrokeWidth = 1
	mark := newCheckImage(checkGlyphOnTeal)
	star := canvas.NewImageFromResource(r.icon)
	star.FillMode = canvas.ImageFillStretch
	if r.icon == nil {
		star.Hide()
	}
	text := canvas.NewText(r.label, design.ColorSectionTitle)
	text.TextSize = 11
	pillBg := canvas.NewRectangle(color.Transparent)
	pillBg.StrokeWidth = 1
	pillBg.StrokeColor = r.badgeLine
	pillTxt := canvas.NewText(r.badge, r.badgeClr)
	pillTxt.TextSize = 8
	pillTxt.TextStyle.Bold = true
	return &protocolPickRowRenderer{
		row:     r,
		box:     box,
		mark:    mark,
		star:    star,
		text:    text,
		pillBg:  pillBg,
		pillTxt: pillTxt,
		objects: []fyne.CanvasObject{box, mark, star, text, pillBg, pillTxt},
	}
}

func (r *protocolPickRow) MinSize() fyne.Size {
	return protocolPickRowMinSize(r.label, r.badge, r.icon != nil)
}

func protocolPickRowMinSize(label, badge string, hasIcon bool) fyne.Size {
	t := canvas.NewText(label, design.ColorSectionTitle)
	t.TextSize = 11
	p := canvas.NewText(badge, design.ColorMutedOlive)
	p.TextSize = 8
	p.TextStyle.Bold = true
	w := float32(20) + t.MinSize().Width + 6 + p.MinSize().Width + 12
	if hasIcon {
		w += 16
	}
	return fyne.NewSize(w, 22)
}

func (r *protocolPickRow) Tapped(*fyne.PointEvent) {
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *protocolPickRow) TappedSecondary(*fyne.PointEvent) {}

func (r *protocolPickRow) MouseIn(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
	r.hovered = true
	r.Refresh()
	if r.onHover != nil {
		r.onHover(true)
	}
}

func (r *protocolPickRow) MouseOut() {
	noteChromeHoverOut()
	r.hovered = false
	r.Refresh()
	if r.onHover != nil {
		r.onHover(false)
	}
}

func (r *protocolPickRow) MouseMoved(ev *desktop.MouseEvent) {
	if ev != nil {
		noteChromeHoverIn(ev.AbsolutePosition)
	}
}

func (r *protocolPickRow) Cursor() desktop.Cursor { return desktop.PointerCursor }

type protocolPickRowRenderer struct {
	row     *protocolPickRow
	box     *canvas.Rectangle
	mark    *canvas.Image
	star    *canvas.Image
	text    *canvas.Text
	pillBg  *canvas.Rectangle
	pillTxt *canvas.Text
	objects []fyne.CanvasObject
}

func (r *protocolPickRowRenderer) Layout(size fyne.Size) {
	const boxSide float32 = 14
	const markSide float32 = 11
	const starSide float32 = 12
	const pillH float32 = 14
	r.box.Resize(fyne.NewSize(boxSide, boxSide))
	r.box.Move(fyne.NewPos(0, (size.Height-boxSide)/2))
	placeSquareIcon(r.mark, fyne.NewPos((boxSide-markSide)/2, (size.Height-markSide)/2-0.5), markSide)
	x := float32(20)
	if r.row.icon != nil {
		placeSquareIcon(r.star, fyne.NewPos(x, (size.Height-starSide)/2), starSide)
		x += starSide + 4
	}
	ts := r.text.MinSize()
	r.text.Resize(ts)
	r.text.Move(fyne.NewPos(x, (size.Height-ts.Height)/2))
	x += ts.Width + 6
	ps := r.pillTxt.MinSize()
	pillW := ps.Width + 10
	r.pillBg.Resize(fyne.NewSize(pillW, pillH))
	r.pillBg.CornerRadius = pillH / 2
	r.pillBg.Move(fyne.NewPos(x, (size.Height-pillH)/2))
	r.pillTxt.Resize(ps)
	py := (size.Height-ps.Height)/2 - 1
	if py < 0 {
		py = 0
	}
	r.pillTxt.Move(fyne.NewPos(x+(pillW-ps.Width)/2, py))
}

func (r *protocolPickRowRenderer) MinSize() fyne.Size { return r.row.MinSize() }

func (r *protocolPickRowRenderer) Refresh() {
	ch := currentChrome()
	switch {
	case r.row.checked:
		r.box.FillColor = ch.Accent
		r.box.StrokeColor = ch.Accent
		r.mark.Resource = checkGlyphOnTeal
		r.mark.Show()
		r.text.Color = ch.Accent
	case r.row.included:
		r.box.FillColor = color.Transparent
		r.box.StrokeColor = design.ColorChromeOlive
		r.mark.Resource = checkGlyphMuted
		r.mark.Show()
		r.text.Color = design.ColorEmptyHint
	default:
		r.box.FillColor = color.Transparent
		r.box.StrokeColor = design.ColorChromeOlive
		r.mark.Hide()
		r.text.Color = design.ColorSectionTitle
	}
	if (r.row.hovered || r.row.preview) && !r.row.checked && !r.row.included {
		r.box.StrokeColor = ch.Accent
		r.text.Color = ch.Accent
	}
	fg, line := protocolBadgeColors(r.row.key)
	r.pillTxt.Color = fg
	r.pillBg.StrokeColor = line
	if r.row.icon != nil {
		r.star.Show()
	} else {
		r.star.Hide()
	}
	r.box.Refresh()
	r.mark.Refresh()
	r.star.Refresh()
	r.text.Refresh()
	r.pillBg.Refresh()
	r.pillTxt.Refresh()
	r.Layout(r.row.Size())
}

func (r *protocolPickRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *protocolPickRowRenderer) Destroy()                     {}

// cardGridLayout is a 2x2 of cards. Each card is laid out at its own
// MinSize height (not stretched to the cell or its neighbour) so leftover
// window space stays empty below the grid. Fyne Border/VBox theme padding
// is avoided at this layer — stretching + theme.Padding()*scale is what
// made paddings jump when dragging the window between monitors.
type cardGridLayout struct {
	gap         float32
	topInset    float32
	bottomInset float32
}

func (l *cardGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) < 4 {
		return
	}
	gap := l.gap
	topH := fyne.Max(objects[0].MinSize().Height, objects[1].MinSize().Height)
	botH := fyne.Max(objects[2].MinSize().Height, objects[3].MinSize().Height)
	need := topH + botH + gap
	if size.Height > 0 && need > size.Height {
		avail := size.Height - gap
		if avail < 0 {
			avail = 0
		}
		// Prefer keeping Tailscale|Permissions at their natural shared
		// height (Linux Permissions is the tall driver). Squeeze
		// Protocol|Status first; only shrink the top row if even that
		// is not enough.
		if topH <= avail {
			botH = avail - topH
		} else {
			botH = 0
			topH = avail
		}
	}
	topW := size.Width - l.topInset*2
	botW := size.Width - l.bottomInset*2
	if topW < 0 {
		topW = 0
	}
	if botW < 0 {
		botW = 0
	}
	topCol := (topW - gap) / 2
	botCol := (botW - gap) / 2
	if topCol < 0 {
		topCol = 0
	}
	if botCol < 0 {
		botCol = 0
	}
	place := func(obj fyne.CanvasObject, x, y, w, h float32) {
		obj.Move(fyne.NewPos(x, y))
		obj.Resize(fyne.NewSize(w, h))
	}
	// Top row (Tailscale | Permissions) shares one height so the cards
	// line up; Permissions growth pulls Tailscale with it via topH =
	// max(mins). Extra space inside a shorter card sits below its last
	// row (viewportFillLayout), not as padding between every row.
	place(objects[0], l.topInset, 0, topCol, topH)
	place(objects[1], l.topInset+topCol+gap, 0, topCol, topH)
	place(objects[2], l.bottomInset, topH+gap, botCol, fyne.Min(objects[2].MinSize().Height, botH))
	place(objects[3], l.bottomInset+botCol+gap, topH+gap, botCol, fyne.Min(objects[3].MinSize().Height, botH))
}

func (l *cardGridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	if len(objects) < 4 {
		return fyne.NewSize(0, 0)
	}
	topH := fyne.Max(objects[0].MinSize().Height, objects[1].MinSize().Height)
	botH := fyne.Max(objects[2].MinSize().Height, objects[3].MinSize().Height)
	topW := objects[0].MinSize().Width + objects[1].MinSize().Width + l.gap + l.topInset*2
	botW := objects[2].MinSize().Width + objects[3].MinSize().Width + l.gap + l.bottomInset*2
	return fyne.NewSize(fyne.Max(topW, botW), topH+botH+l.gap)
}

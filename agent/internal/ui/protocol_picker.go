package ui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/account"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/ui/design"
)

const (
	protocolOpensource = "opensource"
	protocolFree       = "free"
	protocolPro        = "pro"
	protocolEnterprise = "enterprise"
	// protocolPunktfunk is a picker key only: over the wire a Punktfunk
	// agent reports the "opensource" protocol (entitlement.Status.Protocol),
	// since the client treats it like Sunshine.
	protocolPunktfunk = "punktfunk"
	// protocolVibepollo is a picker key only, like protocolPunktfunk: the client treats a
	// Vibepollo agent as an open-source (Sunshine-family) one.
	protocolVibepollo = "vibepollo"
)

type protocolOption struct {
	key       string
	label     string
	badge     string
	badgeClr  color.Color
	badgeLine color.Color
	icon      fyne.Resource
}

func protocolBadgeFill(c color.Color) color.Color {
	n, ok := c.(color.NRGBA)
	if !ok {
		rr, gg, bb, _ := c.RGBA()
		n = color.NRGBA{R: uint8(rr >> 8), G: uint8(gg >> 8), B: uint8(bb >> 8), A: 0xFF}
	}
	n.A = 0x33
	return n
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

// USBridge Streamer leads: it is what the Windows installer selects.
var protocolOptions = []protocolOption{
	{protocolFree, "USBridge Streamer", "Free", design.ColorTeal, design.ColorTeal, nil},
	{protocolPro, "USBridge Streamer", "Pro", design.ColorProSoft, design.ColorProSoft, nil},
	{protocolOpensource, "Sunshine", "Open Source", design.ColorWhite, design.ColorWhite, nil},
}

func protocolPickerKey(key string) string {
	if key == protocolEnterprise {
		return protocolPro
	}
	return key
}

var protocolLockIcon = fyne.NewStaticResource("protocol-lock.svg", []byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill="#9a9d8c" d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1s3.1 1.39 3.1 3.1v2z"/></svg>`))

// protocolPunktfunkOption is the tile for Punktfunk, shown only on a machine
// that has it (entitlement.Status.PunktfunkAvailable): no agent ships it.
var protocolPunktfunkOption = protocolOption{protocolPunktfunk, "Punktfunk", "Open Source", design.ColorWhite, design.ColorWhite, nil}

// protocolVibepolloOption is the tile for Vibepollo (Nonary's Sunshine fork, with the
// USBridge USB broker bridge): offered wherever Streamers-Forks publishes a build for this
// platform (entitlement.Status.VibepolloAvailable); picking it downloads it.
var protocolVibepolloOption = protocolOption{protocolVibepollo, "Vibepollo", "Open Source", design.ColorWhite, design.ColorWhite, nil}

func protocolKeyFromStatus(st entitlement.Status) string {
	if st.ActiveBackend == "punktfunk" {
		return protocolPunktfunk
	}
	if st.ActiveBackend == "vibepollo" {
		return protocolVibepollo
	}
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

func (w *Window) restoreProtocolSelection() {
	st, _ := w.protocolStatus()
	w.protocolApplied = protocolKeyFromStatus(st)
	w.protocolPick = w.protocolApplied
	w.protocolHover = ""
	for _, row := range w.protocolRows {
		if row == nil {
			continue
		}
		row.hovered = false
	}
	w.refreshProtocolPickerVisuals(false)
}

func (w *Window) newProtocolPanel(parent fyne.Window) fyne.CanvasObject {
	st := entitlement.Status{}
	if w.token != nil {
		st = w.token.EntitlementStatus()
	}
	w.protocolApplied = protocolKeyFromStatus(st)
	w.protocolPick = w.protocolApplied

	options := protocolOptions
	if st.PunktfunkAvailable || st.ActiveBackend == "punktfunk" {
		options = append(append([]protocolOption{}, protocolOptions...), protocolPunktfunkOption)
	}
	if st.VibepolloAvailable || st.ActiveBackend == "vibepollo" {
		options = append(append([]protocolOption{}, options...), protocolVibepolloOption)
	}
	w.protocolRows = make([]*protocolPickRow, 0, len(options))
	tiles := make([]fyne.CanvasObject, 0, len(options))
	for _, opt := range options {
		opt := opt
		row := newProtocolPickRow(opt, protocolPickerKey(opt.key) == protocolPickerKey(w.protocolPick), func() {
			if opt.key == protocolPro || opt.key == protocolEnterprise {
				w.restoreProtocolSelection()
				w.showTariffPickerDialog(parent, opt.key)
				return
			}
			w.selectProtocolPick(opt.key)
			w.applySelectedProtocol(parent)
		}, func() {
			w.showTariffPickerDialog(parent, opt.key)
		}, func(on bool) {
			w.setProtocolHover(opt.key, on)
		})
		w.protocolRows = append(w.protocolRows, row)
		tiles = append(tiles, row)
	}

	w.protocolChange = nil
	w.protocolPanel = nil
	w.refreshProtocolPickerVisuals(st.LinkInProgress || st.DownloadInProgress)

	rule := canvas.NewRectangle(design.ColorDivider)
	rule.SetMinSize(fyne.NewSize(0, 1))
	// All protocol tiles share one row. Each card's min width is small
	// enough that four of them fit the width three used to take.
	grid := container.New(&equalHBoxLayout{gap: 8}, tiles...)
	return container.New(&tightVBoxLayout{gap: 12},
		grid,
		rule,
	)
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
	if w.protocolSwitching {
		return
	}
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
	if acc.RebindInProgress || w.protocolSwitching {
		busy = true
	}
	for _, row := range w.protocolRows {
		if row == nil {
			continue
		}
		display := protocolPickerKey(w.protocolPick)
		row.SetChecked(protocolPickerKey(row.key) == display)
		row.SetLocked(protocolNeedsPurchase(row.key, st, acc) && protocolPickerKey(row.key) != display)
		row.SetIncluded(protocolRowIncluded(w.protocolApplied, w.protocolPick, row.key))
		row.SetPreview(protocolHoverHighlights(w.protocolHover, paid, row.key))
		row.SetDisabled(busy)
		w.syncProtocolRowUpdate(row, st, acc)
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

// syncProtocolRowUpdate puts the "check for updates" glyph on every card
// whose streamer is installed: the RustShine tiers once RustShine is staged
// (one build for all of them; checked through the entitlement backend), and
// Punktfunk once the agent downloaded it (checked against Streamers-Forks on
// GitHub), and Sunshine where the agent downloads it too (Windows, Linux
// x86_64, Apple Silicon); elsewhere it ships inside the agent and updates
// with it.
func (w *Window) syncProtocolRowUpdate(row *protocolPickRow, st entitlement.Status, acc account.Status) {
	switch row.key {
	case protocolFree, protocolPro, protocolEnterprise:
		show := st.RustShineStaged && !protocolNeedsPurchase(row.key, st, acc)
		row.SetUpdate(show, st.RustShineUpdateInProgress || w.streamerUpdateChecking, w.beginStreamerUpdateCheck)
		ver := ""
		if st.RustShineStaged {
			ver = shortStreamerVersion(st.RustShineVersion)
		}
		row.SetVersion(ver)
	case protocolPunktfunk:
		row.SetUpdate(st.PunktfunkStaged, st.PunktfunkUpdateInProgress || w.punktfunkUpdateChecking, w.beginPunktfunkUpdateCheck)
		row.SetVersion(shortStreamerVersion(st.PunktfunkVersion))
	case protocolVibepollo:
		row.SetUpdate(st.VibepolloStaged, st.VibepolloUpdateInProgress || w.vibepolloUpdateChecking, w.beginVibepolloUpdateCheck)
		row.SetVersion(shortStreamerVersion(st.VibepolloVersion))
	case protocolOpensource:
		row.SetUpdate(st.SunshineUpdatable, st.SunshineUpdateInProgress || w.sunshineUpdateChecking, w.beginSunshineUpdateCheck)
		row.SetVersion(shortStreamerVersion(st.SunshineVersion))
	default:
		row.SetUpdate(false, false, nil)
		row.SetVersion("")
	}
}

// refreshProtocolUpdateGlyphs re-syncs only the cards' update glyphs.
func (w *Window) refreshProtocolUpdateGlyphs() {
	st, acc := w.protocolStatus()
	for _, row := range w.protocolRows {
		if row != nil {
			w.syncProtocolRowUpdate(row, st, acc)
		}
	}
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
	if w.protocolSwitching || w.token == nil || w.protocolPick == "" {
		return
	}
	st := w.token.EntitlementStatus()
	acc := w.token.AccountStatus()
	w.protocolPick = protocolNormalizePick(w.protocolPick, w.protocolApplied, protocolPaidTier(st, acc))
	if w.protocolPick == w.protocolApplied {
		w.refreshProtocolPickerVisuals(false)
		return
	}
	key := w.protocolPick
	if protocolNeedsPurchase(key, st, acc) {
		w.protocolPick = w.protocolApplied
		w.refreshProtocolPickerVisuals(false)
		w.showTariffPickerDialog(parent, key)
		return
	}

	if key != protocolOpensource && key != protocolPunktfunk && key != protocolVibepollo && !st.RustShineStaged && parent != nil {
		w.startProtocolBusy()
		w.showStreamerConsentDialog(parent, func(confirmed bool) {
			if !confirmed {
				w.protocolPick = w.protocolApplied
				w.finishProtocolSwitch()
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
	w.startProtocolBusy()
	done := w.finishProtocolSwitch

	switch key {
	case protocolOpensource:
		go func() {
			_ = w.token.SetStreamBackend("sunshine")
			fyne.Do(done)
		}()
	case protocolPunktfunk:
		go func() {
			_ = w.token.SetStreamBackend("punktfunk")
			fyne.Do(done)
		}()
	case protocolVibepollo:
		go func() {
			_ = w.token.SetStreamBackend("vibepollo")
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
	pick := protocolPro
	if tier == "enterprise" {
		pick = protocolEnterprise
	}
	w.protocolPick = w.protocolApplied
	if done != nil {
		done()
	}
	w.showTariffPickerDialog(parent, pick)
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
	locked    bool
	disabled  bool
	hovered   bool
	onTap     func()
	onInfo    func()
	onHover   func(bool)
	infoHitX  float32
	infoHitW  float32
	// updatable: this card's streamer is installed and can be checked for
	// a newer build (refresh glyph left of the info icon); updating greys it
	// out while a check runs. onUpdate is the glyph's tap.
	updatable  bool
	updating   bool
	onUpdate   func()
	updateHitX float32
	updateHitW float32
	// version: the installed build's short tag ("v0.3.192"), shown under
	// the badge; "" hides it.
	version string
}

// SetVersion shows the installed streamer's version on the card.
func (r *protocolPickRow) SetVersion(v string) {
	if r.version == v {
		return
	}
	r.version = v
	r.Refresh()
}

// SetUpdate shows or hides the card's "check for updates" glyph.
func (r *protocolPickRow) SetUpdate(updatable, updating bool, onUpdate func()) {
	r.onUpdate = onUpdate
	if r.updatable == updatable && r.updating == updating {
		return
	}
	r.updatable = updatable
	r.updating = updating
	r.Refresh()
}

func newProtocolPickRow(opt protocolOption, checked bool, onTap, onInfo func(), onHover func(bool)) *protocolPickRow {
	r := &protocolPickRow{
		key:       opt.key,
		label:     opt.label,
		badge:     opt.badge,
		badgeClr:  opt.badgeClr,
		badgeLine: opt.badgeLine,
		icon:      opt.icon,
		checked:   checked,
		onTap:     onTap,
		onInfo:    onInfo,
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

func (r *protocolPickRow) SetLocked(on bool) {
	if r.locked == on {
		return
	}
	r.locked = on
	r.Refresh()
}

func (r *protocolPickRow) SetDisabled(on bool) {
	if r.disabled == on {
		return
	}
	r.disabled = on
	if on {
		r.hovered = false
	}
	r.Refresh()
}

func (r *protocolPickRow) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeWidth = 1
	border.StrokeColor = design.ColorChromeOlive

	radio := canvas.NewCircle(color.Transparent)
	radio.StrokeWidth = 1.5
	radio.StrokeColor = design.ColorChromeOlive
	dot := canvas.NewCircle(design.ColorCTA)

	title := canvas.NewText(r.label, design.ColorTextLight)
	title.TextSize = 11
	title.TextStyle.Bold = true
	sub := canvas.NewText(r.badge, r.badgeClr)
	sub.TextSize = 9
	sub.TextStyle.Bold = true
	badgeBg := canvas.NewRectangle(protocolBadgeFill(r.badgeClr))
	badgeBg.CornerRadius = 6
	badgeBg.StrokeWidth = 1
	badgeBg.StrokeColor = r.badgeLine
	ver := canvas.NewText(r.version, design.ColorMutedOlive)
	ver.TextSize = 9

	lock := canvas.NewImageFromResource(protocolLockIcon)
	lock.FillMode = canvas.ImageFillStretch
	lock.SetMinSize(fyne.NewSize(14, 14))

	info := canvas.NewImageFromResource(theme.NewColoredResource(theme.InfoIcon(), design.ColorNameMutedOlive))
	info.FillMode = canvas.ImageFillStretch
	info.SetMinSize(fyne.NewSize(14, 14))

	update := canvas.NewImageFromResource(theme.NewColoredResource(theme.ViewRefreshIcon(), design.ColorNameMutedOlive))
	update.FillMode = canvas.ImageFillStretch
	update.SetMinSize(fyne.NewSize(14, 14))
	updateBusy := canvas.NewImageFromResource(theme.NewColoredResource(theme.ViewRefreshIcon(), theme.ColorNameDisabled))
	updateBusy.FillMode = canvas.ImageFillStretch
	updateBusy.SetMinSize(fyne.NewSize(14, 14))

	rr := &protocolPickRowRenderer{
		row:        r,
		bg:         bg,
		border:     border,
		radio:      radio,
		dot:        dot,
		title:      title,
		sub:        sub,
		badgeBg:    badgeBg,
		ver:        ver,
		lock:       lock,
		info:       info,
		update:     update,
		updateBusy: updateBusy,
		objects:    []fyne.CanvasObject{bg, border, radio, dot, title, badgeBg, sub, ver, lock, info, update, updateBusy},
	}
	// Apply checked/locked/hover state now: the objects above start in
	// their defaults (dot shown), and a card whose state never changes after
	// the picker is built would otherwise look selected until a hover
	// refreshed it.
	rr.Refresh()
	return rr
}

func (r *protocolPickRow) MinSize() fyne.Size {
	return fyne.NewSize(120, 64)
}

func (r *protocolPickRow) Tapped(e *fyne.PointEvent) {
	if r.disabled {
		return
	}
	if e != nil && r.infoHitW > 0 && e.Position.X >= r.infoHitX && e.Position.X < r.infoHitX+r.infoHitW {
		if r.onInfo != nil {
			r.onInfo()
		}
		return
	}
	if e != nil && r.updatable && r.updateHitW > 0 && e.Position.X >= r.updateHitX && e.Position.X < r.updateHitX+r.updateHitW {
		if !r.updating && r.onUpdate != nil {
			r.onUpdate()
		}
		return
	}
	if r.onTap != nil {
		r.onTap()
	}
}

func (r *protocolPickRow) TappedSecondary(*fyne.PointEvent) {}

func (r *protocolPickRow) MouseIn(ev *desktop.MouseEvent) {
	if r.disabled {
		return
	}
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

func (r *protocolPickRow) Cursor() desktop.Cursor {
	if r.disabled {
		return desktop.DefaultCursor
	}
	return desktop.PointerCursor
}

type protocolPickRowRenderer struct {
	row     *protocolPickRow
	bg      *canvas.Rectangle
	border  *canvas.Rectangle
	radio   *canvas.Circle
	dot     *canvas.Circle
	title   *canvas.Text
	sub     *canvas.Text
	badgeBg *canvas.Rectangle
	ver     *canvas.Text
	lock    *canvas.Image
	info    *canvas.Image
	// update / updateBusy: the "check for updates" glyph, idle and while a
	// check runs.
	update     *canvas.Image
	updateBusy *canvas.Image
	objects    []fyne.CanvasObject
}

func (r *protocolPickRowRenderer) Layout(size fyne.Size) {
	r.bg.Move(fyne.NewPos(0, 0))
	r.bg.Resize(size)
	r.border.Move(fyne.NewPos(0, 0))
	r.border.Resize(size)

	const padL float32 = 8
	const radioSide float32 = 14
	const innerSide float32 = 7
	const infoSide float32 = 13
	const padR float32 = 6
	radioY := (size.Height - radioSide) / 2
	r.radio.Move(fyne.NewPos(padL, radioY))
	r.radio.Resize(fyne.NewSize(radioSide, radioSide))
	innerOff := (radioSide - innerSide) / 2
	r.dot.Move(fyne.NewPos(padL+innerOff, radioY+innerOff))
	r.dot.Resize(fyne.NewSize(innerSide, innerSide))

	infoX := size.Width - padR - infoSide
	infoY := (size.Height - infoSide) / 2
	placeSquareIcon(r.info, fyne.NewPos(infoX, infoY), infoSide)
	r.row.infoHitX = infoX - 4
	r.row.infoHitW = infoSide + 8

	trailX := infoX - 8
	if r.row.updatable {
		const up float32 = 14
		trailX -= up
		pos := fyne.NewPos(trailX, (size.Height-up)/2)
		placeSquareIcon(r.update, pos, up)
		placeSquareIcon(r.updateBusy, pos, up)
		r.row.updateHitX = trailX - 4
		r.row.updateHitW = up + 8
		trailX -= 6
		if r.row.updating {
			r.update.Hide()
			r.updateBusy.Show()
		} else {
			r.update.Show()
			r.updateBusy.Hide()
		}
	} else {
		r.update.Hide()
		r.updateBusy.Hide()
		r.row.updateHitW = 0
	}
	if r.row.locked && !r.row.checked {
		const lk float32 = 14
		trailX -= lk
		placeSquareIcon(r.lock, fyne.NewPos(trailX, (size.Height-lk)/2), lk)
		r.lock.Show()
	} else {
		r.lock.Hide()
	}

	textX := padL + radioSide + 6
	textW := trailX - 8 - textX
	if textW < 0 {
		textW = 0
	}
	titleH := float32(16)
	subH := float32(16)
	blockH := titleH + 4 + subH
	verSize := r.ver.MinSize()
	if r.ver.Text != "" {
		blockH += 2 + verSize.Height
	}
	textY := (size.Height - blockH) / 2
	r.title.Move(fyne.NewPos(textX, textY))
	r.title.Resize(fyne.NewSize(textW, titleH))
	chipPadX := float32(6)
	chipPadY := float32(2)
	subSize := r.sub.MinSize()
	chipW := subSize.Width + chipPadX*2
	chipH := subSize.Height + chipPadY*2
	if chipW > textW && textW > 0 {
		chipW = textW
	}
	r.badgeBg.Move(fyne.NewPos(textX, textY+titleH+4))
	r.badgeBg.Resize(fyne.NewSize(chipW, chipH))
	r.sub.Move(fyne.NewPos(textX+chipPadX, textY+titleH+4+chipPadY-0.5))
	r.sub.Resize(subSize)
	if r.ver.Text == "" {
		r.ver.Hide()
	} else {
		r.ver.Move(fyne.NewPos(textX, textY+titleH+4+chipH+2))
		r.ver.Resize(fyne.NewSize(textW, verSize.Height))
		r.ver.Show()
	}
}

func (r *protocolPickRowRenderer) MinSize() fyne.Size { return r.row.MinSize() }

func (r *protocolPickRowRenderer) Refresh() {
	accent := r.row.badgeClr
	var titleClr color.Color = design.ColorMutedOlive
	subClr := accent
	var radioStroke color.Color = design.ColorChromeOlive
	var borderClr color.Color = design.ColorChromeOlive
	if r.row.checked {
		titleClr = design.ColorTextLight
		radioStroke = accent
		borderClr = accent
		r.dot.FillColor = accent
		r.dot.Show()
		r.lock.Hide()
	} else if r.row.locked {
		titleClr = design.ColorEmptyHint
		subClr = design.ColorEmptyHint
		r.dot.Hide()
		r.lock.Show()
	} else {
		r.dot.Hide()
		r.lock.Hide()
	}
	if (r.row.hovered || r.row.preview) && !r.row.disabled {
		borderClr = accent
		if !r.row.checked && !r.row.locked {
			radioStroke = accent
		}
	}
	if r.row.disabled && !r.row.checked {
		titleClr = design.ColorEmptyHint
		subClr = design.ColorEmptyHint
		borderClr = design.ColorChromeOlive
		radioStroke = design.ColorChromeOlive
	}
	r.bg.FillColor = design.ColorGray900
	r.border.StrokeColor = borderClr
	r.radio.StrokeColor = radioStroke
	r.radio.FillColor = color.Transparent
	r.title.Color = titleClr
	r.sub.Color = subClr
	r.badgeBg.StrokeColor = subClr
	r.badgeBg.FillColor = protocolBadgeFill(subClr)
	r.ver.Text = r.row.version
	r.ver.Color = design.ColorMutedOlive
	if r.row.disabled && !r.row.checked || r.row.locked && !r.row.checked {
		r.ver.Color = design.ColorEmptyHint
	}
	r.bg.Refresh()
	r.border.Refresh()
	r.radio.Refresh()
	r.dot.Refresh()
	r.title.Refresh()
	r.badgeBg.Refresh()
	r.sub.Refresh()
	r.ver.Refresh()
	r.lock.Refresh()
	r.info.Refresh()
	r.Layout(r.row.Size())
}

func (r *protocolPickRowRenderer) Objects() []fyne.CanvasObject { return r.objects }
func (r *protocolPickRowRenderer) Destroy()                     {}

type equalHBoxLayout struct {
	gap float32
}

func (l *equalHBoxLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := 0
	for _, o := range objects {
		if o != nil && o.Visible() {
			n++
		}
	}
	if n == 0 {
		return
	}
	col := (size.Width - l.gap*float32(n-1)) / float32(n)
	if col < 0 {
		col = 0
	}
	x := float32(0)
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		o.Move(fyne.NewPos(x, 0))
		o.Resize(fyne.NewSize(col, size.Height))
		x += col + l.gap
	}
}

func (l *equalHBoxLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	var maxW, maxH float32
	n := 0
	for _, o := range objects {
		if o == nil || !o.Visible() {
			continue
		}
		min := o.MinSize()
		if min.Width > maxW {
			maxW = min.Width
		}
		if min.Height > maxH {
			maxH = min.Height
		}
		n++
	}
	if n == 0 {
		return fyne.NewSize(0, 0)
	}
	return fyne.NewSize(maxW*float32(n)+l.gap*float32(n-1), maxH)
}

// cardGridLayout lays Protocol full-width on top, then Permissions|Status.
// A 4th card (Graphics) sits under Permissions at half width.
type cardGridLayout struct {
	gap         float32
	topInset    float32
	bottomInset float32
}

func (l *cardGridLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	n := len(objects)
	if n < 3 {
		return
	}
	gap := l.gap
	place := func(obj fyne.CanvasObject, x, y, w, h float32) {
		obj.Move(fyne.NewPos(x, y))
		obj.Resize(fyne.NewSize(w, h))
	}
	topW := size.Width - l.topInset*2
	botW := size.Width - l.bottomInset*2
	if topW < 0 {
		topW = 0
	}
	if botW < 0 {
		botW = 0
	}
	col := (botW - gap) / 2
	if col < 0 {
		col = 0
	}

	protoH := objects[0].MinSize().Height
	permH := objects[1].MinSize().Height
	statH := objects[2].MinSize().Height
	var gfxH float32
	if n >= 4 {
		gfxH = objects[3].MinSize().Height
	}

	place(objects[0], l.topInset, 0, topW, protoH)
	place(objects[1], l.bottomInset, protoH+gap, col, permH)
	place(objects[2], l.bottomInset+col+gap, protoH+gap, col, statH)
	if n >= 4 {
		place(objects[3], l.bottomInset, protoH+gap+permH+gap, col, gfxH)
	}
}

func (l *cardGridLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	n := len(objects)
	if n < 3 {
		return fyne.NewSize(0, 0)
	}
	protoH := objects[0].MinSize().Height
	permH := objects[1].MinSize().Height
	statH := objects[2].MinSize().Height
	leftH := permH
	if n >= 4 {
		leftH = permH + l.gap + objects[3].MinSize().Height
	}
	pairH := fyne.Max(leftH, statH)
	topW := objects[0].MinSize().Width + l.topInset*2
	botW := objects[1].MinSize().Width + objects[2].MinSize().Width + l.gap + l.bottomInset*2
	if n >= 4 {
		gfxW := objects[3].MinSize().Width + objects[2].MinSize().Width + l.gap + l.bottomInset*2
		botW = fyne.Max(botW, gfxW)
	}
	return fyne.NewSize(fyne.Max(topW, botW), protoH+pairH+l.gap)
}

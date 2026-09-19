package ui

import (
	"fmt"
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	qrcode "github.com/skip2/go-qrcode"

	"usbridge_agent/internal/ui/design"
)

const tokenDialogWidth float32 = 380

func (w *Window) showTokenDialog(parent fyne.Window) {
	if parent == nil {
		return
	}

	qrImage := canvas.NewImageFromResource(nil)
	qrImage.FillMode = canvas.ImageFillContain
	qrImage.SetMinSize(fyne.NewSize(168, 168))
	qrMessage := canvas.NewText("", design.ColorMutedOlive)
	qrMessage.TextSize = 10
	qrMessage.Alignment = fyne.TextAlignCenter
	qrMessage.Hide()

	linkEntry := newReadOnlyEntry()
	linkField := wrapTokenTextField(linkEntry)

	copyLinkBtn := newIconActionButton(loc().CopyLink, theme.ContentCopyIcon(), func() {
		masterKey := strings.TrimSpace(w.cfg.MasterKey)
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, masterKey, protocol)
		if link != "" {
			parent.Clipboard().SetContent(link)
		}
	})
	copyLinkBtn.Compact = true

	regenerateBtn := newIconActionButton(loc().RegenerateKey, theme.ViewRefreshIcon(), nil)
	regenerateBtn.Compact = true
	regenerateBtn.Danger = true

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	refreshDialogContent := func() {
		masterKey := strings.TrimSpace(w.cfg.MasterKey)
		if masterKey == "" {
			masterKey = "unavailable"
		}
		internalHost, tailscaleHost, protocol := w.quickConnectTargets()
		link := buildQuickConnectLink(internalHost, tailscaleHost, masterKey, protocol)

		linkEntry.SetFrozen(link)
		if link == "" {
			copyLinkBtn.Disable()
			qrImage.Resource = nil
			qrImage.Hide()
			qrMessage.Text = loc().QRUnavailable
			qrMessage.Show()
			qrMessage.Refresh()
			qrImage.Refresh()
			return
		}
		copyLinkBtn.Enable()

		pngBytes, err := qrcode.Encode(link, qrcode.Medium, 280)
		if err != nil {
			qrImage.Resource = nil
			qrImage.Hide()
			qrMessage.Text = fmt.Sprintf(loc().QRUnavailableErr, err)
			qrMessage.Show()
			qrMessage.Refresh()
			qrImage.Refresh()
			return
		}

		qrImage.Resource = fyne.NewStaticResource("agent-token-qr.png", pngBytes)
		qrImage.Show()
		qrImage.Refresh()
		qrMessage.Hide()
		qrMessage.Text = ""
		qrMessage.Refresh()
	}

	regenerateBtn.OnTapped = func() {
		if w.token == nil {
			return
		}
		cfg, err := w.token.RegenerateMasterKey()
		if err != nil {
			dialog.ShowError(err, parent)
			return
		}
		w.cfg = cfg
		refreshDialogContent()
	}

	title := canvas.NewText(loc().TokenTitle, design.ColorTextLight)
	title.TextSize = 13
	title.TextStyle.Bold = true
	sep := canvas.NewRectangle(design.ColorDialogSep)
	sep.SetMinSize(fyne.NewSize(0, 1))
	headerBand := canvas.NewRectangle(color.Transparent)
	headerBand.SetMinSize(fyne.NewSize(0, 45))
	header := container.New(&tightVBoxLayout{gap: 0},
		newDialogTopAccentBar(),
		container.NewStack(headerBand, newExactInset(title, 21, 44, 12, 17)),
		sep,
	)

	actions := container.NewCenter(container.New(&tightHBoxLayout{gap: 8}, copyLinkBtn, regenerateBtn))
	body := container.NewVBox(
		container.NewCenter(qrImage),
		container.NewCenter(qrMessage),
		spacerSize(1, 8),
		linkField,
		spacerSize(1, 10),
		actions,
	)

	widthLock := canvas.NewRectangle(color.Transparent)
	widthLock.SetMinSize(fyne.NewSize(tokenDialogWidth, 1))
	inner := container.NewBorder(
		header, nil, nil, nil,
		container.NewVBox(widthLock, newExactInset(body, 18, 18, 10, 16)),
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

	refreshDialogContent()
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

func wrapTokenTextField(entry *readOnlyEntry) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorGray950)
	bg.CornerRadius = 6
	bg.StrokeColor = design.ColorTailscaleChipBorder
	bg.StrokeWidth = 1
	themed := container.NewThemeOverride(entry, &tokenFieldTheme{Theme: design.NewBrandTheme()})
	return container.NewStack(bg, newExactInset(themed, 8, 8, 6, 6))
}

type tokenFieldTheme struct {
	fyne.Theme
}

func (t *tokenFieldTheme) Color(name fyne.ThemeColorName, v fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameInputBackground, theme.ColorNameDisabledButton:
		return color.Transparent
	case theme.ColorNameInputBorder, theme.ColorNameFocus, theme.ColorNamePrimary:
		return color.Transparent
	case theme.ColorNameForeground, theme.ColorNamePlaceHolder:
		return design.ColorAddress
	}
	return t.Theme.Color(name, v)
}

func (t *tokenFieldTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding, theme.SizeNameInnerPadding:
		return 0
	case theme.SizeNameText:
		return 10
	}
	return t.Theme.Size(name)
}

// readOnlyEntry lets the user select and copy a slice of the token link
// without being able to edit it (Disable() also blocks selection).
type readOnlyEntry struct {
	widget.Entry
	frozen string
}

func newReadOnlyEntry() *readOnlyEntry {
	e := &readOnlyEntry{}
	e.MultiLine = true
	e.Wrapping = fyne.TextWrapBreak
	e.ExtendBaseWidget(e)
	e.SetMinRowsVisible(3)
	return e
}

func (e *readOnlyEntry) SetFrozen(s string) {
	e.frozen = s
	e.SetText(s)
}

func (e *readOnlyEntry) TypedRune(rune) {}

func (e *readOnlyEntry) TypedKey(ev *fyne.KeyEvent) {
	if ev == nil {
		return
	}
	switch ev.Name {
	case fyne.KeyLeft, fyne.KeyRight, fyne.KeyUp, fyne.KeyDown,
		fyne.KeyHome, fyne.KeyEnd, fyne.KeyPageUp, fyne.KeyPageDown:
		e.Entry.TypedKey(ev)
	}
}

func (e *readOnlyEntry) TypedShortcut(s fyne.Shortcut) {
	switch s.(type) {
	case *fyne.ShortcutCopy, *fyne.ShortcutSelectAll:
		e.Entry.TypedShortcut(s)
	}
}

func (e *readOnlyEntry) TappedSecondary(*fyne.PointEvent) {}

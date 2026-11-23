package ui

import (
	"image/color"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/guestauth"
	"usbridge_agent/internal/remotelock"
	"usbridge_agent/internal/ui/design"
)

// sessionLockBarrier is a full-window opaque widget that swallows all mouse
// taps so clicks never bleed through to the underlying agent controls while a
// restricted client session is active.
type sessionLockBarrier struct {
	widget.BaseWidget
	content fyne.CanvasObject
}

func newSessionLockBarrier(content fyne.CanvasObject) *sessionLockBarrier {
	b := &sessionLockBarrier{content: content}
	b.ExtendBaseWidget(b)
	return b
}

func (b *sessionLockBarrier) CreateRenderer() fyne.WidgetRenderer {
	bg := canvas.NewRectangle(color.NRGBA{R: 12, G: 14, B: 16, A: 248})
	return widget.NewSimpleRenderer(container.NewStack(bg, b.content))
}

func (b *sessionLockBarrier) Tapped(*fyne.PointEvent)          {}
func (b *sessionLockBarrier) TappedSecondary(*fyne.PointEvent) {}

type sessionLockState struct {
	overlay          *sessionLockBarrier
	clientsLabel     *canvas.Text
	pinEntry         *widget.Entry
	errText          *canvas.Text
	unlockBox        *fyne.Container
	noPINHint        *canvas.Text
	activeRestricted bool
	adminUnlocked    bool
	prevRemoteLock   bool
	remotelockForced bool
}

func (w *Window) buildSessionLockOverlay() fyne.CanvasObject {
	title := canvas.NewText("Agent Interface Locked", design.ColorTextLight)
	title.TextSize = 15
	title.TextStyle.Bold = true
	title.Alignment = fyne.TextAlignCenter

	sub := canvas.NewText("A restricted client session is active. Settings & Master Key are hidden.", design.ColorMutedOlive)
	sub.TextSize = 11
	sub.Alignment = fyne.TextAlignCenter

	clientsLbl := canvas.NewText("", design.ColorAddress)
	clientsLbl.TextSize = 11
	clientsLbl.Alignment = fyne.TextAlignCenter

	pinEntry := widget.NewPasswordEntry()
	pinEntry.SetPlaceHolder("Admin PIN")

	errText := canvas.NewText("", color.NRGBA{R: 239, G: 68, B: 68, A: 255})
	errText.TextSize = 10
	errText.Alignment = fyne.TextAlignCenter
	errText.Hide()

	doUnlock := func() {
		if w.token == nil {
			return
		}
		pin := pinEntry.Text
		if w.token.VerifyAdminPIN(pin) {
			w.sessionLock.adminUnlocked = true
			w.cachedAdminPIN = pin
			pinEntry.SetText("")
			errText.Hide()
			w.sessionLock.overlay.Hide()
			w.sessionLock.overlay.Refresh()
		} else {
			errText.Text = "Invalid Admin PIN"
			errText.Show()
			errText.Refresh()
		}
	}
	pinEntry.OnSubmitted = func(string) { doUnlock() }

	unlockBtn := newIconActionButton("Unlock (Admin)", nil, doUnlock)
	unlockBtn.Compact = true
	unlockBtn.Accent = true

	unlockBox := container.NewVBox(
		pinEntry,
		spacerSize(1, 6),
		container.NewCenter(unlockBtn),
		errText,
	)

	noPINHint := canvas.NewText("Set an Admin PIN in Access → Security to unlock while a session is active.", design.ColorMutedOlive)
	noPINHint.TextSize = 10
	noPINHint.Alignment = fyne.TextAlignCenter
	noPINHint.Hide()

	cardInner := container.NewVBox(
		title,
		spacerSize(1, 4),
		sub,
		spacerSize(1, 8),
		clientsLbl,
		spacerSize(1, 12),
		unlockBox,
		noPINHint,
	)
	widthLock := canvas.NewRectangle(color.Transparent)
	widthLock.SetMinSize(fyne.NewSize(320, 1))

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusMD
	cardBg.StrokeColor = design.ColorBorder
	cardBg.StrokeWidth = 1

	card := container.NewStack(
		cardBg,
		container.NewVBox(widthLock, newExactInset(cardInner, 20, 20, 18, 18)),
	)

	barrier := newSessionLockBarrier(container.NewCenter(card))
	barrier.Hide()

	w.sessionLock = &sessionLockState{
		overlay:      barrier,
		clientsLabel: clientsLbl,
		pinEntry:     pinEntry,
		errText:      errText,
		unlockBox:    unlockBox,
		noPINHint:    noPINHint,
	}
	return barrier
}

// syncSessionLock updates the UI lock overlay and hardware remotelock state
// whenever the set of active restricted sessions changes. Must be called on
// the Fyne UI thread.
func (w *Window) syncSessionLock(active []guestauth.Session, pinConfigured bool) {
	if w.sessionLock == nil || w.sessionLock.overlay == nil {
		return
	}
	st := w.sessionLock
	hasActive := len(active) > 0

	if hasActive {
		names := make([]string, 0, len(active))
		for _, s := range active {
			names = append(names, s.DisplayName())
		}
		st.clientsLabel.Text = "Active: " + strings.Join(names, ", ")
		st.clientsLabel.Refresh()

		if pinConfigured {
			st.unlockBox.Show()
			st.noPINHint.Hide()
		} else {
			st.unlockBox.Hide()
			st.noPINHint.Show()
		}

		if !st.activeRestricted {
			// Transition: idle -> restricted session active.
			st.activeRestricted = true
			st.adminUnlocked = false
			st.prevRemoteLock = w.token != nil && w.token.RemoteWindowLockEnabled()
			remotelock.SetEnabled(true)
			st.remotelockForced = true
			// Dismiss any open popups (e.g. TOKEN / Master Key QR) immediately.
			if w.guiWin != nil && w.guiWin.Canvas() != nil {
				for _, ov := range w.guiWin.Canvas().Overlays().List() {
					ov.Hide()
				}
			}
		}
		if !st.adminUnlocked && st.overlay.Hidden {
			st.overlay.Show()
			st.overlay.Refresh()
		}
	} else {
		if st.activeRestricted {
			// Transition: restricted session ended -> auto-unlock.
			st.activeRestricted = false
			st.adminUnlocked = false
			if st.remotelockForced {
				remotelock.SetEnabled(st.prevRemoteLock)
				st.remotelockForced = false
			}
		}
		if !st.overlay.Hidden {
			st.overlay.Hide()
			st.overlay.Refresh()
		}
	}
}

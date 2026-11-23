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

type sessionLockState struct {
	modal            *widget.PopUp
	clientsLabel     *canvas.Text
	pinEntry         *widget.Entry
	errText          *canvas.Text
	hintText         *canvas.Text
	pinConfigured    bool
	activeRestricted bool
	adminUnlocked    bool
	prevRemoteLock   bool
	remotelockForced bool
}

func (w *Window) initSessionLock(win fyne.Window) {
	if win == nil {
		return
	}
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

	hintText := canvas.NewText("", design.ColorMutedOlive)
	hintText.TextSize = 10
	hintText.Alignment = fyne.TextAlignCenter

	errText := canvas.NewText("", color.NRGBA{R: 239, G: 68, B: 68, A: 255})
	errText.TextSize = 10
	errText.Alignment = fyne.TextAlignCenter
	errText.Hide()

	doUnlock := func() {
		if w.token == nil {
			return
		}
		pin := pinEntry.Text
		if w.sessionLock.pinConfigured {
			if !w.token.VerifyAdminPIN(pin) {
				errText.Text = "Invalid Admin PIN"
				errText.Show()
				errText.Refresh()
				return
			}
			w.cachedAdminPIN = pin
		} else if pin != "" {
			if err := w.token.SetAdminPIN("", pin); err != nil {
				errText.Text = err.Error()
				errText.Show()
				errText.Refresh()
				return
			}
			w.cachedAdminPIN = pin
			w.sessionLock.pinConfigured = true
		}
		w.sessionLock.adminUnlocked = true
		pinEntry.SetText("")
		errText.Hide()
		if w.sessionLock.modal != nil {
			w.sessionLock.modal.Hide()
			if w.guiWin != nil && w.guiWin.Canvas() != nil {
				w.guiWin.Canvas().Overlays().Remove(w.sessionLock.modal)
			}
		}
	}
	pinEntry.OnSubmitted = func(string) { doUnlock() }

	unlockBtn := newIconActionButton("Unlock", nil, doUnlock)
	unlockBtn.Compact = true
	unlockBtn.Accent = true

	cardInner := container.NewVBox(
		title,
		spacerSize(1, 4),
		sub,
		spacerSize(1, 8),
		clientsLbl,
		spacerSize(1, 10),
		pinEntry,
		hintText,
		spacerSize(1, 6),
		container.NewCenter(unlockBtn),
		errText,
	)
	widthLock := canvas.NewRectangle(color.Transparent)
	widthLock.SetMinSize(fyne.NewSize(340, 1))

	cardBg := canvas.NewRectangle(design.ColorGray900)
	cardBg.CornerRadius = design.RadiusMD
	cardBg.StrokeColor = design.ColorBorder
	cardBg.StrokeWidth = 1

	card := container.NewStack(
		cardBg,
		container.NewVBox(widthLock, newExactInset(cardInner, 20, 20, 18, 18)),
	)

	modal := widget.NewModalPopUp(card, win.Canvas())

	w.sessionLock = &sessionLockState{
		modal:        modal,
		clientsLabel: clientsLbl,
		pinEntry:     pinEntry,
		errText:      errText,
		hintText:     hintText,
	}
}

// syncSessionLock updates the modal lock overlay and hardware remotelock state
// whenever the set of active restricted sessions changes. Must be called on
// the Fyne UI thread; prevRemoteLock is pre-fetched off the UI thread.
func (w *Window) syncSessionLock(active []guestauth.Session, pinConfigured bool, prevRemoteLock bool) {
	if w.sessionLock == nil || w.sessionLock.modal == nil {
		return
	}
	st := w.sessionLock
	st.pinConfigured = pinConfigured
	hasActive := len(active) > 0

	if hasActive {
		names := make([]string, 0, len(active))
		for _, s := range active {
			names = append(names, s.DisplayName())
		}
		st.clientsLabel.Text = "Active: " + strings.Join(names, ", ")
		st.clientsLabel.Refresh()

		if pinConfigured {
			st.pinEntry.SetPlaceHolder("Enter Admin PIN to unlock")
			st.hintText.Text = "Enter your Admin PIN to access settings during this session."
		} else {
			st.pinEntry.SetPlaceHolder("Optional: set new Admin PIN (min 4 chars)")
			st.hintText.Text = "No Admin PIN set yet — click Unlock, or enter a PIN to protect settings."
		}
		st.hintText.Refresh()

		if !st.activeRestricted {
			st.activeRestricted = true
			st.adminUnlocked = false
			st.prevRemoteLock = prevRemoteLock
			st.remotelockForced = true
			go remotelock.SetEnabled(true)
			// Dismiss and remove any open popups (e.g. TOKEN / Master Key QR) immediately.
			if w.guiWin != nil && w.guiWin.Canvas() != nil {
				overlays := w.guiWin.Canvas().Overlays()
				for _, ov := range overlays.List() {
					if ov != st.modal {
						ov.Hide()
						overlays.Remove(ov)
					}
				}
			}
		}
		if !st.adminUnlocked && st.modal.Hidden {
			st.modal.Show()
		}
	} else {
		if st.activeRestricted {
			st.activeRestricted = false
			st.adminUnlocked = false
			if st.remotelockForced {
				restore := st.prevRemoteLock
				st.remotelockForced = false
				go remotelock.SetEnabled(restore)
			}
		}
		if !st.modal.Hidden {
			st.modal.Hide()
		}
		if w.guiWin != nil && w.guiWin.Canvas() != nil {
			w.guiWin.Canvas().Overlays().Remove(st.modal)
		}
	}
}

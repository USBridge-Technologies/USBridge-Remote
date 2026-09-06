package gui

import (
	"context"
	"fmt"
	"time"

	"usbridge-client/internal/account"
	"usbridge-client/internal/gui/controller"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

// accountDialogSnapshot is the subset of AccountManager state that actually
// changes what showAccountDialog's body needs to look like -- compared
// tick-to-tick by the background poller (see showAccountDialog) so a
// render only happens on a REAL transition (login started/finished/failed,
// logged out), never unconditionally on every 2s tick. Rebuilding the
// whole body on every tick regardless of whether anything changed is what
// caused the dialog to visibly flicker ("No licenses" flashing in and out)
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
// mirrors the Go agent's showLicenseDialog in spirit (a small
// self-re-rendering dialog driven by a status snapshot) but much simpler:
// the client has no billing of its own (see internal/account's package doc
// comment), this is purely "who am I signed in as, and what does that
// account own" -- plus setting up the sync passphrase that end-to-end
// encrypts the synced connections list (see internal/syncconn,
// connection_manager_sync.go).
func (mw *MainWindow) showAccountDialog() {
	if mw.connectionManager == nil || mw.connectionManager.Account == nil {
		return
	}
	am := mw.connectionManager.Account

	body := container.NewVBox()
	// licensesLoaded/licensesCache/licensesErr: fetched exactly ONCE per
	// dialog open (the first time render() reaches the LoggedIn case), not
	// re-fetched on every render -- see accountLicensesList below.
	var licensesLoaded bool
	var licensesCache []account.License
	var licensesErr error

	var render func()
	render = func() {
		body.RemoveAll()

		switch {
		case am.LoginInProgress():
			body.Add(widget.NewLabel("Waiting for Google login to complete in your browser…"))
			body.Add(widget.NewProgressBarInfinite())
			cancelBtn := widget.NewButton("Cancel", func() {
				am.CancelLogin()
				render()
			})
			cancelBtn.Importance = widget.LowImportance
			body.Add(container.NewCenter(cancelBtn))

		case am.LoggedIn():
			body.Add(widget.NewLabel(fmt.Sprintf("Signed in as %s", am.Email())))
			if errMsg := am.LastError(); errMsg != "" {
				body.Add(widget.NewLabel(errMsg))
			}
			body.Add(widget.NewSeparator())
			body.Add(accountLicensesList(am, &licensesLoaded, &licensesCache, &licensesErr, render))
			body.Add(widget.NewSeparator())
			body.Add(accountSyncPassphraseSection(am, render))

			logoutBtn := widget.NewButton("Log out", func() {
				am.Logout()
				// Forget the cached license list too -- otherwise logging
				// back in within the same dialog session (LoggedIn()
				// flips true again on the next poll tick) would show the
				// PREVIOUS login's stale cached licenses instead of
				// re-fetching for the new one.
				licensesLoaded = false
				licensesCache = nil
				licensesErr = nil
				render()
			})
			logoutBtn.Importance = widget.LowImportance
			body.Add(container.NewCenter(logoutBtn))

		default:
			intro := widget.NewLabel("Log in to see your USBridge licenses and sync your saved connections across devices.")
			intro.Wrapping = fyne.TextWrapWord
			body.Add(intro)
			if errMsg := am.LastError(); errMsg != "" {
				body.Add(widget.NewLabel(errMsg))
			}
			loginBtn := widget.NewButton("Log in with Google", func() {
				if err := am.StartLogin(); err == nil {
					render()
				}
			})
			loginBtn.Importance = widget.HighImportance
			body.Add(container.NewCenter(loginBtn))
		}

		body.Refresh()
	}
	render()

	dlg := dialog.NewCustom("Account", "Close", body, mw.window)
	dlg.Resize(fyne.NewSize(420, 0))

	stop := make(chan struct{})
	stopped := false
	dlg.SetOnClosed(func() {
		if !stopped {
			stopped = true
			close(stop)
		}
	})
	dlg.Show()

	// Polls while the dialog is open (same 2s cadence the agent's own
	// license dialog uses) so a login completing in the browser is
	// reflected without needing to close and reopen this dialog -- but
	// only actually re-renders (rebuilding every widget, including
	// whatever Entry the human might be mid-typing into) when the
	// snapshot genuinely changed since the last tick. See
	// accountDialogSnapshot's own doc comment for why this matters.
	go func() {
		last := newAccountDialogSnapshot(am)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
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

// accountLicensesList fetches the logged-in account's licenses exactly
// once per dialog open (guarded by *loaded) and renders from the cached
// result on every subsequent render() call -- render() itself only runs on
// a real state transition now (see accountDialogSnapshot), but this cache
// also means a manual re-render (e.g. after setting a sync passphrase)
// doesn't refire an unnecessary network call.
func accountLicensesList(am *controller.AccountManager, loaded *bool, cache *[]account.License, cacheErr *error, render func()) fyne.CanvasObject {
	if *loaded {
		return renderLicenses(*cache, *cacheErr)
	}

	box := container.NewVBox(widget.NewLabel("Loading your licenses…"))
	go func() {
		licenses, err := am.Licenses(context.Background())
		*cache = licenses
		*cacheErr = err
		*loaded = true
		fyne.Do(render)
	}()
	return box
}

func renderLicenses(licenses []account.License, err error) fyne.CanvasObject {
	box := container.NewVBox()
	switch {
	case err != nil:
		box.Add(widget.NewLabel(fmt.Sprintf("Could not load licenses: %v", err)))
	case len(licenses) == 0:
		box.Add(widget.NewLabel("No licenses on this account yet."))
	default:
		for _, lic := range licenses {
			box.Add(widget.NewLabel(fmt.Sprintf("[%s] %s — %s", lic.Kind, lic.Identifier, lic.Status)))
		}
	}
	return box
}

// accountSyncPassphraseSection lets the human set (or, on a second device,
// re-enter the same one they used on the first) the passphrase that
// derives this device's connections-sync encryption key -- see
// internal/syncconn's doc comment for why this is a SEPARATE secret from
// the Google login above, never sent to any server.
func accountSyncPassphraseSection(am *controller.AccountManager, render func()) fyne.CanvasObject {
	if am.HasSyncKey() {
		return widget.NewLabel("Connections sync: on")
	}

	label := widget.NewLabel("Set a sync passphrase to sync your saved connections across devices (never sent to our servers):")
	label.Wrapping = fyne.TextWrapWord
	entry := widget.NewPasswordEntry()
	entry.SetPlaceHolder("Sync passphrase")
	saveBtn := widget.NewButton("Set passphrase", func() {
		if entry.Text == "" {
			return
		}
		am.SetSyncPassphrase(entry.Text)
		render()
	})
	return container.NewVBox(label, entry, container.NewCenter(saveBtn))
}

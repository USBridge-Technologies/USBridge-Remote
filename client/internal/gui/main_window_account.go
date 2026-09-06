package gui

import (
	"context"
	"fmt"
	"time"

	"usbridge-client/internal/gui/controller"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

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
			body.Add(accountLicensesList(am))
			body.Add(widget.NewSeparator())
			body.Add(accountSyncPassphraseSection(am, render))

			logoutBtn := widget.NewButton("Log out", func() {
				am.Logout()
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
	// reflected without needing to close and reopen this dialog.
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fyne.Do(render)
			}
		}
	}()
}

// accountLicensesList fetches and renders the logged-in account's licenses
// -- kept as its own function so showAccountDialog's switch above stays
// readable. Fetched once per dialog open (not on the 2s poll -- these
// rarely change mid-dialog and a license list is a much heavier call than
// the local status render's).
func accountLicensesList(am *controller.AccountManager) fyne.CanvasObject {
	box := container.NewVBox(widget.NewLabel("Loading your licenses…"))
	go func() {
		licenses, err := am.Licenses(context.Background())
		fyne.Do(func() {
			box.RemoveAll()
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
			box.Refresh()
		})
	}()
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

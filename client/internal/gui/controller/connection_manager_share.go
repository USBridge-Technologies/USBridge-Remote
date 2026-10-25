package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

// Guest access from the connections screen: "Share" on a contact opens a
// dialog that talks to that agent (owner calls signed with the contact's
// master key, see api.USBClient.InviteGuest). The agent keeps the list of
// guests; this screen only remembers how many there were last time it
// looked, so the card can show "3 with access" without dialing every agent.

type shareStat struct {
	Live int `json:"live"`
	// Waiting counts invitations not yet accepted plus guests asking to connect.
	Waiting int `json:"waiting"`
}

// shareStats caches shareStat per master key (a contact's stable identity,
// see connection_manager_sync.go). Not part of SavedConnection on purpose:
// that struct is synced to the account, this is local observation.
type shareStats struct {
	mu sync.Mutex
	m  map[string]shareStat
}

const shareStatsFile = "connection_shares.json"

func (cm *ConnectionManager) shareStatsURI() fyne.URI {
	uri, err := storage.Child(cm.app.Storage().RootURI(), shareStatsFile)
	if err != nil {
		u, _ := url.Parse("file://" + shareStatsFile)
		return storage.NewFileURI(u.String())
	}
	return uri
}

func (cm *ConnectionManager) loadShareStats() {
	cm.shareStats.mu.Lock()
	defer cm.shareStats.mu.Unlock()
	if cm.shareStats.m != nil {
		return
	}
	cm.shareStats.m = map[string]shareStat{}
	r, err := storage.Reader(cm.shareStatsURI())
	if err != nil {
		return
	}
	defer r.Close()
	if raw, err := io.ReadAll(r); err == nil {
		_ = json.Unmarshal(raw, &cm.shareStats.m)
	}
}

func (cm *ConnectionManager) shareStatFor(masterKey string) shareStat {
	if strings.TrimSpace(masterKey) == "" {
		return shareStat{}
	}
	cm.loadShareStats()
	cm.shareStats.mu.Lock()
	defer cm.shareStats.mu.Unlock()
	return cm.shareStats.m[masterKey]
}

func (cm *ConnectionManager) setShareStat(masterKey string, st shareStat) {
	cm.loadShareStats()
	cm.shareStats.mu.Lock()
	old := cm.shareStats.m[masterKey]
	cm.shareStats.m[masterKey] = st
	raw, _ := json.Marshal(cm.shareStats.m)
	cm.shareStats.mu.Unlock()
	if old == st {
		return
	}
	if w, err := storage.Writer(cm.shareStatsURI()); err == nil {
		defer w.Close()
		_, _ = w.Write(raw)
	}
}

func summarizeGuests(l *api.GuestList) shareStat {
	st := shareStat{}
	for _, g := range l.Guests {
		if !g.Live {
			continue
		}
		st.Live++
		if g.Status == "invited" || g.Session == "pending" {
			st.Waiting++
		}
	}
	return st
}

// agentClient builds an owner client for a saved contact.
func (cm *ConnectionManager) agentClient(conn SavedConnection) *api.USBClient {
	host := cm.resolveHostForProtocol(conn, normalizeConnectionProtocol(conn.Protocol))
	var c *api.USBClient
	if cm.ts != nil && host != "" && host == strings.TrimSpace(conn.TailscaleHost) {
		if hc, err := cm.ts.HTTPClient(); err == nil {
			c = api.NewUSBClientWithHTTPClient(host, 8080, 8, hc)
		}
	}
	if c == nil {
		c = api.NewUSBClient(host, 8080, 8)
	}
	c.SetAPISecretV2([]byte(strings.TrimSpace(conn.MasterKey)))
	return c
}

// shareDraft is what the dialog remembers across reopening (the dialog
// closes itself on Send; it reopens with the outcome).
type shareDraft struct {
	email   string
	role    int
	expiry  int
	consent bool
	message string
	isError bool
}

var shareExpirySeconds = []int64{3600, 24 * 3600, 7 * 24 * 3600, 30 * 24 * 3600, 0}
var shareRoles = []string{"viewer", "operator", "technician"}

func shareErrorText(err error) string {
	switch {
	case errors.Is(err, api.ErrGuestFreeLimit):
		return i18n.Current.ShareErrFree
	case errors.Is(err, api.ErrGuestCloudDown):
		return i18n.Current.ShareErrCloud
	case errors.Is(err, api.ErrGuestRateLimit):
		return i18n.Current.ShareErrRate
	case errors.Is(err, api.ErrGuestUnsupported):
		return i18n.Current.ShareErrUnsupported
	}
	return err.Error()
}

// openShareDialog is the card's Share button.
func (cm *ConnectionManager) openShareDialog(idx int) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	cm.showShareDialog(idx, shareDraft{role: 1, expiry: 1, consent: true}, nil)
}

func (cm *ConnectionManager) showShareDialog(idx int, draft shareDraft, known *api.GuestList) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	conn := cm.connections[idx]
	if strings.TrimSpace(conn.MasterKey) == "" {
		return
	}
	if known == nil {
		// Ask the agent first, then show the dialog with what it said.
		go func() {
			list, err := cm.agentClient(conn).ListGuests()
			if err != nil && draft.message == "" {
				draft.message, draft.isError = shareErrorText(err), true
			}
			if err == nil {
				cm.setShareStat(conn.MasterKey, summarizeGuests(list))
			}
			fyne.Do(func() {
				if err != nil {
					list = &api.GuestList{}
				}
				cm.refreshConnectionsList()
				cm.buildShareDialog(idx, draft, list, err == nil)
			})
		}()
		return
	}
	cm.buildShareDialog(idx, draft, known, true)
}

func (cm *ConnectionManager) buildShareDialog(idx int, draft shareDraft, list *api.GuestList, reachable bool) {
	if idx < 0 || idx >= len(cm.connections) {
		return
	}
	conn := cm.connections[idx]
	t := i18n.Current

	reopen := func(d shareDraft) {
		cm.showShareDialog(idx, d, nil)
	}
	act := func(fn func(c *api.USBClient) error) {
		go func() {
			err := fn(cm.agentClient(conn))
			d := draft
			d.message, d.isError = "", false
			if err != nil {
				d.message, d.isError = shareErrorText(err), true
			}
			fyne.Do(func() { reopen(d) })
		}()
	}

	body := container.NewVBox()
	body.Add(view.NewDialogHint(t.ShareHint, 0))

	var emailEntry *view.StyledEntry
	var roleSel, expirySel *view.HeaderDropdown
	var consent view.DialogToggle
	roleLabels := []string{t.ShareRoleViewer, t.ShareRoleOperator, t.ShareRoleTechnician}
	expiryLabels := []string{t.ShareExpiry1h, t.ShareExpiry1d, t.ShareExpiry7d, t.ShareExpiry30d, t.ShareExpiryNever}
	roleIdx, expiryIdx := draft.role, draft.expiry
	if reachable {
		emailEntry = view.NewStyledEntry()
		emailEntry.SetPlaceHolder(t.ShareEmailPlaceholder)
		emailEntry.SetText(draft.email)
		roleSel = view.NewDialogPicker(roleLabels, roleLabels[roleIdx], func(s string) {
			for i, l := range roleLabels {
				if l == s {
					roleIdx = i
				}
			}
		})
		expirySel = view.NewDialogPicker(expiryLabels, expiryLabels[expiryIdx], func(s string) {
			for i, l := range expiryLabels {
				if l == s {
					expiryIdx = i
				}
			}
		})
		consent = view.NewDialogToggle(draft.consent, nil)
		body.Add(view.NewDialogField(t.ShareEmailLabel, emailEntry))
		body.Add(view.NewDialogField(t.ShareRoleLabel, roleSel))
		body.Add(view.NewDialogField(t.ShareExpiryLabel, expirySel))
		body.Add(view.NewDialogToggleRow(consent, t.ShareAskConsent, t.ShareAskConsentDesc, 0))
	}

	if draft.message != "" {
		lbl := widget.NewLabel(draft.message)
		lbl.Wrapping = fyne.TextWrapWord
		body.Add(lbl)
	}

	if len(list.Guests) > 0 {
		body.Add(view.NewDialogField(t.ShareGuestsLabel, widget.NewLabel("")))
		rows := container.NewVBox()
		for _, g := range list.Guests {
			if !g.Live {
				continue
			}
			g := g
			status := t.ShareStatusIdle
			switch {
			case g.Session == "allowed":
				status = t.ShareStatusActive
			case g.Session == "pending":
				status = t.ShareStatusWaiting
			case g.Status == "invited":
				status = t.ShareStatusInvited
			}
			title := widget.NewLabel(fmt.Sprintf("%s  ·  %s", g.Email, status))
			title.Wrapping = fyne.TextWrapWord
			buttons := container.NewHBox()
			if g.Session == "pending" {
				buttons.Add(widget.NewButton(t.ShareAllow, func() { act(func(c *api.USBClient) error { return c.DecideGuest(g.ID, true) }) }))
				buttons.Add(widget.NewButton(t.ShareDeny, func() { act(func(c *api.USBClient) error { return c.DecideGuest(g.ID, false) }) }))
			}
			if g.Session == "allowed" {
				buttons.Add(widget.NewButton(t.ShareDisconnect, func() { act(func(c *api.USBClient) error { return c.EndGuestSession(g.ID) }) }))
			}
			buttons.Add(widget.NewButton(t.ShareRevoke, func() { act(func(c *api.USBClient) error { return c.RevokeGuest(g.ID) }) }))
			rows.Add(container.NewBorder(nil, nil, nil, buttons, title))
		}
		scroll := container.NewVScroll(rows)
		scroll.SetMinSize(fyne.NewSize(0, 120))
		body.Add(scroll)
	} else if reachable {
		body.Add(widget.NewLabel(t.ShareNoGuests))
	}

	spec := view.BrandFormDialogSpec{
		Parent:     cm.window,
		Title:      fmt.Sprintf("%s: %s", t.ShareTitle, conn.Name),
		Body:       body,
		CancelText: t.ShareClose,
		ApplyText:  t.ShareInvite,
		MinWidth:   440,
	}
	if reachable {
		spec.OnApply = func() {
			email := strings.TrimSpace(emailEntry.Text)
			d := shareDraft{email: email, role: roleIdx, expiry: expiryIdx, consent: consent.IsChecked()}
			if !strings.Contains(email, "@") || strings.ContainsAny(email, " ,;") {
				d.message, d.isError = t.ShareErrEmail, true
				reopen2(cm, idx, d, list)
				return
			}
			inv := api.GuestInvite{Email: email, Role: shareRoles[roleIdx], ExpiresIn: shareExpirySeconds[expiryIdx], RequireConsent: d.consent}
			go func() {
				link, sent, err := cm.agentClient(conn).InviteGuest(inv)
				switch {
				case err != nil:
					d.message, d.isError = shareErrorText(err), true
				case sent:
					d = shareDraft{role: roleIdx, expiry: expiryIdx, consent: d.consent, message: fmt.Sprintf(t.ShareInvitedOK, email)}
				default:
					logrus.Warnf("guest invitation for %s created without e-mail", email)
					d = shareDraft{role: roleIdx, expiry: expiryIdx, consent: d.consent, message: fmt.Sprintf(t.ShareInvitedNoMail, email, link)}
				}
				fyne.Do(func() { reopen(d) })
			}()
		}
	} else {
		spec.ApplyText = t.ShareClose
	}
	_ = time.Now
	view.ShowBrandFormDialog(spec)
}

// reopen2 re-shows the dialog with the data it already has (a local
// validation message needs no round trip to the agent).
func reopen2(cm *ConnectionManager, idx int, d shareDraft, list *api.GuestList) {
	fyne.Do(func() { cm.showShareDialog(idx, d, list) })
}

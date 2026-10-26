package gui

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"usbridge-client/internal/guestclient"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"
	"github.com/sirupsen/logrus"
)

// shareIDPattern is what the agent's NewID produces (128 random bits, base64url).
var shareIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{22,64}$`)

// parseJoinLink reads usbridge://join?share=<id>. Where the computer is
// comes with the guest's ticket, not with the link.
func parseJoinLink(uri string) (shareID string, err error) {
	u, perr := url.Parse(uri)
	if perr != nil {
		return "", errors.New("invalid invitation link")
	}
	if u.Scheme != "usbridge" || u.Host != "join" {
		return "", errors.New("not an invitation link")
	}
	shareID = u.Query().Get("share")
	if !shareIDPattern.MatchString(shareID) {
		return "", errors.New("the invitation link is incomplete")
	}
	return shareID, nil
}

// guestSessionSetter is implemented by the Moonlight video client.
type guestSessionSetter interface {
	SetGuestSession(*guestclient.Session)
}

// setGuestSession makes the next connection a guest's (or, with nil, the
// owner's again): agent requests carry the guest ticket instead of the master
// key, and Moonlight pairing is bound to the share.
func (mw *MainWindow) setGuestSession(s *guestclient.Session) {
	mw.guestSession = s
	mw.useGuestNetwork(s)
	if s != nil {
		// A guest never carries a master key: drop whatever an earlier owner
		// connection left, so it cannot be sent to this (other) computer.
		mw.activeAPISecret = nil
		if ms, ok := mw.videoClient.(interface{ SetAPISecret([]byte) }); ok {
			ms.SetAPISecret(nil)
		}
	}
	if g, ok := mw.videoClient.(guestSessionSetter); ok {
		g.SetGuestSession(s)
	}
}

// useGuestNetwork points everything that dials the computer (API, Moonlight,
// disk widget) at the guest's own node on the guest network while a guest
// session is active, and back at the user's own Tailscale afterwards. The
// user's personal Tailscale login is never touched.
func (mw *MainWindow) useGuestNetwork(s *guestclient.Session) {
	if s != nil && s.Tailnet != nil {
		if mw.ownerTailscale == nil {
			mw.ownerTailscale = mw.tailscaleService
		}
		if mw.guestTailscale != nil {
			_ = mw.guestTailscale.Stop()
		}
		mw.guestTailscale = service.NewGuestTailscaleService(s.Tailnet.ControlURL, s.Tailnet.AuthKey, s.Tailnet.Hostname)
		mw.injectTailscale(mw.guestTailscale)
		return
	}
	if mw.guestTailscale != nil {
		old := mw.guestTailscale
		mw.guestTailscale = nil
		go func() { _ = old.Stop() }()
	}
	if mw.ownerTailscale != nil {
		mw.injectTailscale(mw.ownerTailscale)
		mw.ownerTailscale = nil
	}
}

func (mw *MainWindow) injectTailscale(ts *service.TailscaleService) {
	mw.tailscaleService = ts
	if setter, ok := mw.videoClient.(interface {
		SetTailscaleService(*service.TailscaleService)
	}); ok {
		setter.SetTailscaleService(ts)
	}
	if mw.diskWidget != nil {
		mw.diskWidget.SetTailscaleService(ts)
	}
	if mw.videoWidget != nil {
		mw.videoWidget.SetTailscaleService(ts)
	}
}

// handleJoinFromDeepLink opens a guest invitation: sign in with the invited
// account, get a ticket for the share, then connect with the role it grants.
func (mw *MainWindow) handleJoinFromDeepLink(shareID string) {
	logrus.Infof("🔗 Guest invitation opened for share %s", shareID)
	if mw.isConnected {
		fyne.Do(func() {
			dialog.ShowInformation("USBridge", "Disconnect from the current computer first, then open the invitation again.", mw.window)
		})
		return
	}
	var token string
	if mw.connectionManager != nil && mw.connectionManager.Account != nil {
		token, _ = mw.connectionManager.Account.AccountToken()
	}
	if token == "" {
		fyne.Do(func() {
			dialog.ShowConfirm("Sign in to accept the invitation",
				"Sign in to USBridge with the e-mail address that received the invitation, then open the link from the e-mail again.",
				func(ok bool) {
					if ok {
						mw.showAccountDialog()
					}
				}, mw.window)
		})
		return
	}

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		sess, err := guestclient.Join(ctx, "", func() string {
			t, _ := mw.connectionManager.Account.AccountToken()
			return t
		}, shareID)
		if err != nil {
			fyne.Do(func() { dialog.ShowError(guestJoinError(err), mw.window) })
			return
		}
		if sess.Host == "" {
			fyne.Do(func() {
				dialog.ShowError(errors.New("This invitation has no address for the computer. Ask the owner to send a new one."), mw.window)
			})
			return
		}
		fyne.Do(func() { mw.connectAsGuest(sess, sess.Host) })
	}()
}

// guestJoinError turns the guest client's errors into something to show.
func guestJoinError(err error) error {
	switch {
	case errors.Is(err, guestclient.ErrNotInvited):
		return errors.New("This account is not the one that was invited. Sign in with the e-mail address that received the invitation.")
	case errors.Is(err, guestclient.ErrShareEnded):
		return errors.New("This access has ended.")
	case errors.Is(err, guestclient.ErrNeedSignIn):
		return errors.New("Your sign-in has expired. Sign in again and open the link once more.")
	}
	return fmt.Errorf("could not open the invitation: %w", err)
}

// connectAsGuest starts the normal connect flow with the guest session in place.
func (mw *MainWindow) connectAsGuest(sess *guestclient.Session, host string) {
	mw.setGuestSession(sess)
	setForm := func() {
		mw.hostEntry.SetText(host)
		mw.tokenEntry.SetText("")
		// A Tailscale address is reached through the tailnet the guest was given
		// access to (the app's own Tailscale), anything else directly.
		if isLikelyTailscaleHost(host) {
			mw.protocolSelect.SetSelected(models.ConnectionProtocolTailscale)
		} else {
			mw.protocolSelect.SetSelected(models.ConnectionProtocolDirect)
		}
	}
	if mw.connectionManager != nil {
		mw.connectionManager.SetFormTextSilently(setForm)
	} else {
		setForm()
	}
	mw.handleConnectionToggle()
}

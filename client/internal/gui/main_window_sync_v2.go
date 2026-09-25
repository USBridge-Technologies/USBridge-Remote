package gui

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"usbridge-client/internal/api"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// syncWithBridgeV2 performs master sync with the bridge.
// Returns (tailscaleReady, error).
// tailscaleReady is true when the bridge is already logged in to Tailscale
// and a Tailscale address was obtained from the sync response.
// tailscaleRegister=true makes the bridge run 'tailscale up' (returns AuthURL if not yet logged in).
func (mw *MainWindow) syncWithBridgeV2(ctx context.Context, bootstrapHost, input string, tailscaleRegister ...bool) (bool, error) {
	doRegister := len(tailscaleRegister) > 0 && tailscaleRegister[0]
	_ = doRegister // used below
	secret := input

	// If it's a deep link, extract the secret
	if strings.HasPrefix(input, "usbridge://sync") {
		u, _ := url.Parse(input)
		if u != nil {
			secret = u.Query().Get("secret")
		}
	}

	if secret == "" {
		return false, fmt.Errorf("empty API secret")
	}

	mw.activeAPISecret = []byte(secret)

	// On Android with userspace Tailscale (tsnet), the OS dialer can't reach
	// Tailscale IPs. Use the tsnet-aware HTTP client so the sync goes through
	// the Tailscale netstack instead of failing with "connection refused".
	var bootstrapClient *api.USBClient
	if isLikelyTailscaleHost(bootstrapHost) && mw.tailscaleService != nil {
		if tsHTTPClient, tsErr := mw.tailscaleService.HTTPClient(); tsErr == nil {
			bootstrapClient = api.NewUSBClientWithHTTPClient(bootstrapHost, mw.config.USBPort, mw.config.APITimeout, tsHTTPClient)
		}
	}
	if bootstrapClient == nil {
		// Use the LAN-bound client for direct (non-Tailscale) hosts so that
		// socket binding bypasses any VPN routing table interference.
		bootstrapClient = api.NewDirectUSBClient(bootstrapHost, mw.config.USBPort, mw.config.USBTLSPort, mw.config.APITimeout)
	}
	bootstrapClient.SetAPISecretV2(mw.activeAPISecret)

	logrus.Infof("🔄 [SYNC] Performing master sync with bridge (host=%s)...", bootstrapHost)

	// Include Tailscale auth key if stored — server registers Tailscale internally.
	// Only when registration was actually requested: the bridge treats a
	// non-empty TailscaleKey as "register me" regardless of TailscaleRegister
	// (see agent/internal/api/sync.go's switch — a present key wins over the
	// flag), so sending it unconditionally here would silently register the
	// bridge even after the user unchecked "Register in Tailscale".
	tailscaleAuthKey := ""
	if doRegister {
		_, tailscaleAuthKey = mw.resolveBridgeAuthInputs(bootstrapHost, secret)
	}

	syncPayload := api.MasterSyncPayloadV2{
		TailscaleKey:      tailscaleAuthKey,
		TailscaleRegister: doRegister,
		Hostname:          "usbridge",
		ClientID:          "usbridge-client-desktop",
	}

	resp, err := bootstrapClient.MasterSyncV2(ctx, syncPayload)
	if err != nil {
		return false, fmt.Errorf("master sync failed: %v", err)
	}

	logrus.Info("✅ [SYNC] Master sync successful.")

	// If server returned a Tailscale IP, remember it so the Tailscale protocol
	// can connect directly without an additional API call.
	tailscaleReady := false
	if resp.TailscaleStatus != nil {
		tsIP := strings.TrimSpace(resp.TailscaleStatus.IP4)
		tsHost := strings.TrimSpace(resp.TailscaleStatus.DNSName)
		resolved := tsIP
		if resolved == "" {
			resolved = tsHost
		}
		if resolved != "" && mw.connectionManager != nil {
			// Pass empty internalHost when bootstrapHost is itself a Tailscale IP —
			// otherwise RememberResolvedTailscaleHost would overwrite the saved LAN
			// address with the Tailscale IP, losing the direct-LAN path.
			internalForSave := bootstrapHost
			if isLikelyTailscaleHost(bootstrapHost) {
				internalForSave = ""
			}
			mw.connectionManager.RememberResolvedTailscaleHost(bootstrapHost, internalForSave, resolved, secret)
			logrus.Infof("🛰️ [SYNC] Bridge Tailscale address: %s", resolved)
			tailscaleReady = true
		}
		logrus.Infof("🛰️ [SYNC] Bridge Tailscale status: logged_in=%v backend=%s ip=%s",
			resp.TailscaleStatus.LoggedIn, resp.TailscaleStatus.Backend, tsIP)

		// If bridge returned an AuthURL, open it in the browser for user approval.
		// Only open when the URL is new — polling runs every 10s and we must not
		// spam the browser with a new tab on every tick. Also only when THIS
		// call actually requested registration (doRegister): the bridge keeps
		// reporting a dangling AuthURL from any earlier registration attempt
		// (interactive tsnet login stays pending until approved/expired) even
		// on a plain status sync, so without this gate the browser popped open
		// on every connect regardless of the "Register in Tailscale" checkbox
		// for the *current* attempt — see resolveBridgeAuthInputs's sibling
		// gate a few lines up, which stops us from initiating a new one but
		// doesn't stop us from acting on a leftover one already in flight.
		authURL := strings.TrimSpace(resp.TailscaleStatus.AuthURL)
		logrus.Debugf("🛰️ [SYNC] authURL=%q doRegister=%v tailscaleReady=%v lastAuthURL=%q",
			authURL, doRegister, tailscaleReady, mw.lastTailscaleAuthURL)
		if authURL != "" && !tailscaleReady && doRegister {
			if authURL != mw.lastTailscaleAuthURL {
				mw.lastTailscaleAuthURL = authURL
				logrus.Infof("🛰️ [SYNC] Bridge Tailscale needs approval — opening browser: %s", authURL)
				fyne.Do(func() {
					if u, err := url.Parse(authURL); err == nil {
						_ = fyne.CurrentApp().OpenURL(u)
					}
				})
			} else {
				logrus.Debugf("🛰️ [SYNC] Bridge Tailscale still needs approval (same URL)")
			}
		} else if authURL != "" && !doRegister {
			logrus.Infof("🛰️ [SYNC] Bridge has a pending Tailscale AuthURL but registration was not requested this attempt — not opening browser")
		}
	}

	return tailscaleReady, nil
}

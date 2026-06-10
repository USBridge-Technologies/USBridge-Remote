package gui

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"usbridge-client/internal/api"

	"github.com/sirupsen/logrus"
)

func (mw *MainWindow) syncWithBridgeV2(ctx context.Context, bootstrapHost, input string) (string, error) {
	secret := input

	// If it's a deep link, extract the secret
	if strings.HasPrefix(input, "usbridge://sync") {
		u, _ := url.Parse(input)
		if u != nil {
			secret = u.Query().Get("secret")
		}
	}

	if secret == "" {
		return "", fmt.Errorf("empty API secret")
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
		bootstrapClient = api.NewUSBClient(bootstrapHost, mw.config.USBPort, mw.config.APITimeout)
	}
	bootstrapClient.SetAPISecretV2(mw.activeAPISecret)

	logrus.Infof("🔄 [SYNC] Performing master sync with bridge (host=%s)...", bootstrapHost)

	// Include Tailscale auth key if stored — server registers Tailscale internally.
	_, tailscaleAuthKey := mw.resolveBridgeAuthInputs(bootstrapHost, secret)

	syncPayload := api.MasterSyncPayloadV2{
		TailscaleKey: tailscaleAuthKey,
		Hostname:     "usbridge",
		ClientID:     "usbridge-client-desktop",
	}

	resp, err := bootstrapClient.MasterSyncV2(ctx, syncPayload)
	if err != nil {
		return "", fmt.Errorf("master sync failed: %v", err)
	}

	logrus.Infof("✅ [SYNC] Master sync successful. FRP Token received.")

	// If server returned a Tailscale IP, remember it so the Tailscale protocol
	// can connect directly without an additional API call.
	if resp.TailscaleStatus != nil {
		tsIP := strings.TrimSpace(resp.TailscaleStatus.IP4)
		tsHost := strings.TrimSpace(resp.TailscaleStatus.DNSName)
		resolved := tsIP
		if resolved == "" {
			resolved = tsHost
		}
		if resolved != "" && mw.connectionManager != nil {
			mw.connectionManager.RememberResolvedTailscaleHost(bootstrapHost, bootstrapHost, resolved, secret)
			logrus.Infof("🛰️ [SYNC] Bridge Tailscale address: %s", resolved)
		}
	}

	return resp.FRPToken, nil
}

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
	
	bootstrapClient := api.NewUSBClient(bootstrapHost, mw.config.USBPort, mw.config.APITimeout)
	bootstrapClient.SetAPISecretV2(mw.activeAPISecret)

	logrus.Infof("🔄 [SYNC] Performing master sync with bridge (host=%s)...", bootstrapHost)
	
	// Resolve TS key from current input
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
	return resp.FRPToken, nil
}

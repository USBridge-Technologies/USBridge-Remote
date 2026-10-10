package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"usbridge-client/internal/api"
)

// A USBridge KVM that hasn't been set up yet is reachable over its USB
// cable: it's 10.55.0.1 on a USB network adapter (NCM) it adds to this PC,
// and hands its master key to the first one asking within 30 minutes of
// powering on (usbridge's modules/setupnet). This is the client's side of
// that: "Over USB" in the Add Connection dialog.
const usbSetupClaimURL = "http://10.55.0.1/api/setup/claim"

type usbSetupClaim struct {
	MasterKey string     `json:"master_key"`
	Host      string     `json:"host"`
	LANAddrs  []string   `json:"lan_addrs"`
	APIPort   int        `json:"api_port"`
	Hostname  string     `json:"hostname"`
	Error     string     `json:"error"`
	ClaimedAt *time.Time `json:"claimed_at"`
}

var (
	// errUSBSetupBrowserHTTPS: a page loaded over https can't make the
	// plain-http request (mixed content).
	errUSBSetupBrowserHTTPS = errors.New("https page")
	errUSBSetupNotFound     = errors.New("no KVM over USB")
	errUSBSetupWindowClosed = errors.New("setup window closed")
)

// usbSetupClaimedError: the KVM was set up already (its key handed out).
type usbSetupClaimedError struct{ at *time.Time }

func (e usbSetupClaimedError) Error() string { return "already set up" }

// claimKVMOverUSB asks the KVM on the USB cable for its master key.
func claimKVMOverUSB() (usbSetupClaim, error) {
	var res usbSetupClaim
	if api.BrowserIsHTTPS() {
		return res, errUSBSetupBrowserHTTPS
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(usbSetupClaimURL)
	if err != nil {
		return res, errUSBSetupNotFound
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return res, fmt.Errorf("KVM answer: %w", err)
	}
	switch {
	case resp.StatusCode == http.StatusOK && res.MasterKey != "" && res.Host != "":
		return res, nil
	case res.Error == "claimed":
		return res, usbSetupClaimedError{at: res.ClaimedAt}
	case res.Error == "window_closed":
		return res, errUSBSetupWindowClosed
	default:
		return res, fmt.Errorf("KVM: %s (HTTP %d)", res.Error, resp.StatusCode)
	}
}

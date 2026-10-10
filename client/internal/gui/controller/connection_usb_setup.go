package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
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
	// errUSBSetupForbidden: the KVM answered 403 "forbidden" (plain text):
	// the request's origin isn't a page the KVM serves, or it didn't come
	// over the setup network.
	errUSBSetupForbidden = errors.New("forbidden")
)

// usbSetupClaimedError: the KVM was set up already (its key handed out).
type usbSetupClaimedError struct{ at *time.Time }

func (e usbSetupClaimedError) Error() string { return "already set up" }

// usbSetupRetryFor: how long "Over USB" keeps asking while the KVM isn't
// there yet. Right after a KVM powers on or reboots (an update) its USB
// network adapter and the setup server come up a few seconds apart: the
// first asks are refused or unanswered, and giving up at once made it look
// as if it took several tries.
const (
	usbSetupRetryFor   = 25 * time.Second
	usbSetupRetryEvery = 1500 * time.Millisecond
)

// claimKVMOverUSB asks the KVM on the USB cable for its master key; while it
// isn't reachable yet (not found, refused, an answer that isn't the setup
// server's) it asks again for usbSetupRetryFor.
func claimKVMOverUSB() (usbSetupClaim, error) {
	deadline := time.Now().Add(usbSetupRetryFor)
	for {
		res, err := claimKVMOverUSBOnce()
		if err == nil || !usbSetupRetryable(err) || time.Now().Add(usbSetupRetryEvery).After(deadline) {
			return res, err
		}
		time.Sleep(usbSetupRetryEvery)
	}
}

// usbSetupRetryable: the KVM may just not be up yet. Not an answer it gave
// on purpose (already set up, window closed) and not the https case.
func usbSetupRetryable(err error) bool {
	var claimed usbSetupClaimedError
	switch {
	case errors.Is(err, errUSBSetupBrowserHTTPS), errors.Is(err, errUSBSetupWindowClosed), errors.As(err, &claimed):
		return false
	}
	return true
}

func claimKVMOverUSBOnce() (usbSetupClaim, error) {
	var res usbSetupClaim
	if api.BrowserIsHTTPS() {
		return res, errUSBSetupBrowserHTTPS
	}
	client := &http.Client{Timeout: 5 * time.Second}
	// The KVM has no clock of its own until it's online: it takes ours (only
	// while its own is unset) so what it stamps from here on is right.
	resp, err := client.Get(usbSetupClaimURL + "?t=" + strconv.FormatInt(time.Now().Unix(), 10))
	if err != nil {
		return res, errUSBSetupNotFound
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if resp.StatusCode == http.StatusForbidden {
		return res, errUSBSetupForbidden
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return res, fmt.Errorf("KVM answer (HTTP %d): %q", resp.StatusCode, strings.TrimSpace(string(body)))
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

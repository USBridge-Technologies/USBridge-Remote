// Package account talks to usbridge-entitlement-backend's device-code login
// (see that repo's src/deviceAuth.ts) and the customer self-service
// endpoints it fronts (src/index.ts's handleCustomerLicenses/
// handleCustomerRebind, normally used by billing.usbridge.io/manage) to let
// this agent log in as a real USBridge account -- Google login, same
// identity as the web dashboard -- instead of the agent's own hardware-bound
// license/trial flow (internal/entitlement) knowing nothing about "who"
// bought it.
//
// Deliberately a SEPARATE login from internal/entitlement's hardware-bound
// trial/purchase tokens: those still gate whether RustShine runs on THIS
// machine and need no account at all (see that package's own doc comment on
// why hardware binding, not an account, is what stops a copied token from
// working elsewhere). This package only ever answers "which account is the
// human running this agent signed into, and which desktop licenses does
// that account own" -- what that's used for (see app.go's
// RebindLicenseToThisDevice) is picking one of THOSE licenses and handing
// its identifier to the existing, unchanged
// POST /v1/desktop-license/rebind-shaped flow
// (/manage/api/rebind, Bearer-authenticated instead of cookie-authenticated
// -- same backend logic either way).
//
// Device-code flow, mirroring GitHub CLI/Docker: StartLogin asks the
// backend for a one-time code and a URL to open in the system browser: the
// human logs in with Google there (a billing.usbridge.io page, not
// anything this binary serves), and Poll is called repeatedly until the
// backend reports that code claimed by an email, handing back a long-lived
// Bearer account token this package caches on disk (see tokenfile.go) and
// sends on every subsequent call.
package account

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// backendBaseURL mirrors internal/entitlement/pubkey.go's own var of the
// same name and same value -- kept as a separate copy (not imported across
// packages) for the same reason internal/entitlement's own doc comment
// gives for not sharing code with rust-shine/crates/license: this package
// stays self-contained rather than reaching into a sibling package's
// internals for one constant.
var backendBaseURL = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// TestSetBackendBaseURL mirrors entitlement.TestSetBackendBaseURL -- see
// that function's doc comment.
func TestSetBackendBaseURL(url string) string {
	prev := backendBaseURL
	backendBaseURL = url
	return prev
}

const httpTimeout = 10 * time.Second

func httpClient() *http.Client { return &http.Client{Timeout: httpTimeout} }

// LoginStart is StartLogin's result: open VerificationURL in the system
// browser, then poll Poll(ctx, Code) until it reports "complete".
type LoginStart struct {
	Code            string
	VerificationURL string
	ExpiresIn       int
}

// StartLogin asks the backend to mint a fresh device code (POST
// /v1/account/login/start) -- safe to call again if a previous attempt's
// code expired (10 minutes, see deviceAuth.ts's DEVICE_CODE_TTL_SECONDS)
// without the human ever finishing the browser step.
func StartLogin(ctx context.Context) (*LoginStart, error) {
	var out struct {
		Code            string `json:"code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/account/login/start", nil, "", &out); err != nil {
		return nil, err
	}
	return &LoginStart{Code: out.Code, VerificationURL: out.VerificationURL, ExpiresIn: out.ExpiresIn}, nil
}

// LoginPollResult is Poll's outcome for one poll tick.
type LoginPollResult struct {
	// Status is "pending", "expired", or "complete".
	Status string
	// Email/AccountToken are only set once Status == "complete".
	Email        string
	AccountToken string
	ExpiresIn    int
}

// Poll asks whether `code` (from StartLogin) has been claimed yet -- call
// on a short interval (the GUI uses 2s, same cadence as
// internal/app's existing pollForLicense) until Status is "complete" or
// "expired". Never blocks waiting for the human; each call is a single
// cheap GET.
func Poll(ctx context.Context, code string) (*LoginPollResult, error) {
	var out struct {
		Status       string `json:"status"`
		Email        string `json:"email"`
		AccountToken string `json:"account_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodGet, "/v1/account/login/poll?code="+code, nil, "", &out); err != nil {
		return nil, err
	}
	return &LoginPollResult{Status: out.Status, Email: out.Email, AccountToken: out.AccountToken, ExpiresIn: out.ExpiresIn}, nil
}

// Status is what the GUI needs to render the account login/rebind
// affordance in the License dialog -- mirrors entitlement.Status's own
// role for the hardware-bound trial/purchase flow, kept as a separate type
// since these two logins are independent (see this package's doc comment).
type Status struct {
	LoggedIn bool   `json:"logged_in"`
	Email    string `json:"email,omitempty"`
	// Licenses is this account's own desktop licenses (last successful
	// ListLicenses result) -- refreshed right after login completes and
	// again after a successful Rebind, not polled continuously.
	Licenses []License `json:"licenses,omitempty"`

	LoginInProgress  bool `json:"login_in_progress"`
	RebindInProgress bool `json:"rebind_in_progress"`

	LastError string `json:"last_error,omitempty"`
}

// License is one row from the account's desktop-license catalog -- same
// shape as billing.usbridge.io/manage's own license list (db.ts's
// LicenseRow, the fields this package's caller actually needs).
type License struct {
	Identifier string `json:"identifier"`
	Status     string `json:"status"` // "licensed" | "trial" | "trial_used" | "revoked"
	Tier       string `json:"tier"`
}

// ListLicenses fetches every desktop license belonging to the account
// identified by accountToken (GET /manage/api/licenses?kind=desktop,
// Bearer-authenticated -- see usbridge-entitlement-backend's
// currentCustomerFromRequest).
func ListLicenses(ctx context.Context, accountToken string) ([]License, error) {
	var out struct {
		Licenses []License `json:"licenses"`
	}
	if err := doJSON(ctx, http.MethodGet, "/manage/api/licenses?kind=desktop", nil, accountToken, &out); err != nil {
		return nil, err
	}
	return out.Licenses, nil
}

// Rebind moves a desktop license this account owns (oldIdentifier -- one of
// ListLicenses's own Identifier values) onto newHwID, this machine's own
// hardware id. Same backend operation billing.usbridge.io/manage's rebind
// button performs (POST /manage/api/rebind), just Bearer- instead of
// cookie-authenticated, and only ever a license already confirmed to
// belong to this account (see internal/app's RebindLicenseToThisDevice,
// which always calls ListLicenses first).
func Rebind(ctx context.Context, accountToken, oldIdentifier, newHwID string) error {
	reqBody, _ := json.Marshal(map[string]string{"identifier": oldIdentifier, "new_hw_id": newHwID})
	return doJSON(ctx, http.MethodPost, "/manage/api/rebind", reqBody, accountToken, nil)
}

func doJSON(ctx context.Context, method, path string, body []byte, bearer string, out any) error {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, backendBaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "usbridge-agent-account")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}

	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("account: request %s: %w", path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("account: %s: HTTP %d: %s", path, resp.StatusCode, truncate(respBody))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("account: parse response from %s: %w", path, err)
	}
	return nil
}

func truncate(b []byte) string {
	const max = 300
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + "..."
}

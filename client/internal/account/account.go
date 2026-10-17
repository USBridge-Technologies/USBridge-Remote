// Package account talks to usbridge-entitlement-backend's device-code login
// (see that repo's src/deviceAuth.ts) and the customer license listing it
// fronts (src/index.ts's handleCustomerLicenses, normally used by
// billing.usbridge.io/manage) to let the USBridge client show a "logged in
// as <email>, these are your licenses" account view.
//
// The client itself has no billing of its own -- nothing here gates any
// client feature. This is purely an identity/profile affordance: log in
// with the same Google account used at checkout, see which SBC and desktop
// licenses that account owns. See agent/internal/account (a near-identical
// package in the sibling module) for the same login also used to rebind a
// desktop license onto a machine -- something only the agent needs.
//
// Device-code flow, mirroring GitHub CLI/Docker: StartLogin asks the
// backend for a one-time code and a URL to open in the system browser: the
// human logs in with Google there (a billing.usbridge.io page, not
// anything this binary serves), and Poll is called repeatedly until the
// backend reports that code claimed by an email, handing back a long-lived
// Bearer account token this package's caller persists (see
// internal/models.AppConfig's AccountEmail/AccountToken fields) and sends
// on every subsequent call.
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

var backendBaseURL = "https://usbridge-entitlement.fatkulinamir80.workers.dev"

// TestSetBackendBaseURL points every call in this package at url (typically
// a local httptest.Server) for the duration of a test, and returns the
// previous value so the caller can restore it.
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
// /v1/account/login/start).
func StartLogin(ctx context.Context) (*LoginStart, error) {
	var out struct {
		Code            string `json:"code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/account/login/start", startBody(), "", &out); err != nil {
		return nil, err
	}
	return &LoginStart{Code: out.Code, VerificationURL: out.VerificationURL, ExpiresIn: out.ExpiresIn}, nil
}

// startBody is the (optional, best-effort) hardware summary sent with the
// login start request -- see DeviceInfo's doc comment. A marshal failure
// just means the request goes out without a body, same as before.
func startBody() []byte {
	b, err := json.Marshal(map[string]any{"device_info": CollectDeviceInfo()})
	if err != nil {
		return nil
	}
	return b
}

// LoginPollResult is Poll's outcome for one poll tick.
type LoginPollResult struct {
	// Status is "pending", "expired", or "complete".
	Status       string
	Email        string
	AccountToken string
	ExpiresIn    int
}

// Poll asks whether `code` (from StartLogin) has been claimed yet -- call
// on a short interval (2s, matching the agent's equivalent) until Status is
// "complete" or "expired".
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

// License is one row from the account's license catalog (either kind --
// see db.ts's LicenseRow for the full backend shape, this is only the
// fields the client's read-only account view needs).
type License struct {
	Kind       string `json:"kind"` // "desktop" | "sbc"
	Identifier string `json:"identifier"`
	Status     string `json:"status"` // "licensed" | "trial" | "trial_used" | "revoked"
	Tier       string `json:"tier"`
}

// ListLicenses fetches EVERY license (both SBC and desktop) belonging to
// the account identified by accountToken (GET /manage/api/licenses, no
// ?kind= filter -- see handleCustomerLicenses's doc comment on why the
// default is "both, merged").
func ListLicenses(ctx context.Context, accountToken string) ([]License, error) {
	var out struct {
		Licenses []License `json:"licenses"`
	}
	if err := doJSON(ctx, http.MethodGet, "/manage/api/licenses", nil, accountToken, &out); err != nil {
		return nil, err
	}
	return out.Licenses, nil
}

// DeleteAccount sends a deletion request for the account identified by accountToken.
// If active commercial licenses block deletion (HTTP 409), returns the server-provided message.
func DeleteAccount(ctx context.Context, accountToken string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, backendBaseURL+"/v1/account", nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "usbridge-client-account")
	if accountToken != "" {
		req.Header.Set("Authorization", "Bearer "+accountToken)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("account: request /v1/account: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error   string `json:"error"`
			Message string `json:"message"`
		}
		if json.Unmarshal(respBody, &errResp) == nil && errResp.Message != "" {
			return fmt.Errorf("%s", errResp.Message)
		}
		return fmt.Errorf("account: /v1/account: HTTP %d: %s", resp.StatusCode, truncate(respBody))
	}
	return nil
}

// ErrUnauthorized is what a 401 from the backend unwraps to: the account
// token is expired, forged, or past the refresh grace window.
var ErrUnauthorized = errors.New("account: not authenticated")

// TokenExpiry reads the exp field of a "<b64url(email)>.<exp>.<hmac>"
// account token (see the backend's googleOAuth.ts signSessionValue). It is
// only a hint for when to refresh -- the server still checks the signature.
// ok is false for anything that does not look like such a token.
func TokenExpiry(token string) (exp time.Time, ok bool) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || sec <= 0 {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// RefreshResult is RefreshToken's answer: a new token with a full TTL.
type RefreshResult struct {
	Email        string
	AccountToken string
	ExpiresIn    int
}

// RefreshToken trades accountToken for a fresh one (POST
// /v1/account/token/refresh). The backend also accepts a token that expired
// recently, so a client that sat closed for a while recovers without a new
// Google login. Returns an error wrapping ErrUnauthorized when it cannot.
func RefreshToken(ctx context.Context, accountToken string) (*RefreshResult, error) {
	var out struct {
		Email        string `json:"email"`
		AccountToken string `json:"account_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/account/token/refresh", nil, accountToken, &out); err != nil {
		return nil, err
	}
	if out.AccountToken == "" {
		return nil, fmt.Errorf("account: refresh returned no token")
	}
	return &RefreshResult{Email: out.Email, AccountToken: out.AccountToken, ExpiresIn: out.ExpiresIn}, nil
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
	req.Header.Set("User-Agent", "usbridge-client-account")
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
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("%w (%s)", ErrUnauthorized, path)
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

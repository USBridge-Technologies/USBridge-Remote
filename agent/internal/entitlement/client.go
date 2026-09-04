package entitlement

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strings"
	"time"
)

// httpTimeout bounds every call to the backend -- none of these are on the
// agent's startup path (see app.go's applyPreferredBackend, which only
// ever uses an already-cached, already-verified local token), so a slow or
// unreachable backend just makes a background check take a few seconds
// longer, never blocks the engine coming up.
const httpTimeout = 10 * time.Second

func httpClient() *http.Client { return &http.Client{Timeout: httpTimeout} }

// Platform reports this build's platform key exactly as
// usbridge-entitlement-backend's PLATFORM_ASSET map and rust-shine's
// release-gamestream-server.yml asset names use it -- "" for anything not
// covered (there's no RustShine build for that platform, e.g. Linux arm64
// or any non-Sunshine-supported OS).
func Platform() string {
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return "linux-x86_64"
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return "windows-x86_64"
	case runtime.GOOS == "darwin" && runtime.GOARCH == "arm64":
		return "macos-arm64"
	default:
		return ""
	}
}

// IssueResult is the outcome of asking the backend for a license/trial
// token bound to this machine's hwid.Get() value. Deliberately the same
// shape for both StartTrial and RefreshLicense (mirrors
// usbridge-entitlement-backend's own register/refresh being the same
// handler) -- see each function's own doc comment for how NotLicensed vs.
// TrialUsed differ in meaning.
type IssueResult struct {
	// NotLicensed: no purchase on record for this hardware id yet (or a
	// prior purchase was refunded) -- RefreshLicense's own "not an error"
	// outcome, expected for every install that hasn't bought a license.
	NotLicensed bool
	// TrialUsed: this hardware id's one-time 7-day trial window has
	// already been granted AND has now passed -- StartTrial's own "not an
	// error" outcome for a machine that already had its trial.
	TrialUsed bool
	Token     string
	ExpiresIn int // seconds
}

// StartTrial grants (or, if already granted and still inside its window,
// re-fetches) this machine's one-time 7-day trial -- safe to call on every
// app launch before a purchase: the backend's own KV record is what makes
// this a no-op past the first successful grant (see
// usbridge-entitlement-backend's desktopLicense.ts issueOrRefreshTrial),
// not anything client-side, so wiping local config and calling this again
// does NOT grant a second trial.
func StartTrial(ctx context.Context, hwID string) (*IssueResult, error) {
	reqBody, _ := json.Marshal(map[string]string{"hw_id": hwID})
	var raw struct {
		Status    string `json:"status"`
		Trial     string `json:"trial"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/desktop-license/trial-start", reqBody, "", &raw); err != nil {
		return nil, err
	}
	if raw.Status == "trial_used" {
		return &IssueResult{TrialUsed: true}, nil
	}
	return &IssueResult{Token: raw.Trial, ExpiresIn: raw.ExpiresIn}, nil
}

// RefreshLicense re-derives a fresh desktop-license token for this
// machine's hardware id if (and only if) the backend currently has it on
// record as licensed (i.e. a completed, not-since-refunded Stripe
// purchase) -- called both right after StartCheckoutURL's flow completes
// and on the periodic watchdog. No browser interaction, no stored
// credential to refresh unlike the old Patreon flow's provider refresh
// token -- hwID itself is the only correlating value, re-derived locally
// (hwid.Get()) on every call rather than persisted.
func RefreshLicense(ctx context.Context, hwID string) (*IssueResult, error) {
	reqBody, _ := json.Marshal(map[string]string{"hw_id": hwID})
	var raw struct {
		Status    string `json:"status"`
		License   string `json:"license"`
		ExpiresIn int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/desktop-license/refresh", reqBody, "", &raw); err != nil {
		return nil, err
	}
	if raw.Status == "not_licensed" {
		return &IssueResult{NotLicensed: true}, nil
	}
	return &IssueResult{Token: raw.License, ExpiresIn: raw.ExpiresIn}, nil
}

// StartCheckoutURL asks the backend for a Stripe Checkout Session URL tied
// to this machine's hardware id -- open it in the system browser (or an
// embedded webview; it's just an https:// URL either way). The backend's
// webhook marks hwID licensed the moment payment completes; RefreshLicense
// is what actually picks that up afterward -- there is no separate
// "purchase complete" callback into this process the way the old OAuth
// flow's poll loop had one; see app.go's pollForLicense for how the caller
// bridges that gap.
func StartCheckoutURL(ctx context.Context, hwID string) (string, error) {
	reqBody, _ := json.Marshal(map[string]string{"hw_id": hwID})
	var out struct {
		URL string `json:"url"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/desktop-billing/checkout", reqBody, "", &out); err != nil {
		return "", err
	}
	if out.URL == "" {
		return "", fmt.Errorf("entitlement: checkout response had no url")
	}
	return out.URL, nil
}

type DownloadInfo struct {
	URL       string `json:"download_url"`
	SHA256    string `json:"sha256"`
	Version   string `json:"version"`
	SizeBytes int64  `json:"size_bytes"`
}

// ResolveDownload asks the backend for a short-lived signed URL to the
// RustShine build for this platform -- only succeeds if entitlementToken
// still verifies as current and unexpired on the backend's own side too
// (this package's local Verify is necessary but not sufficient: the
// backend independently re-checks, since a token could be locally valid
// but for a platform/version combination it no longer wants to serve).
func ResolveDownload(ctx context.Context, entitlementToken, platform string) (*DownloadInfo, error) {
	var out DownloadInfo
	path := "/v1/download/rustshine?platform=" + url.QueryEscape(platform)
	if err := doJSON(ctx, http.MethodGet, path, nil, entitlementToken, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func newRequest(ctx context.Context, method, path string, body []byte) (*http.Request, error) {
	var reader io.Reader
	if body != nil {
		reader = strings.NewReader(string(body))
	}
	req, err := http.NewRequestWithContext(ctx, method, backendBaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "usbridge-agent-entitlement")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

func doJSON(ctx context.Context, method, path string, body []byte, bearer string, out any) error {
	req, err := newRequest(ctx, method, path, body)
	if err != nil {
		return err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("entitlement: request %s: %w", path, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("entitlement: %s: HTTP %d: %s", path, resp.StatusCode, truncate(respBody))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("entitlement: parse response from %s: %w", path, err)
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

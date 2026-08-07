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

type StartResult struct {
	State         string `json:"state"`
	AuthorizeURL  string `json:"authorize_url"`
	ExpiresInSecs int    `json:"expires_in"`
}

// StartOAuth begins a login attempt. Callers open AuthorizeURL in the
// system browser, then poll PollOAuth(State) until it resolves.
func StartOAuth(ctx context.Context) (*StartResult, error) {
	var out StartResult
	if err := doJSON(ctx, http.MethodPost, "/v1/oauth/start", nil, "", &out); err != nil {
		return nil, err
	}
	return &out, nil
}

type PollStatus string

const (
	PollPending  PollStatus = "pending"
	PollComplete PollStatus = "complete"
	PollError    PollStatus = "error"
	// PollExpired is synthesized locally (HTTP 404) -- the backend's own
	// oauthstate.ts drops the KV entry after STATE_TTL_SECONDS, so an
	// unknown state and an expired one are indistinguishable server-side;
	// both mean "start over."
	PollExpired PollStatus = "expired"
)

type PollResult struct {
	Status               PollStatus
	Reason               string // set when Status == PollError
	EntitlementToken     string // set when Status == PollComplete
	ProviderRefreshToken string // set when Status == PollComplete
	EntitlementExpiresIn int    // seconds, set when Status == PollComplete
}

// PollOAuth checks a login attempt's status. Call every ~2s (short-poll,
// not long-poll -- the backend runs on Cloudflare Workers, which has a
// bounded per-invocation CPU budget that makes an in-handler wait
// impractical) until it stops returning PollPending.
func PollOAuth(ctx context.Context, state string) (*PollResult, error) {
	req, err := newRequest(ctx, http.MethodGet, "/v1/oauth/poll?state="+url.QueryEscape(state), nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("entitlement: poll request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &PollResult{Status: PollExpired}, nil
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return nil, fmt.Errorf("entitlement: poll request: HTTP %d: %s", resp.StatusCode, truncate(body))
	}

	var raw struct {
		Status               string `json:"status"`
		Reason               string `json:"reason"`
		Entitlement          string `json:"entitlement"`
		ProviderRefreshToken string `json:"provider_refresh_token"`
		ExpiresIn            int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("entitlement: parse poll response: %w", err)
	}
	return &PollResult{
		Status:               PollStatus(raw.Status),
		Reason:               raw.Reason,
		EntitlementToken:     raw.Entitlement,
		ProviderRefreshToken: raw.ProviderRefreshToken,
		EntitlementExpiresIn: raw.ExpiresIn,
	}, nil
}

type RefreshResult struct {
	NotEntitled          bool
	Reason               string
	EntitlementToken     string
	ProviderRefreshToken string
	ExpiresIn            int
}

// RefreshEntitlement silently re-derives a fresh entitlement token from a
// previously-saved provider refresh token, with no browser interaction --
// this is what the agent's periodic watchdog and startup check both call.
// NotEntitled covers both "membership lapsed" and "refresh token itself
// was rejected" (revoked, expired) identically: either way, the caller
// must downgrade to Sunshine, not keep retrying the same dead token.
func RefreshEntitlement(ctx context.Context, providerRefreshToken string) (*RefreshResult, error) {
	reqBody, _ := json.Marshal(map[string]string{"provider_refresh_token": providerRefreshToken})
	var raw struct {
		Status               string `json:"status"`
		Reason               string `json:"reason"`
		Entitlement          string `json:"entitlement"`
		ProviderRefreshToken string `json:"provider_refresh_token"`
		ExpiresIn            int    `json:"expires_in"`
	}
	if err := doJSON(ctx, http.MethodPost, "/v1/entitlement/refresh", reqBody, "", &raw); err != nil {
		return nil, err
	}
	if raw.Status == "not_entitled" {
		return &RefreshResult{NotEntitled: true, Reason: raw.Reason}, nil
	}
	return &RefreshResult{
		EntitlementToken:     raw.Entitlement,
		ProviderRefreshToken: raw.ProviderRefreshToken,
		ExpiresIn:            raw.ExpiresIn,
	}, nil
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

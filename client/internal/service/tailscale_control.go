package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"sync"
)

// The user's own control server (a self-hosted Headscale) for this app's
// Tailscale node; "" = Tailscale's own. Guest nodes ignore it: they always
// join the USBridge-run guest network named in their ticket.

var (
	ownControlMu  sync.Mutex
	ownControlURL string
)

// OwnControlURL is the control server the user's own node registers with,
// "" for Tailscale.
func OwnControlURL() string {
	ownControlMu.Lock()
	defer ownControlMu.Unlock()
	return ownControlURL
}

// SetOwnControlURL sets it (normalized, see NormalizeControlURL); a running
// node keeps the old one until Reset.
func SetOwnControlURL(u string) {
	ownControlMu.Lock()
	ownControlURL = u
	ownControlMu.Unlock()
}

// NormalizeControlURL checks a user-entered control server address and
// returns the form tsnet takes: "" stays "", a bare host gets https://, a
// trailing slash goes.
func NormalizeControlURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", errors.New("enter the server's address, like https://headscale.example.com")
	}
	return strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/"), nil
}

// ownControlStateSubdir: each control server keeps its node identity in its
// own state directory, so switching to a Headscale and back resumes the
// earlier Tailscale login.
func ownControlStateSubdir(controlURL string) string {
	if controlURL == "" {
		return "tailscale"
	}
	sum := sha256.Sum256([]byte(strings.ToLower(controlURL)))
	return "tailscale-own-" + hex.EncodeToString(sum[:4])
}

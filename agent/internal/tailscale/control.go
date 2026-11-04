package tailscale

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"

	"github.com/sirupsen/logrus"
)

// NormalizeControlURL checks a user-entered control server address (a
// self-hosted Headscale) and returns it in the form tsnet takes: "" stays
// "" (Tailscale's own), a bare host gets https://, a trailing slash goes.
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

// controlStateSubdir keeps each control server's node identity in its own
// state directory, so switching to a Headscale and back to Tailscale
// resumes the earlier Tailscale login instead of replacing it.
func controlStateSubdir(controlURL string) string {
	if controlURL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(strings.ToLower(controlURL)))
	return "tailscale-own-" + hex.EncodeToString(sum[:4])
}

// ControlURL is the control server this node registers with, "" for
// Tailscale's own.
func (s *Service) ControlURL() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.controlURL
}

// SetControlURL points this (owner's) node at controlURL (already
// normalized, "" = Tailscale) and stops the running tsnet node, so the next
// Server() call starts one for the new server. Callers re-open whatever
// listened on the old node. Reports whether anything changed.
func (s *Service) SetControlURL(controlURL string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if controlURL == s.controlURL && s.subdir == controlStateSubdir(controlURL) {
		return false
	}
	if s.server != nil {
		if err := s.server.Close(); err != nil {
			logrus.Warnf("🛰️ [Tailscale] closing node for control server switch: %v", err)
		}
		s.server = nil
	}
	s.controlURL, s.subdir = controlURL, controlStateSubdir(controlURL)
	s.lastStartErr = nil
	logrus.Infof("🛰️ [Tailscale] control server: %s", displayControl(controlURL))
	return true
}

func displayControl(u string) string {
	if u == "" {
		return "Tailscale"
	}
	return u
}

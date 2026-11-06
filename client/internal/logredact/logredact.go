// Package logredact masks secrets in log lines before they reach app.log,
// which users attach to bug reports. Values keep their first and last 4
// characters (enough to tell two apart), the rest becomes "…". Public
// material -- certificates, public keys, disco/node keys -- is left alone.
//
// Installed once as a logrus hook (Install), so it covers every logger in
// the process, including tsnet's internal log lines routed through logrus.
package logredact

import (
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

// Mask keeps the first and last 4 characters of v.
func Mask(v string) string {
	if len(v) <= 12 {
		return "****"
	}
	return v[:4] + "…" + v[len(v)-4:]
}

type rule struct {
	re *regexp.Regexp
	// group is the submatch holding the secret value.
	group int
}

var rules = []rule{
	// Moonlight/GameStream pairing XML: the pairing secret and challenge
	// material are what a MITM needs; <plaincert> is public and stays.
	{regexp.MustCompile(`(?i)<(pairingsecret|challengeresponse|encodedcipher|serverchallengeresp|clientpairingsecret)>([^<]+)<`), 2},
	// Same values as URL query parameters (pair requests).
	{regexp.MustCompile(`(?i)[?&](salt|clientchallenge|serverchallengeresp|clientpairingsecret|pairingsecret|pin)=([^&\s"']+)`), 2},
	// Tailscale login links: whoever opens one can claim the node.
	{regexp.MustCompile(`(login\.tailscale\.com/a/)([A-Za-z0-9]+)`), 2},
	// Tailscale auth keys.
	{regexp.MustCompile(`(tskey-[A-Za-z0-9-]+)`), 1},
	// JWTs.
	{regexp.MustCompile(`(eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]+)`), 1},
	// key=value / "key": "value" for credential-looking names.
	{regexp.MustCompile(`(?i)["']?\b(access_token|refresh_token|id_token|token|auth_token|authkey|auth_key|api_key|apikey|master_key|secret|client_secret|password|passwd|private_key|privkey|otp)\b["']?\s*[:=]\s*["']?([^"'\s,&}\]]{4,})`), 2},
	// Authorization headers.
	{regexp.MustCompile(`(?i)\b(?:Bearer|Basic)\s+([A-Za-z0-9._~+/=-]{8,})`), 1},
}

var pemPrivate = regexp.MustCompile(`-----BEGIN ([A-Z ]*)PRIVATE KEY-----[\s\S]*?(-----END [A-Z ]*PRIVATE KEY-----|$)`)

// String returns s with every secret masked.
func String(s string) string {
	if s == "" {
		return s
	}
	s = pemPrivate.ReplaceAllString(s, "-----BEGIN ${1}PRIVATE KEY----- [redacted] -----END ${1}PRIVATE KEY-----")
	for _, r := range rules {
		idx := r.re.FindAllStringSubmatchIndex(s, -1)
		if idx == nil {
			continue
		}
		var out strings.Builder
		last := 0
		for _, m := range idx {
			va, vb := m[2*r.group], m[2*r.group+1]
			if va < 0 {
				continue
			}
			v := s[va:vb]
			// Already masked (a second pass, or "****"): leave it.
			if v == "****" || strings.HasPrefix(s[vb:], "…") || strings.Contains(v, "…") {
				continue
			}
			out.WriteString(s[last:va])
			out.WriteString(Mask(v))
			last = vb
		}
		out.WriteString(s[last:])
		s = out.String()
	}
	return s
}

type hook struct{}

func (hook) Levels() []logrus.Level { return logrus.AllLevels }

func (hook) Fire(e *logrus.Entry) error {
	e.Message = String(e.Message)
	for k, v := range e.Data {
		if s, ok := v.(string); ok {
			e.Data[k] = String(s)
		}
	}
	return nil
}

// Install adds the redaction hook to the standard logrus logger.
func Install() { logrus.AddHook(hook{}) }

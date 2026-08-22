//go:build js && wasm

package webrtcweb

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"syscall/js"
	"time"
)

// FetchStreamerName is a preflight probe against the agent's own
// /api/status (a route that exists on every backend, Sunshine or
// RustShine, since long before WebRTC was a thing) -- used purely so
// ConnectToMoonlight can tell "this agent doesn't speak WebRTC because
// it's running Sunshine" apart from an ordinary network/host-unreachable
// failure *before* trying (and retrying 20x) against rustshine's
// WebRTC-only /webrtc/offer endpoint, which a Sunshine-backed agent never
// exposes at all. Signed the same HMAC-SHA256 way as every other /api/*
// call this client makes (see WebRTCClient.signHMAC's doc comment) --
// duplicated here rather than shared because it targets the agent's own
// REST port (apiPort), not rustshine's separate WebRTC port a
// *WebRTCClient is constructed against.
func FetchStreamerName(apiHost string, apiPort int, masterKey string) (string, error) {
	const path = "/api/status"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	derived := sha256.Sum256([]byte(masterKey))
	mac := hmac.New(sha256.New, derived[:])
	mac.Write([]byte("GET" + path + ts))
	sig := hex.EncodeToString(mac.Sum(nil))

	headers := js.Global().Get("Object").New()
	headers.Set("X-Auth-Timestamp", ts)
	headers.Set("X-Auth-Signature", sig)
	opts := js.Global().Get("Object").New()
	opts.Set("method", "GET")
	opts.Set("headers", headers)

	url := fmt.Sprintf("http://%s:%d%s", apiHost, apiPort, path)
	fetchPromise := js.Global().Call("fetch", url, opts)
	respVal, err := awaitPromise(fetchPromise)
	if err != nil {
		return "", fmt.Errorf("fetch %s: %w", path, err)
	}
	textPromise := respVal.Call("text")
	textVal, err := awaitPromise(textPromise)
	if err != nil {
		return "", fmt.Errorf("reading response body: %w", err)
	}
	if !respVal.Get("ok").Bool() {
		return "", fmt.Errorf("agent returned HTTP %d: %s", respVal.Get("status").Int(), textVal.String())
	}

	var parsed struct {
		Data struct {
			Streamer string `json:"streamer"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(textVal.String()), &parsed); err != nil {
		return "", fmt.Errorf("decoding agent status response: %w", err)
	}
	return parsed.Data.Streamer, nil
}

// StreamerSupportsWebRTC reports whether name (as returned by
// FetchStreamerName, e.g. "RustShine (Proprietary)" or "Sunshine (Open
// Source)") is a backend that implements the WebRTC signaling endpoint
// this package's Connect/postOffer target. Only rustshine does -- upstream
// Sunshine has no WebRTC support at all, classic GameStream/Moonlight
// protocol only.
func StreamerSupportsWebRTC(name string) bool {
	return strings.Contains(strings.ToLower(name), "rustshine")
}

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"

	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/hwid"

	"github.com/gorilla/websocket"
)

// webrtcSignalRelayTierRecheckInterval bounds how often a not-currently-
// entitled (or relay-disabled) box re-checks whether it should start
// dialing the relay. Deliberately much longer than
// turnCredentialsRefreshInterval: unlike a TURN credential (which expires
// and must be refreshed even while still entitled), "should I even be
// trying to connect" rarely flips, so there's no reason to poll it as
// eagerly.
const webrtcSignalRelayTierRecheckInterval = 10 * time.Minute

// webrtcSignalRelayFreeGuestRecheck is how often a Free machine looks (locally)
// for a guest that would need the relay.
const webrtcSignalRelayFreeGuestRecheck = 15 * time.Second

// webrtcSignalRelayReconnectBackoff bounds how quickly a dropped or refused
// relay connection is retried -- long enough not to hammer the backend
// during a persistent outage, short enough that a transient network blip
// doesn't leave a remote browser session locked out for long.
const webrtcSignalRelayReconnectBackoff = 15 * time.Second

// rustshineWebRTCOfferURL is this agent's own local rustshine signaling
// endpoint -- the same fixed address api/server.go's webrtcProxy forwards a
// direct browser request to (see that function's doc comment); a relayed
// offer is replayed here identically, just sourced from the WebSocket
// instead of a direct HTTP request. A var, not a const, purely so tests can
// point it at an httptest.Server standing in for rustshine (same pattern as
// entitlement.TestSetBackendBaseURL) -- every production caller only ever
// sees the real address.
// relayOfferAuthorizer is api.Server.AuthorizeOffer, set once the API server exists.
var relayOfferAuthorizer func(method, uri string, h http.Header, body []byte) (int, string)

var rustshineWebRTCOfferURL = "http://127.0.0.1:8444/webrtc/offer"

// webrtcSignalRelayWatchdog maintains this agent's persistent WebSocket to
// usbridge-entitlement's WebRTC signaling relay (see usbridge-entitlement-
// backend's webrtcSignalRelay.ts and client/internal/webrtcweb/
// client_wasm.go's postOffer) -- pro/enterprise tier only, the same local
// pre-check tickTurnCredentials already does before ever touching the
// network. Lets a browser client with no LAN/Tailscale route to this agent
// still deliver its WebRTC "POST /webrtc/offer" signaling request; the
// actual media/data path is completely unaffected either way (still the
// real RTCPeerConnection once negotiated, this relay never carries it).
//
// A dedicated redial loop, not a plain ticker like turnCredentialsWatchdog:
// a WebSocket is either up (this function blocks inside
// dialAndServeSignalRelay for as long as it stays that way) or down (and
// needs a bounded-backoff reconnect attempt, not a wait for the next tick).
func (a *App) webrtcSignalRelayWatchdog(ctx context.Context) {
	for {
		if !a.rustshineStaged() || !a.cfg.WebRTCSignalRelayEnabledOK() {
			if !sleepOrDone(ctx, webrtcSignalRelayTierRecheckInterval) {
				return
			}
			continue
		}
		hwID, err := hwid.Get()
		if err != nil {
			if !sleepOrDone(ctx, webrtcSignalRelayTierRecheckInterval) {
				return
			}
			continue
		}
		claims, verifyErr := entitlement.VerifyForHardware(a.cfg.EntitlementToken, hwID)
		paid := verifyErr == nil && (claims.Tier == "pro" || claims.Tier == "enterprise")
		// A Free machine keeps the relay open for its one guest: the check is
		// local, so it is cheap to repeat often (an invitation shouldn't wait
		// ten minutes for the relay to come up).
		if !paid && !a.hasLiveGuestShare() {
			if !sleepOrDone(ctx, webrtcSignalRelayFreeGuestRecheck) {
				return
			}
			continue
		}

		if err := dialAndServeSignalRelay(ctx, hwID); err != nil {
			log.Printf("[app] webrtc signal relay: %v (will retry)", err)
		}
		if !sleepOrDone(ctx, webrtcSignalRelayReconnectBackoff) {
			return
		}
	}
}

// sleepOrDone waits for d or ctx cancellation, whichever comes first,
// returning false iff ctx cancellation was the reason it returned (the
// caller should stop looping in that case).
func sleepOrDone(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// dialAndServeSignalRelay opens one WebSocket to the relay and services
// incoming offers on it until the connection drops or ctx is cancelled --
// blocks for the connection's whole lifetime. writeMu serializes answer
// writes: gorilla/websocket.Conn forbids concurrent writers, and offers are
// handled concurrently (one goroutine per message) below.
func dialAndServeSignalRelay(ctx context.Context, hwID string) error {
	conn, err := entitlement.DialSignalRelay(ctx, hwID)
	if err != nil {
		return err
	}
	defer conn.Close()

	var writeMu sync.Mutex
	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		if ctx.Err() != nil {
			return nil
		}
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		msg := raw // capture for the goroutine below
		wg.Add(1)
		go func() {
			defer wg.Done()
			handleSignalRelayOffer(conn, &writeMu, msg)
		}()
	}
}

// handleSignalRelayOffer replays one relayed offer message onto this
// agent's local rustshine signaling port and writes the answer (or an
// error) back down conn, guarded by writeMu.
func handleSignalRelayOffer(conn *websocket.Conn, writeMu *sync.Mutex, raw []byte) {
	var msg struct {
		Type      string            `json:"type"`
		RequestID string            `json:"request_id"`
		Headers   map[string]string `json:"headers"`
	}
	if err := json.Unmarshal(raw, &msg); err != nil || msg.Type != "offer" || msg.RequestID == "" {
		return
	}

	var reply map[string]string
	sdp, err := replayOfferTo(rustshineWebRTCOfferURL, raw, msg.Headers)
	if err != nil {
		reply = map[string]string{"type": "error", "request_id": msg.RequestID, "message": err.Error()}
	} else {
		reply = map[string]string{"type": "answer", "request_id": msg.RequestID, "sdp": sdp}
	}
	replyBytes, err := json.Marshal(reply)
	if err != nil {
		return
	}

	writeMu.Lock()
	defer writeMu.Unlock()
	if err := conn.WriteMessage(websocket.TextMessage, replyBytes); err != nil {
		log.Printf("[app] webrtc signal relay: write answer: %v", err)
	}
}

// replayOfferTo re-POSTs raw -- the exact JSON bytes the relay message
// carried, "type"/"request_id"/"headers"/"hw_id" included, which rustshine's
// own OfferRequest (crates/webrtc-video/src/signaling.rs) simply ignores as
// unknown fields alongside the sdp/bitrate_kbps/hdr/codec ones it does read
// -- to targetURL (rustshineWebRTCOfferURL in production; a parameter
// purely so tests can point this at an httptest.Server standing in for
// rustshine's local port), carrying the same X-Auth-Timestamp/
// X-Auth-Signature headers the relay message wrapped. rustshine's
// verify_offer_auth is what actually authenticates this, completely
// unchanged from a direct /webrtc/offer call -- this function is a pure
// byte-for-byte replay, it never re-signs or re-derives anything.
func replayOfferTo(targetURL string, raw []byte, headers map[string]string) (string, error) {
	req, err := http.NewRequest(http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		if k != "" && v != "" {
			req.Header.Set(k, v)
		}
	}
	// A guest's offer is verified (and re-signed for rustshine) by the agent
	// here; an owner's passes through unchanged. See api.Server.AuthorizeOffer.
	if relayOfferAuthorizer != nil {
		if status, code := relayOfferAuthorizer("POST", "/webrtc/offer", req.Header, raw); status != 0 {
			return "", fmt.Errorf("%s", code)
		}
	}
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("rustshine offer HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out struct {
		SDP string `json:"sdp"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("parse rustshine answer: %w", err)
	}
	return out.SDP, nil
}

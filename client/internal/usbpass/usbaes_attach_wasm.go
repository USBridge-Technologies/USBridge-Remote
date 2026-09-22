//go:build js && wasm

package usbpass

// Browser-sourced USB/IP attach: the wasm counterpart of usbaes_attach.go's
// Attach(), for a synthetic gamepad whose input comes from the browser's
// Gamepad API instead of a real local device. Two things differ from the
// native flow, both because a browser tab can never accept an inbound TCP
// connection (no listen()/accept() in any web platform API) and has no
// working net.Dial either (see wsconn_wasm.go's doc comment):
//
//  1. There's no local device to describe or export -- the agent hosts the
//     loopback USB/IP Server itself (agent/internal/browserusb) and hands
//     back a bus id + loopback port via an ordinary authenticated HTTP POST
//     (postBrowserSession) instead of this code picking one.
//  2. The AES Hello/Attach handshake below is byte-for-byte the same
//     protocol usbaes_attach.go speaks (same attachPayload/encodeAttachFrame/
//     aeadStream, all pure Go -- see usbaes_protocol.go/usbaes_transport.go's
//     widened build tags), just carried over a platform.DialWebSocket
//     connection to the agent's own relay endpoint
//     (agent/internal/api/usb_passthrough_browser.go), which is what
//     actually reaches the usbridge-usb-broker's AES port on the agent's
//     behalf.
//
// Unlike native Attach(), the TunnelNonce here is *not* generated locally:
// the agent already generated one (and armed its own TunnelListener with the
// key derived from it) by the time postBrowserSession returns, since the
// agent -- not this browser tab -- is the one hosting the tunnel listener
// that protects the loopback exporter (see agent/internal/browserusb's doc
// comment for why a production agent's --tsnet-bridge makes that tunnel
// mandatory here, unlike the plain-loopback case native Attach() gets away
// with). This code just has to echo the same nonce back in its Attach frame
// so the broker derives the identical key agent already did.

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"usbridge-client/internal/platform"

	"github.com/sirupsen/logrus"
)

// calculateHMACV2 duplicates client/internal/api.CalculateHMACV2's exact
// algorithm (HMAC-SHA256 of METHOD+PATH+TIMESTAMP+BODY, keyed by SHA-256 of
// the shared secret) rather than importing that package: internal/api pulls
// in internal/localui (ONNX-backed local-UI OCR), which has no wasm build
// at all -- importing the package for one small pure function would drag a
// build-breaking dependency into the wasm binary. This same primitive is
// already independently reimplemented on the agent side too
// (agent/internal/api/security.go's CalculateHMAC) -- a third small,
// self-contained copy here follows that existing pattern rather than
// fighting it.
func calculateHMACV2(method, path, timestamp, body string, key []byte) string {
	h := sha256.Sum256(key)
	mac := hmac.New(sha256.New, h[:])
	mac.Write([]byte(method + path + timestamp + body))
	return hex.EncodeToString(mac.Sum(nil))
}

// BrowserGamepadAttachOptions is the wasm-side counterpart of AttachOptions.
type BrowserGamepadAttachOptions struct {
	// AgentBaseURL is the same base URL the rest of the web client's HTTP
	// API calls already use, e.g. "https://192.168.1.20:47990".
	AgentBaseURL string
	Secret       []byte
}

type browserSessionData struct {
	BusID         string `json:"bus_id"`
	ExportHost    string `json:"export_host"`
	ExportService string `json:"export_service"`
	TunnelNonce   string `json:"tunnel_nonce"` // hex-encoded; see AttachBrowserGamepad
}

type browserSessionEnvelope struct {
	Success bool               `json:"success"`
	Error   string             `json:"error"`
	Data    browserSessionData `json:"data"`
}

func postBrowserSession(opts BrowserGamepadAttachOptions) (browserSessionData, error) {
	const path = "/api/usb/passthrough/browser-session"
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := calculateHMACV2(http.MethodPost, path, ts, "", opts.Secret)

	req, err := http.NewRequest(http.MethodPost, opts.AgentBaseURL+path, nil)
	if err != nil {
		return browserSessionData{}, err
	}
	req.Header.Set("X-Auth-Signature", sig)
	req.Header.Set("X-Auth-Timestamp", ts)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return browserSessionData{}, fmt.Errorf("browser session request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return browserSessionData{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return browserSessionData{}, fmt.Errorf("browser session: HTTP %d: %s", resp.StatusCode, string(body))
	}
	var env browserSessionEnvelope
	if err := json.Unmarshal(body, &env); err != nil {
		return browserSessionData{}, fmt.Errorf("browser session: decode response: %w", err)
	}
	if !env.Success {
		return browserSessionData{}, fmt.Errorf("browser session: %s", env.Error)
	}
	return env.Data, nil
}

// browserRelayWSURL builds an authenticated ws(s):// URL for one of the
// agent's browser-relay endpoints -- signed via query params, not the usual
// X-Auth-Signature/X-Auth-Timestamp headers, because the browser's
// WebSocket constructor cannot set custom request headers at all (see
// agent/internal/api/usb_passthrough_browser.go's verifyWSAuth doc comment).
func browserRelayWSURL(agentBaseURL, path string, secret []byte) (string, error) {
	u, err := url.Parse(agentBaseURL)
	if err != nil {
		return "", err
	}
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	u.Path = path
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := calculateHMACV2(http.MethodGet, path, ts, "", secret)
	q := url.Values{}
	q.Set("ts", ts)
	q.Set("sig", sig)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// AttachBrowserGamepad asks the agent for a loopback USB/IP export of a
// synthetic Xbox 360 controller, performs the AES Hello/Attach handshake
// against it, and returns send (push one browserGamepadFrameLen-shaped
// state frame) and stop (detach and close both relay connections).
func AttachBrowserGamepad(opts BrowserGamepadAttachOptions) (send func([]byte), stop func(), err error) {
	session, err := postBrowserSession(opts)
	if err != nil {
		return nil, nil, err
	}

	attachURL, err := browserRelayWSURL(opts.AgentBaseURL, "/api/usb/passthrough/browser-attach", opts.Secret)
	if err != nil {
		return nil, nil, err
	}
	conn, err := platform.DialWebSocket(attachURL)
	if err != nil {
		return nil, nil, fmt.Errorf("browser attach relay: %w", err)
	}

	key := deriveSessionKey(opts.Secret)
	stream, err := newAeadStream(conn, key)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("aes session: %w", err)
	}

	if err := stream.sendFrame(encodeHelloFrame(usbAesRoleClient, usbAesProtoVersion)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello: %w", err)
	}
	ackRaw, err := stream.recvFrame()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello-ack: %w", err)
	}
	msgType, payload, err := decodeUsbpFrame(ackRaw)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello-ack: %w", err)
	}
	if msgType != msgHelloAck {
		conn.Close()
		return nil, nil, fmt.Errorf("expected hello-ack, got msg type %d", msgType)
	}
	ok, detail, err := decodeHelloAck(payload)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("hello-ack: %w", err)
	}
	if !ok {
		conn.Close()
		return nil, nil, fmt.Errorf("agent refused hello: %s", detail)
	}

	tunnelNonce, err := hex.DecodeString(session.TunnelNonce)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("tunnel nonce: %w", err)
	}

	attachFrame := attachPayload{
		BusID:         session.BusID,
		VID:           x360VID,
		PID:           x360PID,
		Speed:         3, // matches usbaes_attach.go's own placeholder value
		DeviceDesc:    x360DeviceDesc(),
		ConfigDesc:    x360ConfigDesc(),
		ExportHost:    session.ExportHost,
		ExportService: session.ExportService,
		TunnelNonce:   tunnelNonce,
	}
	if err := stream.sendFrame(encodeAttachFrame(attachFrame)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("attach: %w", err)
	}
	logrus.Infof("usbpass(wasm): browser gamepad attach sent bus=%s -> %s:%s", session.BusID, session.ExportHost, session.ExportService)

	// Mirrors usbaes_attach.go's holdAttachSession: loop until the agent
	// sends Detach/Reset or the connection drops. stop() below closes conn,
	// which unblocks a pending recvFrame() with an error the same way
	// closing a real net.Conn would.
	go func() {
		for {
			raw, err := stream.recvFrame()
			if err != nil {
				logrus.Infof("usbpass(wasm): browser attach session ended: %v", err)
				return
			}
			msgType, _, err := decodeUsbpFrame(raw)
			if err != nil {
				continue
			}
			if msgType == msgDetach || msgType == msgReset {
				return
			}
		}
	}()

	gpURL, err := browserRelayWSURL(opts.AgentBaseURL, "/api/usb/passthrough/browser-gamepad", opts.Secret)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}
	// bus_id rides the query string here too (unlike the attach relay,
	// where it's already inside the AES-encrypted Attach frame) since this
	// second, independent WebSocket is how the agent maps a gamepad-state
	// stream back to the right loopback session.
	gpURL += "&bus_id=" + url.QueryEscape(session.BusID)
	gpConn, err := platform.DialWebSocket(gpURL)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("browser gamepad relay: %w", err)
	}

	send = func(frame []byte) {
		_, _ = gpConn.Write(frame)
	}
	stop = func() {
		_ = stream.sendFrame(encodeDetachFrame(1))
		_ = conn.Close()
		_ = gpConn.Close()
	}
	return send, stop, nil
}

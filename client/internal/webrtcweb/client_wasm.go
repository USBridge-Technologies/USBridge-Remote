//go:build js && wasm

// Package service (wasm build): the browser-native counterpart to the
// desktop client's moonlight_cgo_*.go files, but for the WebRTC path
// instead of Moonlight/GameStream. There is no cgo under GOOS=js — the
// browser's own RTCPeerConnection/RTCDataChannel/fetch APIs are driven
// directly via syscall/js instead of a vendored C library, mirroring how
// the desktop build drives moonlight-common-c.
//
// Stage 1 of the web-client rollout (see the implementation plan) only
// needs signaling + a DataChannel round trip working end-to-end through a
// real browser, so WebRTCClient below intentionally does not implement the
// full VideoClient interface yet (video/audio tracks land in stage 2/3,
// wired into VideoClient once internal/gui itself is made wasm-buildable in
// stage 7) — it's a standalone, directly test-and-driveable building block.
package webrtcweb

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"syscall/js"
	"time"
)

// WebRTCClient drives a browser RTCPeerConnection against the agent's
// /api/webrtc/offer signaling endpoint. One instance per session.
type WebRTCClient struct {
	baseURL   string
	masterKey string

	pc *js.Value
	dc *js.Value

	mu          sync.Mutex
	onOpen      func()
	onMessage   func(data []byte)
	onStateChg  func(state string)
	closeCalled bool
}

// NewWebRTCClient creates a client bound to the agent at baseURL (e.g.
// "https://192.168.1.50:18080" or a Tailscale-IP equivalent — see the
// plan's note that the web client never speaks tsnet itself; the browser's
// host OS is expected to already be on the tailnet via a normal Tailscale
// client if that's the desired transport). masterKey is the hex secret
// obtained from the agent's QR code (see client/docs/api_endpoints.md) —
// this reimplements the same HMAC-SHA256 signature scheme the desktop
// client uses, byte for byte, so it authenticates against the exact same
// agent API without any protocol changes on the agent side.
func NewWebRTCClient(baseURL, masterKey string) *WebRTCClient {
	return &WebRTCClient{baseURL: baseURL, masterKey: masterKey}
}

// signHMAC reproduces agent/internal/api/security.go's CalculateHMAC:
// HMAC-SHA256(SHA256(masterKeyBytes), METHOD+PATH+TIMESTAMP+BODY). Pure
// crypto/* stdlib — compiles and runs identically under GOOS=js the same as
// any other platform, no browser SubtleCrypto involvement needed.
func (c *WebRTCClient) signHMAC(method, path, body string) (ts, sig string) {
	ts = strconv.FormatInt(time.Now().Unix(), 10)
	derived := sha256.Sum256([]byte(c.masterKey))
	mac := hmac.New(sha256.New, derived[:])
	mac.Write([]byte(method + path + ts + body))
	sig = hex.EncodeToString(mac.Sum(nil))
	return
}

// OnOpen registers a callback fired when the "input" DataChannel opens.
func (c *WebRTCClient) OnOpen(fn func()) { c.mu.Lock(); c.onOpen = fn; c.mu.Unlock() }

// OnMessage registers a callback fired for every DataChannel message.
func (c *WebRTCClient) OnMessage(fn func(data []byte)) { c.mu.Lock(); c.onMessage = fn; c.mu.Unlock() }

// OnStateChange registers a callback fired on RTCPeerConnection state
// transitions ("connecting", "connected", "failed", ...).
func (c *WebRTCClient) OnStateChange(fn func(state string)) {
	c.mu.Lock()
	c.onStateChg = fn
	c.mu.Unlock()
}

// Connect performs the full stage-1 handshake: create RTCPeerConnection,
// open the "input" DataChannel, create+set a local offer, wait for ICE
// gathering to finish (no trickle-ICE signaling channel yet — matches the
// agent's webrtcbridge.Bridge.Offer, which also gathers fully before
// answering), POST the offer to /api/webrtc/offer, and apply the answer.
func (c *WebRTCClient) Connect(sessionID string) error {
	rtcCtor := js.Global().Get("RTCPeerConnection")
	if rtcCtor.IsUndefined() {
		return fmt.Errorf("webrtc: RTCPeerConnection unavailable in this browser")
	}
	pc := rtcCtor.New(map[string]interface{}{})
	c.pc = &pc

	pc.Call("addEventListener", "connectionstatechange", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		state := pc.Get("connectionState").String()
		c.mu.Lock()
		cb := c.onStateChg
		c.mu.Unlock()
		if cb != nil {
			cb(state)
		}
		return nil
	}))

	dc := pc.Call("createDataChannel", "input")
	c.dc = &dc
	dc.Call("addEventListener", "open", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		c.mu.Lock()
		cb := c.onOpen
		c.mu.Unlock()
		if cb != nil {
			cb()
		}
		return nil
	}))
	dc.Call("addEventListener", "message", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		event := args[0]
		data := event.Get("data")
		c.mu.Lock()
		cb := c.onMessage
		c.mu.Unlock()
		if cb == nil {
			return nil
		}
		// DataChannel text frames arrive as JS strings here (the agent's
		// echo/ping-pong handler in stage 1 sends bytes that map to a text
		// frame); binary frames would arrive as an ArrayBuffer instead --
		// handle both so this doesn't silently drop messages once stage 4
		// starts sending binary input events.
		if data.Type() == js.TypeString {
			cb([]byte(data.String()))
		} else {
			cb(jsArrayBufferToBytes(data))
		}
		return nil
	}))

	offerPromise := pc.Call("createOffer")
	offerVal, err := awaitPromise(offerPromise)
	if err != nil {
		return fmt.Errorf("webrtc: createOffer: %w", err)
	}
	setLocalPromise := pc.Call("setLocalDescription", offerVal)
	if _, err := awaitPromise(setLocalPromise); err != nil {
		return fmt.Errorf("webrtc: setLocalDescription: %w", err)
	}

	if err := c.waitForICEGatheringComplete(pc); err != nil {
		return err
	}

	localSDP := pc.Get("localDescription").Get("sdp").String()

	answerSDP, err := c.postOffer(sessionID, localSDP)
	if err != nil {
		return err
	}

	answerDesc := js.Global().Get("Object").New()
	answerDesc.Set("type", "answer")
	answerDesc.Set("sdp", answerSDP)
	setRemotePromise := pc.Call("setRemoteDescription", answerDesc)
	if _, err := awaitPromise(setRemotePromise); err != nil {
		return fmt.Errorf("webrtc: setRemoteDescription: %w", err)
	}

	return nil
}

// waitForICEGatheringComplete polls iceGatheringState via an event
// listener + a channel — there's no direct promise for this in the WebRTC
// API, unlike pion's GatheringCompletePromise on the agent side.
func (c *WebRTCClient) waitForICEGatheringComplete(pc js.Value) error {
	if pc.Get("iceGatheringState").String() == "complete" {
		return nil
	}
	done := make(chan struct{})
	var listener js.Func
	listener = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if pc.Get("iceGatheringState").String() == "complete" {
			pc.Call("removeEventListener", "icegatheringstatechange", listener)
			listener.Release()
			close(done)
		}
		return nil
	})
	pc.Call("addEventListener", "icegatheringstatechange", listener)
	select {
	case <-done:
		return nil
	case <-time.After(15 * time.Second):
		return fmt.Errorf("webrtc: timed out waiting for ICE gathering to complete")
	}
}

// postOffer signs and POSTs the SDP offer to the agent's
// /api/webrtc/offer, exactly like the desktop client signs any other
// /api/* call (see client/docs/api_endpoints.md) — reuses the browser's
// fetch() rather than net/http, since GOOS=js has no real network stack of
// its own to run net/http's transport over.
func (c *WebRTCClient) postOffer(sessionID, offerSDP string) (string, error) {
	reqBody, err := json.Marshal(map[string]string{
		"session_id": sessionID,
		"sdp":        offerSDP,
	})
	if err != nil {
		return "", err
	}
	path := "/api/webrtc/offer"
	ts, sig := c.signHMAC("POST", path, string(reqBody))

	headers := js.Global().Get("Object").New()
	headers.Set("Content-Type", "application/json")
	headers.Set("X-Auth-Timestamp", ts)
	headers.Set("X-Auth-Signature", sig)

	opts := js.Global().Get("Object").New()
	opts.Set("method", "POST")
	opts.Set("headers", headers)
	opts.Set("body", string(reqBody))

	fetchPromise := js.Global().Call("fetch", c.baseURL+path, opts)
	respVal, err := awaitPromise(fetchPromise)
	if err != nil {
		return "", fmt.Errorf("webrtc: fetch /api/webrtc/offer: %w", err)
	}
	if !respVal.Get("ok").Bool() {
		return "", fmt.Errorf("webrtc: agent returned HTTP %d", respVal.Get("status").Int())
	}
	textPromise := respVal.Call("text")
	textVal, err := awaitPromise(textPromise)
	if err != nil {
		return "", fmt.Errorf("webrtc: reading response body: %w", err)
	}

	var parsed struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
		Data    struct {
			SDP string `json:"sdp"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(textVal.String()), &parsed); err != nil {
		return "", fmt.Errorf("webrtc: decoding agent response: %w", err)
	}
	if !parsed.Success {
		return "", fmt.Errorf("webrtc: agent rejected offer: %s", parsed.Error)
	}
	return parsed.Data.SDP, nil
}

// Send writes a message on the "input" DataChannel.
func (c *WebRTCClient) Send(data []byte) error {
	if c.dc == nil {
		return fmt.Errorf("webrtc: DataChannel not open")
	}
	c.dc.Call("send", string(data))
	return nil
}

// Close tears down the RTCPeerConnection.
func (c *WebRTCClient) Close() {
	c.mu.Lock()
	if c.closeCalled {
		c.mu.Unlock()
		return
	}
	c.closeCalled = true
	c.mu.Unlock()
	if c.dc != nil {
		c.dc.Call("close")
	}
	if c.pc != nil {
		c.pc.Call("close")
	}
}

// awaitPromise blocks the calling goroutine until a JS Promise settles,
// translating resolve/reject into a normal (value, error) pair. Safe to
// call from any goroutine since Go's wasm scheduler is cooperative and
// js.FuncOf callbacks always run on the same underlying JS event loop
// thread — the channel handoff below just bridges that callback-style API
// into blocking Go code.
func awaitPromise(promise js.Value) (js.Value, error) {
	resultCh := make(chan js.Value, 1)
	errCh := make(chan error, 1)

	var thenFunc, catchFunc js.Func
	thenFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer thenFunc.Release()
		defer catchFunc.Release()
		if len(args) > 0 {
			resultCh <- args[0]
		} else {
			resultCh <- js.Undefined()
		}
		return nil
	})
	catchFunc = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		defer thenFunc.Release()
		defer catchFunc.Release()
		msg := "promise rejected"
		if len(args) > 0 {
			msg = args[0].Get("message").String()
			if msg == "" {
				msg = fmt.Sprint(args[0])
			}
		}
		errCh <- fmt.Errorf("%s", msg)
		return nil
	})
	promise.Call("then", thenFunc).Call("catch", catchFunc)

	select {
	case v := <-resultCh:
		return v, nil
	case err := <-errCh:
		return js.Value{}, err
	case <-time.After(30 * time.Second):
		return js.Value{}, fmt.Errorf("timed out waiting for JS promise")
	}
}

// jsArrayBufferToBytes copies a JS ArrayBuffer into a Go byte slice.
func jsArrayBufferToBytes(arrayBuffer js.Value) []byte {
	uint8Array := js.Global().Get("Uint8Array").New(arrayBuffer)
	buf := make([]byte, uint8Array.Get("length").Int())
	js.CopyBytesToGo(buf, uint8Array)
	return buf
}

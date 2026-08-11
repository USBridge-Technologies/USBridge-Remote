//go:build js && wasm

// Package service (wasm build): the browser-native counterpart to the
// desktop client's moonlight_cgo_*.go files, but for the WebRTC path
// instead of Moonlight/GameStream. There is no cgo under GOOS=js — the
// browser's own RTCPeerConnection/RTCDataChannel/fetch APIs are driven
// directly via syscall/js instead of a vendored C library, mirroring how
// the desktop build drives moonlight-common-c.
//
// WebRTCClient itself is signaling + media plumbing only, not a
// service.VideoClient -- see webrtc_video_client_wasm.go for the adapter
// that wires this into the same VideoClient interface every other platform
// implements, so internal/gui's VideoWidget needs no wasm-specific code of
// its own.
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

// WebRTCClient drives a browser RTCPeerConnection against rustshine's own
// native WebRTC signaling endpoint (POST /webrtc/offer, on rustshine's own
// port -- see ConnectToMoonlight's doc comment), NOT the agent's /api/*
// surface. One instance per session.
type WebRTCClient struct {
	baseURL string
	// masterKey/signHMAC are currently unused: rustshine's signaling
	// endpoint doesn't authenticate requests at all yet (see
	// postOffer's doc comment) -- kept in case that changes, rather than
	// ripping out working HMAC plumbing that's identical to every other
	// platform's /api/* auth.
	masterKey string

	pc      *js.Value
	dc      *js.Value
	videoEl js.Value // hidden <video>, srcObject set from the video ontrack event

	mu           sync.Mutex
	onOpen       func()
	onMessage    func(data []byte)
	onStateChg   func(state string)
	onVideoTrack func()
	closeCalled  bool

	frameStop chan struct{}
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

// OnVideoTrack registers a callback fired once the browser's video
// <video>/track element is ready to read frames from (i.e. once the remote
// video track has actually arrived and been wired up) — the signal to
// start calling StartFrameCapture.
func (c *WebRTCClient) OnVideoTrack(fn func()) {
	c.mu.Lock()
	c.onVideoTrack = fn
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

	// recvonly video+audio transceivers: this client only ever receives
	// media from the agent (Sunshine's own capture), never sends any --
	// matching the webrtcbridge.Bridge.addMediaTracks on the agent side,
	// which expects these m-lines to already exist in the offer (JSEP: an
	// answer can't add new m-lines the offer didn't have).
	pc.Call("addTransceiver", "video", map[string]interface{}{"direction": "recvonly"})
	pc.Call("addTransceiver", "audio", map[string]interface{}{"direction": "recvonly"})

	doc := js.Global().Get("document")
	videoEl := doc.Call("createElement", "video")
	videoEl.Set("autoplay", true)
	videoEl.Set("muted", true) // audio plays through a separate <audio> element below; browsers block autoplay with sound without a user gesture, but always allow it muted
	videoEl.Set("playsInline", true)
	videoEl.Set("id", "usbridge-video-overlay")
	style := videoEl.Get("style")
	// position:fixed + an explicit z-index makes this a *positioned* element,
	// which per normal CSS stacking rules always paints above Fyne's own
	// wasm <canvas> (a plain, non-positioned element with implicit z-index
	// "auto") regardless of DOM append order -- no need to fight over
	// whether the canvas is transparent or opaque underneath. Sits below
	// the touch-overlay div (z-index 10, video_gestures_wasm.go) and the
	// virtual-cursor dot (z-index 11, video_widget_cursor_wasm.go), which
	// both need to stay clickable/visible above the actual video pixels.
	// object-fit:fill + an exact-letterboxed-size box (set by
	// video_widget_dom_overlay_wasm.go's syncVideoOverlay, mirroring the
	// same scale-to-fit math VideoWidget already does for every other
	// platform) means this never needs its own aspect-ratio logic -- the
	// box handed to it is already the right shape.
	style.Set("position", "fixed")
	style.Set("left", "0px")
	style.Set("top", "0px")
	style.Set("width", "0px")
	style.Set("height", "0px")
	style.Set("zIndex", "5")
	style.Set("objectFit", "fill")
	style.Set("pointerEvents", "none")
	style.Set("visibility", "hidden")
	style.Set("background", "#000")
	doc.Get("body").Call("appendChild", videoEl)
	c.videoEl = videoEl

	audioEl := doc.Call("createElement", "audio")
	audioEl.Set("autoplay", true)
	doc.Get("body").Call("appendChild", audioEl)

	pc.Call("addEventListener", "track", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		event := args[0]
		track := event.Get("track")
		streams := event.Get("streams")
		var stream js.Value
		if streams.Get("length").Int() > 0 {
			stream = streams.Index(0)
		} else {
			stream = js.Global().Get("MediaStream").New([]interface{}{track})
		}
		if track.Get("kind").String() == "video" {
			videoEl.Set("srcObject", stream)
			playPromise := videoEl.Call("play")
			if !playPromise.IsUndefined() {
				playPromise.Call("catch", js.FuncOf(func(this js.Value, args []js.Value) interface{} { return nil }))
			}
			c.mu.Lock()
			cb := c.onVideoTrack
			c.mu.Unlock()
			if cb != nil {
				cb()
			}
		} else {
			audioEl.Set("srcObject", stream)
			playPromise := audioEl.Call("play")
			if !playPromise.IsUndefined() {
				playPromise.Call("catch", js.FuncOf(func(this js.Value, args []js.Value) interface{} { return nil }))
			}
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

// postOffer POSTs the SDP offer straight to rustshine's own native WebRTC
// signaling endpoint (crates/webrtc-video/src/signaling.rs's
// POST /webrtc/offer) — NOT the agent's /api/* surface: rustshine's
// webrtc-video crate is a separate, additional server surface alongside
// its classic Sunshine/GameStream-compatible protocol, reachable directly
// on its own port (c.baseURL already points there, see
// WebRTCVideoClient.ConnectToMoonlight's doc comment on how that base URL
// is built), not proxied through the agent's HMAC-signed /api/* scheme at
// all. Reuses the browser's fetch() rather than net/http, since GOOS=js
// has no real network stack of its own to run net/http's transport over.
//
// Deliberately NOT signHMAC'd (unlike every /api/* call the desktop client
// makes) -- rustshine's signaling endpoint doesn't check for or expect
// that scheme; it authenticates nothing yet (tracked as a known gap, see
// this crate's own doc comments). The request/response shapes here match
// rustshine's actual wire format exactly: {"sdp": "..."} in both
// directions, no success/error envelope -- a prior version of this file
// assumed the agent's own envelope shape ({"success", "error", "data":
// {"sdp"}}), which silently failed against rustshine's flat response
// (parsed.Success stayed false, masking a perfectly good SDP answer as a
// rejected offer) once the client was pointed at rustshine directly.
func (c *WebRTCClient) postOffer(sessionID, offerSDP string) (string, error) {
	_ = sessionID // rustshine's endpoint doesn't take a session id -- one PeerConnection per POST, matching its own signaling.rs
	reqBody, err := json.Marshal(map[string]string{
		"sdp": offerSDP,
	})
	if err != nil {
		return "", err
	}
	path := "/webrtc/offer"

	headers := js.Global().Get("Object").New()
	headers.Set("Content-Type", "application/json")

	opts := js.Global().Get("Object").New()
	opts.Set("method", "POST")
	opts.Set("headers", headers)
	opts.Set("body", string(reqBody))

	fetchPromise := js.Global().Call("fetch", c.baseURL+path, opts)
	respVal, err := awaitPromise(fetchPromise)
	if err != nil {
		return "", fmt.Errorf("webrtc: fetch /webrtc/offer: %w", err)
	}
	textPromise := respVal.Call("text")
	textVal, err := awaitPromise(textPromise)
	if err != nil {
		return "", fmt.Errorf("webrtc: reading response body: %w", err)
	}
	if !respVal.Get("ok").Bool() {
		return "", fmt.Errorf("webrtc: rustshine returned HTTP %d: %s", respVal.Get("status").Int(), textVal.String())
	}

	var parsed struct {
		SDP string `json:"sdp"`
	}
	if err := json.Unmarshal([]byte(textVal.String()), &parsed); err != nil {
		return "", fmt.Errorf("webrtc: decoding rustshine response: %w", err)
	}
	if parsed.SDP == "" {
		return "", fmt.Errorf("webrtc: rustshine response had no sdp")
	}
	return parsed.SDP, nil
}

// VideoElement returns the underlying <video> DOM element, so the gui
// controller layer (video_widget_dom_overlay_wasm.go) can position/size it
// directly as a CSS overlay and read its videoWidth/videoHeight -- both
// free JS property reads, no pixel readback involved. Returns the zero
// js.Value before Connect has run.
func (c *WebRTCClient) VideoElement() js.Value {
	return c.videoEl
}

// WatchVideoFrames drives onFrame once per real presented video frame
// using HTMLVideoElement.requestVideoFrameCallback -- a browser API built
// exactly for this (Chrome/Edge 79+, Safari 15.4+): it fires in step with
// the decoder/compositor's own timing, carries no pixel payload, and costs
// nothing beyond a JS→Go callback hop, unlike the drawImage+getImageData
// readback StartFrameCapture below does. Falls back to a plain 30Hz
// setInterval poll on engines that don't support it (older
// Firefox/WebKit); either way, onFrame is only ever called to drive
// VideoWidget's existing frame-arrival bookkeeping (frame counters, FPS,
// IsStreaming freshness) with a nil image.Image, the same convention every
// native GPU-overlay platform (Android/Metal) already uses -- see
// handleVideoFrame's doc comment. Returns a stop function.
func (c *WebRTCClient) WatchVideoFrames(onFrame func()) func() {
	if c.videoEl.IsUndefined() || c.videoEl.IsNull() {
		return func() {}
	}
	stopped := false
	var stopMu sync.Mutex
	isStopped := func() bool {
		stopMu.Lock()
		defer stopMu.Unlock()
		return stopped
	}

	if rvfc := c.videoEl.Get("requestVideoFrameCallback"); !rvfc.IsUndefined() {
		var tick js.Func
		tick = js.FuncOf(func(this js.Value, args []js.Value) interface{} {
			if isStopped() {
				return nil
			}
			onFrame()
			c.videoEl.Call("requestVideoFrameCallback", tick)
			return nil
		})
		c.videoEl.Call("requestVideoFrameCallback", tick)
		return func() {
			stopMu.Lock()
			stopped = true
			stopMu.Unlock()
			tick.Release()
		}
	}

	// Fallback path: no requestVideoFrameCallback support.
	handle := js.Global().Call("setInterval", js.FuncOf(func(this js.Value, args []js.Value) interface{} {
		if isStopped() {
			return nil
		}
		onFrame()
		return nil
	}), 1000/30)
	return func() {
		stopMu.Lock()
		stopped = true
		stopMu.Unlock()
		js.Global().Call("clearInterval", handle)
	}
}

// StartFrameCapture begins pulling decoded video frames off the hidden
// <video> element and delivering them to onFrame as image.Image, at
// roughly targetFPS. This is the "software rendering" path VideoWidget
// already knows how to draw (see service.VideoClient.SetOnFrameReceived) --
// the browser does the actual H.264/VP8/etc. decode natively inside the
// <video> element (hardware-accelerated where available), this just reads
// the already-decoded RGBA pixels back into Go via an offscreen <canvas>,
// the same drawImage+getImageData technique
// qr_camera_scanner_wasm.go uses for its camera preview. This has real CPU
// cost (a full-frame pixel readback every tick) compared to a native
// GPU-composited video overlay, but works today without needing to solve
// DOM-canvas z-index compositing with Fyne's own canvas -- see the
// implementation plan's note on this being the higher-risk piece deferred
// to a later pass.
func (c *WebRTCClient) StartFrameCapture(targetFPS int, onFrame func(w, h int, rgba []byte)) {
	if c.videoEl.IsUndefined() || c.videoEl.IsNull() {
		return
	}
	c.mu.Lock()
	if c.frameStop != nil {
		c.mu.Unlock()
		return // already capturing
	}
	stop := make(chan struct{})
	c.frameStop = stop
	c.mu.Unlock()

	go func() {
		doc := js.Global().Get("document")
		var canvasEl, ctx2d js.Value
		var w, h int

		interval := time.Second / time.Duration(targetFPS)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}

			vw := c.videoEl.Get("videoWidth").Int()
			vh := c.videoEl.Get("videoHeight").Int()
			if vw == 0 || vh == 0 {
				continue // no frame decoded yet
			}
			if vw != w || vh != h {
				w, h = vw, vh
				canvasEl = doc.Call("createElement", "canvas")
				canvasEl.Set("width", w)
				canvasEl.Set("height", h)
				ctx2d = canvasEl.Call("getContext", "2d")
			}

			ctx2d.Call("drawImage", c.videoEl, 0, 0, w, h)
			imageData := ctx2d.Call("getImageData", 0, 0, w, h)
			jsPixels := imageData.Get("data")

			buf := make([]byte, w*h*4)
			js.CopyBytesToGo(buf, jsPixels)
			onFrame(w, h, buf)
		}
	}()
}

// StopFrameCapture stops the goroutine started by StartFrameCapture, if any.
func (c *WebRTCClient) StopFrameCapture() {
	c.mu.Lock()
	stop := c.frameStop
	c.frameStop = nil
	c.mu.Unlock()
	if stop != nil {
		close(stop)
	}
}

// Send writes a text message on the "input" DataChannel -- fine for JSON,
// but NOT for the raw binary NV_INPUT_HEADER-prefixed packets
// input_wasm.go builds (see SendBinary): a Go string is decoded as UTF-8
// on the way into a JS string by syscall/js's own marshaling, which
// silently mangles arbitrary non-UTF8-valid byte sequences -- exactly what
// a binary protocol full of raw magic/keycode/coordinate bytes is.
func (c *WebRTCClient) Send(data []byte) error {
	if c.dc == nil {
		return fmt.Errorf("webrtc: DataChannel not open")
	}
	c.dc.Call("send", string(data))
	return nil
}

// SendBinary writes a binary message on the "input" DataChannel as a real
// JS Uint8Array (RTCDataChannel.send(ArrayBufferView)), not a JS string --
// see Send's doc comment for why that distinction matters for raw input
// packet bytes.
func (c *WebRTCClient) SendBinary(data []byte) error {
	if c.dc == nil {
		return fmt.Errorf("webrtc: DataChannel not open")
	}
	array := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(array, data)
	c.dc.Call("send", array)
	return nil
}

// Close tears down the RTCPeerConnection.
func (c *WebRTCClient) Close() {
	c.StopFrameCapture()
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
	if !c.videoEl.IsUndefined() && !c.videoEl.IsNull() {
		c.videoEl.Set("srcObject", js.Null())
		if parent := c.videoEl.Get("parentNode"); !parent.IsNull() && !parent.IsUndefined() {
			parent.Call("removeChild", c.videoEl)
		}
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

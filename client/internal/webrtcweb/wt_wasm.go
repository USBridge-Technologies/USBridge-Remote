//go:build js && wasm

package webrtcweb

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"syscall/js"
	"time"
)

// WebTransportClient streams from rust-shine without WebRTC: WebTransport
// (HTTP/3 over QUIC) to its /webtransport endpoint, decoded with WebCodecs
// (see rust-shine's crates/webrtc-video/src/webtransport.rs for the
// protocol, and wt_player.js for the per-frame half, which runs in JS). On a
// small board this is several times cheaper than SRTP: QUIC runs
// ChaCha20-Poly1305 where the core has no AES instructions, and a whole
// access unit is one stream write instead of an RTP packetizer.
//
// The same surface WebRTCClient offers service.WebRTCVideoClient: decoded
// frames reach the same kind of hidden <video> (through a
// MediaStreamTrackGenerator), input and the named channels (USB
// passthrough, clipboard, api-tunnel) ride the same connection. One
// instance per session.
type WebTransportClient struct {
	baseURL   string
	masterKey string

	bitrateKbps   int
	fps           int
	videoCodec    string
	displayCursor *bool
	// codec: what the welcome says the host streams ("h264"/"h265").
	codec string

	session js.Value
	videoEl js.Value
	audioEl js.Value
	onState js.Func

	mu           sync.Mutex
	onStateChg   func(state string)
	onVideoTrack func()
	closeCalled  bool
}

//go:embed wt_player.js
var wtPlayerJS string

var wtPlayerOnce sync.Once

func wtPlayer() js.Value {
	wtPlayerOnce.Do(func() {
		if js.Global().Get("__usbridgeWT").IsUndefined() {
			js.Global().Get("Function").New(wtPlayerJS).Invoke()
		}
	})
	return js.Global().Get("__usbridgeWT")
}

// WebTransportSupported reports whether this page can run the WebTransport
// path at all: a secure context with WebTransport, WebCodecs and
// MediaStreamTrackGenerator (Chromium today; Firefox and Safari use WebRTC).
func WebTransportSupported() bool {
	p := wtPlayer()
	return !p.IsUndefined() && p.Call("supported").Bool()
}

// ErrWebTransportUnavailable: the streamer has no WebTransport endpoint
// (an older rust-shine, or one built without it).
var ErrWebTransportUnavailable = errors.New("webtransport: streamer has no WebTransport endpoint")

// NewWebTransportClient binds a client to the same baseURL and master key
// WebRTCClient's /webrtc/offer uses.
func NewWebTransportClient(baseURL, masterKey string) *WebTransportClient {
	return &WebTransportClient{baseURL: baseURL, masterKey: masterKey}
}

// SetBitrateKbps, SetFPS, SetDisplayCursor: this session's hello; call
// before Connect. 0 leaves the server's own default.
func (c *WebTransportClient) SetBitrateKbps(kbps int) { c.bitrateKbps = kbps }
func (c *WebTransportClient) SetFPS(fps int)          { c.fps = fps }

// SetVideoCodec: the codec picked in the video settings
// (models.VideoModeH264/H265). H.265 is asked for only when this browser
// can decode it with WebCodecs; otherwise the stream is H.264.
func (c *WebTransportClient) SetVideoCodec(codec string) { c.videoCodec = codec }
func (c *WebTransportClient) SetDisplayCursor(show bool) {
	c.displayCursor = &show
}

func (c *WebTransportClient) OnStateChange(fn func(state string)) {
	c.mu.Lock()
	c.onStateChg = fn
	c.mu.Unlock()
}

func (c *WebTransportClient) OnVideoTrack(fn func()) {
	c.mu.Lock()
	c.onVideoTrack = fn
	c.mu.Unlock()
}

type wtInfo struct {
	Port     int    `json:"port"`
	CertHash string `json:"cert_hash"`
	Path     string `json:"path"`
}

// connectTimeout bounds the whole attempt (info, QUIC handshake, hello),
// so an unreachable UDP port falls back to WebRTC quickly.
const wtConnectTimeout = 5 * time.Second

// Connect looks up the endpoint (signed GET /webtransport/info), opens the
// session and starts playback. sessionID is unused (one session per
// connection, like /webrtc/offer).
func (c *WebTransportClient) Connect(sessionID string) error {
	_ = sessionID
	if !WebTransportSupported() {
		return errors.New("webtransport: not supported by this browser/page")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("webtransport: base URL: %w", err)
	}

	const infoPath = "/webtransport/info"
	ts, sig := signHMAC(c.masterKey, "GET", infoPath, "")
	body, err := doFetch("GET", c.baseURL+infoPath, nil, map[string]string{"X-Auth-Timestamp": ts, "X-Auth-Signature": sig})
	if err != nil {
		var httpErr *offerHTTPError
		if errors.As(err, &httpErr) && (httpErr.Status == 404 || httpErr.Status == 503) {
			return ErrWebTransportUnavailable
		}
		return fmt.Errorf("webtransport: %s: %w", infoPath, err)
	}
	var info wtInfo
	if err := json.Unmarshal(body, &info); err != nil || info.Port == 0 || info.CertHash == "" || info.Path == "" {
		return fmt.Errorf("webtransport: bad %s answer: %q", infoPath, strings.TrimSpace(string(body)))
	}

	host := base.Hostname()
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	ts, sig = signHMAC(c.masterKey, "CONNECT", info.Path, "")
	sessionURL := fmt.Sprintf("https://%s:%d%s?ts=%s&sig=%s", host, info.Port, info.Path, ts, sig)

	hello := map[string]any{}
	if c.bitrateKbps > 0 {
		hello["bitrate_kbps"] = c.bitrateKbps
	}
	if c.fps > 0 {
		hello["fps"] = c.fps
	}
	if c.displayCursor != nil {
		hello["display_cursor"] = *c.displayCursor
	}
	hello["codec"] = "h264"
	if strings.EqualFold(c.videoCodec, "h265") || strings.EqualFold(c.videoCodec, "hevc") {
		if ok, err := awaitPromise(wtPlayer().Call("hevcSupported")); err == nil && ok.Truthy() {
			hello["codec"] = "h265"
		} else {
			js.Global().Get("console").Call("warn", "[webrtc-video] H.265 picked, but this browser can't decode HEVC with WebCodecs -- streaming H.264")
		}
	}

	c.videoEl, c.audioEl = newMediaElements()
	// A worker-painted <canvas> in the <video>'s place where the browser
	// can (see wt_player.js videoSurface): decoding off the page's thread.
	c.videoEl = wtPlayer().Call("videoSurface", c.videoEl)
	c.onState = js.FuncOf(func(this js.Value, args []js.Value) any {
		state := args[0].String()
		c.mu.Lock()
		cb := c.onStateChg
		closed := c.closeCalled
		c.mu.Unlock()
		if cb != nil && !closed {
			// Not inline: this runs inside a JS callback, and the state
			// handlers take locks and queue UI work.
			go cb(state)
		}
		return nil
	})
	opts := map[string]any{
		"url":      sessionURL,
		"certHash": info.CertHash,
		"hello":    hello,
		"videoEl":  c.videoEl,
		"audioEl":  c.audioEl,
		"onState":  c.onState,
	}
	session, err := awaitPromise(wtPlayer().Call("open", opts, wtConnectTimeout.Milliseconds()))
	if err != nil {
		c.release()
		return fmt.Errorf("webtransport: connect %s:%d: %w", host, info.Port, err)
	}
	c.session = session
	c.codec = "h264"
	if w := session.Get("welcome"); w.Truthy() && w.Get("codec").Type() == js.TypeString {
		c.codec = w.Get("codec").String()
	}

	c.mu.Lock()
	onTrack := c.onVideoTrack
	onState := c.onStateChg
	c.mu.Unlock()
	if onTrack != nil {
		onTrack()
	}
	if onState != nil {
		onState("connected")
	}
	return nil
}

func (c *WebTransportClient) VideoElement() js.Value { return c.videoEl }

func (c *WebTransportClient) WatchVideoFrames(onFrame func()) func() {
	return watchVideoFrames(c.videoEl, onFrame)
}

// NegotiatedVideoCodec: what the host's welcome said it streams.
func (c *WebTransportClient) NegotiatedVideoCodec() (string, bool) {
	if c.session.IsUndefined() || c.codec == "" {
		return "", false
	}
	return c.codec, true
}

// SendBinary sends one input packet (NV_INPUT_HEADER-prefixed, same bytes as
// the "input" DataChannel carries) on the control stream.
func (c *WebTransportClient) SendBinary(data []byte) error {
	if c.session.IsUndefined() {
		return errors.New("webtransport: not connected")
	}
	arr := js.Global().Get("Uint8Array").New(len(data))
	js.CopyBytesToJS(arr, data)
	if !c.session.Call("sendInput", arr).Bool() {
		return errors.New("webtransport: session closed")
	}
	return nil
}

// OpenDataChannel opens a named channel (a bidirectional stream) the same
// way WebRTCClient.OpenDataChannel opens a DataChannel; rust-shine bridges
// it into the agent by the same label.
func (c *WebTransportClient) OpenDataChannel(label string) (net.Conn, error) {
	if c.session.IsUndefined() {
		return nil, errors.New("webtransport: session not established")
	}
	return openDCConn(c.session.Call("createDataChannel", label), label)
}

type wtStats struct {
	framesReceived, framesDecoded, framesDropped, keyframes float64
	bytesReceived, audioPackets, decodeErrors               float64
	decodeTimeMs, rttMs                                     float64
	decoder                                                 string // "hardware"/"software"; "" off the worker path
}

func (c *WebTransportClient) stats() (wtStats, bool) {
	if c.session.IsUndefined() {
		return wtStats{}, false
	}
	s := c.session.Call("getStats")
	return wtStats{
		framesReceived: s.Get("framesReceived").Float(),
		framesDecoded:  s.Get("framesDecoded").Float(),
		framesDropped:  s.Get("framesDropped").Float(),
		keyframes:      s.Get("keyframes").Float(),
		bytesReceived:  s.Get("bytesReceived").Float(),
		audioPackets:   s.Get("audioPackets").Float(),
		decodeErrors:   s.Get("decodeErrors").Float(),
		decodeTimeMs:   s.Get("decodeTimeMs").Float(),
		rttMs:          s.Get("rttMs").Float(),
		decoder:        jsString(s.Get("decoder")),
	}, true
}

func jsString(v js.Value) string {
	if v.Type() == js.TypeString {
		return v.String()
	}
	return ""
}

// pollEvery runs fn every interval until the returned stop is called.
func pollEvery(interval time.Duration, fn func()) func() {
	stop := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				fn()
			}
		}
	}()
	return func() { once.Do(func() { close(stop) }) }
}

// StartStatsLogging: WebRTCClient.StartStatsLogging's counterpart, the same
// STALLED marker when no new frame was decoded between two polls.
func (c *WebTransportClient) StartStatsLogging(interval time.Duration, logFn func(msg string)) func() {
	last := -1.0
	return pollEvery(interval, func() {
		s, ok := c.stats()
		if !ok {
			return
		}
		prefix := "webtransport stats: "
		if last >= 0 && s.framesDecoded <= last {
			prefix += "STALLED (no new decoded frames) "
		}
		last = s.framesDecoded
		logFn(fmt.Sprintf("%sframesReceived=%.0f framesDecoded=%.0f framesDropped=%.0f keyframes=%.0f decodeErrors=%.0f audioPackets=%.0f rtt=%.1fms decoder=%s",
			prefix, s.framesReceived, s.framesDecoded, s.framesDropped, s.keyframes, s.decodeErrors, s.audioPackets, s.rttMs, s.decoder))
	})
}

// StartNetGraphStatsPolling feeds LatestNetGraphSnapshot like the WebRTC
// client does; "packets" are access units here (one per stream).
func (c *WebTransportClient) StartNetGraphStatsPolling() func() {
	return pollEvery(netGraphStatsPollInterval, func() {
		s, ok := c.stats()
		if !ok {
			return
		}
		snap := NetGraphSnapshot{
			Valid:             true,
			PacketsReceived:   uint32(s.framesReceived),
			FramesDecoded:     uint32(s.framesDecoded),
			FramesDropped:     uint32(s.framesDropped),
			TotalDecodeTimeMs: s.decodeTimeMs,
			RTTMs:             s.rttMs,
			RTTValid:          s.rttMs > 0,
			BytesReceived:     uint64(s.bytesReceived),
			Codec:             c.codec,
			At:                time.Now(),
		}
		netGraphSnapshotAtomic.Store(&snap)
	})
}

// Close ends the session and removes its media elements.
func (c *WebTransportClient) Close() {
	c.mu.Lock()
	if c.closeCalled {
		c.mu.Unlock()
		return
	}
	c.closeCalled = true
	c.mu.Unlock()
	if !c.session.IsUndefined() {
		c.session.Call("close")
	}
	c.release()
}

func (c *WebTransportClient) release() {
	removeMediaElements(c.videoEl, c.audioEl)
	// The JS side may still report its end after close(); the callback
	// checks closeCalled, but must stay callable, so it's released on the
	// next tick rather than now.
	if c.onState.Truthy() {
		f := c.onState
		time.AfterFunc(time.Second, f.Release)
	}
}

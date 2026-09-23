//go:build js && wasm

// The wasm/browser counterpart of moonlight_service.go: implements the same
// VideoClient interface, but backed by client/internal/webrtcweb's
// RTCPeerConnection wrapper (agent/internal/webrtcbridge on the other end)
// instead of the real Moonlight/GameStream protocol. VideoWidget and the
// rest of internal/gui talk to this exactly like they talk to
// MoonlightService -- no wasm-specific code needed in the GUI layer beyond
// the factory that picks which of the two to construct
// (client/internal/gui/video_client_factory_*.go).
package service

import (
	"fmt"
	"image"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/models"
	"usbridge-client/internal/webrtcweb"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// webrtcVideoFPS is the rate the legacy StartFrameCapture pixel-readback
// path pulls decoded frames at, kept only as a manual fallback (see its
// own doc comment) -- the default path is the DOM `<video>` overlay
// (video_widget_dom_overlay_wasm.go), which needs no fixed capture rate at
// all since the browser itself renders every frame directly.
const webrtcVideoFPS = 24

// WebRTCVideoClient implements service.VideoClient over WebRTC for the
// browser build.
type WebRTCVideoClient struct {
	config *models.AppConfig

	mu             sync.Mutex
	host           string
	apiSecret      string // hex, same format webrtcweb.NewWebRTCClient expects
	client         *webrtcweb.WebRTCClient
	connected      atomic.Bool
	stopFrameWatch func()
	stopStatsLog   func()
	stopNetGraph   func()
	// bitrateKbps: see SetBitrate's own doc comment -- 0 means "use the
	// server's own --webrtc-bitrate-kbps ceiling", same as never calling
	// SetBitrate at all.
	bitrateKbps int

	onFrame        func(image.Image)
	onStateChanged func(string)
	onError        func(error)

	autoReconnect     bool
	maxReconnectTries int
}

// VideoElement exposes the underlying <video> DOM element so
// video_widget_dom_overlay_wasm.go can position it as a CSS overlay
// directly over the video widget's on-screen rect and read its
// videoWidth/videoHeight -- both free JS property reads, no relation to
// the onFrame(image.Image) callback path at all. Returns the zero js.Value
// before a connection has been established.
func (c *WebRTCVideoClient) VideoElement() js.Value {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return js.Value{}
	}
	return client.VideoElement()
}

// OpenDataChannel creates a new labeled DataChannel on the already-connected
// WebRTC PeerConnection this video/control session is using, returning it as
// a net.Conn. Used by client/internal/usbpass' browser-sourced USB/IP
// passthrough (gamepad/pen) to ride the same PeerConnection instead of a
// separate ws:// WebSocket to the agent -- see
// client/internal/gui/controller/disk_widget.go's SetPeerConnection, wired
// from main_window.go via an optional-interface probe on VideoClient (same
// pattern VideoElement above already uses for lazily reading c.client).
// Fails with a clear error before ConnectToMoonlight has succeeded (or after
// Disconnect), which is a hard precondition now that browser USB passthrough
// has no other transport to fall back to.
func (c *WebRTCVideoClient) OpenDataChannel(label string) (net.Conn, error) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf("webrtc video: not connected -- connect video/control before attaching a browser USB device")
	}
	return client.OpenDataChannel(label)
}

// NewWebRTCVideoClient mirrors NewMoonlightService(cfg)'s shape.
func NewWebRTCVideoClient(config *models.AppConfig) *WebRTCVideoClient {
	return &WebRTCVideoClient{config: config, autoReconnect: true, maxReconnectTries: 20}
}

// SetAPISecret matches the optional interface main_window.go's
// attachUSBClient already type-asserts for on MoonlightService (see its
// SetAPISecret) -- picked up automatically, no gui-layer change needed.
//
// secret arrives as the raw bytes of the hex master-key *string* (see
// main_window_sync_v2.go's `mw.activeAPISecret = []byte(secret)`, where
// secret is already the hex text from the agent's QR code) -- not a
// hex-decoded 32-byte key. That's also exactly what
// webrtcweb.signHMAC/agent/internal/api/security.go's deriveKey expect
// (SHA256 of the ASCII hex string itself), so this must NOT hex-encode
// secret again -- confirmed live: doing so (via fmt.Sprintf("%x", secret))
// silently produced a different, wrong derived key and every
// /api/webrtc/offer call came back 401 despite the same master key working
// fine for every other /api/* call through USBClient.
func (c *WebRTCVideoClient) SetAPISecret(secret []byte) {
	c.mu.Lock()
	c.apiSecret = string(secret)
	c.mu.Unlock()
}

// SetTailscaleService exists only so this type satisfies the same optional
// interface main_window.go probes MoonlightService for
// (`interface{ SetTailscaleService(*TailscaleService) }`); a real tailnet
// connection from inside a browser tab isn't possible (see
// tailscale_service_wasm.go's doc comment) so this is a no-op.
func (c *WebRTCVideoClient) SetTailscaleService(*TailscaleService) {}

func (c *WebRTCVideoClient) ConnectToMoonlight() error {
	c.mu.Lock()
	host := c.host
	secret := c.apiSecret
	bitrateKbps := c.bitrateKbps
	c.mu.Unlock()
	if host == "" {
		return fmt.Errorf("webrtc video: no host set")
	}
	// secret is signed into every /webrtc/offer request the same way it's
	// signed into every other /api/* call this client makes -- see
	// webrtcweb.WebRTCClient's doc comment on masterKey/signHMAC and
	// postOffer's doc comment for how rustshine verifies it.

	// rustshine's own --webrtc-port default -- a genuinely separate port
	// from the agent's REST API (c.config.USBPort), since this hits
	// rustshine's webrtc-video crate directly, not a route on the agent
	// (there is no such route: an earlier version of this code posted to
	// the agent's own now-removed /api/webrtc/offer, which silently
	// worked only because a stale agent build still had that Go bridge
	// compiled in -- see webrtcweb.WebRTCClient.postOffer's doc comment
	// for the full story). Not yet configurable from the UI/QR payload;
	// tracked as a follow-up once rustshine's webrtc port becomes
	// something the agent reports rather than a fixed default both sides
	// happen to agree on.
	scheme := "http"
	port := c.config.USBPort
	if port == 0 {
		port = 8080
	}

	if api.BrowserIsHTTPS() {
		scheme = "https"
		if c.config.USBTLSPort > 0 {
			port = c.config.USBTLSPort
		} else {
			port = 8443
		}
	}

	baseURL := scheme + "://" + host + ":" + strconv.Itoa(port)

	// Preflight against the agent's ordinary REST API (a route every
	// backend answers, Sunshine included) before ever touching rustshine's
	// WebRTC-only port above -- tells "this agent is running Sunshine,
	// which never speaks WebRTC" apart from "this agent is just
	// unreachable right now", which the raw fetch failure against
	// rustshineWebRTCPort alone can't distinguish. Best-effort: if the
	// probe itself fails (network hiccup, older agent build without
	// /api/status's streamer field, whatever), fall through to the normal
	// WebRTC attempt below rather than blocking on it -- this is purely an
	// early, friendlier error path, not a hard gate.
	if streamer, err := webrtcweb.FetchStreamerName(host, port, secret); err == nil && streamer != "" && !webrtcweb.StreamerSupportsWebRTC(streamer) {
		return fmt.Errorf("%s: %w", streamer, ErrStreamerUnsupportedWebRTC)
	}

	client := webrtcweb.NewWebRTCClient(baseURL, secret)
	client.SetBitrateKbps(bitrateKbps)
	sessionID := uuid.NewString()

	client.OnStateChange(func(state string) {
		logrus.Infof("[webrtc-video] connection state: %s", state)
		c.mu.Lock()
		cb := c.onStateChanged
		c.mu.Unlock()
		switch state {
		case "connected":
			c.connected.Store(true)
			if cb != nil {
				cb("connected")
			}
		case "failed", "disconnected", "closed":
			c.connected.Store(false)
			if cb != nil {
				cb("disconnected")
			}
		}
	})

	client.OnVideoTrack(func() {
		logrus.Info("[webrtc-video] video track ready -- browser renders it directly (DOM overlay), no pixel readback")
		// No pixel data crosses into Go at all on this path (see
		// video_widget_dom_overlay_wasm.go, which positions
		// client.VideoElement() as a CSS overlay directly over the video
		// widget's on-screen rect and reads videoWidth/videoHeight as
		// plain JS properties for the aspect-ratio/content-rect math).
		// onFrame(nil) is still driven off WatchVideoFrames purely to keep
		// VideoWidget's existing frame-arrival bookkeeping (counters, FPS,
		// IsStreaming freshness) alive -- the same nil-frame convention
		// every native GPU-overlay platform (Android/Metal) already uses,
		// see handleVideoFrame's doc comment.
		stop := client.WatchVideoFrames(func() {
			c.mu.Lock()
			cb := c.onFrame
			c.mu.Unlock()
			if cb != nil {
				cb(nil)
			}
		})
		// See StartStatsLogging's own doc comment -- this is what
		// diagnosed the real, previously-invisible server-side capture
		// stall (rust-shine's capture-kms hitting a sustained
		// "framebuffer has no exportable plane-0 handle" condition) that
		// was masquerading as flaky WebRTC/ICE instability. Kept running
		// for the rest of this session so any recurrence shows up in the
		// ordinary client log stream without needing to reattach Chrome
		// DevTools Protocol by hand.
		stopStats := client.StartStatsLogging(2*time.Second, func(msg string) {
			logrus.Info("[webrtc-video] " + msg)
		})
		// Feeds service.NetGraph's netGraphNetworkStatsFn hook
		// (net_graph_wasm.go) -- separate from stopStats above since the
		// HUD wants a much tighter poll interval than the diagnostic
		// stall logger does (see StartNetGraphStatsPolling's doc comment).
		stopNetGraph := client.StartNetGraphStatsPolling()
		c.mu.Lock()
		c.stopFrameWatch = stop
		c.stopStatsLog = stopStats
		c.stopNetGraph = stopNetGraph
		c.mu.Unlock()
	})

	if err := client.Connect(sessionID); err != nil {
		return fmt.Errorf("webrtc video: connect: %w", err)
	}

	c.mu.Lock()
	c.client = client
	c.mu.Unlock()

	return nil
}

func (c *WebRTCVideoClient) ConnectToUDPViaPipe(pipeReader *os.File) error {
	return fmt.Errorf("webrtc video: UDP pipe path not applicable in the browser")
}

func (c *WebRTCVideoClient) Disconnect() error {
	c.mu.Lock()
	client := c.client
	c.client = nil
	stop := c.stopFrameWatch
	c.stopFrameWatch = nil
	stopStats := c.stopStatsLog
	c.stopStatsLog = nil
	stopNetGraph := c.stopNetGraph
	c.stopNetGraph = nil
	c.mu.Unlock()
	if stop != nil {
		stop()
	}
	if stopStats != nil {
		stopStats()
	}
	if stopNetGraph != nil {
		stopNetGraph()
	}
	if client != nil {
		client.Close()
	}
	c.connected.Store(false)
	return nil
}

func (c *WebRTCVideoClient) Reconnect() error {
	_ = c.Disconnect()
	return c.ConnectToMoonlight()
}

func (c *WebRTCVideoClient) SetOnFrameReceived(callback func(image.Image)) {
	c.mu.Lock()
	c.onFrame = callback
	c.mu.Unlock()
}

func (c *WebRTCVideoClient) SetOnStateChanged(callback func(string)) {
	c.mu.Lock()
	c.onStateChanged = callback
	c.mu.Unlock()
}

func (c *WebRTCVideoClient) SetOnError(callback func(error)) {
	c.mu.Lock()
	c.onError = callback
	c.mu.Unlock()
}

// SetOnPairingPINRequired/SetOnPairingPINResolved: the WebRTC path pairs
// via the same master-key HMAC scheme every other /api/* call on this
// agent uses (see agent/internal/api/webrtc.go's auth), never a Moonlight
// PIN -- there's nothing for these to ever fire.
func (c *WebRTCVideoClient) SetOnPairingPINRequired(callback func(pin string)) {}
func (c *WebRTCVideoClient) SetOnPairingPINResolved(callback func())           {}

func (c *WebRTCVideoClient) IsConnected() bool { return c.connected.Load() }

func (c *WebRTCVideoClient) GetStats() map[string]interface{} {
	return map[string]interface{}{"protocol": "webrtc"}
}

func (c *WebRTCVideoClient) GetConfig() *models.AppConfig { return c.config }

func (c *WebRTCVideoClient) GetBindHost() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.host
}

func (c *WebRTCVideoClient) UpdateHost(host string) {
	c.mu.Lock()
	c.host = host
	c.mu.Unlock()
}

// UpdateVideoPort/UpdateVideoUDPPort: real Moonlight-protocol port
// bookkeeping (RTSP/RTP ports on the agent's own Sunshine backend) -- the
// WebRTC path negotiates its own transport entirely inside the SDP
// offer/answer, no separate port to track on the client side.
func (c *WebRTCVideoClient) UpdateVideoPort(port int)    {}
func (c *WebRTCVideoClient) UpdateVideoUDPPort(port int) {}

// SetVideoMode/SetExpectedVideoSize/SetFPS: real Moonlight stream-parameter
// negotiation (LiInitializeVideoCallbacks etc.) has no WebRTC equivalent
// yet in this client -- Sunshine's own configured defaults apply for now.
// Wiring these into the SDP offer (bandwidth hints) or a control-channel
// message to the agent is a reasonable follow-up, not required for a
// first working video path.
func (c *WebRTCVideoClient) SetVideoMode(mode string)               {}
func (c *WebRTCVideoClient) SetExpectedVideoSize(width, height int) {}
func (c *WebRTCVideoClient) SetFPS(fps int)                         {}

// SetBitrate stores the video-settings dialog's bitrate request for the
// *next* ConnectToMoonlight call -- previously a no-op here (only the
// classic Moonlight/GameStream path's real ANNOUNCE negotiation honored
// it). Takes effect via webrtcweb.WebRTCClient.SetBitrateKbps, sent as
// OfferRequest.bitrate_kbps in the /webrtc/offer POST body; rustshine
// clamps it to its own --webrtc-bitrate-kbps ceiling server-side (see
// rust-shine's signaling.rs, resolve_session_bitrate_bps) -- this can only
// ever lower the session's ceiling, never raise it past what the operator
// configured. Does NOT affect an already-connected session (there's no
// mid-session renegotiation path here yet, same limitation
// SetVideoMode/SetFPS/SetExpectedVideoSize above already have) -- call
// before Connect, e.g. before the user hits "Apply" mid-session expects a
// reconnect anyway, same as every other setting in that dialog today.
func (c *WebRTCVideoClient) SetBitrate(kbps int) {
	c.mu.Lock()
	c.bitrateKbps = kbps
	c.mu.Unlock()
}

// SetColor444: the RustShine Pro color upgrade is HEVC/VAAPI-specific
// (moonlight-common-c ANNOUNCE negotiation) -- no WebRTC equivalent, same
// reasoning as SetVideoMode above.
func (c *WebRTCVideoClient) SetColor444(enabled bool) {}

// SetHdr: same reasoning as SetColor444 -- the RustShine HDR upgrade is
// also moonlight-common-c ANNOUNCE-specific, no WebRTC equivalent.
func (c *WebRTCVideoClient) SetHdr(enabled bool) {}

// NegotiatedVideoCodecName: the browser's RTCPeerConnection negotiates
// this internally (via the SDP answer's codec preference order); exposing
// which one it actually picked would need reading back
// RTCRtpReceiver.getParameters() or getStats() from JS -- not implemented
// yet, so this reports "unknown" rather than a guess.
func (c *WebRTCVideoClient) NegotiatedVideoCodecName() (string, bool) { return "", false }

// SupportsNativeFullscreen/native-fullscreen controls: the browser build
// has no OS-level fullscreen window of its own the way desktop/mobile
// platforms do (see fullscreen_dialog_mobile.go, which wasm already shares)
// -- Fyne's own SetFullScreen on the single browser window/tab covers this
// case instead, so this reports false/no-op throughout.
func (c *WebRTCVideoClient) SupportsNativeFullscreen() bool { return false }
func (c *WebRTCVideoClient) IsNativeFullscreenActive() bool { return false }
func (c *WebRTCVideoClient) StartNativeFullscreen() error   { return nil }
func (c *WebRTCVideoClient) StopNativeFullscreen() error    { return nil }
func (c *WebRTCVideoClient) ResetRuntimeDecoderFallback()   {}

func (c *WebRTCVideoClient) SetAutoReconnect(enabled bool) {
	c.mu.Lock()
	c.autoReconnect = enabled
	c.mu.Unlock()
}

func (c *WebRTCVideoClient) SetMaxReconnectAttempts(max int) {
	c.mu.Lock()
	c.maxReconnectTries = max
	c.mu.Unlock()
}

// MoonlightInputSender implementation -- VideoWidget (internal/gui/
// controller/video_widget_input.go) type-asserts every VideoClient for
// this interface and, when present, routes mouse/keyboard input through
// it exactly like the desktop/mobile cgo builds do via LiSend*Event. This
// is what was actually missing before (not a bug in VideoWidget's own
// input handling, which is shared and platform-agnostic) -- mouse/
// keyboard input over the web client silently did nothing because
// WebRTCVideoClient never implemented this interface at all, so the type
// assertion always failed and every input call was a no-op.
//
// Each method here builds the exact same NV_INPUT_HEADER-prefixed wire
// packet a real Moonlight client would send over the ENet control channel
// (see webrtcweb/input_wasm.go's doc comment) and writes it as a binary
// message on the "input" DataChannel. rustshine's crates/enet-input/src/
// input_decode.rs::decode_input_packet on the other end parses this same
// byte layout and injects it via uinput -- there is no separate "WebRTC
// input protocol"; it's the real protocol, just carried over a DataChannel
// instead of ENet.
func (c *WebRTCVideoClient) sendInput(packet []byte) {
	c.mu.Lock()
	client := c.client
	c.mu.Unlock()
	if client == nil {
		return
	}
	if err := client.SendBinary(packet); err != nil {
		logrus.Debugf("[webrtc-video] input send failed: %v", err)
	}
}

func (c *WebRTCVideoClient) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {
	c.sendInput(webrtcweb.EncodeKey(uint8(vkCode), uint8(modifiers), action == LiKeyActionDown))
}

func (c *WebRTCVideoClient) SendMoonlightMouseMove(dx, dy int16) {
	c.sendInput(webrtcweb.EncodeMouseMoveRelative(dx, dy))
}

func (c *WebRTCVideoClient) SendMoonlightMousePosition(x, y, refW, refH int16) {
	c.sendInput(webrtcweb.EncodeMouseMoveAbsolute(x, y, refW, refH))
}

func (c *WebRTCVideoClient) SendMoonlightMouseButton(action int8, button int) {
	c.sendInput(webrtcweb.EncodeMouseButton(uint8(button), action == LiMouseButtonPress))
}

func (c *WebRTCVideoClient) SendMoonlightScroll(clicks int8) {
	// Real Moonlight clients send scroll amount in WHEEL_DELTA (120) units
	// per notch -- LiSendScrollEvent(clicks) internally does this same
	// clicks*WHEEL_DELTA multiply before it reaches the wire (see
	// Limelight-common's own client.c); this client's callers already pass
	// "clicks" the same way every other platform's LiSendScrollEvent call
	// site does, so match that convention here rather than sending clicks
	// directly (which decode_input_packet's ScrollVertical would then
	// mis-scale by 120x relative to a real client's notches).
	const wheelDelta = 120
	c.sendInput(webrtcweb.EncodeScroll(int16(clicks) * wheelDelta))
}

func (c *WebRTCVideoClient) SendMoonlightControllerEvent(controllerNumber uint16, activeGamepadMask uint16, buttons uint16, leftTrigger uint8, rightTrigger uint8, leftStickX int16, leftStickY int16, rightStickX int16, rightStickY int16) {
	// Not implemented yet -- input_wasm.go only encodes the packet types
	// VideoWidget's mouse/keyboard paths actually exercise today (see its
	// own doc comment). No gamepad UI is wired up to the web client yet
	// either, so there's nothing that would call this in practice.
}

func (c *WebRTCVideoClient) SendMoonlightPenEvent(
	eventType, toolType, penButtons uint8,
	x, y, pressureOrDistance float32,
	rotation uint16, tilt uint8,
) {
	// Not implemented yet -- no browser-side pen/tablet capture exists (see
	// SendMoonlightControllerEvent's doc comment for the same reasoning).
}

func (c *WebRTCVideoClient) SendMoonlightUtf8Text(text string) {
	c.sendInput(webrtcweb.EncodeUtf8Text(text))
}

// IsInputActive: real Moonlight builds gate this on the underlying stream
// being fully set up (LiSend* would silently fail/no-op before that).
// Here that's simply "DataChannel open", which IsConnected already tracks
// via the OnStateChange "connected" callback.
func (c *WebRTCVideoClient) IsInputActive() bool {
	return c.connected.Load()
}

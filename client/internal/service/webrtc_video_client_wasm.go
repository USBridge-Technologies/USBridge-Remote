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
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"usbridge-client/internal/models"
	"usbridge-client/internal/webrtcweb"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// webrtcVideoFPS is the rate StartFrameCapture pulls decoded frames off the
// hidden <video> element at. 24fps balances CPU cost (a full-frame pixel
// readback through Go on every tick, see StartFrameCapture's doc comment)
// against visible smoothness for a remote-desktop use case -- not a
// twitch-shooter target, this is "read a doc / watch a build run" motion.
const webrtcVideoFPS = 24

// WebRTCVideoClient implements service.VideoClient over WebRTC for the
// browser build.
type WebRTCVideoClient struct {
	config *models.AppConfig

	mu        sync.Mutex
	host      string
	apiSecret string // hex, same format webrtcweb.NewWebRTCClient expects
	client    *webrtcweb.WebRTCClient
	connected atomic.Bool

	onFrame        func(image.Image)
	onStateChanged func(string)
	onError        func(error)

	autoReconnect     bool
	maxReconnectTries int
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
	c.mu.Unlock()
	if host == "" {
		return fmt.Errorf("webrtc video: no host set")
	}
	// secret is NOT required here (unlike every other /api/* call this
	// client makes elsewhere) -- rustshine's own native WebRTC signaling
	// endpoint doesn't authenticate requests at all yet, see
	// webrtcweb.WebRTCClient's doc comment on masterKey/signHMAC being
	// unused for that reason. Still passed through below so
	// webrtcweb.NewWebRTCClient's signature doesn't need to special-case
	// an empty string, and so it's a one-line change to wire real auth in
	// later.

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
	const rustshineWebRTCPort = 8443
	baseURL := "http://" + host + ":" + strconv.Itoa(rustshineWebRTCPort)

	client := webrtcweb.NewWebRTCClient(baseURL, secret)
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
		logrus.Info("[webrtc-video] video track ready, starting frame capture")
		client.StartFrameCapture(webrtcVideoFPS, func(w, h int, rgba []byte) {
			c.mu.Lock()
			cb := c.onFrame
			c.mu.Unlock()
			if cb == nil {
				return
			}
			img := &image.RGBA{
				Pix:    rgba,
				Stride: w * 4,
				Rect:   image.Rect(0, 0, w, h),
			}
			cb(img)
		})
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
	c.mu.Unlock()
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

// SetVideoMode/SetExpectedVideoSize/SetFPS/SetBitrate: real Moonlight
// stream-parameter negotiation (LiInitializeVideoCallbacks etc.) has no
// WebRTC equivalent yet in this client -- Sunshine's own configured
// defaults apply for now. Wiring these into the SDP offer (bandwidth
// hints) or a control-channel message to the agent is a reasonable
// follow-up, not required for a first working video path.
func (c *WebRTCVideoClient) SetVideoMode(mode string)               {}
func (c *WebRTCVideoClient) SetExpectedVideoSize(width, height int) {}
func (c *WebRTCVideoClient) SetFPS(fps int)                         {}
func (c *WebRTCVideoClient) SetBitrate(kbps int)                    {}

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

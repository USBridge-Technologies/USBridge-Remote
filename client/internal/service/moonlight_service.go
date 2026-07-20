package service

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"image"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	usbapi "usbridge-client/internal/api"
	"usbridge-client/internal/api/moonlight"
	"usbridge-client/internal/models"

	"github.com/sirupsen/logrus"
)

// MoonlightService is an implementation of VideoClient for the GameStream/Moonlight protocol.
type MoonlightService struct {
	config *models.AppConfig

	onFrameReceived func(image.Image)
	onStateChanged  func(string)
	onError         func(error)

	mu         sync.Mutex    // protects isRunning, stopPlayerCh, activeWrapper, abort
	abort      chan struct{} // closed by Disconnect to cancel an in-progress ConnectToMoonlight
	isRunning  bool
	serverHost string
	videoMode  string
	width      int
	height     int
	fps        int // overrides config.VideoFPS when > 0; set via SetFPS before ConnectToMoonlight
	bitrate    int // kbps; overrides the 10 Mbps default when > 0; set via SetBitrate before ConnectToMoonlight
	audioMuted bool

	apiSecret []byte // master secret for HMAC-signed API requests

	client             *moonlight.Client
	pairingPIN         string               // retained across reconnects so the user only needs to enter one PIN
	lastAppId          int                  // app ID from the last Launch(); used to quit before reconnect
	stopPlayerCh       chan struct{}        // closed to stop the active video/audio decoder goroutines
	activeWrapper      *MoonlightCgoWrapper // set while a stream is running, used for input routing
	tailscaleSvc       *TailscaleService    // optional; if set, Moonlight uses its dialer for Tailscale IPs
	stopMoonlightProxy func()               // non-nil when a tsnet proxy is active for internet streaming
}

// SetAPISecret stores the master secret used to HMAC-sign requests to the usbridge API.
func (m *MoonlightService) SetAPISecret(secret []byte) {
	m.apiSecret = secret
}

// SetTailscaleService wires in the Tailscale service so that Moonlight can use
// the Tailscale-aware dialer when userspace Tailscale is active (Android).
// On kernel Tailscale or desktop, the standard OS dialer already routes correctly.
func (m *MoonlightService) SetTailscaleService(ts *TailscaleService) {
	m.tailscaleSvc = ts
}

// NewMoonlightService creates a new MoonlightService.
func NewMoonlightService(config *models.AppConfig) *MoonlightService {
	// On Android, point identity storage at the persistent files dir before loading.
	initMoonlightConfigDir()
	// Initialize Moonlight Identity and Client
	identity, err := moonlight.LoadOrGenerateIdentity()
	if err != nil {
		logrus.Panicf("❌ Failed to load or generate Moonlight identity: %v", err)
	}

	return &MoonlightService{
		config: config,
		// Standard GameStream ports: HTTP=47989, HTTPS=47984.
		// If usbridge_service exposes them differently, we need to pass the proxied ports.
		// For now, we try the standard ports, or the configured USBPort.
		client: moonlight.NewClient("", 47989, 47984, identity),
	}
}

func (m *MoonlightService) ConnectToMoonlight() error {
	// Create a fresh abort channel for this connection attempt so Disconnect()
	// can interrupt any blocking HTTP call or post-connect setup.
	abort := make(chan struct{})
	m.mu.Lock()
	m.isRunning = true
	// Close any previous abort channel from a stale concurrent call.
	if m.abort != nil {
		select {
		case <-m.abort: // already closed
		default:
			close(m.abort)
		}
	}
	m.abort = abort
	m.mu.Unlock()

	aborted := func() bool {
		select {
		case <-abort:
			return true
		default:
			return false
		}
	}

	logrus.Info("🌕 Moonlight protocol: ConnectToMoonlight called")

	// 1. Setup client with the correct host
	m.client.Host = m.serverHost
	if m.client.Host == "0.0.0.0" || m.client.Host == "" {
		m.client.Host = "127.0.0.1" // Default to localhost if unbound
	}

	// 1b. Use the tsnet-aware dialer for Moonlight HTTP only when the target host is
	// actually a Tailscale peer. tsnet's userspace netstack does not route plain LAN
	// addresses (e.g. a "protocol=direct" 192.168.x.x host) reliably — when tsnet has
	// no login/tailnet session, dialing such a host through it hangs indefinitely
	// (srv.HTTPClient() has no request timeout), which looked like a silent black
	// screen: GetServerInfo() below never returned and never logged an error.
	if m.tailscaleSvc != nil && isLikelyTailnetHost(m.client.Host) {
		if tsHTTP, err := m.tailscaleSvc.HTTPClient(); err == nil && tsHTTP.Transport != nil {
			if tr, ok := tsHTTP.Transport.(*http.Transport); ok && tr.DialContext != nil {
				m.client.SetDialTransport(tr.DialContext)
				logrus.Info("🌕 [Moonlight] using Tailscale tsnet dialer for Sunshine HTTP")
			}
		}
	}

	tConnect := time.Now()

	// 2. Fetch Server Info
	serverInfo, err := m.client.GetServerInfo()
	logrus.Infof("⏱️ [Moonlight] serverinfo: %.0fms", float64(time.Since(tConnect).Milliseconds()))
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("failed to fetch server info (Sunshine down?): %v", err)
	}

	if serverInfo.PairStatus == 0 {
		logrus.Info("🔒 Moonlight Host is NOT PAIRED. Starting pairing flow...")

		// Reuse the same PIN across reconnects so the user only needs to enter it once.
		if m.pairingPIN == "" {
			m.pairingPIN = moonlight.GeneratePIN()
		}
		pin := m.pairingPIN
		logrus.Infof("🔐 MOONLIGHT PAIRING REQUIRED. Auto-submitting PIN %s to usbridge service...", pin)

		// Pair() blocks in the getservercert stage until Sunshine receives the PIN via its web API.
		// Start it in a goroutine, then submit the PIN after giving Sunshine time to register the request.
		pairErrCh := make(chan error, 1)
		go func() { pairErrCh <- m.client.Pair(pin) }()

		time.Sleep(500 * time.Millisecond) // let Sunshine register the pending pairing

		if submitErr := m.submitPinToService(pin); submitErr != nil {
			logrus.Warnf("⚠️ [Moonlight] Auto-PIN failed (%v). Enter PIN manually on Sunshine host: %s", submitErr, pin)
		}

		if err := <-pairErrCh; err != nil {
			errStr := fmt.Errorf("pairing failed: %v", err)
			logrus.Error(errStr)
			if m.onError != nil {
				m.onError(errStr)
			}
			m.isRunning = false
			return errStr
		}

		if aborted() {
			m.isRunning = false
			return fmt.Errorf("connect aborted by disconnect (post-pair)")
		}

		m.pairingPIN = "" // clear after success so future disconnects get a fresh PIN
		logrus.Info("✅ Moonlight pairing successful!")

		// Retry getting server info
		serverInfo, err = m.client.GetServerInfo()
		if err != nil {
			m.isRunning = false
			return fmt.Errorf("failed to get server info after pairing: %v", err)
		}
	}

	if aborted() {
		m.isRunning = false
		return fmt.Errorf("connect aborted by disconnect (post-serverinfo)")
	}

	logrus.Infof("🖥️ Sunshine Server Info: AppVersion=%s, GfeVersion=%s", serverInfo.AppVersion, serverInfo.GfeVersion)

	// 3. Fetch App List to find 'Desktop'
	t1 := time.Now()
	apps, err := m.client.GetAppList()
	logrus.Infof("⏱️ [Moonlight] applist: %.0fms (total %.0fms)", float64(time.Since(t1).Milliseconds()), float64(time.Since(tConnect).Milliseconds()))
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("failed to get app list: %v", err)
	}

	if aborted() {
		m.isRunning = false
		return fmt.Errorf("connect aborted by disconnect (post-applist)")
	}

	appId := 0
	for _, app := range apps {
		logrus.Infof("🎮 Found Moonlight App: %s (ID: %d)", app.AppTitle, app.ID)
		if appId == 0 || app.AppTitle == "Desktop" {
			appId = app.ID
		}
	}

	// 4. Launch App
	fps := 30
	if m.fps > 0 {
		fps = m.fps
	} else if m.config.VideoFPS > 0 {
		fps = m.config.VideoFPS
	}
	bitrate := 10000 // 10 Mbps default, overridden by SetBitrate (wired to the video-start dialog's slider)
	if m.bitrate > 0 {
		bitrate = m.bitrate
	}

	t2 := time.Now()
	sessionUrl, rikey, err := m.client.Launch(appId, m.videoMode, m.width, m.height, fps, bitrate)
	logrus.Infof("⏱️ [Moonlight] launch/resume HTTP: %.0fms (total %.0fms)", float64(time.Since(t2).Milliseconds()), float64(time.Since(tConnect).Milliseconds()))
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("failed to launch app: %v", err)
	}

	if aborted() {
		m.isRunning = false
		return fmt.Errorf("connect aborted by disconnect (post-launch)")
	}

	m.lastAppId = appId
	logrus.Infof("🚀 Moonlight App Launched! RTSP Session URL: %s", sessionUrl)

	// Stop any previous player goroutines before starting new ones.
	// Protected by mu to prevent concurrent close from Disconnect().
	m.mu.Lock()
	prevStopCh := m.stopPlayerCh
	newStopCh := make(chan struct{})
	m.stopPlayerCh = newStopCh
	m.mu.Unlock()

	if prevStopCh != nil {
		close(prevStopCh)
	}
	stopCh := newStopCh // capture for closures below

	width, height := m.width, m.height
	if width == 0 {
		width = 1920
	}
	if height == 0 {
		height = 1080
	}

	// Determine decode path once so closures below capture a stable value.
	vtPath := usesVideoToolbox()

	// 5a. Video pipe. Every platform now decodes in-process (VideoToolbox /
	//     libavcodec) via a direct frame callback and closes pipeRead
	//     immediately without reading from it — the pipe exists only to keep
	//     startMoonlightVideoDecoder's signature uniform across platforms.
	pipeRead, pipeWrite, err := os.Pipe()
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("pipe: %v", err)
	}

	// 5aa. Audio pipe: ar_decode writes S16LE PCM for startMoonlightAudio to consume.
	var audioPipeWrite *os.File
	if audioPipeRead, apw, aerr := os.Pipe(); aerr != nil {
		logrus.Warnf("🔊 [Moonlight/Audio] failed to create audio pipe: %v — audio disabled", aerr)
	} else {
		audioPipeWrite = apw
		if aerr := startMoonlightAudio(audioPipeRead, stopCh, func(err error) {
			if err != nil {
				logrus.Warnf("🔊 [Moonlight/Audio] stopped: %v", err)
			} else {
				logrus.Info("🔊 [Moonlight/Audio] stopped cleanly")
			}
		}); aerr != nil {
			logrus.Warnf("🔊 [Moonlight/Audio] failed to start: %v — audio disabled", aerr)
			_ = audioPipeRead.Close()
			_ = audioPipeWrite.Close()
			audioPipeWrite = nil
		}
	}

	// 5b. Start video decode path (non-blocking). On every platform,
	//     startMoonlightVideoDecoder registers the in-process frame callback
	//     (VideoToolbox / libavcodec) and closes pipeRead; no subprocess is started.
	if err := startMoonlightVideoDecoder(pipeRead, width, height, stopCh,
		func(img image.Image) {
			if m.onFrameReceived != nil {
				m.onFrameReceived(img)
			}
		},
		func(playerErr error) {
			m.isRunning = false
			if playerErr != nil {
				logrus.Errorf("🌕 [Moonlight/Player] stopped with error: %v", playerErr)
				if m.onError != nil {
					m.onError(fmt.Errorf("moonlight stream ended: %v", playerErr))
				}
			} else {
				logrus.Info("🌕 [Moonlight/Player] stopped cleanly")
			}
			if m.onStateChanged != nil {
				m.onStateChanged("disconnected")
			}
		},
	); err != nil {
		_ = pipeRead.Close()
		_ = pipeWrite.Close()
		if audioPipeWrite != nil {
			_ = audioPipeWrite.Close()
		}
		m.isRunning = false
		return fmt.Errorf("failed to start video decode path: %v", err)
	}

	// 5c. Start LiStartConnection in background goroutine.
	//   dr_submit feeds the platform decoder (VideoToolbox / libavcodec /
	//   AMediaCodec) directly on every platform; pipeWrite is passed through
	//   but its fd is unused by do_li_start.
	// On Android with userspace tsnet, C-level BSD sockets bypass the Go tsnet
	// stack and have no kernel route to a 100.x tailnet IP at all.
	// m.client.Host being a tailnet address is the user's explicit choice to
	// connect over Tailscale — always honor that and route the C-level
	// sockets through the local tsnet proxy. This used to opportunistically
	// probe for a same-LAN direct IP and silently switch to it instead,
	// which (a) meant "Tailscale" didn't actually mean Tailscale, and (b)
	// wasn't even the fix for the control-stream disconnects it was
	// suspected of — reconnects were reproduced on both paths.
	moonlightHost := m.client.Host
	if m.tailscaleSvc != nil && isLikelyTailnetHost(m.client.Host) {
		rtspPort := 48010 // default; parse from sessionUrl for accuracy
		if _, portStr, splitErr := net.SplitHostPort(sessionUrl); splitErr == nil {
			if p, perr := strconv.Atoi(portStr); perr == nil && p > 0 {
				rtspPort = p
			}
		}
		stopProxy, localRTSPPort, proxyErr := startMoonlightProxy(m.tailscaleSvc, m.client.Host, rtspPort)
		if proxyErr != nil {
			logrus.Warnf("🌕 [Moonlight/tsnet] proxy failed (%v) — C sockets will use the Tailscale IP directly (needs system VPN)", proxyErr)
		} else {
			moonlightHost = "127.0.0.1"
			// Keep host:port format (no rtsp:// prefix; CGO wrapper handles that per-platform).
			sessionUrl = fmt.Sprintf("127.0.0.1:%d", localRTSPPort)
			m.stopMoonlightProxy = stopProxy
			logrus.Infof("🌕 [Moonlight/Proxy] ✅ tsnet proxy active: moonlightHost=127.0.0.1 rtspProxy=127.0.0.1:%d", localRTSPPort)
		}
	}
	t3 := time.Now()
	wrapper := NewMoonlightCgoWrapper(moonlightHost)
	wrapper.SetAudioMuted(m.audioMuted)

	m.mu.Lock()
	m.activeWrapper = wrapper
	m.mu.Unlock()

	if err := wrapper.StartStream(
		sessionUrl, rikey,
		serverInfo.AppVersion, serverInfo.GfeVersion,
		serverInfo.ServerCodecModeSupport,
		moonlightVideoFormat(m.videoMode),
		width, height, fps, bitrate,
		pipeWrite, audioPipeWrite,
		func(cgoErr error) {
			// The tsnet proxy (if any) belongs to this specific connection
			// attempt. Whatever ends the stream — a clean stop or a mid-stream
			// C error like a failed RTSP handshake — this callback is the one
			// place guaranteed to fire for every path (vtPath and not), so it
			// owns the proxy teardown. Leaving it running past this point just
			// leaks its bound UDP port (127.0.0.1:47999), which fails the next
			// reconnect's proxy bind with "address already in use" and forces
			// it onto the unreliable direct-Tailscale-IP fallback instead.
			m.stopActiveProxy()

			if cgoErr != nil {
				logrus.Errorf("🌕 [Moonlight/CGO] stream error: %v", cgoErr)
				if m.onError != nil {
					m.onError(cgoErr)
				}
			}
			// In-process decode path: there's no subprocess to signal stream
			// end, so we handle the state change here instead.
			if vtPath {
				m.mu.Lock()
				m.isRunning = false
				m.mu.Unlock()
				if cgoErr == nil {
					logrus.Info("🌕 [Moonlight/VT] stream stopped cleanly")
				}
				if m.onStateChanged != nil {
					m.onStateChanged("disconnected")
				}
			}
		},
	); err != nil {
		_ = pipeWrite.Close()
		if audioPipeWrite != nil {
			_ = audioPipeWrite.Close()
		}
		m.mu.Lock()
		m.activeWrapper = nil
		m.isRunning = false
		m.mu.Unlock()
		return fmt.Errorf("failed to start LiStartConnection: %v", err)
	}

	// Final abort check: if Disconnect was called while StartStream was setting up,
	// stop the stream now and return an error so the caller (VideoWidget) knows not
	// to mark the session as active.
	if aborted() {
		logrus.Info("🌕 [Moonlight] connect was aborted during stream setup — stopping immediately")
		wrapper.StopStream()
		m.mu.Lock()
		m.activeWrapper = nil
		m.isRunning = false
		m.mu.Unlock()
		return fmt.Errorf("connect aborted by disconnect (post-start)")
	}

	logrus.Infof("⏱️ [Moonlight] LiStartConnection submitted: %.0fms (total %.0fms). Waiting for first frame...", float64(time.Since(t3).Milliseconds()), float64(time.Since(tConnect).Milliseconds()))

	if m.onStateChanged != nil {
		m.onStateChanged("connected")
	}

	return nil
}

func (m *MoonlightService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	return fmt.Errorf("ConnectToUDPViaPipe not supported by MoonlightService")
}

// stopActiveProxy stops and clears the tsnet proxy for the current connection
// attempt, if one is active. Safe to call multiple times (e.g. once from the
// stream-end callback and again from a later Disconnect()) since it's a no-op
// once m.stopMoonlightProxy has already been cleared.
func (m *MoonlightService) stopActiveProxy() {
	m.mu.Lock()
	stopProxy := m.stopMoonlightProxy
	m.stopMoonlightProxy = nil
	m.mu.Unlock()
	if stopProxy != nil {
		stopProxy()
	}
}

func (m *MoonlightService) Disconnect() error {
	logrus.Info("🌕 Moonlight protocol: Disconnect called")

	// Take a snapshot of everything we need to clean up under the lock,
	// then do all blocking operations outside the lock.
	m.mu.Lock()
	m.isRunning = false

	// Signal any in-progress ConnectToMoonlight to abort.
	if m.abort != nil {
		select {
		case <-m.abort: // already closed
		default:
			close(m.abort)
		}
		m.abort = nil
	}

	activeWrapper := m.activeWrapper
	m.activeWrapper = nil

	stopCh := m.stopPlayerCh
	m.stopPlayerCh = nil
	m.mu.Unlock()

	// LiStopConnection interrupts the LiStartConnection goroutine; the stream's
	// completion callback (registered in StartStream) then reports "disconnected".
	//
	// This must run BEFORE stopActiveProxy(), not after: moonlight-common-c's
	// control stream teardown attempts a *graceful* ENet disconnect --
	// send a disconnect packet, then wait up to CONTROL_STREAM_LINGER_TIMEOUT_SEC
	// (2s, fixed) for the peer to ACK it. Tearing down our local tsnet proxy
	// first closes the UDP relay that packet and its ACK would have to travel
	// through, so the ACK can never arrive and this always burns the full 2s
	// timeout ("Timed out waiting for ENet peer to acknowledge disconnection").
	// Every tailnet connection goes through this proxy now (see Connect()), so
	// this was adding a guaranteed ~2s stall to every disconnect+reconnect
	// (e.g. switching codecs) that a direct LAN connection never used to hit.
	if activeWrapper != nil {
		activeWrapper.StopStream()
	} else {
		NewMoonlightCgoWrapper(m.host()).StopStream()
	}

	m.stopActiveProxy()

	if stopCh != nil {
		close(stopCh)
	}
	if m.onStateChanged != nil {
		m.onStateChanged("disconnected")
	}

	// Tell Sunshine to end the app session on every disconnect, not just before
	// a Reconnect(). Without this, Sunshine keeps reporting "an app is already
	// running" on the *next* connect (even a clean one, e.g. a plain
	// disconnect+reconnect from the Control tab), forcing that connect down the
	// /resume path instead of /launch. Measured cost of that: ~5s HTTP round
	// trip before the Moonlight handshake even starts (tests/test_android_video_launch.sh),
	// vs the handshake itself completing in ~450ms once /resume returns.
	// Fired async and best-effort — the local session is already torn down
	// above, so a slow or failed /quit here must never block or fail Disconnect().
	if m.lastAppId != 0 {
		appID := m.lastAppId
		client := m.client
		go func() {
			if err := client.Quit(appID); err != nil {
				logrus.Warnf("🌕 [Moonlight] /cancel on disconnect failed (non-fatal): %v", err)
			} else {
				logrus.Info("🌕 [Moonlight] /cancel sent on disconnect — Sunshine session reset for next connect")
			}
		}()
	}

	return nil
}

// ── MoonlightInputSender implementation ──────────────────────────────────────

func (m *MoonlightService) SendMoonlightKey(vkCode int16, action int8, modifiers int8) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightKey(vkCode, action, modifiers)
	}
}

func (m *MoonlightService) SendMoonlightMouseMove(dx, dy int16) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightMouseMove(dx, dy)
	}
}

func (m *MoonlightService) SendMoonlightMousePosition(x, y, refW, refH int16) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightMousePosition(x, y, refW, refH)
	}
}

func (m *MoonlightService) SendMoonlightMouseButton(action int8, button int) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightMouseButton(action, button)
	}
}

func (m *MoonlightService) SendMoonlightScroll(clicks int8) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightScroll(clicks)
	}
}

func (m *MoonlightService) SendMoonlightControllerEvent(controllerNumber uint16, activeGamepadMask uint16, buttons uint16, leftTrigger uint8, rightTrigger uint8, leftStickX int16, leftStickY int16, rightStickX int16, rightStickY int16) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightControllerEvent(controllerNumber, activeGamepadMask, buttons, leftTrigger, rightTrigger, leftStickX, leftStickY, rightStickX, rightStickY)
	}
}

func (m *MoonlightService) IsInputActive() bool {
	return m.activeWrapper != nil && m.activeWrapper.IsInputActive()
}

// NegotiatedVideoCodecName returns the codec moonlight-common-c actually
// negotiated with the server for the current session, straight from
// dr_setup's NegotiatedVideoFormat — see MoonlightCgoWrapper.NegotiatedVideoCodecName.
func (m *MoonlightService) NegotiatedVideoCodecName() (string, bool) {
	if m.activeWrapper == nil {
		return "", false
	}
	return m.activeWrapper.NegotiatedVideoCodecName()
}

func (m *MoonlightService) SendMoonlightUtf8Text(text string) {
	if m.activeWrapper != nil {
		m.activeWrapper.SendMoonlightUtf8Text(text)
	}
}

var _ MoonlightInputSender = (*MoonlightService)(nil)

// ── Audio mute ────────────────────────────────────────────────────────────────

func (m *MoonlightService) SetAudioMuted(muted bool) {
	m.audioMuted = muted
	if m.activeWrapper != nil {
		m.activeWrapper.SetAudioMuted(muted)
	}
}

func (m *MoonlightService) GetAudioMuted() bool {
	return m.audioMuted
}

func (m *MoonlightService) host() string {
	if m.serverHost != "" {
		return m.serverHost
	}
	return m.client.Host
}

func (m *MoonlightService) Reconnect() error {
	_ = m.Disconnect()
	// Tell Sunshine to end the current session before reconnecting. This resets
	// Sunshine's internal session state so the next Launch() gets a fresh session
	// instead of resuming a potentially-corrupted one (which causes code=60 / ETIMEDOUT).
	if m.lastAppId != 0 {
		if err := m.client.Quit(m.lastAppId); err != nil {
			logrus.Warnf("🌕 [Moonlight] /cancel failed (non-fatal): %v", err)
		} else {
			logrus.Info("🌕 [Moonlight] /cancel sent — Sunshine session reset")
		}
	}
	return m.ConnectToMoonlight()
}

// submitPinToService sends the pairing PIN to the usbridge service, which forwards it
// to Sunshine's local web API so the getservercert handshake can complete automatically.
func (m *MoonlightService) submitPinToService(pin string) error {
	host := m.serverHost
	if host == "" {
		host = "127.0.0.1"
	}
	port := m.config.USBPort
	if port == 0 {
		port = 8080
	}

	body, _ := json.Marshal(map[string]string{"pin": pin})

	// Signed requests to an actual Tailscale peer must go through the tsnet dialer —
	// the OS dialer can't reach Tailscale IPs. But for a plain LAN host
	// (protocol=direct), tsnet's netstack can't route there either (when
	// unauthenticated it black-holes the connection), so this must only apply
	// when the target is actually a tailnet address — see isLikelyTailnetHost.
	tsHTTPClient := (*http.Client)(nil)
	if m.tailscaleSvc != nil && isLikelyTailnetHost(host) {
		if c, err := m.tailscaleSvc.HTTPClient(); err == nil {
			tsHTTPClient = c
		}
	}

	// Use a plain HTTP client when we have no API secret (pre-pair state).
	// Once paired the secret is set via SetAPISecret and we use HMAC signing.
	if len(m.apiSecret) == 0 {
		url := fmt.Sprintf("http://%s:%d/api/moonlight/pin", host, port)
		client := &http.Client{Timeout: 10 * time.Second}
		if tsHTTPClient != nil {
			client.Transport = tsHTTPClient.Transport
		} else {
			client.Transport = &http.Transport{
				Proxy:        http.ProxyURL(nil),
				TLSNextProto: make(map[string]func(authority string, c *tls.Conn) http.RoundTripper),
			}
		}
		resp, err := client.Post(url, "application/json", bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("POST %s: %w", url, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("usbridge service returned HTTP %d", resp.StatusCode)
		}
		return nil
	}

	var usbClient *usbapi.USBClient
	if tsHTTPClient != nil {
		usbClient = usbapi.NewUSBClientWithHTTPClient(host, port, 10, tsHTTPClient)
	} else {
		usbClient = usbapi.NewUSBClient(host, port, 10)
	}
	usbClient.SetAPISecretV2(m.apiSecret)
	_, err := usbClient.PostRaw("/api/moonlight/pin", body)
	return err
}

func (m *MoonlightService) SetOnFrameReceived(callback func(image.Image)) {
	m.onFrameReceived = callback
}

func (m *MoonlightService) SetOnStateChanged(callback func(string)) {
	m.onStateChanged = callback
}

func (m *MoonlightService) SetOnError(callback func(error)) {
	m.onError = callback
}

func (m *MoonlightService) IsConnected() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunning
}

func (m *MoonlightService) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"protocol": "moonlight (stub)",
	}
}

func (m *MoonlightService) GetConfig() *models.AppConfig {
	return m.config
}

func (m *MoonlightService) GetBindHost() string {
	return m.serverHost
}

func (m *MoonlightService) UpdateHost(host string) {
	m.serverHost = host
}

func (m *MoonlightService) UpdateVideoPort(port int) {
	// Moonlight handles its own ports for NVST
}

func (m *MoonlightService) UpdateVideoUDPPort(port int) {
}

func (m *MoonlightService) SetVideoMode(mode string) {
	m.videoMode = mode
}

// moonlightVideoFormat maps a video mode string to the VIDEO_FORMAT_* constant
// used by moonlight-common-c (matches Limelight.h defines).
func moonlightVideoFormat(mode string) int {
	switch mode {
	case models.VideoModeH265:
		return 0x0100 // VIDEO_FORMAT_H265
	case models.VideoModeAV1:
		return 0x1000 // VIDEO_FORMAT_AV1_MAIN8
	default:
		return 0x0001 // VIDEO_FORMAT_H264
	}
}

func (m *MoonlightService) SetExpectedVideoSize(width, height int) {
	m.width = width
	m.height = height
}

func (m *MoonlightService) SetFPS(fps int) {
	m.fps = fps
}

func (m *MoonlightService) SetBitrate(kbps int) {
	m.bitrate = kbps
}

func (m *MoonlightService) SupportsNativeFullscreen() bool {
	return false
}

func (m *MoonlightService) IsNativeFullscreenActive() bool {
	return false
}

func (m *MoonlightService) StartNativeFullscreen() error {
	return nil
}

func (m *MoonlightService) StopNativeFullscreen() error {
	return nil
}

func (m *MoonlightService) ResetRuntimeDecoderFallback() {
}

func (m *MoonlightService) SetAutoReconnect(enabled bool) {
}

func (m *MoonlightService) SetMaxReconnectAttempts(max int) {
}

// isLikelyTailnetHost reports whether host is a Tailscale address (100.64.0.0/10
// CGNAT range or a *.ts.net MagicDNS name), mirroring the same check the deep
// link handler uses to pick internal_host vs tailscale_host.
func isLikelyTailnetHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.HasSuffix(strings.ToLower(host), ".ts.net") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return netip.MustParsePrefix("100.64.0.0/10").Contains(addr)
}

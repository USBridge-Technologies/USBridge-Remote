package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"net/http"
	"os"
	"time"

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

	isRunning  bool
	serverHost string
	videoMode  string
	width      int
	height     int

	client        *moonlight.Client
	pairingPIN    string             // retained across reconnects so the user only needs to enter one PIN
	stopPlayerCh  chan struct{}       // closed to stop the active GStreamer player goroutines
	activeWrapper *MoonlightCgoWrapper // set while a stream is running, used for input routing
}

// NewMoonlightService creates a new MoonlightService.
func NewMoonlightService(config *models.AppConfig) *MoonlightService {
	// Initialize Moonlight Identity and Client
	identity, err := moonlight.LoadOrGenerateIdentity()
	if err != nil {
		logrus.Errorf("❌ Failed to load or generate Moonlight identity: %v", err)
	}

	return &MoonlightService{
		config: config,
		// Standard GameStream ports: HTTP=47989, HTTPS=47984.
		// If usbridge_service exposes them differently, we need to pass the proxied ports.
		// For now, we try the standard ports, or the configured USBPort.
		client: moonlight.NewClient("", 47989, 47984, identity),
	}
}

func (m *MoonlightService) ConnectToRTP() error {
	m.isRunning = true
	logrus.Info("🌕 Moonlight protocol: ConnectToRTP called")

	// 1. Setup client with the correct host
	m.client.Host = m.serverHost
	if m.client.Host == "0.0.0.0" || m.client.Host == "" {
		m.client.Host = "127.0.0.1" // Default to localhost if unbound
	}

	// 2. Fetch Server Info
	serverInfo, err := m.client.GetServerInfo()
	if err != nil || serverInfo.PairStatus == 0 {
		if err != nil {
			logrus.Warnf("⚠️ Moonlight ServerInfo failed (Needs pairing?): %v", err)
		} else {
			logrus.Info("🔒 Moonlight Host is NOT PAIRED. Starting pairing flow...")
		}

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

		m.pairingPIN = "" // clear after success so future disconnects get a fresh PIN
		logrus.Info("✅ Moonlight pairing successful!")
		
		// Retry getting server info
		serverInfo, err = m.client.GetServerInfo()
		if err != nil {
			m.isRunning = false
			return fmt.Errorf("failed to get server info after pairing: %v", err)
		}
	}
	
	logrus.Infof("🖥️ Sunshine Server Info: AppVersion=%s, GfeVersion=%s", serverInfo.AppVersion, serverInfo.GfeVersion)

	// 3. Fetch App List to find 'Desktop'
	apps, err := m.client.GetAppList()
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("failed to get app list: %v", err)
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
	if m.config.VideoFPS > 0 {
		fps = m.config.VideoFPS
	}
	bitrate := 10000 // 10 Mbps default
	
	sessionUrl, rikey, err := m.client.Launch(appId, m.videoMode, m.width, m.height, fps, bitrate)
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("failed to launch app: %v", err)
	}
	logrus.Infof("🚀 Moonlight App Launched! RTSP Session URL: %s", sessionUrl)

	// Stop any previous player.
	if m.stopPlayerCh != nil {
		close(m.stopPlayerCh)
	}
	m.stopPlayerCh = make(chan struct{})
	stopCh := m.stopPlayerCh // capture for closure

	width, height := m.width, m.height
	if width == 0 {
		width = 1920
	}
	if height == 0 {
		height = 1080
	}

	// 5a. Create a pipe: LiStartConnection writes Annex-B H.264 to pipeWrite;
	//     GStreamer fdsrc reads from pipeRead and decodes with vtdec.
	pipeRead, pipeWrite, err := os.Pipe()
	if err != nil {
		m.isRunning = false
		return fmt.Errorf("pipe: %v", err)
	}

	// 5b. Start GStreamer fdsrc pipeline (non-blocking).
	if err := startMoonlightGStreamer(pipeRead, width, height, stopCh,
		func(img image.Image) {
			if m.onFrameReceived != nil {
				m.onFrameReceived(img)
			}
		},
		func(playerErr error) {
			m.isRunning = false
			if playerErr != nil {
				logrus.Errorf("🌕 [Moonlight/GStreamer] stopped with error: %v", playerErr)
				if m.onError != nil {
					m.onError(fmt.Errorf("moonlight stream ended: %v", playerErr))
				}
			} else {
				logrus.Info("🌕 [Moonlight/GStreamer] stopped cleanly")
			}
			if m.onStateChanged != nil {
				m.onStateChanged("disconnected")
			}
		},
	); err != nil {
		_ = pipeRead.Close()
		_ = pipeWrite.Close()
		m.isRunning = false
		return fmt.Errorf("failed to start Moonlight GStreamer player: %v", err)
	}
	// 5c. Start LiStartConnection in background goroutine.
	//     submitDecodeUnit writes H.264 frames to pipeWrite; when the session ends
	//     pipeWrite is closed → GStreamer sees EOF and stops.
	wrapper := NewMoonlightCgoWrapper(m.client.Host)
	m.activeWrapper = wrapper
	if err := wrapper.StartStream(
		sessionUrl, rikey,
		serverInfo.AppVersion, serverInfo.GfeVersion,
		serverInfo.ServerCodecModeSupport,
		width, height, fps, bitrate,
		pipeWrite,
		func(cgoErr error) {
			// pipeWrite is closed inside StartStream when LiStartConnection returns.
			if cgoErr != nil {
				logrus.Errorf("🌕 [Moonlight/CGO] stream error: %v", cgoErr)
				if m.onError != nil {
					m.onError(cgoErr)
				}
			}
		},
	); err != nil {
		_ = pipeWrite.Close()
		m.isRunning = false
		return fmt.Errorf("failed to start LiStartConnection: %v", err)
	}

	if m.onStateChanged != nil {
		m.onStateChanged("connected")
	}

	return nil
}

func (m *MoonlightService) ConnectToUDPViaPipe(pipeReader *os.File) error {
	return fmt.Errorf("ConnectToUDPViaPipe not supported by MoonlightService")
}

func (m *MoonlightService) Disconnect() error {
	m.isRunning = false
	logrus.Info("🌕 Moonlight protocol: Disconnect called")
	// LiStopConnection interrupts the LiStartConnection goroutine, which closes
	// pipeWrite → GStreamer gets EOF → frame reader goroutine exits.
	NewMoonlightCgoWrapper(m.host()).StopStream()
	m.activeWrapper = nil
	if m.stopPlayerCh != nil {
		close(m.stopPlayerCh)
		m.stopPlayerCh = nil
	}
	if m.onStateChanged != nil {
		m.onStateChanged("disconnected")
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

var _ MoonlightInputSender = (*MoonlightService)(nil)

func (m *MoonlightService) host() string {
	if m.serverHost != "" {
		return m.serverHost
	}
	return m.client.Host
}

func (m *MoonlightService) Reconnect() error {
	_ = m.Disconnect()
	return m.ConnectToRTP()
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
	url := fmt.Sprintf("http://%s:%d/api/moonlight/pin", host, port)

	client := &http.Client{Timeout: 10 * time.Second}
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

func (m *MoonlightService) SetExpectedVideoSize(width, height int) {
	m.width = width
	m.height = height
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

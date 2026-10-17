package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"usbridge_agent/internal/clipboard"
	"usbridge_agent/internal/display"
	"usbridge_agent/internal/displaypower"
	"usbridge_agent/internal/hostload"
	"usbridge_agent/internal/usbpass"
	"usbridge_agent/internal/vdisplay"
)

type Application interface {
	Status() SystemStatus
	DeviceInfo() DeviceInfoResponse
	ReplaceDevices([]DeviceRequest) error
	ClearDevices() error
	Input() interface {
		Key(uint8) error
		Combo(uint8, uint8) error
		Text(string) error
		MouseMove(int8, int8) error
		MouseClick(uint8) error
		MouseScroll(int8) error
		MouseAction(uint8, int8, int8, int8) error
		AbsoluteEvent(uint8, uint16, uint16, int8) error
	}
	Screen() interface {
		Snapshot() (*ScreenSnapshot, error)
	}
	VideoDevices() []VideoDeviceInfo
	// SunshineOutputName returns the monitor Sunshine is pinned to capture
	// (its connected-output index, stringified), or "" for auto-pick.
	SunshineOutputName() string
	// SetSunshineOutputName pins Sunshine's capture to the given monitor.
	SetSunshineOutputName(name string) error
	// SunshineStreamHost returns the IP Sunshine advertises to Moonlight clients
	// (external_ip from sunshine.conf, or "" if auto-detect).
	SunshineStreamHost() string
	// SunshineAdminPort returns the Sunshine web admin / NvHTTP port (default 47990).
	SunshineAdminPort() int
	// SubmitMoonlightPIN sends the PIN shown by a Moonlight client to the
	// streaming host to complete the pairing handshake.
	SubmitMoonlightPIN(pin string) error
	CurrentVideoCodec() string
	// SupportedVideoCodecs returns which of h264/h265/av1 the host's hardware
	// encoder can actually produce right now (Sunshine's live capability probe).
	SupportedVideoCodecs() []string
	// Color444Status reports the RustShine Pro color upgrade's state: active
	// is whether the current/most recent session actually negotiated 4:4:4
	// chroma, available is whether this host could offer it right now
	// (hardware AND license tier). Always (false, false) on Sunshine.
	Color444Status() (active bool, available bool)
	// HdrStatus mirrors Color444Status's shape, for the RustShine HDR color
	// upgrade -- unlike Color444Status, available is hardware-only: HDR is
	// free-tier, no license check.
	HdrStatus() (active bool, available bool)
	// PyroWaveColorStatus reports whether 4:4:4 (Pro) and HDR can be offered
	// with PyroWave (Linux and macOS hosts take them on any GPU they encode on;
	// Windows hosts encode 8-bit 4:2:0) -- independent of the HEVC encoder's
	// Color444Status/HdrStatus.
	PyroWaveColorStatus() (color444 bool, hdr bool)
	// VirtualDisplayPrimary / SetVirtualDisplayPrimary: whether the virtual
	// monitor is made the primary display; see
	// streamhost.Backend.SetVirtualDisplayPrimary.
	VirtualDisplayPrimary() bool
	SetVirtualDisplayPrimary(primary bool) (live bool, err error)
	// SetVirtualDisplayExclusive saves whether the next virtual monitor
	// switches every other monitor off while it exists (back on when it goes
	// away). Backends without it do nothing.
	SetVirtualDisplayExclusive(exclusive bool) error
	// ClientSessionEnded: the client says its stream is over (it disconnects or quits).
	ClientSessionEnded()
	// ClientSessionStarting: the client is about to start its stream; returns once the
	// virtual monitor (when streamed) is up, or after timeout.
	ClientSessionStarting(timeout time.Duration)
	// VirtualDisplaySupported reports whether the current stream backend
	// supports native virtual displays.
	VirtualDisplaySupported() bool
	// RawHIDSupported reports whether the active stream backend understands
	// LiSendRawHidEvent -- see app.App.RawHIDSupported's doc comment.
	RawHIDSupported() bool
	AudioSinks() ([]AudioSink, error)
	CurrentAudioSink() (string, error)
	SetAudioSink(sink string) error
	TailscaleStatus() *TailscaleStatusInfo
	RegisterTailscale(ctx context.Context, authKey, hostname string) (*TailscaleStatusInfo, error)
	// Clipboard returns the agent's clipboard sync manager, or nil if
	// clipboard sync isn't available.
	Clipboard() *clipboard.Manager
	// ClipboardMaxBytes returns the configured per-transfer size cap for
	// clipboard image/file payloads (0 means "use the built-in default").
	ClipboardMaxBytes() int64
}

type Server struct {
	app      Application
	upgrader websocket.Upgrader
	// benchLoad samples host CPU/GPU load during a benchmark run.
	benchLoad hostload.Sampler

	// keyMu guards masterKey — see SetMasterKey.
	keyMu        sync.RWMutex
	masterKey    []byte
	sunshinePort int
	sec          *SecurityMiddleware

	usb *usbpass.Service

	// selfHTTPPort is this agent's own plain-HTTP listen port (cfg.HTTPPort,
	// wired via SetSelfHTTPPort) -- StartUSBPassBridge's "API" preamble
	// dials 127.0.0.1:<selfHTTPPort> to relay the browser web client's
	// WebRTC-tunneled /api/*+/v1/sync/* traffic back into this same
	// process's own Routes() handler, the same way usbPassBridgeAttach
	// dials the USB broker's port. Always the plain-HTTP listener, never
	// TLSPort: the DataChannel this arrived over is already
	// DTLS-encrypted end to end, so a second TLS handshake for a
	// loopback-only hop would just add a self-signed-cert dance for no
	// security benefit.
	selfHTTPPort int

	clipboardBlobs *clipboardBlobStore

	virtMu          sync.Mutex
	virtualDisplays []VideoDeviceInfo

	// mouseHold -- see mouse_hold.go.
	mouseHold mouseHold
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func NewServer(app Application) *Server {
	return NewServerWithAuth(app, nil, 0)
}

func NewServerWithAuth(app Application, masterKey []byte, sunshinePort int) *Server {
	if sunshinePort == 0 {
		sunshinePort = 47990
	}
	return &Server{
		app:            app,
		upgrader:       websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		masterKey:      masterKey,
		sunshinePort:   sunshinePort,
		sec:            NewSecurityMiddleware(masterKey),
		clipboardBlobs: newClipboardBlobStore(),
	}
}

// SetMasterKey swaps in a freshly rotated master key on an already-running
// server (both the HMAC/AES-GCM secret used directly by handlers like Sync,
// and the copy SecurityMiddleware verifies incoming requests against) — so a
// regenerated key takes effect immediately instead of requiring an agent
// restart to reload it from disk.
func (s *Server) SetMasterKey(key []byte) {
	s.keyMu.Lock()
	s.masterKey = key
	s.keyMu.Unlock()
	s.sec.SetMasterKey(key)
}

func (s *Server) currentMasterKey() []byte {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	return s.masterKey
}

func (s *Server) Routes() http.Handler {
	sec := s.sec
	mux := http.NewServeMux()

	// --- Public endpoints (no HMAC required) ---
	mux.HandleFunc("/api/healthz", sec.Public(s.health))
	mux.HandleFunc("/api/auth/qr/link", sec.Public(s.AuthQRLink))

	// --- HMAC-authenticated, PIN rate limit ---
	mux.HandleFunc("/api/moonlight/pin", sec.LimitPIN(s.MoonlightPIN))

	// --- AES-GCM auth only (no HMAC, rate-limited) ---
	mux.HandleFunc("/api/auth/sync", sec.LimitSync(s.Sync))

	// --- HMAC-authenticated endpoints ---
	mux.HandleFunc("/api/status", sec.LimitPolling(s.status))
	mux.HandleFunc("/api/service/status", sec.LimitPolling(s.status))
	mux.HandleFunc("/api/service/stop", sec.LimitPolling(s.serviceStop))
	mux.HandleFunc("/api/service/start", sec.LimitPolling(s.serviceStart))
	mux.HandleFunc("/api/service/restart", sec.LimitPolling(s.serviceRestart))
	mux.HandleFunc("/api/device/start", sec.LimitPolling(s.deviceStart))
	mux.HandleFunc("/api/device/stop", sec.LimitPolling(s.deviceStop))
	mux.HandleFunc("/api/device/info", sec.LimitPolling(s.deviceInfo))
	mux.HandleFunc("/api/device/status", sec.LimitPolling(s.deviceStatus))
	mux.HandleFunc("/api/drives/local", sec.LimitPolling(s.localDrives))
	mux.HandleFunc("/api/iso/space", sec.LimitPolling(s.isoSpace))
	mux.HandleFunc("/api/backup/get_snapshots", sec.LimitPolling(s.backupGetSnapshots))
	// Audio device selection: real sinks (pactl), applied to Sunshine's own
	// audio_sink config — the agent never captures audio itself.
	mux.HandleFunc("/api/audio/info", sec.LimitPolling(s.audioInfo))
	mux.HandleFunc("/api/audio/devices", sec.LimitPolling(s.audioDevices))
	mux.HandleFunc("/api/audio/start", sec.LimitPolling(s.audioStart))
	mux.HandleFunc("/api/audio/stop", sec.LimitPolling(s.audioStop))
	// Hardware-only features (SD/eMMC storage, on-device scripts) that this
	// software agent doesn't implement — stubbed as "nothing here" rather
	// than 404 so client polling doesn't treat them as protocol errors.
	mux.HandleFunc("/api/storage/status", sec.LimitPolling(s.storageStatus))
	mux.HandleFunc("/api/scripts/list", sec.LimitPolling(s.scriptsList))
	mux.HandleFunc("/api/scripts/status", sec.LimitPolling(s.scriptsStatus))
	mux.HandleFunc("/api/auth/tailscale/status", sec.LimitPolling(s.tailscaleStatus))
	mux.HandleFunc("/api/auth/tailscale/register", sec.LimitPolling(s.tailscaleRegister))
	mux.HandleFunc("/api/keyboard", sec.LimitRealtime(s.keyboard))
	mux.HandleFunc("/api/keyboard/layout", sec.LimitRealtime(s.keyboardLayout))
	mux.HandleFunc("/api/mouse", sec.LimitRealtime(s.mouse))
	mux.HandleFunc("/api/mouse/ws", sec.LimitRealtime(s.mouseWS))
	mux.HandleFunc("/api/clipboard/ws", sec.LimitRealtime(s.clipboardWS))
	mux.HandleFunc("PUT /api/clipboard/blob/{id}", sec.LimitBlobTransfer(s.clipboardBlobPut))
	mux.HandleFunc("GET /api/clipboard/blob/{id}", sec.LimitBlobTransfer(s.clipboardBlobGet))
	mux.HandleFunc("/api/video/info", sec.LimitPolling(s.videoInfo))
	mux.HandleFunc("/api/video/devices", sec.LimitPolling(s.videoDevices))
	mux.HandleFunc("/api/video/set_device", sec.LimitPolling(s.videoSetDevice))
	mux.HandleFunc("/api/video/session_end", sec.LimitPolling(s.videoSessionEnd))
	mux.HandleFunc("/api/video/session_start", sec.LimitPolling(s.videoSessionStart))
	mux.HandleFunc("POST /api/video/virtual_displays", sec.LimitPolling(s.virtualDisplayCreate))
	mux.HandleFunc("DELETE /api/video/virtual_displays/{id}", sec.LimitPolling(s.virtualDisplayDelete))
	mux.HandleFunc("POST /api/video/virtual_displays/{id}/primary", sec.LimitPolling(s.virtualDisplaySetPrimary))
	mux.HandleFunc("POST /api/video/monitors/enabled", sec.LimitPolling(s.monitorSetEnabled))
	mux.HandleFunc("/api/screen", sec.LimitPolling(s.screen))
	mux.HandleFunc("GET /api/bench/status", sec.LimitPolling(s.benchStatus))
	mux.HandleFunc("POST /api/bench/backend", sec.LimitPolling(s.benchBackend))
	mux.HandleFunc("POST /api/bench/monitor", sec.LimitPolling(s.benchMonitor))
	mux.HandleFunc("POST /api/bench/prepare", sec.LimitPolling(s.benchPrepare))
	mux.HandleFunc("POST /api/bench/video/start", sec.LimitPolling(s.benchVideoStart))
	mux.HandleFunc("POST /api/bench/video/stop", sec.LimitPolling(s.benchVideoStop))
	mux.HandleFunc("POST /api/bench/load/start", sec.LimitPolling(s.benchLoadStart))
	mux.HandleFunc("POST /api/bench/load/stop", sec.LimitPolling(s.benchLoadStop))
	mux.HandleFunc("/api/devices", sec.LimitPolling(s.devicesLegacy))
	mux.HandleFunc("/api/pcpanel/leds", sec.LimitPolling(s.leds))
	mux.HandleFunc("/api/pcpanel/button", sec.LimitPolling(s.button))
	// MCP (Model Context Protocol): JSON-RPC 2.0 over this one POST endpoint,
	// same shape and auth as the hardware KVM's own /api/mcp (see mcp.go) --
	// the client's MCP proxy (client/internal/api/mcp_proxy.go) forwards to
	// whichever of the two it's paired with without needing to know which.
	mux.HandleFunc("/api/mcp", sec.LimitPolling(s.mcp))
	mux.HandleFunc("/api/usb/passthrough/status", sec.LimitPolling(s.usbPassthroughStatus))
	mux.HandleFunc("/api/usb/passthrough/install", sec.LimitPolling(s.usbPassthroughInstall))
	mux.HandleFunc("/api/usb/passthrough/session", sec.LimitPolling(s.usbPassthroughSession))
	mux.HandleFunc("/api/usb/passthrough/browser-session", sec.LimitPolling(s.usbPassthroughBrowserSession))
	mux.HandleFunc("/api/usb/passthrough/browser-pen-session", sec.LimitPolling(s.usbPassthroughBrowserPenSession))
	// browser-attach/browser-gamepad/browser-pen: LEGACY plain-WebSocket
	// transport for browser USB/IP passthrough, opened directly by browser
	// JS (`new WebSocket(url)`), which cannot set the custom
	// X-Auth-Signature/X-Auth-Timestamp headers sec.LimitRealtime/
	// LimitPolling verify -- these check the same HMAC via ?ts=&sig= query
	// params themselves (see verifyWSAuth in usb_passthrough_browser.go)
	// instead of going through the shared header-based middleware.
	// browser-attach is shared by both the gamepad and pen paths (it's a
	// plain, device-agnostic relay to the broker's AES port -- see its own
	// doc comment).
	//
	// Superseded by StartUSBPassBridge (usb_passthrough_browser.go), which
	// carries the same traffic over a WebRTC DataChannel on the
	// video/control PeerConnection instead of a separate ws:// connection --
	// see that function's doc comment for why. Kept registered until the
	// DataChannel path is confirmed working end to end live; remove these
	// three lines (and their handlers/verifyWSAuth) once it is.
	mux.HandleFunc("/api/usb/passthrough/browser-attach", s.usbPassthroughBrowserAttach)
	mux.HandleFunc("/api/usb/passthrough/browser-gamepad", s.usbPassthroughBrowserGamepad)
	mux.HandleFunc("/api/usb/passthrough/browser-pen", s.usbPassthroughBrowserPen)

	// Proxy /webrtc/* requests to rustshine's native WebRTC signaling listener (port 8444)
	mux.HandleFunc("/webrtc/", s.webrtcProxy)

	return s.withCORS(s.withLogging(s.withRecovery(mux)))
}

func (s *Server) webrtcProxy(w http.ResponseWriter, r *http.Request) {
	target, err := url.Parse("http://127.0.0.1:8444")
	if err != nil {
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Del("Access-Control-Allow-Origin")
		resp.Header.Del("Access-Control-Allow-Methods")
		resp.Header.Del("Access-Control-Allow-Headers")
		resp.Header.Del("Access-Control-Max-Age")
		return nil
	}
	proxy.ServeHTTP(w, r)
}

// withCORS lets the browser/WASM web client (served from its own origin —
// either a separate static host, or the agent's own /app/ route once that
// lands, which would still count as a distinct origin from the page's point
// of view whenever it's opened via a different scheme/port during
// development) call this API with fetch(). Every other USBridge client
// (desktop, Android, iOS) talks over net/http with no browser CORS
// enforcement at all, so this is purely additive — it doesn't change what
// the HMAC/AES-GCM auth layer already guards. Reflecting the request's
// Origin (rather than a fixed "*") keeps this compatible with credentialed
// fetch() calls in case a later stage needs those; the actual auth secret
// still lives in the HMAC signature/AES-GCM payload, never in a cookie, so
// there's nothing CORS itself needs to protect against here.
func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Auth-Signature, X-Auth-Timestamp, X-USBridge-Video-Trace")
		w.Header().Set("Access-Control-Max-Age", "600")

		// Private Network Access (PNA) requirement for Chrome:
		// If the browser preflights a private network request, it sends this header.
		if r.Header.Get("Access-Control-Request-Private-Network") == "true" {
			w.Header().Set("Access-Control-Allow-Private-Network", "true")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				log.Printf("🔥 [api] PANIC in %s %s: %v", r.Method, r.URL.Path, err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *loggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return h.Hijack()
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		traceID := strings.TrimSpace(r.Header.Get("X-USBridge-Video-Trace"))
		if traceID != "" {
			log.Printf("[http] -> %s %s from=%s trace=%s", r.Method, r.URL.RequestURI(), r.RemoteAddr, traceID)
		} else {
			log.Printf("[http] -> %s %s from=%s", r.Method, r.URL.RequestURI(), r.RemoteAddr)
		}
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		if traceID != "" {
			log.Printf("[http] <- %s %s status=%d dur=%s trace=%s", r.Method, r.URL.RequestURI(), lrw.status, time.Since(start).Round(time.Millisecond), traceID)
		} else {
			log.Printf("[http] <- %s %s status=%d dur=%s", r.Method, r.URL.RequestURI(), lrw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func (s *Server) respond(w http.ResponseWriter, status int, payload APIResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) ok(w http.ResponseWriter, message string, data interface{}) {
	s.respond(w, http.StatusOK, APIResponse{Success: true, Message: message, Data: data})
}

func (s *Server) fail(w http.ResponseWriter, status int, message string, err error) {
	details := ""
	if err != nil {
		details = err.Error()
	}
	s.respond(w, status, APIResponse{Success: false, Error: message, Details: details})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "OK", map[string]any{"status": "ok", "timestamp": time.Now().Unix()})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "status", s.app.Status())
}

func (s *Server) serviceStop(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] service_stop")
	_ = s.app.ClearDevices()
	s.ok(w, "service_stopped", nil)
}

func (s *Server) serviceStart(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] service_start")
	s.ok(w, "service_started", nil)
}

func (s *Server) serviceRestart(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] service_restart")
	_ = s.app.ClearDevices()
	s.ok(w, "service_restarted", nil)
}

func (s *Server) deviceStart(w http.ResponseWriter, r *http.Request) {
	devices, err := decodeDeviceStart(r)
	if err != nil {
		log.Printf("[api] device_start invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	devices = filterDevices(devices)
	log.Printf("[api] device_start count=%d", len(devices))
	if err := s.app.ReplaceDevices(devices); err != nil {
		log.Printf("[api] device_start failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "device_start_failed", err)
		return
	}
	s.ok(w, "devices_started", map[string]any{"count": len(devices)})
}

func (s *Server) deviceStop(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] device_stop")
	if err := s.app.ClearDevices(); err != nil {
		log.Printf("[api] device_stop failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "device_stop_failed", err)
		return
	}
	s.ok(w, "devices_stopped", nil)
}

func (s *Server) deviceInfo(w http.ResponseWriter, r *http.Request) {
	info := s.app.DeviceInfo()
	types := make([]string, 0, len(info.Devices))
	for _, device := range info.Devices {
		types = append(types, fmt.Sprintf("%s:%s", device.Device, device.Type))
	}
	log.Printf("[api] device_info count=%d devices=%s", len(info.Devices), strings.Join(types, ","))
	s.ok(w, "device_info", info)
}

func (s *Server) deviceStatus(w http.ResponseWriter, r *http.Request) {
	info := s.app.DeviceInfo()
	names := make([]string, 0, len(info.Devices))
	for _, d := range info.Devices {
		names = append(names, d.Device)
	}
	s.ok(w, "device_status", MountDriveStatus{
		Available:         true,
		UnmountAvailable:  len(names) > 0,
		Version:           "software",
		ConnectedDevices:  names,
		KeyboardAvailable: true,
		RNDISAvailable:    false,
	})
}

func (s *Server) localDrives(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "local_drives", map[string]any{"drives": []any{}, "count": 0, "paths": []string{}})
}

func (s *Server) isoSpace(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "iso_space", map[string]any{
		"total_space":     0,
		"used_space":      0,
		"available_space": 0,
		"total_space_gb":  "0",
		"used_space_gb":   "0",
		"available_gb":    "0",
		"used_percent":    0,
		"iso_directory":   "",
	})
}

func (s *Server) backupGetSnapshots(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] backup_get_snapshots")
	s.ok(w, "snapshots", map[string]any{
		"count":     0,
		"total":     0,
		"snapshots": []any{},
	})
}

func (s *Server) audioInfo(w http.ResponseWriter, r *http.Request) {
	sink, err := s.app.CurrentAudioSink()
	if err != nil {
		s.ok(w, "audio_info", map[string]any{"streaming": false, "device_path": "", "device_name": "", "muted": false})
		return
	}

	deviceName := sink
	if sinks, err := s.app.AudioSinks(); err == nil {
		for _, sk := range sinks {
			if sk.Name == sink {
				deviceName = sk.Description
				break
			}
		}
	}

	// The streamer always streams this sink's sound (it captures its
	// monitor), so it counts as the selected one: the client's Audio card
	// marks a device only while "streaming" is set.
	s.ok(w, "audio_info", map[string]any{
		"streaming":   sink != "",
		"device_path": sink,
		"device_name": deviceName,
		"muted":       false,
	})
}

func (s *Server) audioDevices(w http.ResponseWriter, r *http.Request) {
	sinks, err := s.app.AudioSinks()
	if err != nil {
		log.Printf("[api] audio_devices: %v", err)
		s.ok(w, "audio_devices", map[string]any{"devices": []any{}, "count": 0})
		return
	}
	devices := make([]map[string]any, 0, len(sinks))
	for _, sk := range sinks {
		devices = append(devices, map[string]any{
			"name":        sk.Description,
			"path":        sk.Name,
			"connected":   true,
			"description": sk.Description,
		})
	}
	s.ok(w, "audio_devices", map[string]any{"devices": devices, "count": len(devices)})
}

func (s *Server) audioStart(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DevicePath string `json:"device_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api] audio_start invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	log.Printf("[api] audio_start device=%s", req.DevicePath)
	if err := s.app.SetAudioSink(req.DevicePath); err != nil {
		log.Printf("[api] audio_start failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "audio_start_failed", err)
		return
	}
	s.ok(w, "audio_started", nil)
}

func (s *Server) audioStop(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] audio_stop (reset to system default sink)")
	if err := s.app.SetAudioSink(""); err != nil {
		log.Printf("[api] audio_stop failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "audio_stop_failed", err)
		return
	}
	s.ok(w, "audio_stopped", nil)
}

func (s *Server) storageStatus(w http.ResponseWriter, r *http.Request) {
	empty := map[string]any{
		"total": 0, "used": 0, "free": 0, "percent": 0,
		"total_human": "0", "used_human": "0", "free_human": "0",
		"mounted": false,
	}
	s.ok(w, "storage_status", map[string]any{"sdcard": empty, "emmc": empty})
}

func (s *Server) scriptsList(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "scripts", []any{})
}

func (s *Server) scriptsStatus(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "script_status", []any{})
}

func (s *Server) tailscaleStatus(w http.ResponseWriter, r *http.Request) {
	status := s.app.TailscaleStatus()
	if status == nil {
		status = &TailscaleStatusInfo{Backend: "unavailable"}
	}
	s.ok(w, "tailscale_status", status)
}

func (s *Server) tailscaleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AuthKey  string `json:"auth_key"`
		Hostname string `json:"hostname"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api] tailscale_register invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	log.Printf("[api] tailscale_register hostname=%s auth_key_present=%v", req.Hostname, req.AuthKey != "")
	status, err := s.app.RegisterTailscale(r.Context(), req.AuthKey, req.Hostname)
	if err != nil {
		log.Printf("[api] tailscale_register failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "tailscale_register_failed", err)
		return
	}
	s.ok(w, "tailscale_registered", status)
}

func (s *Server) keyboard(w http.ResponseWriter, r *http.Request) {
	var req KeyboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api] keyboard invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	log.Printf("[api] keyboard action=%s", req.Action)

	if badReq, err := s.applyKeyboard(req); badReq != "" {
		s.fail(w, http.StatusBadRequest, badReq, nil)
		return
	} else if err != nil {
		log.Printf("[api] keyboard failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "keyboard_failed", err)
		return
	}
	s.ok(w, "keyboard_ok", nil)
}

func (s *Server) mouse(w http.ResponseWriter, r *http.Request) {
	var req MouseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api] mouse invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	log.Printf("[api] mouse action=%s", req.Action)

	owner := nextMouseOwner()
	if s.noteMouse(owner, req) {
		s.releaseHTTPMouseLater(owner)
	}
	if err := s.applyMouse(req); err != nil {
		log.Printf("[api] mouse failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "mouse_failed", err)
		return
	}
	s.ok(w, "mouse_ok", MouseResponseData{})
}

const (
	wsReadTimeout  = 90 * time.Second
	wsPingInterval = 25 * time.Second
)

func (s *Server) mouseWS(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] mouse_ws incoming request from %s", r.RemoteAddr)
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[api] mouse_ws upgrade failed: %v", err)
		return
	}
	log.Printf("[api] mouse_ws upgraded successfully for %s", r.RemoteAddr)
	defer conn.Close()

	// A new mouse connection is a (re)connect: nothing held by a previous
	// one may carry over. Released on close only if still ours.
	owner := nextMouseOwner()
	s.releaseHeldMouse(0, "new mouse_ws connection")
	defer s.releaseHeldMouse(owner, "mouse_ws closed")

	// lastSeen: any message or pong from the client (unix nanos).
	var lastSeen atomic.Int64
	lastSeen.Store(time.Now().UnixNano())

	// Refresh read deadline on every pong so idle connections survive NAT/Tailscale
	conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	conn.SetPongHandler(func(string) error {
		lastSeen.Store(time.Now().UnixNano())
		conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
		return nil
	})

	var writeMu sync.Mutex
	safePing := func() error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteMessage(websocket.PingMessage, []byte("ping"))
	}
	safeWriteJSON := func(v interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Keepalive ping pusher
	stopPush := make(chan struct{})
	defer close(stopPush)

	// Keepalive pings every wsPingInterval; while this connection holds a
	// button, ping every mouseWSHeldPing instead and treat mouseWSHeldStale
	// of total silence as a dead client: release and close, rather than
	// leaving the host mouse held until wsReadTimeout (90s) expires.
	go func() {
		tick := time.NewTicker(mouseWSHeldPing)
		defer tick.Stop()
		lastPing := time.Now()

		for {
			select {
			case <-stopPush:
				return
			case now := <-tick.C:
				held := s.mouseHeldBy(owner)
				if held && now.Sub(time.Unix(0, lastSeen.Load())) > mouseWSHeldStale {
					log.Printf("[api] mouse_ws silent for >%s with buttons held, releasing and closing", mouseWSHeldStale)
					s.releaseHeldMouse(owner, "mouse_ws went silent")
					conn.Close()
					return
				}
				if !held && now.Sub(lastPing) < wsPingInterval {
					continue
				}
				lastPing = now
				if err := safePing(); err != nil {
					log.Printf("[api] mouse_ws ping failed, closing: %v", err)
					conn.Close()
					return
				}
			}
		}
	}()

	for {
		var req MouseRequest
		if err := conn.ReadJSON(&req); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("[api] mouse_ws read error: %v", err)
			}
			return
		}
		lastSeen.Store(time.Now().UnixNano())
		s.noteMouse(owner, req)
		if err := s.applyMouse(req); err != nil {
			log.Printf("[api] mouse_ws failed action=%s: %v", req.Action, err)
			_ = safeWriteJSON(APIResponse{Success: false, Error: "mouse_failed", Details: err.Error()})
			continue
		}
		_ = safeWriteJSON(APIResponse{Success: true, Message: "ok", Data: MouseResponseData{}})
	}
}

// applyKeyboard dispatches a decoded KeyboardRequest against s.app.Input(),
// shared by the /api/keyboard HTTP handler and the MCP keyboard.send tool
// (see mcp.go) so the two don't drift on validation. badReq is a non-empty
// APIResponse.Error code when the request itself was malformed (missing a
// required field for its action, or an unrecognized action) -- distinct
// from err, which is a real Input() failure at the OS level.
func (s *Server) applyKeyboard(req KeyboardRequest) (badReq string, err error) {
	switch req.Action {
	case "key":
		if req.KeyCode == nil {
			return "missing_key_code", nil
		}
		return "", s.app.Input().Key(*req.KeyCode)
	case "combo":
		if req.KeyCode == nil || req.Modifiers == nil {
			return "missing_combo_fields", nil
		}
		return "", s.app.Input().Combo(*req.Modifiers, *req.KeyCode)
	case "text":
		if req.Text == nil {
			return "missing_text", nil
		}
		return "", s.app.Input().Text(*req.Text)
	default:
		return "unsupported_action", nil
	}
}

func (s *Server) applyMouse(req MouseRequest) error {
	switch req.Action {
	case "move":
		return s.app.Input().MouseMove(ptrInt8(req.DX), ptrInt8(req.DY))
	case "click":
		return s.app.Input().MouseClick(ptrUint8(req.Button))
	case "scroll":
		return s.app.Input().MouseScroll(ptrInt8(req.Scroll))
	case "action":
		return s.app.Input().MouseAction(ptrUint8(req.Button), ptrInt8(req.DX), ptrInt8(req.DY), ptrInt8(req.Scroll))
	case "touch", "touch_position", "absolute_event", "absolute":
		// "absolute" was declared in the MCP mouse.action tool schema
		// (mcp.go) but never handled here -- it fell through to default and
		// silently no-op'd, an advertised action that couldn't actually do
		// anything. Same handler as the other three legacy names, which
		// already take x/y on this same raw 0..32767 axis.
		return s.app.Input().AbsoluteEvent(ptrUint8(req.ButtonState), uint16(ptrInt(req.X)), uint16(ptrInt(req.Y)), ptrInt8(req.Scroll))
	default:
		return nil
	}
}

func (s *Server) videoInfo(w http.ResponseWriter, r *http.Request) {
	devices := s.app.VideoDevices()
	requested := strings.TrimSpace(r.URL.Query().Get("device"))

	var selected *VideoDeviceInfo
	for i := range devices {
		if devices[i].Path == requested {
			selected = &devices[i]
			break
		}
	}
	if selected == nil && strings.HasPrefix(requested, "virtual:") {
		// The virtual display isn't among the captured monitors (it is up
		// only for a session); answering with the first physical monitor's
		// modes offered a 4K virtual display 1024x768 and below.
		if vd, ok := virtualDeviceInfo(requested); ok {
			selected = &vd
		}
	}
	if selected == nil && len(devices) > 0 {
		selected = &devices[0]
	}

	devicePath := "desktop"
	width, height, fps := 1920, 1080, 60
	var modes []VideoCaptureMode
	if selected != nil {
		devicePath = selected.Path
		modes = selected.SupportedModes
		if len(modes) > 0 {
			width, height = modes[0].Width, modes[0].Height
			if n := len(modes[0].FPS); n > 0 {
				fps = modes[0].FPS[n-1]
			}
		}
	} else if dw, dh, ok := display.MaxResolution(); ok {
		// No physical capture device (software agent streaming the whole
		// desktop) — 1920x1080 above is just a last-resort guess. Query the
		// host's actual connected display instead, so a client that doesn't
		// explicitly pick a resolution defaults to the real maximum
		// (e.g. a 4K monitor) rather than always being capped at 1080p.
		width, height = dw, dh
	}

	moonlightHost := s.app.SunshineStreamHost()
	sunshinePort := s.app.SunshineAdminPort()
	color444Active, color444Available := s.app.Color444Status()
	hdrActive, hdrAvailable := s.app.HdrStatus()
	pyroWave444Available, pyroWaveHdrAvailable := s.app.PyroWaveColorStatus()
	currentCodec := s.app.CurrentVideoCodec()
	supportedCodecs := s.app.SupportedVideoCodecs()
	log.Printf("🎯 [CODEC-TRACE] GET /api/video/info device=%q -> encoding=%q supported=%v", devicePath, currentCodec, supportedCodecs)
	s.ok(w, "video_info", map[string]any{
		"device":            devicePath,
		"width":             width,
		"height":            height,
		"fps":               fps,
		"mode":              "moonlight",
		"transport":         "moonlight",
		"encoding":          currentCodec,
		"streaming":         false,
		"capture_modes":     modes,
		"supported_modes":   videoCodecModes(supportedCodecs),
		"available_devices": devices,
		"moonlight_host":    moonlightHost,
		"sunshine_port":     sunshinePort,
		// RustShine Pro's $8/mo color upgrade -- see
		// Application.Color444Status's doc comment. "active" reflects the
		// most recently started session (post-fallback truth, same as
		// "encoding"); "available" is whether the popup's 4:4:4 checkbox
		// should even be shown/enabled before the user has started
		// streaming at all.
		"color_444_active":    color444Active,
		"color_444_available": color444Available,
		// RustShine's HDR color upgrade -- mirrors color_444_active/
		// color_444_available exactly, see Application.HdrStatus's doc
		// comment.
		"hdr_active":    hdrActive,
		"hdr_available": hdrAvailable,
		// The same two upgrades with PyroWave (a Linux or macOS host's
		// PyroWave GPU; 4:4:4 still license-gated) -- see
		// Application.PyroWaveColorStatus.
		"pyrowave_color_444_available": pyroWave444Available,
		"pyrowave_hdr_available":       pyroWaveHdrAvailable,
		"virtual_display_supported":    s.app.VirtualDisplaySupported(),
	})
}

// videoCodecModeInfo holds the display metadata for each Moonlight-supported
// codec; videoCodecModes filters this against what the host can actually
// encode (from Application.SupportedVideoCodecs) so the client never offers
// a codec the hardware can't produce — e.g. AV1 on GPUs without an AV1
// encoder, which Sunshine's own capability probe reports as unsupported.
var videoCodecModeInfo = []map[string]string{
	{"id": "h264", "name": "H.264", "description": "H.264 (AVC) Hardware Encoding", "transport": "rtp", "encoding": "h264"},
	{"id": "h265", "name": "H.265", "description": "H.265 (HEVC) Hardware Encoding", "transport": "rtp", "encoding": "h265"},
	{"id": "av1", "name": "AV1", "description": "AV1 Hardware Encoding", "transport": "rtp", "encoding": "av1"},
	{"id": "pyrowave", "name": "PyroWave", "description": "PyroWave intra-only wavelet codec for a fast local link", "transport": "rtp", "encoding": "pyrowave"},
}

func videoCodecModes(supported []string) []map[string]string {
	supportedSet := make(map[string]bool, len(supported))
	for _, codec := range supported {
		supportedSet[codec] = true
	}
	modes := make([]map[string]string, 0, len(videoCodecModeInfo))
	for _, mode := range videoCodecModeInfo {
		if supportedSet[mode["encoding"]] {
			modes = append(modes, mode)
		}
	}
	if len(modes) == 0 {
		// Never return an empty list — h264 is Sunshine's protocol-mandated
		// baseline, so this only happens if the capability query itself
		// failed in a way that still returned a codec list without h264 in
		// it (shouldn't happen given SupportedVideoCodecs' own fallback, but
		// guard the client's UI from an empty codec picker regardless).
		modes = append(modes, videoCodecModeInfo[0])
	}
	return modes
}

func isLoopbackHost(host string) bool {

	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// isLoopbackRequest reports whether r reached this server from the loopback
// interface itself, regardless of what interface the server is actually
// bound to (ListenHost defaults to 0.0.0.0, and the same handler is also
// served over the Tailscale/tsnet listener — see app.go). It additionally
// refuses any request carrying an Origin header, since that always means
// the caller is browser JS (native HTTP clients never set it) — a page
// open in the user's own browser could otherwise read a loopback-bound
// response just as easily as a LAN attacker could read a 0.0.0.0-bound one.
// Used to gate handlers whose response must never leave the machine that
// owns it, such as AuthQRLink (see sync.go), which hands back the master
// key — the sole credential for every other endpoint in this API.
func isLoopbackRequest(r *http.Request) bool {
	if r.Header.Get("Origin") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return isLoopbackHost(host)
}

func (s *Server) screen(w http.ResponseWriter, r *http.Request) {
	snap, err := s.app.Screen().Snapshot()
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "screen_failed", err)
		return
	}
	s.ok(w, "screen", snap)
}

func (s *Server) devicesLegacy(w http.ResponseWriter, r *http.Request) {
	info := s.app.DeviceInfo()
	items := make([]LegacyDeviceInfo, 0, len(info.Devices))
	for _, d := range info.Devices {
		items = append(items, LegacyDeviceInfo{Name: d.Device, Path: d.Server, Connected: true, Description: d.Type})
	}
	s.ok(w, "devices", items)
}

func (s *Server) leds(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "leds", map[string]bool{"power": false, "hdd": false})
}

func (s *Server) button(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "button", nil)
}

func ptrInt8(v *int8) int8 {
	if v == nil {
		return 0
	}
	return *v
}

func ptrUint8(v *uint8) uint8 {
	if v == nil {
		return 0
	}
	return *v
}

func ptrInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func decodeDeviceStart(r *http.Request) ([]DeviceRequest, error) {
	defer r.Body.Close()

	var body bytes.Buffer
	if _, err := body.ReadFrom(r.Body); err != nil {
		return nil, err
	}

	data := bytes.TrimSpace(body.Bytes())
	if len(data) == 0 {
		return nil, nil
	}

	var batch DeviceStartBatchRequest
	if err := json.Unmarshal(data, &batch); err == nil && batch.Devices != nil {
		return batch.Devices, nil
	}

	var devices []DeviceRequest
	if err := json.Unmarshal(data, &devices); err == nil {
		return devices, nil
	}

	return nil, json.Unmarshal(data, &batch)
}

func filterDevices(devices []DeviceRequest) []DeviceRequest {
	out := make([]DeviceRequest, 0, len(devices))
	for _, device := range devices {
		if device.Device == "rndis" {
			continue
		}
		out = append(out, device)
	}
	return out
}

func (s *Server) videoDevices(w http.ResponseWriter, r *http.Request) {
	devices := markVkmsConnectors(s.app.VideoDevices())

	if s.app.VirtualDisplaySupported() {
		s.virtMu.Lock()
		// The pinned virtual output survives an agent restart in the config
		// only; rebuild its entry from there so it stays listed (and
		// deletable) instead of surfacing as a raw vkms connector.
		if cur := s.app.SunshineOutputName(); strings.HasPrefix(cur, "virtual:") {
			s.addVirtualEntryLocked(cur)
		}
		primary := s.app.VirtualDisplayPrimary()
		for _, vd := range s.virtualDisplays {
			vd.Primary = &primary
			devices = append(devices, vd)
		}
		s.virtMu.Unlock()
	}

	s.ok(w, "video devices list", map[string]any{"devices": devices, "count": len(devices)})
}

func (s *Server) virtualDisplayCreate(w http.ResponseWriter, r *http.Request) {
	if !s.app.VirtualDisplaySupported() {
		s.fail(w, http.StatusBadRequest, "virtual_displays_unsupported", nil)
		return
	}
	var req struct {
		Width  int `json:"width"`
		Height int `json:"height"`
		FPS    int `json:"fps"`
		// Primary: make it the primary display (taskbar and new windows go
		// there). Omitted means yes -- a remote user can't see the
		// physical screens.
		Primary *bool `json:"primary,omitempty"`
		// Exclusive: switch every other monitor off while it exists (back
		// on when it goes away). Omitted means no.
		Exclusive bool `json:"exclusive,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	if req.Width <= 0 || req.Height <= 0 || req.FPS <= 0 {
		s.fail(w, http.StatusBadRequest, "invalid_resolution", nil)
		return
	}

	path := fmt.Sprintf("virtual:%dx%d@%d", req.Width, req.Height, req.FPS)

	primary := req.Primary == nil || *req.Primary
	if _, err := s.app.SetVirtualDisplayPrimary(primary); err != nil {
		log.Printf("[api] virtual display create: primary=%v not applied: %v", primary, err)
	}
	if err := s.app.SetVirtualDisplayExclusive(req.Exclusive); err != nil {
		log.Printf("[api] virtual display create: exclusive=%v not saved: %v", req.Exclusive, err)
	}

	s.virtMu.Lock()
	vd := s.addVirtualEntryLocked(path)
	s.virtMu.Unlock()

	s.ok(w, "virtual_display_created", vd)
}

// monitorSetEnabled switches a host monitor (physical, or the MttVDD virtual
// monitor) on or off, like Windows' "Disconnect this display": its display path
// is dropped or added, no elevation needed (displaypower). Switching off the
// only active display is refused. Windows only.
// videoSessionEnd is the client saying its stream is over (it disconnects or quits):
// the agent then puts the host's monitors back at once instead of waiting to see the
// session gone (app.virtualOthersWatchdog).
func (s *Server) videoSessionEnd(w http.ResponseWriter, r *http.Request) {
	s.app.ClientSessionEnded()
	s.ok(w, "session_end", nil)
}

// videoSessionStart is the client saying it is about to start its stream: on a
// Windows host the agent brings the virtual monitor up first, when it is the one
// streamed, so the stream host captures it from the first frame.
func (s *Server) videoSessionStart(w http.ResponseWriter, r *http.Request) {
	s.app.ClientSessionStarting(10 * time.Second)
	s.ok(w, "session_start", nil)
}

func (s *Server) monitorSetEnabled(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MonitorID string `json:"monitor_id"`
		Enabled   bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.MonitorID == "" {
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	err := displaypower.SetEnabledInSession(req.MonitorID, req.Enabled)
	switch {
	case errors.Is(err, displaypower.ErrUnsupported):
		s.fail(w, http.StatusNotImplemented, "monitor_power_unsupported", err)
	case errors.Is(err, displaypower.ErrNotFound):
		s.fail(w, http.StatusNotFound, "monitor_not_found", err)
	case errors.Is(err, displaypower.ErrLastDisplay):
		s.fail(w, http.StatusConflict, "monitor_last_display", err)
	case err != nil:
		s.fail(w, http.StatusInternalServerError, "monitor_power_failed", err)
	default:
		log.Printf("[api] monitor %s enabled=%v", req.MonitorID, req.Enabled)
		s.ok(w, "monitor_enabled", map[string]any{"monitor_id": req.MonitorID, "enabled": req.Enabled})
	}
}

// virtualDisplaySetPrimary turns "the virtual monitor is the primary display"
// on or off: saved for the next start and applied to the live virtual monitor
// right away (no stream restart). The choice covers every virtual display
// entry -- only one exists at a time.
func (s *Server) virtualDisplaySetPrimary(w http.ResponseWriter, r *http.Request) {
	if !s.app.VirtualDisplaySupported() {
		s.fail(w, http.StatusBadRequest, "virtual_displays_unsupported", nil)
		return
	}
	var req struct {
		Primary bool `json:"primary"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	live, err := s.app.SetVirtualDisplayPrimary(req.Primary)
	if err != nil {
		s.fail(w, http.StatusInternalServerError, "virtual_display_primary_failed", err)
		return
	}
	s.ok(w, "virtual_display_primary", map[string]any{"primary": req.Primary, "applied_live": live})
}

func (s *Server) virtualDisplayDelete(w http.ResponseWriter, r *http.Request) {
	id, err := url.PathUnescape(r.PathValue("id"))
	if err != nil {
		id = r.PathValue("id")
	}

	cur := s.app.SunshineOutputName()
	curVirtual := strings.HasPrefix(cur, "virtual:")

	// id is either one of the "virtual:WxH@FPS" entries or the row of the live
	// vkms connector. Only the display that is actually running is torn down;
	// other listed virtual displays are just saved specs and stay untouched.
	isConnector := false
	for _, d := range s.app.VideoDevices() {
		if d.Path == id && isVkmsConnector(d) {
			isConnector = true
			break
		}
	}
	teardown := isConnector || (curVirtual && cur == id)

	s.virtMu.Lock()
	drop := map[string]bool{id: true}
	if isConnector && curVirtual {
		drop[cur] = true
	}
	filtered := make([]VideoDeviceInfo, 0, len(s.virtualDisplays))
	for _, vd := range s.virtualDisplays {
		if !drop[vd.Path] {
			filtered = append(filtered, vd)
		}
	}
	s.virtualDisplays = filtered
	s.virtMu.Unlock()

	if teardown {
		// Back to a physical output (restarts the stream host) whenever the
		// pin points at the virtual display, either as the "virtual:" spec or
		// as the vkms connector picked from the DRM list ("cardN|Virtual-M").
		if curVirtual || strings.Contains(cur, "Virtual-") {
			// Always pin an explicit physical output: clearing only the
			// virtual_display key leaves adapter_name/kms_connector on the
			// vkms connector picked earlier, so the stream host would come
			// straight back to it.
			back := s.firstPhysicalOutput(curVirtual || strings.Contains(cur, "|"))
			if err := s.app.SetSunshineOutputName(back); err != nil {
				log.Printf("[api] virtual display delete: unpin failed: %v", err)
				s.fail(w, http.StatusInternalServerError, "virtual_display_unpin_failed", err)
				return
			}
		}
		if err := vdisplay.Unload(); err != nil {
			log.Printf("[api] virtual display delete: %v", err)
		}
	}

	s.ok(w, "virtual_display_deleted", nil)
}

var drmConnectorNameRe = regexp.MustCompile(`^(card\d+)-(.+)$`)

// firstPhysicalOutput returns the output_name value of the first non-virtual
// DRM device, or "" when none is listed. compound selects RustShine's
// "/dev/dri/cardN|CONNECTOR" form (by name, so it does not depend on index
// order once the vkms connector disappears) instead of the numeric index.
func (s *Server) firstPhysicalOutput(compound bool) string {
	for _, d := range s.app.VideoDevices() {
		if d.Bus == "virtual" || isVkmsConnector(d) {
			continue
		}
		if compound {
			if m := drmConnectorNameRe.FindStringSubmatch(d.Name); m != nil {
				return "/dev/dri/" + m[1] + "|" + m[2]
			}
		}
		return stripDevicePrefix(d.Path)
	}
	return ""
}

// stripDevicePrefix turns a VideoDeviceInfo.Path ("drm:1", "raw:...",
// "winid:...", "display:0") into the bare value SetSunshineOutputName wants.
func stripDevicePrefix(p string) string {
	for _, prefix := range []string{"drm:", "winid:", "display:", "raw:"} {
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	return p
}

// addVirtualEntryLocked adds the "virtual:WxH@FPS" entry if missing and
// returns it. Caller holds virtMu.
func (s *Server) addVirtualEntryLocked(path string) VideoDeviceInfo {
	for _, existing := range s.virtualDisplays {
		if existing.Path == path {
			return existing
		}
	}
	vd, ok := virtualDeviceInfo(path)
	if !ok {
		vd = VideoDeviceInfo{
			Name:      "Virtual Display (" + strings.TrimPrefix(path, "virtual:") + ")",
			Path:      path,
			Bus:       "virtual",
			Connected: true,
		}
	}
	s.virtualDisplays = append(s.virtualDisplays, vd)
	return vd
}

// virtualStreamResolutions are the stream sizes offered below a virtual
// display's own (the client downscales to them), highest first.
var virtualStreamResolutions = []struct{ w, h int }{
	{3840, 2160}, {3440, 1440}, {2560, 1600}, {2560, 1440}, {1920, 1200}, {1920, 1080}, {1600, 900}, {1280, 800}, {1280, 720},
}

var virtualStreamFPS = []int{30, 60, 90, 120, 144, 165, 240}

// virtualDeviceInfo is the device entry of a "virtual:WxH@HZ" path, with its
// own mode first and the standard sizes that fit in it after, each at the
// usual rates up to its own.
func virtualDeviceInfo(path string) (VideoDeviceInfo, bool) {
	m, err := displaypower.ParseMode(strings.TrimPrefix(path, "virtual:"))
	if err != nil {
		return VideoDeviceInfo{}, false
	}
	// The display refreshes at its own rate: nothing above it, that one last
	// (video/info's default).
	var fps []int
	for _, f := range virtualStreamFPS {
		if f < m.Hz {
			fps = append(fps, f)
		}
	}
	fps = append(fps, m.Hz)
	modes := []VideoCaptureMode{{Width: m.Width, Height: m.Height, FPS: fps, PixelFormat: "BGRA"}}
	for _, r := range virtualStreamResolutions {
		if r.w <= m.Width && r.h <= m.Height && (r.w != m.Width || r.h != m.Height) {
			modes = append(modes, VideoCaptureMode{Width: r.w, Height: r.h, FPS: fps, PixelFormat: "BGRA"})
		}
	}
	return VideoDeviceInfo{
		Name:           "Virtual Display (" + m.String() + ")",
		Path:           path,
		Bus:            "virtual",
		Connected:      true,
		SupportedModes: modes,
	}, true
}

func isVkmsConnector(d VideoDeviceInfo) bool {
	return strings.Contains(d.Name, "-Virtual-") && !strings.Contains(d.Name, "Writeback")
}

// markVkmsConnectors labels the kernel vkms connector ("cardN-Virtual-M") as a
// virtual display (Bus "virtual") instead of a physical DRM output, so clients
// offer the delete action on it.
func markVkmsConnectors(in []VideoDeviceInfo) []VideoDeviceInfo {
	out := make([]VideoDeviceInfo, len(in))
	copy(out, in)
	for i := range out {
		if out[i].Bus == "drm" && isVkmsConnector(out[i]) {
			out[i].Bus = "virtual"
		}
	}
	return out
}

// videoSetDevice pins Sunshine's capture to the monitor identified by
// "device" (a VideoDeviceInfo.Path, e.g. "drm:1" from /api/video/devices).
// An empty device clears the pin (Sunshine auto-picks).
func (s *Server) videoSetDevice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Device string `json:"device"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Printf("[api] video_set_device invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	// VideoDevices() reports "drm:<index>" on Linux (correlated to Sunshine's
	// own KMS output index — see sunshine.MonitorIndexByName), "winid:<id>"
	// on Windows (Sunshine's own EDID+instance-derived device_id — see
	// sunshine.WindowsDisplayDevices), "display:<index>" as a fallback on
	// Windows/macOS/Linux-X11 before Sunshine has logged its real device list
	// this session (see screen_windows.go, screen_darwin.go,
	// screen_linux_x11.go), or "raw:<value>" for a backend whose OutputName
	// isn't a plain numeric index (e.g. rustshine's "cardPath|connector" —
	// see screen_linux_x11.go's rawOutputName). Sunshine's/rustshine's
	// output_name wants just the bare value in every case, so whichever
	// prefix is present must be stripped — "raw:" specifically must pass
	// its remainder through completely unchanged (no reinterpreting as a
	// numeric index), since it already IS the exact string SetOutputName
	// expects. Previously only "drm:" was handled, so on Windows the
	// literal string "display:0" (or "winid:{...}") was written to
	// output_name verbatim, which Sunshine can't parse and silently falls
	// back to auto-pick — monitor switching had no effect.
	outputName := stripDevicePrefix(req.Device)
	if id, ok := strings.CutPrefix(req.Device, "monitor:"); ok {
		// A monitor listed switched off (screen_windows.go) -- e.g. one a session on
		// the virtual monitor switched off. Its path is the monitor's device path,
		// which no stream host takes (and whose '#' starts a comment in
		// sunshine.conf: Vibepollo got "monitor:\\?\DISPLAY", found nothing and
		// streamed nothing). Switch it on and stream it under the name the stream
		// host lists it by.
		outputName = s.outputNameForMonitor(id)
	}
	// A vkms connector forced off by an earlier delete must be brought back
	// before the stream host looks for it.
	if strings.HasPrefix(outputName, "virtual:") || strings.Contains(outputName, "Virtual-") {
		if err := vdisplay.Revive(); err != nil {
			log.Printf("[api] video_set_device: reviving virtual display: %v", err)
		}
	}
	log.Printf("[api] video_set_device device=%s", req.Device)
	if err := s.app.SetSunshineOutputName(outputName); err != nil {
		log.Printf("[api] video_set_device failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "video_set_device_failed", err)
		return
	}
	s.ok(w, "video_device_set", nil)
}

// outputNameForMonitor switches the monitor with this device path on and returns what
// the stream host selects it by: the path its own device list gives it once it shows
// up there, else (a list the stream host only refreshes on restart, Sunshine's) the
// monitor's name, which Sunshine and Vibepollo map to the display too.
func (s *Server) outputNameForMonitor(id string) string {
	if err := displaypower.SetEnabledInSession(id, true); err != nil {
		log.Printf("[api] video_set_device: switching monitor %s on: %v", id, err)
	}
	name := ""
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(300 * time.Millisecond) {
		on := false
		for _, d := range s.app.VideoDevices() {
			if d.MonitorID != id {
				continue
			}
			if !strings.HasPrefix(d.Path, "monitor:") {
				return stripDevicePrefix(d.Path)
			}
			name = strings.TrimSuffix(d.Name, " (off)")
			// Listed by its device path while on: the stream host has no
			// list of its own, nothing more will show up.
			on = d.Enabled != nil && *d.Enabled
		}
		if on || time.Now().After(deadline) {
			break
		}
	}
	if monitors, err := displaypower.ListInSession(); err == nil {
		for _, m := range monitors {
			if m.ID == id && m.Name != "" {
				name = m.Name
			}
		}
	}
	return name
}

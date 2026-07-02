package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
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
	TailscaleStatus() *TailscaleStatusInfo
	RegisterTailscale(ctx context.Context, authKey, hostname string) (*TailscaleStatusInfo, error)
}

type Server struct {
	app          Application
	upgrader     websocket.Upgrader
	masterKey    []byte
	sunshinePort int
	sec          *SecurityMiddleware
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
		app:          app,
		upgrader:     websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		masterKey:    masterKey,
		sunshinePort: sunshinePort,
		sec:          NewSecurityMiddleware(masterKey),
	}
}

func (s *Server) Routes() http.Handler {
	sec := s.sec
	mux := http.NewServeMux()

	// --- Public endpoints (no HMAC required) ---
	mux.HandleFunc("/api/healthz", sec.Public(s.health))
	mux.HandleFunc("/api/auth/qr/link", sec.Public(s.AuthQRLink))

	// --- HMAC verified when present, allowed unsigned pre-pair ---
	mux.HandleFunc("/api/moonlight/pin", sec.OptionalVerify(s.MoonlightPIN))

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
	// Hardware-only features (SD/eMMC storage, on-device scripts, ALSA audio capture)
	// that this software agent doesn't implement — stubbed as "nothing here" rather
	// than 404 so client polling doesn't treat them as protocol errors.
	mux.HandleFunc("/api/audio/info", sec.LimitPolling(s.audioInfo))
	mux.HandleFunc("/api/audio/devices", sec.LimitPolling(s.audioDevices))
	mux.HandleFunc("/api/storage/status", sec.LimitPolling(s.storageStatus))
	mux.HandleFunc("/api/scripts/list", sec.LimitPolling(s.scriptsList))
	mux.HandleFunc("/api/scripts/status", sec.LimitPolling(s.scriptsStatus))
	mux.HandleFunc("/api/auth/tailscale/status", sec.LimitPolling(s.tailscaleStatus))
	mux.HandleFunc("/api/auth/tailscale/register", sec.LimitPolling(s.tailscaleRegister))
	mux.HandleFunc("/api/keyboard", sec.LimitRealtime(s.keyboard))
	mux.HandleFunc("/api/mouse", sec.LimitRealtime(s.mouse))
	mux.HandleFunc("/api/mouse/ws", sec.LimitRealtime(s.mouseWS))
	mux.HandleFunc("/api/video/info", sec.LimitPolling(s.videoInfo))
	mux.HandleFunc("/api/video/start", sec.LimitPolling(s.videoStart))
	mux.HandleFunc("/api/video/stop", sec.LimitPolling(s.videoStop))
	mux.HandleFunc("/api/video/devices", sec.LimitPolling(s.videoDevices))
	mux.HandleFunc("/api/screen", sec.LimitPolling(s.screen))
	mux.HandleFunc("/api/devices", sec.LimitPolling(s.devicesLegacy))
	mux.HandleFunc("/api/pcpanel/leds", sec.LimitPolling(s.leds))
	mux.HandleFunc("/api/pcpanel/button", sec.LimitPolling(s.button))

	return s.withLogging(s.withRecovery(mux))
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
	s.ok(w, "audio_info", map[string]any{
		"streaming":   false,
		"device_path": "",
		"device_name": "",
		"muted":       false,
	})
}

func (s *Server) audioDevices(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "audio_devices", map[string]any{
		"devices": []any{},
		"count":   0,
	})
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

	var err error
	switch req.Action {
	case "key":
		if req.KeyCode == nil {
			s.fail(w, http.StatusBadRequest, "missing_key_code", nil)
			return
		}
		err = s.app.Input().Key(*req.KeyCode)
	case "combo":
		if req.KeyCode == nil || req.Modifiers == nil {
			s.fail(w, http.StatusBadRequest, "missing_combo_fields", nil)
			return
		}
		err = s.app.Input().Combo(*req.Modifiers, *req.KeyCode)
	case "text":
		if req.Text == nil {
			s.fail(w, http.StatusBadRequest, "missing_text", nil)
			return
		}
		err = s.app.Input().Text(*req.Text)
	default:
		s.fail(w, http.StatusBadRequest, "unsupported_action", nil)
		return
	}

	if err != nil {
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

	// Refresh read deadline on every pong so idle connections survive NAT/Tailscale
	conn.SetReadDeadline(time.Now().Add(wsReadTimeout))
	conn.SetPongHandler(func(string) error {
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

	go func() {
		pingTicker := time.NewTicker(wsPingInterval)
		defer pingTicker.Stop()

		for {
			select {
			case <-stopPush:
				return
			case <-pingTicker.C:
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
		if err := s.applyMouse(req); err != nil {
			log.Printf("[api] mouse_ws failed action=%s: %v", req.Action, err)
			_ = safeWriteJSON(APIResponse{Success: false, Error: "mouse_failed", Details: err.Error()})
			continue
		}
		_ = safeWriteJSON(APIResponse{Success: true, Message: "ok", Data: MouseResponseData{}})
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
	case "touch", "touch_position", "absolute_event":
		return s.app.Input().AbsoluteEvent(ptrUint8(req.ButtonState), uint16(ptrInt(req.X)), uint16(ptrInt(req.Y)), ptrInt8(req.Scroll))
	default:
		return nil
	}
}

// videoInfo/videoStart/videoStop/videoDevices are stubs kept for wire
// compatibility with usbridge_client, which still polls them — the actual
// video/input path is Moonlight/Sunshine (GameStream ports 47984/47989),
// not this REST API. The agent's own former FFmpeg+XDG-portal capture
// pipeline was removed: it ran redundantly alongside Sunshine and triggered
// its own independent portal permission prompt after a Moonlight session
// had already connected successfully.
func (s *Server) videoInfo(w http.ResponseWriter, r *http.Request) {
	s.ok(w, "video_info", map[string]any{
		"streaming": false,
		"transport": "moonlight",
		"encoding":  "h264",
		"mode":      "moonlight",
	})
}

func (s *Server) videoStart(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] video_start (no-op — video is served via Sunshine/Moonlight)")
	s.ok(w, "video_started", nil)
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if xrip := r.Header.Get("X-Real-IP"); xrip != "" {
		if net.ParseIP(xrip) != nil {
			return xrip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return strings.Trim(r.RemoteAddr, "[]")
	}
	return host
}

func isLoopbackHost(host string) bool {

	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) videoStop(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] video_stop (no-op — video is served via Sunshine/Moonlight)")
	s.ok(w, "video_stopped", nil)
}

func (s *Server) videoDevices(w http.ResponseWriter, r *http.Request) {
	// usbridge_client's device-resolution flow (resolvePreferredVideoConfig)
	// errors out and refuses to proceed to Moonlight if this list is empty —
	// even though video is actually served by Sunshine, not through this
	// legacy device-enumeration path. One synthetic "desktop" entry keeps
	// that client-side gate satisfied.
	devices := []map[string]any{
		{
			"name":        "Desktop (Sunshine)",
			"path":        "desktop",
			"connected":   true,
			"description": "Full desktop capture via Sunshine/Moonlight",
		},
	}
	s.ok(w, "video_devices", map[string]any{
		"devices": devices,
		"count":   len(devices),
	})
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

	var legacy []LegacyDeviceStartRequest
	if err := json.Unmarshal(data, &legacy); err == nil {
		out := make([]DeviceRequest, 0, len(legacy))
		for _, item := range legacy {
			out = append(out, DeviceRequest{
				Device:                  item.Device,
				Type:                    item.Type,
				Server:                  item.Server,
				Port:                    item.Port,
				ExportName:              item.ExportName,
				NBDHandshakeEmptyExport: item.NBDHandshakeEmptyExport,
				ReadOnly:                item.ReadOnly,
				VendorID:                item.VendorID,
				ProductID:               item.ProductID,
				ProductName:             item.ProductName,
				Manufacturer:            item.Manufacturer,
				RNDISMode:               item.RNDISMode,
			})
		}
		return out, nil
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

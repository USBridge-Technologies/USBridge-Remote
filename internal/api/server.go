package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
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
	Video() interface {
		Start(VideoStartRequest) error
		Stop() error
		Info() map[string]interface{}
	}
	VideoDevices() []VideoDeviceInfo
}

type Server struct {
	app      Application
	upgrader websocket.Upgrader
}

type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func NewServer(app Application) *Server {
	return &Server{app: app, upgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}}
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/healthz", s.health)
	mux.HandleFunc("/api/status", s.status)
	mux.HandleFunc("/api/service/status", s.status)
	mux.HandleFunc("/api/service/stop", s.serviceStop)
	mux.HandleFunc("/api/service/start", s.serviceStart)
	mux.HandleFunc("/api/service/restart", s.serviceRestart)
	mux.HandleFunc("/api/device/start", s.deviceStart)
	mux.HandleFunc("/api/device/stop", s.deviceStop)
	mux.HandleFunc("/api/device/info", s.deviceInfo)
	mux.HandleFunc("/api/device/status", s.deviceStatus)
	mux.HandleFunc("/api/device/local_drives", s.localDrives)
	mux.HandleFunc("/api/iso/space", s.isoSpace)
	mux.HandleFunc("/api/backup/get_snapshots", s.backupGetSnapshots)
	mux.HandleFunc("/api/keyboard", s.keyboard)
	mux.HandleFunc("/api/mouse", s.mouse)
	mux.HandleFunc("/api/mouse/ws", s.mouseWS)
	mux.HandleFunc("/api/video/info", s.videoInfo)
	mux.HandleFunc("/api/video/start", s.videoStart)
	mux.HandleFunc("/api/video/stop", s.videoStop)
	mux.HandleFunc("/api/video/devices", s.videoDevices)
	mux.HandleFunc("/api/screen", s.screen)
	mux.HandleFunc("/api/devices", s.devicesLegacy)
	mux.HandleFunc("/api/pcpanel/leds", s.leds)
	mux.HandleFunc("/api/pcpanel/button", s.button)
	return s.withLogging(mux)
}

func (w *loggingResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		log.Printf("[http] -> %s %s from=%s", r.Method, r.URL.RequestURI(), r.RemoteAddr)
		lrw := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(lrw, r)
		log.Printf("[http] <- %s %s status=%d dur=%s", r.Method, r.URL.RequestURI(), lrw.status, time.Since(start).Round(time.Millisecond))
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
	_ = s.app.Video().Stop()
	_ = s.app.ClearDevices()
	s.ok(w, "service_stopped", nil)
}

func (s *Server) serviceStart(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] service_start")
	s.ok(w, "service_started", nil)
}

func (s *Server) serviceRestart(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] service_restart")
	_ = s.app.Video().Stop()
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
	s.ok(w, "mouse_ok", nil)
}

func (s *Server) mouseWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	for {
		var req MouseRequest
		if err := conn.ReadJSON(&req); err != nil {
			return
		}
		if err := s.applyMouse(req); err != nil {
			log.Printf("[api] mouse_ws failed action=%s: %v", req.Action, err)
			_ = conn.WriteJSON(APIResponse{Success: false, Error: "mouse_failed", Details: err.Error()})
			continue
		}
		_ = conn.WriteJSON(APIResponse{Success: true, Message: "ok"})
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

func (s *Server) videoInfo(w http.ResponseWriter, r *http.Request) {
	info := enrichVideoInfo(s.app.Video().Info(), s.app.VideoDevices(), r.URL.Query().Get("device"))
	log.Printf("[api] video_info device=%s modes=%d transports=%d streaming=%v", asString(info["device"]), len(asCaptureModes(info["capture_modes"])), len(asTransportModes(info["supported_modes"])), asBool(info["streaming"]))
	s.ok(w, "video_info", info)
}

func (s *Server) videoStart(w http.ResponseWriter, r *http.Request) {
	var req VideoStartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
		log.Printf("[api] video_start invalid_json: %v", err)
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = strings.TrimSpace(host)
		if host != "" && !isLoopbackHost(host) {
			req.ClientHost = host
		}
	}
	log.Printf("[api] video_start width=%d height=%d fps=%d bitrate=%s mode=%s", req.VideoWidth, req.VideoHeight, req.VideoFPS, req.VideoBitrate, req.VideoMode)
	if err := s.app.Video().Start(req); err != nil {
		log.Printf("[api] video_start failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "video_start_failed", err)
		return
	}
	s.ok(w, "video_started", nil)
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) videoStop(w http.ResponseWriter, r *http.Request) {
	log.Printf("[api] video_stop")
	if err := s.app.Video().Stop(); err != nil {
		log.Printf("[api] video_stop failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "video_stop_failed", err)
		return
	}
	s.ok(w, "video_stopped", nil)
}

func (s *Server) videoDevices(w http.ResponseWriter, r *http.Request) {
	devices := s.app.VideoDevices()
	log.Printf("[api] video_devices count=%d", len(devices))
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

func enrichVideoInfo(info map[string]interface{}, devices []VideoDeviceInfo, requestedDevice string) map[string]interface{} {
	if info == nil {
		info = make(map[string]interface{})
	}

	devicePath := strings.TrimSpace(requestedDevice)
	if devicePath == "" && len(devices) > 0 {
		devicePath = devices[0].Path
	}

	width, _ := info["width"].(int)
	height, _ := info["height"].(int)
	fps, _ := info["fps"].(int)
	if width <= 0 {
		width = 1280
	}
	if height <= 0 {
		height = 720
	}
	if fps <= 0 {
		fps = 30
	}

	info["device"] = devicePath
	info["mode"] = firstNonEmptyString(asString(info["mode"]), "h264")
	info["transport"] = firstNonEmptyString(asString(info["transport"]), "rtp")
	info["encoding"] = firstNonEmptyString(asString(info["encoding"]), "h264")
	info["source_format"] = firstNonEmptyString(asString(info["source_format"]), "BGRA")
	info["server_decodes_jpeg"] = true
	info["udp_port"] = firstPositiveInt(info["udp_port"], 55000)
	info["udp_listener_ready"] = true
	info["stream_url"] = fmt.Sprintf("rtp://127.0.0.1:%d", firstPositiveInt(info["udp_port"], 55000))
	info["available_devices"] = devices
	info["capture_modes"] = []VideoCaptureMode{
		{Width: 640, Height: 480, FPS: []int{15, 30}, PixelFormat: "BGRA"},
		{Width: 1280, Height: 720, FPS: []int{15, 30, 60}, PixelFormat: "BGRA"},
		{Width: 1920, Height: 1080, FPS: []int{15, 30, 60}, PixelFormat: "BGRA"},
	}
	info["supported_modes"] = []VideoTransportMode{
		{
			ID:                "h264",
			Name:              "H.264",
			Description:       "Desktop capture -> H.264 encode -> RTP/UDP",
			Transport:         "rtp",
			Encoding:          "h264",
			ServerDecodesJPEG: false,
		},
	}

	if _, ok := info["width"]; !ok {
		info["width"] = width
	}
	if _, ok := info["height"]; !ok {
		info["height"] = height
	}
	if _, ok := info["fps"]; !ok {
		info["fps"] = fps
	}

	return info
}

func asString(v interface{}) string {
	s, _ := v.(string)
	return s
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func firstPositiveInt(value interface{}, fallback int) int {
	if v, ok := value.(int); ok && v > 0 {
		return v
	}
	if v, ok := value.(float64); ok && v > 0 {
		return int(v)
	}
	return fallback
}

func asCaptureModes(value interface{}) []VideoCaptureMode {
	modes, _ := value.([]VideoCaptureMode)
	return modes
}

func asTransportModes(value interface{}) []VideoTransportMode {
	modes, _ := value.([]VideoTransportMode)
	return modes
}

func asBool(value interface{}) bool {
	v, _ := value.(bool)
	return v
}

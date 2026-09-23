package controller

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

var (
	videoInfoCacheMu    sync.Mutex
	videoInfoCachedAt   time.Time
	videoInfoCachedData *models.VideoInfoData
	videoInfoCachedErr  error

	captureModesCacheMu        sync.Mutex
	captureModesCacheDevice    string
	captureModesCacheInfo      *models.VideoInfoData
	captureModesCacheAt        time.Time
	captureModesRefreshRunning atomic.Bool
	captureHostDesktopW        float32
	captureHostDesktopH        float32
)

// captureModesCacheStaleAfter: menu/dialog still use the cache immediately
// (stale-while-revalidate). A background refresh runs if the snapshot is
// older than this, so a newly plugged HDMI source shows up without a
// blocking fetch on click.
const captureModesCacheStaleAfter = 20 * time.Second

func getVideoInfoData(usbClient *api.USBClient) (*models.VideoInfoData, error) {
	return getVideoInfoDataForDevice(usbClient, "")
}

func PeekVideoInfoData() *models.VideoInfoData {
	videoInfoCacheMu.Lock()
	defer videoInfoCacheMu.Unlock()
	return videoInfoCachedData
}

func getVideoInfoDataForDevice(usbClient *api.USBClient, devicePath string) (*models.VideoInfoData, error) {
	if usbClient == nil {
		return nil, fmt.Errorf("%s", i18n.Current.ErrorNoConnection)
	}

	videoInfoCacheMu.Lock()
	if strings.TrimSpace(devicePath) == "" && time.Since(videoInfoCachedAt) < 750*time.Millisecond {
		cached := videoInfoCachedData
		err := videoInfoCachedErr
		videoInfoCacheMu.Unlock()
		if err != nil {
			return nil, err
		}
		if cached != nil {
			return cached, nil
		}
	} else {
		videoInfoCacheMu.Unlock()
	}

	logrus.Infof("🎯 [VIDEO-INFO-TRACE] Fetching video info for device=%q...", devicePath)
	start := time.Now()
	resp, err := usbClient.GetVideoInfoForDevice(devicePath)
	duration := time.Since(start)
	if err != nil {
		logrus.Errorf("❌ [VIDEO-INFO-TRACE] GetVideoInfoForDevice failed after %v for device=%q: %v", duration, devicePath, err)
		if strings.TrimSpace(devicePath) == "" {
			videoInfoCacheMu.Lock()
			videoInfoCachedAt = time.Now()
			videoInfoCachedData = nil
			videoInfoCachedErr = err
			videoInfoCacheMu.Unlock()
		}
		return nil, err
	}
	logrus.Infof("✅ [VIDEO-INFO-TRACE] GetVideoInfoForDevice succeeded in %v for device=%q", duration, devicePath)
	if resp == nil || !resp.Success || resp.Data == nil {
		err := fmt.Errorf("%s", i18n.Current.VideoInfoUnavailable)
		if strings.TrimSpace(devicePath) == "" {
			videoInfoCacheMu.Lock()
			videoInfoCachedAt = time.Now()
			videoInfoCachedData = nil
			videoInfoCachedErr = err
			videoInfoCacheMu.Unlock()
		}
		return nil, err
	}

	info, err := models.ParseVideoInfoData(resp.Data)
	if err == nil && info != nil {
		logrus.Infof("🎯 [CODEC-TRACE] GET /api/video/info (device=%q) -> encoding=%q mode=%q streaming=%v -- \"encoding\" is the agent's best-effort report of what the server is ACTUALLY running right now",
			devicePath, info.Encoding, info.Mode, info.Streaming)
	}
	if strings.TrimSpace(devicePath) == "" {
		videoInfoCacheMu.Lock()
		videoInfoCachedAt = time.Now()
		videoInfoCachedData = info
		videoInfoCachedErr = err
		videoInfoCacheMu.Unlock()
	}
	if err == nil && info != nil && len(info.CaptureModes) > 0 {
		rememberCaptureModes(devicePath, info)
	}
	return info, err
}

func resetVideoInfoCache() {
	videoInfoCacheMu.Lock()
	videoInfoCachedAt = time.Time{}
	videoInfoCachedData = nil
	videoInfoCachedErr = nil
	videoInfoCacheMu.Unlock()
}

func rememberCaptureModes(devicePath string, info *models.VideoInfoData) {
	if info == nil || len(info.CaptureModes) == 0 {
		return
	}
	key := strings.TrimSpace(devicePath)
	if key == "" {
		key = strings.TrimSpace(info.Device)
	}
	if key == "" {
		return
	}
	captureModesCacheMu.Lock()
	captureModesCacheDevice = key
	captureModesCacheInfo = cloneVideoInfoData(info)
	captureModesCacheAt = time.Now()
	captureHostDesktopW, captureHostDesktopH = hostDesktopSizeFromModes(info.CaptureModes)
	if nw, nh, ok := parseWxH(info.Device); ok && nw*nh > captureHostDesktopW*captureHostDesktopH {
		captureHostDesktopW, captureHostDesktopH = nw, nh
	}
	for _, d := range info.AvailableDevices {
		if key != "" && d.Path != "" && d.Path != key && d.Path != info.Device {
			continue
		}
		if nw, nh, ok := parseWxH(d.Name); ok && nw*nh >= captureHostDesktopW*captureHostDesktopH {
			captureHostDesktopW, captureHostDesktopH = nw, nh
		}
	}
	captureModesCacheMu.Unlock()
}

func cachedCaptureInfo(devicePath string) (*models.VideoInfoData, bool) {
	key := strings.TrimSpace(devicePath)
	captureModesCacheMu.Lock()
	defer captureModesCacheMu.Unlock()
	if captureModesCacheInfo == nil || len(captureModesCacheInfo.CaptureModes) == 0 {
		return nil, false
	}
	if key != "" && captureModesCacheDevice != "" && captureModesCacheDevice != key {
		return nil, false
	}
	return cloneVideoInfoData(captureModesCacheInfo), true
}

func captureModesCacheStale() bool {
	captureModesCacheMu.Lock()
	defer captureModesCacheMu.Unlock()
	return captureModesCacheInfo == nil || time.Since(captureModesCacheAt) > captureModesCacheStaleAfter
}

func clearCaptureModesCache() {
	captureModesCacheMu.Lock()
	captureModesCacheDevice = ""
	captureModesCacheInfo = nil
	captureModesCacheAt = time.Time{}
	captureHostDesktopW, captureHostDesktopH = 0, 0
	captureModesCacheMu.Unlock()
}

func cloneVideoInfoData(info *models.VideoInfoData) *models.VideoInfoData {
	if info == nil {
		return nil
	}
	cp := *info
	if info.CaptureModes != nil {
		cp.CaptureModes = make([]models.VideoCaptureMode, len(info.CaptureModes))
		for i, m := range info.CaptureModes {
			cp.CaptureModes[i] = m
			cp.CaptureModes[i].FPS = append([]int(nil), m.FPS...)
		}
	}
	if info.SupportedModes != nil {
		cp.SupportedModes = append([]models.VideoTransportMode(nil), info.SupportedModes...)
	}
	return &cp
}

func localPreferredVideoConfig() (models.VideoDeviceConfig, error) {
	path := selectedVideoDevicePath()
	if path == "" {
		return models.VideoDeviceConfig{}, fmt.Errorf("%s", i18n.Current.VideoDevicesNotFound)
	}
	cfg := loadSavedVideoDeviceConfig(path, "")
	cfg.DevicePath = path
	return cfg, nil
}

// CurrentCaptureDevice is the saved selected capture path/name, without a
// bridge round-trip -- Control's monitor chip reads this for its label.
func (vw *VideoWidget) CurrentCaptureDevice() (path, name string) {
	if vw == nil {
		return "", ""
	}
	cfg, err := localPreferredVideoConfig()
	if err != nil {
		return selectedVideoDevicePath(), ""
	}
	name = strings.TrimSpace(cfg.DeviceName)
	if name == "" {
		name = filepath.Base(cfg.DevicePath)
	}
	return cfg.DevicePath, name
}

func normalizeCaptureVideoDevices(devices []models.SystemDevice) []models.SystemDevice {
	merged := make(map[string]models.SystemDevice, len(devices))
	for _, device := range devices {
		path := strings.TrimSpace(device.Path)
		if path == "" {
			continue
		}
		device.Path = path
		merged[path] = normalizeCaptureVideoDevice(device)
	}

	result := make([]models.SystemDevice, 0, len(merged))
	for _, device := range merged {
		result = append(result, device)
	}
	sort.Slice(result, func(i, j int) bool {
		wi := videoDeviceBusWeight(result[i].Bus)
		wj := videoDeviceBusWeight(result[j].Bus)
		if wi != wj {
			return wi < wj
		}
		return result[i].Path < result[j].Path
	})
	return result
}

// videoDeviceBusWeight mirrors the server-side busWeight: USB first, then MIPI-CSI, rest last.
func videoDeviceBusWeight(bus string) int {
	switch {
	case strings.HasPrefix(bus, "usb"):
		return 0
	case bus == "mipi-csi":
		return 1
	default:
		return 2
	}
}

func mergeVideoDeviceSet(dst map[string]models.SystemDevice, devices []models.SystemDevice) {
	for _, device := range devices {
		path := strings.TrimSpace(device.Path)
		if path == "" {
			continue
		}
		device.Path = path
		if existing, ok := dst[path]; ok {
			if strings.TrimSpace(existing.Name) == "" {
				existing.Name = device.Name
			}
			if strings.TrimSpace(existing.Description) == "" {
				existing.Description = device.Description
			}
			existing.Connected = existing.Connected || device.Connected
			dst[path] = normalizeCaptureVideoDevice(existing)
			continue
		}
		dst[path] = normalizeCaptureVideoDevice(device)
	}
}

func currentVideoInfoDevice(info *models.VideoInfoData) []models.SystemDevice {
	if info == nil {
		return nil
	}
	path := strings.TrimSpace(info.Device)
	// Skip server-side aliases ("auto") that are not real device paths.
	// Real paths contain "/" (Linux /dev/videoN) or ":" (display:N, pipewire:N).
	if path == "" || (!strings.Contains(path, "/") && !strings.Contains(path, ":")) {
		return nil
	}

	return []models.SystemDevice{
		{
			Path:      path,
			Name:      filepath.Base(path),
			Connected: info.Enabled || info.Streaming,
		},
	}
}

func getAvailableVideoDevices(usbClient *api.USBClient) ([]models.SystemDevice, error) {
	if usbClient == nil {
		return nil, fmt.Errorf("%s", i18n.Current.ErrorNoConnection)
	}

	merged := make(map[string]models.SystemDevice)
	var fetchErrs []string

	devices, err := usbClient.GetVideoDevices()
	if err != nil {
		logrus.Debugf("video devices endpoint unavailable: %v", err)
		fetchErrs = append(fetchErrs, err.Error())
	} else {
		mergeVideoDeviceSet(merged, devices)
	}

	currentInfo, infoErr := getVideoInfoData(usbClient)
	if infoErr != nil {
		logrus.Debugf("video info endpoint unavailable: %v", infoErr)
		fetchErrs = append(fetchErrs, infoErr.Error())
	} else {
		mergeVideoDeviceSet(merged, currentVideoInfoDevice(currentInfo))
	}

	result := normalizeCaptureVideoDevices(func() []models.SystemDevice {
		items := make([]models.SystemDevice, 0, len(merged))
		for _, device := range merged {
			items = append(items, device)
		}
		return items
	}())
	if len(result) == 0 {
		if len(fetchErrs) > 0 {
			return nil, fmt.Errorf("%s", strings.Join(fetchErrs, "; "))
		}
		return nil, fmt.Errorf("%s", i18n.Current.VideoDevicesNotFound)
	}
	return result, nil
}

func normalizeCaptureVideoDevice(device models.SystemDevice) models.SystemDevice {
	device.Path = strings.TrimSpace(device.Path)
	if device.Name == "" {
		device.Name = filepath.Base(device.Path)
	}
	if device.Description == "" {
		device.Description = i18n.Current.CaptureDevice
	}
	return device
}

func (vw *VideoWidget) GetAvailableVideoDevices() ([]models.SystemDevice, error) {
	return getAvailableVideoDevices(vw.usbClient)
}

func (vw *VideoWidget) resolvePreferredVideoConfig() (models.VideoDeviceConfig, error) {
	devices, err := vw.GetAvailableVideoDevices()
	if err != nil {
		return models.VideoDeviceConfig{}, err
	}
	if len(devices) == 0 {
		return models.VideoDeviceConfig{}, fmt.Errorf("%s", i18n.Current.VideoDevicesNotFound)
	}

	deviceByPath := make(map[string]models.SystemDevice, len(devices))
	devicePaths := make([]string, 0, len(devices))
	for _, device := range devices {
		deviceByPath[device.Path] = device
		devicePaths = append(devicePaths, device.Path)
	}

	var info *models.VideoInfoData
	rawSelectedPath := selectedVideoDevicePath()
	selectedPath := rawSelectedPath
	// Only inherit server's current device when the client already has a saved
	// preference — on a fresh install, default to devices[0] (USB first after sort).
	if selectedPath != "" && vw.usbClient != nil {
		if currentInfo, err := getVideoInfoData(vw.usbClient); err == nil && currentInfo != nil {
			info = currentInfo
		}
	}
	if selectedPath == "" {
		selectedPath = devices[0].Path
	}

	device, ok := deviceByPath[selectedPath]
	if !ok {
		logrus.Warnf("🎯 [CODEC-TRACE] resolvePreferredVideoConfig: saved SelectedDevice=%q NOT FOUND in current device list %v -- falling back to devices[0]=%q, its saved config (if any) will be used instead, NOT the one just configured",
			rawSelectedPath, devicePaths, devices[0].Path)
		device = devices[0]
		selectedPath = device.Path
		// Self-heal: without this, prefs.SelectedDevice stays permanently
		// pointed at the phantom device forever (see
		// correctSelectedVideoDevicePath's doc comment) -- every future
		// ShowCurrentVideoSettings (header/status-bar gear) would keep
		// opening the settings popup for a device that doesn't exist, and
		// changes made there would never reach the device actually
		// streaming.
		correctSelectedVideoDevicePath(selectedPath)
	}

	cfg := loadSavedVideoDeviceConfig(selectedPath, device.Name)
	logrus.Infof("🎯 [CODEC-TRACE] resolvePreferredVideoConfig: loaded from saved prefs VideoMode=%q device=%s (rawSelectedPath=%q, in list=%v)", cfg.VideoMode, selectedPath, rawSelectedPath, ok)
	cfg.DeviceName = device.Name
	cfg.DevicePath = selectedPath
	if vw.usbClient != nil {
		if deviceInfo, err := getVideoInfoDataForDevice(vw.usbClient, selectedPath); err == nil && deviceInfo != nil {
			info = deviceInfo
		}
	}
	if !hasSavedVideoDeviceConfig(selectedPath) && info != nil && info.Device == selectedPath {
		cfg = mergeVideoConfigWithInfo(cfg, info)
		logrus.Infof("🎯 [CODEC-TRACE] resolvePreferredVideoConfig: no saved prefs yet, merged server info.Encoding=%q -> VideoMode=%q", info.Encoding, cfg.VideoMode)
	}
	logrus.Infof("🎯 [CODEC-TRACE] resolvePreferredVideoConfig: final VideoMode=%q device=%s", cfg.VideoMode, selectedPath)
	return cfg, nil
}

// mergeVideoConfigResolution refreshes only the width/height in cfg from
// info, leaving quality/bitrate/fps/mode untouched. Physical monitor
// resolution is a hardware fact that can change between switches to the
// same device (e.g. the connected display's mode changed), unlike
// quality/bitrate which are deliberately preserved as user preferences —
// see mergeVideoConfigWithInfo's callers.
func mergeVideoConfigResolution(cfg models.VideoDeviceConfig, info *models.VideoInfoData) models.VideoDeviceConfig {
	if info == nil {
		return cfg
	}
	if info.Width > 0 {
		cfg.VideoWidth = info.Width
	}
	if info.Height > 0 {
		cfg.VideoHeight = info.Height
	}
	return cfg
}

func mergeVideoConfigWithInfo(cfg models.VideoDeviceConfig, info *models.VideoInfoData) models.VideoDeviceConfig {
	if info == nil {
		return cfg
	}
	if info.Width > 0 {
		cfg.VideoWidth = info.Width
	}
	if info.Height > 0 {
		cfg.VideoHeight = info.Height
	}
	if info.FPS > 0 {
		cfg.VideoFPS = info.FPS
	}
	if info.Quality > 0 {
		cfg.VideoQuality = info.Quality
	}
	if strings.TrimSpace(info.Bitrate) != "" {
		cfg.VideoBitrate = info.Bitrate
	}
	// info.Mode is the TRANSPORT label ("moonlight", VideoStatus.Mode) --
	// always the literal string "moonlight" per the agent's /api/video/info
	// handler (server.go), never a codec. cfg.VideoMode is the CODEC
	// selector (models.VideoModeH264/H265/AV1) that flows into
	// MoonlightService.SetVideoMode -> moonlightVideoFormat. Assigning
	// info.Mode here used to stuff the literal string "moonlight" into
	// cfg.VideoMode whenever this device had no saved config yet (e.g. the
	// reconcile-time device-list fallback landing on a device that was never
	// explicitly configured) -- moonlightVideoFormat doesn't recognize
	// "moonlight" as a mode, so it silently defaulted to VIDEO_FORMAT_H264,
	// making a codec switch look like it was completely ignored even though
	// every step up to this merge had the right value. info.Encoding is the
	// field that actually carries the codec the server is running right now
	// (VideoStatus.Encoding, "h264"/"h265"/"av1") -- see CurrentVideoCodec on
	// the agent.
	//
	// EXCEPT av1: confirmed live via [CODEC-TRACE] (2026-09-19, app.log
	// ~14:42:36) that a mid-stream reconnect can hand back a device list
	// whose winid paths changed shape (plain "winid:0"/"winid:1" ->
	// GUID-keyed "winid:{...}", e.g. after the host re-enumerates its
	// outputs), which makes resolvePreferredVideoConfig's saved-device
	// lookup miss and fall back to devices[0]. That device has no saved
	// config yet, so this merge runs -- and if the server just happened to
	// be reporting "av1" at that instant (for reasons unrelated to any
	// choice made here), the client would permanently adopt av1 for that
	// device path. win_deliver_frame's win32 win_deliver_frame_vulkan
	// comment records why that's specifically bad on this client: AV1 has
	// no Vulkan Video zero-copy decode implemented here and always falls
	// through to the CPU sws_scale+overlay path (moonlight_cgo_windows.go),
	// costing ~25-45ms/frame at high resolutions instead of a few ms --
	// exactly the "DEC 25-30ms, scattered yellow" NetGraph symptom this was
	// diagnosed from, sustained for the rest of that session because the
	// device had no prior config to fall back to instead. Never silently
	// auto-adopt av1 this way; require the user to pick it explicitly via
	// the device-settings dialog. h264/h265 are unaffected -- both have the
	// zero-copy path and are safe to inherit.
	if enc := strings.TrimSpace(info.Encoding); enc != "" && enc != models.VideoModeAV1 {
		cfg.VideoMode = enc
	}
	return cfg
}

// videoDeviceConfigFromRequest builds the VideoDeviceConfig to persist from a
// submitted VideoStartRequest. Both the "Start Video" dialog
// (handleVideoStartWithParams) and the device-settings "Apply" dialog
// (ShowVideoDeviceSettings) funnel through this single place so every field
// the dialog collects survives into the saved config -- Color444/Hdr in
// particular have no other persistence path, so dropping them here (as two
// separate hand-written struct literals previously did, independently)
// silently discards the user's 4:4:4/HDR checkbox choice: the save clobbers
// whatever was on disk with false, and the very next reconcile/restart reads
// that same false back via VideoDeviceConfig.ToVideoStartRequest().
func videoDeviceConfigFromRequest(devicePath, deviceName string, request *models.VideoStartRequest) models.VideoDeviceConfig {
	logrus.Infof("🎯 [CODEC-TRACE] videoDeviceConfigFromRequest: request.VideoMode=%q device=%s", request.VideoMode, devicePath)
	return models.VideoDeviceConfig{
		DevicePath:         devicePath,
		DeviceName:         deviceName,
		VideoWidth:         request.VideoWidth,
		VideoHeight:        request.VideoHeight,
		VideoFPS:           request.VideoFPS,
		VideoQuality:       request.VideoQuality,
		VideoBitrate:       request.VideoBitrate,
		VideoMode:          request.VideoMode,
		CapturePixelFormat: request.CapturePixelFormat,
		EnableVSync:        request.EnableVSync,
		Color444:           request.Color444,
		Hdr:                request.Hdr,
	}
}

func (vw *VideoWidget) applyVideoDeviceConfig(cfg models.VideoDeviceConfig, restart bool) error {
	if cfg.DevicePath == "" {
		return fmt.Errorf("%s", i18n.Current.VideoDeviceEmpty)
	}

	if cfg.VideoQuality <= 0 {
		cfg.VideoQuality = 100
	}
	if cfg.VideoBitrate == "" {
		cfg.VideoBitrate = "20000K"
	}
	if cfg.VideoMode == "" {
		cfg.VideoMode = models.VideoModeH264
	}
	logrus.Infof("🎯 [CODEC-TRACE] applyVideoDeviceConfig: VideoMode=%q restart=%v device=%s", cfg.VideoMode, restart, cfg.DevicePath)

	if vw.usbClient != nil {
		if err := vw.usbClient.SetVideoDevice(cfg.DevicePath, cfg.CapturePixelFormat); err != nil {
			logrus.Warnf("⚠️ failed to set video device on server: %v", err)
		}
	}

	saveVideoDeviceConfig(cfg)
	resetVideoInfoCache()
	vw.refreshAgentProtocol()
	vw.rememberHostDesktopFromConfig(cfg)

	if vw.onResolutionChanged != nil {
		vw.onResolutionChanged(cfg.VideoWidth, cfg.VideoHeight)
	}

	if !restart {
		return nil
	}

	vw.setDesiredStreaming(true)
	vw.videoOpMu.Lock()
	vw.videoRestartPending = true
	vw.videoOpMu.Unlock()
	vw.scheduleVideoReconcile("apply-video-config")
	return nil
}

func (vw *VideoWidget) StartConfiguredVideoAsync() {
	vw.setDesiredStreaming(true)
	vw.scheduleVideoReconcile("start-configured-async")
}

func (vw *VideoWidget) RequestStreaming(shouldStream bool) {
	vw.videoOpMu.Lock()
	vw.desiredStreaming = shouldStream
	running := vw.videoOpRunning
	streaming := vw.isStreaming
	vw.videoOpMu.Unlock()

	if running {
		return
	}
	if shouldStream {
		if !streaming {
			vw.scheduleVideoReconcile("request-streaming-on")
		}
		return
	}
	if streaming {
		vw.scheduleVideoReconcile("request-streaming-off")
	}
}

func (vw *VideoWidget) StartVideoDeviceAsync(devicePath string) {
	go vw.StartVideoDevice(devicePath)
}

// StartVideoDevice pins capture to devicePath, restarts the stream, and
// blocks until the first frame of that new session arrives (or a timeout).
// Called from the Devices radio path so the dashboard stays locked like a
// keyboard/mouse connect until the picture is actually up.
func (vw *VideoWidget) StartVideoDevice(devicePath string) {
	vw.setDesiredStreaming(true)
	prevTrace := vw.videoTraceID.Load()

	devices, err := vw.GetAvailableVideoDevices()
	if err != nil {
		logrus.Warnf("⚠️ cannot load available video devices: %v", err)
	}

	var selectedDevice *models.SystemDevice
	for i := range devices {
		if devices[i].Path == devicePath {
			selectedDevice = &devices[i]
			break
		}
	}

	var cfg models.VideoDeviceConfig
	var deviceName string
	if selectedDevice != nil {
		deviceName = selectedDevice.Name
		cfg = loadSavedVideoDeviceConfig(selectedDevice.Path, selectedDevice.Name)
	} else {
		deviceName = filepath.Base(devicePath)
		cfg = loadSavedVideoDeviceConfig(devicePath, deviceName)
	}

	cfg.DevicePath = devicePath
	cfg.DeviceName = deviceName

	// Query /api/video/info scoped to THIS device, not whatever's
	// currently streaming — at switch time the server is still on
	// the old device, so its default (unscoped) video info reports
	// the old device's path/resolution and the merge below would
	// silently no-op, leaving the stale 1280x720 default in cfg
	// (see defaultVideoDeviceConfig) and mis-sizing absolute mouse
	// mapping for the newly selected monitor.
	if info, err := getVideoInfoDataForDevice(vw.usbClient, devicePath); err == nil && info != nil && info.Device == devicePath {
		if !hasSavedVideoDeviceConfig(devicePath) {
			cfg = mergeVideoConfigWithInfo(cfg, info)
		} else {
			// A saved config already exists for this device (we've switched to
			// it before), so don't clobber the user's saved quality/bitrate/fps
			// preferences — but resolution must still be refreshed every time:
			// the cached width/height is whatever was true the first time this
			// monitor was selected, and a switch back to it after switching
			// through others otherwise reused that stale value, mis-scaling
			// absolute mouse mapping (or, with a different-aspect monitor in
			// between, making the touch field look like it spans both).
			cfg = mergeVideoConfigResolution(cfg, info)
		}
	}
	if err := vw.applyVideoDeviceConfig(cfg, true); err != nil {
		logrus.Warnf("⚠️ cannot start selected video device %s: %v", devicePath, err)
		return
	}
	vw.waitForNextFirstFrame(prevTrace, videoSwitchReadyTimeout)
}

func (vw *VideoWidget) StopVideoAsync() {
	vw.setDesiredStreaming(false)
	vw.scheduleVideoReconcile("stop-video-async")
}

func (vw *VideoWidget) ShowCurrentVideoSettings(showFullscreen bool) {
	// Do not resolvePreferredVideoConfig here: it hits the bridge on the
	// caller thread, and this is invoked from header/status-bar taps on the
	// Fyne UI goroutine. That froze the whole desktop window for a couple of
	// seconds whenever video/info or /devices was slow.
	_ = showFullscreen
	logrus.Infof("🎯 [CODEC-TRACE] video settings opened from header/status bar: restartOnApply=true (always)")
	vw.ShowVideoDeviceSettings(selectedVideoDevicePath(), true, false)
}

// AvailableCaptureModes returns the current device's known capture modes
// (each a resolution with its own supported fps list) and its live config.
// Uses a per-device cache so header FPS/resolution menus can open without
// waiting on the bridge; a stale snapshot is still returned immediately and
// refreshed in the background. Callers may invoke this from the UI thread
// when a cache hit is likely (after PrefetchCaptureModesAsync / a prior open).
func (vw *VideoWidget) AvailableCaptureModes() ([]models.VideoCaptureMode, models.VideoDeviceConfig, error) {
	cfg, cfgErr := localPreferredVideoConfig()
	if info, ok := cachedCaptureInfo(cfg.DevicePath); ok {
		if captureModesCacheStale() {
			vw.refreshCaptureModesAsync(cfg.DevicePath)
		}
		modes := info.CaptureModes
		if cfgErr != nil {
			cfg = loadSavedVideoDeviceConfig(info.Device, info.Device)
			cfg.DevicePath = info.Device
		}
		if len(modes) == 0 {
			modes = []models.VideoCaptureMode{{Width: cfg.VideoWidth, Height: cfg.VideoHeight, FPS: []int{cfg.VideoFPS}}}
		}
		return modes, cfg, nil
	}

	if cfgErr != nil {
		var err error
		cfg, err = vw.resolvePreferredVideoConfig()
		if err != nil {
			return nil, models.VideoDeviceConfig{}, err
		}
	}
	info := vw.fetchVideoInfoForStartDialog(cfg.DevicePath)
	if info != nil && len(info.CaptureModes) > 0 {
		rememberCaptureModes(cfg.DevicePath, info)
	}
	var modes []models.VideoCaptureMode
	if info != nil && len(info.CaptureModes) > 0 {
		modes = info.CaptureModes
	}
	if len(modes) == 0 {
		modes = []models.VideoCaptureMode{{Width: cfg.VideoWidth, Height: cfg.VideoHeight, FPS: []int{cfg.VideoFPS}}}
	}
	return modes, cfg, nil
}

func (vw *VideoWidget) refreshCaptureModesAsync(devicePath string) {
	if vw == nil || vw.usbClient == nil {
		return
	}
	if !captureModesRefreshRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer captureModesRefreshRunning.Store(false)
		info := vw.fetchVideoInfoForStartDialogAttempts(devicePath, 1)
		if info != nil && len(info.CaptureModes) > 0 {
			rememberCaptureModes(devicePath, info)
		}
	}()
}

func (vw *VideoWidget) PrefetchCaptureModesAsync() {
	path := selectedVideoDevicePath()
	vw.refreshCaptureModesAsync(path)
}

// PeekCaptureModes returns cached capture modes without hitting the bridge.
// ok is false when nothing has been fetched yet (call AvailableCaptureModes
// in a goroutine). Used by the header FPS/resolution menus so the popup can
// open on the same tap, not after a round-trip.
func (vw *VideoWidget) PeekCaptureModes() ([]models.VideoCaptureMode, models.VideoDeviceConfig, bool) {
	cfg, _ := localPreferredVideoConfig()
	info, ok := cachedCaptureInfo(cfg.DevicePath)
	if !ok {
		return nil, cfg, false
	}
	if cfg.DevicePath == "" {
		cfg = loadSavedVideoDeviceConfig(info.Device, "")
		cfg.DevicePath = info.Device
	}
	modes := info.CaptureModes
	if len(modes) == 0 {
		modes = []models.VideoCaptureMode{{Width: cfg.VideoWidth, Height: cfg.VideoHeight, FPS: []int{cfg.VideoFPS}}}
	}
	return modes, cfg, true
}

func (vw *VideoWidget) RefreshCaptureModesIfStaleAsync() {
	if captureModesCacheStale() {
		vw.PrefetchCaptureModesAsync()
	}
}

// ApplyVideoFPS restarts the current stream with a new fps, keeping every
// other current setting (resolution, bitrate, mode, pixel format...) as-is
// -- see AvailableCaptureModes' own doc comment.
func (vw *VideoWidget) ApplyVideoFPS(fps int) error {
	cfg, err := localPreferredVideoConfig()
	if err != nil {
		cfg, err = vw.resolvePreferredVideoConfig()
		if err != nil {
			return err
		}
	}
	cfg.VideoFPS = fps
	return vw.applyVideoDeviceConfig(cfg, true)
}

// ApplyVideoResolution restarts the current stream at a new resolution,
// keeping every other current setting as-is -- see AvailableCaptureModes'
// own doc comment. Does not change fps even if the new resolution doesn't
// actually support the current one; the server is the authority on that
// and the fps dropdown will show whatever it reports as valid next time
// it's opened.
func (vw *VideoWidget) ApplyVideoResolution(width, height int) error {
	cfg, err := localPreferredVideoConfig()
	if err != nil {
		cfg, err = vw.resolvePreferredVideoConfig()
		if err != nil {
			return err
		}
	}
	cfg.VideoWidth = width
	cfg.VideoHeight = height
	return vw.applyVideoDeviceConfig(cfg, true)
}

func (vw *VideoWidget) ensureStartDialog() {
	if vw.startDialog != nil || vw.parentWindow == nil {
		return
	}
	vw.startDialog = view.NewVideoStartDialog(vw.parentWindow)
	vw.startDialog.SetLiveCodecProvider(func() (string, bool) {
		if vw.videoClient == nil {
			return "", false
		}
		return vw.videoClient.NegotiatedVideoCodecName()
	})
}

func (vw *VideoWidget) ShowVideoDeviceSettings(devicePath string, restartOnApply bool, showFullscreen bool) {
	if vw.usbClient == nil || vw.parentWindow == nil {
		logrus.Warn("⚠️ cannot show video settings: usbClient or parentWindow is nil")
		return
	}

	logrus.Infof("⚙️ opening video settings for device: %s", devicePath)

	go func() {
		var device models.SystemDevice
		if strings.TrimSpace(devicePath) != "" {
			device.Path = devicePath
			device.Name = filepath.Base(devicePath)
			device.Description = i18n.Current.CaptureDevice
		} else {
			devices, err := vw.GetAvailableVideoDevices()
			if err != nil {
				logrus.Warnf("⚠️ failed to load video devices: %v", err)
			}
			if len(devices) > 0 {
				device = devices[0]
			}
		}

		if device.Path == "" {
			logrus.Warnf("⚠️ video device %s not found and path is empty", devicePath)
			return
		}

		cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
		cfg.DevicePath = device.Path
		cfg.DeviceName = device.Name

		present := func(info *models.VideoInfoData, cfg models.VideoDeviceConfig) {
			info, cfg = prepareVideoStartDialogInfo(device, info, cfg)
			started := time.Now()
			logrus.Infof("📦 showing video start dialog for %s", device.Path)
			vw.ensureStartDialog()
			vw.startDialog.Configure(info, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS, cfg.VideoBitrate)
			vw.startDialog.SetDeviceLabel(device.Path)
			vw.startDialog.SetPrimaryAction(i18n.Current.Apply)
			_ = showFullscreen
			vw.startDialog.SetExtraAction("", nil)
			logrus.Infof("📦 video start dialog ready in %s", time.Since(started).Round(time.Millisecond))
			vw.startDialog.Show(func(request *models.VideoStartRequest) {
				applied := videoDeviceConfigFromRequest(device.Path, device.Name, request)
				logrus.Infof("💾 applying video settings for %s: %dx%d @ %d fps", device.Path, applied.VideoWidth, applied.VideoHeight, applied.VideoFPS)
				logrus.Infof("🎯 [CODEC-TRACE] ShowVideoDeviceSettings onApply: VideoMode=%q restartOnApply=%v -- if false, the stream is NOT restarted and the old codec keeps running on the server regardless of what was just saved", applied.VideoMode, restartOnApply)
				go func() {
					if err := vw.applyVideoDeviceConfig(applied, restartOnApply); err != nil {
						logrus.Warnf("⚠️ failed to apply video config: %v", err)
						fyne.Do(func() {
							vw.statusLabel.SetText(fmt.Sprintf("❌ %v", err))
						})
					}
				}()
			})
		}

		shownFromCache := false
		if cached, ok := cachedCaptureInfo(device.Path); ok {
			cachedCfg := cfg
			fyne.Do(func() { present(cached, cachedCfg) })
			shownFromCache = true
		}

		attempts := 2
		if shownFromCache {
			attempts = 1
		}
		info := vw.fetchVideoInfoForStartDialogAttempts(device.Path, attempts)
		if info != nil && len(info.CaptureModes) > 0 {
			rememberCaptureModes(device.Path, info)
		}
		if shownFromCache {
			return
		}
		freshCfg := cfg
		fyne.Do(func() { present(info, freshCfg) })
	}()
}

func prepareVideoStartDialogInfo(device models.SystemDevice, info *models.VideoInfoData, cfg models.VideoDeviceConfig) (*models.VideoInfoData, models.VideoDeviceConfig) {
	isDisplayDevice := strings.HasPrefix(device.Path, "display:") || strings.HasPrefix(device.Path, "drm:")
	if info != nil && info.Streaming && (info.Device == device.Path || isDisplayDevice) {
		cfg = mergeVideoConfigWithInfo(cfg, info)
	}
	if isDisplayDevice {
		if cfg.VideoWidth <= 0 || cfg.VideoHeight <= 0 {
			w, h := 1920, 1080
			re := regexp.MustCompile(`\((\d+)x(\d+)\)`)
			matches := re.FindStringSubmatch(device.Name)
			if len(matches) == 3 {
				if parsedW, err := strconv.Atoi(matches[1]); err == nil {
					w = parsedW
				}
				if parsedH, err := strconv.Atoi(matches[2]); err == nil {
					h = parsedH
				}
			}
			cfg.VideoWidth = w
			cfg.VideoHeight = h
		}
		if cfg.VideoFPS <= 0 {
			cfg.VideoFPS = 30
		}
		if info == nil {
			info = &models.VideoInfoData{
				VideoStatus: models.VideoStatus{
					Device: device.Path,
					Width:  cfg.VideoWidth,
					Height: cfg.VideoHeight,
					FPS:    cfg.VideoFPS,
				},
			}
		}
	}
	return info, cfg
}

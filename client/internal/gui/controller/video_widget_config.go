package controller

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
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
)

func getVideoInfoData(usbClient *api.USBClient) (*models.VideoInfoData, error) {
	return getVideoInfoDataForDevice(usbClient, "")
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

	resp, err := usbClient.GetVideoInfoForDevice(devicePath)
	if err != nil {
		if strings.TrimSpace(devicePath) == "" {
			videoInfoCacheMu.Lock()
			videoInfoCachedAt = time.Now()
			videoInfoCachedData = nil
			videoInfoCachedErr = err
			videoInfoCacheMu.Unlock()
		}
		return nil, err
	}
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
	if strings.TrimSpace(devicePath) == "" {
		videoInfoCacheMu.Lock()
		videoInfoCachedAt = time.Now()
		videoInfoCachedData = info
		videoInfoCachedErr = err
		videoInfoCacheMu.Unlock()
	}
	return info, err
}

func resetVideoInfoCache() {
	videoInfoCacheMu.Lock()
	defer videoInfoCacheMu.Unlock()
	videoInfoCachedAt = time.Time{}
	videoInfoCachedData = nil
	videoInfoCachedErr = nil
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
	for _, device := range devices {
		deviceByPath[device.Path] = device
	}

	var info *models.VideoInfoData
	selectedPath := selectedVideoDevicePath()
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
		device = devices[0]
		selectedPath = device.Path
	}

	cfg := loadSavedVideoDeviceConfig(selectedPath, device.Name)
	cfg.DeviceName = device.Name
	cfg.DevicePath = selectedPath
	if vw.usbClient != nil {
		if deviceInfo, err := getVideoInfoDataForDevice(vw.usbClient, selectedPath); err == nil && deviceInfo != nil {
			info = deviceInfo
		}
	}
	if !hasSavedVideoDeviceConfig(selectedPath) && info != nil && info.Device == selectedPath {
		cfg = mergeVideoConfigWithInfo(cfg, info)
	}
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
	if strings.TrimSpace(info.Mode) != "" {
		cfg.VideoMode = info.Mode
	}
	return cfg
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

	if vw.usbClient != nil {
		if err := vw.usbClient.SetVideoDevice(cfg.DevicePath, cfg.CapturePixelFormat); err != nil {
			logrus.Warnf("⚠️ failed to set video device on server: %v", err)
		}
	}

	saveVideoDeviceConfig(cfg)
	resetVideoInfoCache()

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
	vw.setDesiredStreaming(true)
	go func() {
		devices, err := vw.GetAvailableVideoDevices()
		if err != nil {
			logrus.Warnf("⚠️ cannot load available video devices: %v", err)
			// Continue with fallback below
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
		}
	}()
}

func (vw *VideoWidget) StopVideoAsync() {
	vw.setDesiredStreaming(false)
	vw.scheduleVideoReconcile("stop-video-async")
}

func (vw *VideoWidget) ShowCurrentVideoSettings(showFullscreen bool) {
	cfg, err := vw.resolvePreferredVideoConfig()
	if err != nil {
		logrus.Warnf("⚠️ cannot open video settings: %v", err)
		return
	}
	vw.ShowVideoDeviceSettings(cfg.DevicePath, true, false)
}

func (vw *VideoWidget) ShowVideoDeviceSettings(devicePath string, restartOnApply bool, showFullscreen bool) {
	if vw.usbClient == nil || vw.parentWindow == nil {
		logrus.Warn("⚠️ cannot show video settings: usbClient or parentWindow is nil")
		return
	}

	logrus.Infof("⚙️ opening video settings for device: %s", devicePath)

	go func() {
		devices, err := vw.GetAvailableVideoDevices()
		if err != nil {
			logrus.Warnf("⚠️ failed to load video devices: %v", err)
			// Continue with fallback
		}

		var device models.SystemDevice
		for _, candidate := range devices {
			if candidate.Path == devicePath {
				device = candidate
				break
			}
		}

		if device.Path == "" && devicePath != "" {
			device.Path = devicePath
			device.Name = filepath.Base(devicePath)
			device.Description = i18n.Current.CaptureDevice
		}

		if device.Path == "" {
			logrus.Warnf("⚠️ video device %s not found and path is empty", devicePath)
			return
		}

		cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
		cfg.DevicePath = device.Path
		cfg.DeviceName = device.Name

		info := vw.fetchVideoInfoForStartDialog(device.Path)
		isDisplayDevice := strings.HasPrefix(device.Path, "display:") || strings.HasPrefix(device.Path, "drm:")
		// Only merge server params into the dialog defaults when the server is actively
		// streaming. When not streaming, the server returns its hard-coded config defaults
		// (1280x720 @ 30fps) which would silently overwrite the client's saved preferences.
		if info != nil && info.Streaming && (info.Device == device.Path || isDisplayDevice) {
			cfg = mergeVideoConfigWithInfo(cfg, info)
		}
		if isDisplayDevice {
			// Parse resolution from display name like "Display 0 (1920x1080)" as a
			// fallback for when the server returned zero dimensions.
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

		fyne.Do(func() {
			logrus.Infof("📦 showing video start dialog for %s", device.Path)
			if vw.startDialog == nil {
				vw.startDialog = view.NewVideoStartDialog(vw.parentWindow)
			}

			vw.startDialog.Configure(info, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS, cfg.VideoBitrate)
			vw.startDialog.SetDeviceLabel(device.Path)
			vw.startDialog.SetPrimaryAction(i18n.Current.Apply)
			_ = showFullscreen
			vw.startDialog.SetExtraAction("", nil)

			vw.startDialog.Show(func(request *models.VideoStartRequest) {
				applied := models.VideoDeviceConfig{
					DevicePath:         device.Path,
					DeviceName:         device.Name,
					VideoWidth:         request.VideoWidth,
					VideoHeight:        request.VideoHeight,
					VideoFPS:           request.VideoFPS,
					VideoQuality:       request.VideoQuality,
					VideoBitrate:       request.VideoBitrate,
					VideoMode:          request.VideoMode,
					CapturePixelFormat: request.CapturePixelFormat,
				}
				logrus.Infof("💾 applying video settings for %s: %dx%d @ %d fps", device.Path, applied.VideoWidth, applied.VideoHeight, applied.VideoFPS)
				if err := vw.applyVideoDeviceConfig(applied, restartOnApply); err != nil {
					logrus.Warnf("⚠️ failed to apply video config: %v", err)
					fyne.Do(func() {
						vw.statusLabel.SetText(fmt.Sprintf("❌ %v", err))
					})
				}
			})
		})
	}()
}

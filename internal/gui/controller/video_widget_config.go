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
		return result[i].Path < result[j].Path
	})
	return result
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
	if path == "" {
		return nil
	}

	return []models.SystemDevice{
		{
			Path:        path,
			Name:        filepath.Base(path),
			Description: i18n.Current.CaptureDevice,
			Connected:   info.Enabled || info.Streaming,
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
	if selectedPath == "" && vw.usbClient != nil {
		if currentInfo, err := getVideoInfoData(vw.usbClient); err == nil && currentInfo != nil {
			info = currentInfo
		}
		if info != nil && info.Device != "" {
			selectedPath = info.Device
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
		cfg.VideoQuality = 80
	}
	if cfg.VideoBitrate == "" {
		cfg.VideoBitrate = "2000K"
	}
	if cfg.VideoMode == "" {
		cfg.VideoMode = models.VideoModeH264
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

		if !hasSavedVideoDeviceConfig(devicePath) {
			if info, err := getVideoInfoData(vw.usbClient); err == nil && info != nil && info.Device == devicePath {
				cfg = mergeVideoConfigWithInfo(cfg, info)
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
		if info != nil && info.Device == device.Path {
			cfg = mergeVideoConfigWithInfo(cfg, info)
		} else if strings.HasPrefix(device.Path, "display:") {
			// Try to parse resolution from name like "Display 0 (1920x1080)"
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

			if info == nil {
				info = &models.VideoInfoData{
					VideoStatus: models.VideoStatus{
						Device: device.Path,
						Width:  w,
						Height: h,
						FPS:    30,
					},
				}
			}
			// Update cfg if it was default
			if cfg.VideoWidth <= 0 || cfg.VideoHeight <= 0 {
				cfg.VideoWidth = w
				cfg.VideoHeight = h
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
					DevicePath:   device.Path,
					DeviceName:   device.Name,
					VideoWidth:   request.VideoWidth,
					VideoHeight:  request.VideoHeight,
					VideoFPS:     request.VideoFPS,
					VideoQuality: request.VideoQuality,
					VideoBitrate: request.VideoBitrate,
					VideoMode:    request.VideoMode,
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

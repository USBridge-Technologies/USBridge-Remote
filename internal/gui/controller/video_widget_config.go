package controller

import (
	"fmt"
	"path/filepath"
	"sort"
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

func isLikelyCaptureVideoDevice(device models.SystemDevice) bool {
	if !strings.HasPrefix(device.Path, "/dev/video") {
		return false
	}

	text := strings.ToLower(device.Name + " " + device.Description + " " + filepath.Base(device.Path))
	rejectTokens := []string{
		"codec", "enc", "dec", "m2m", "isp", "metadata", "radio", "vbi", "platform", "pcie",
		"mem2mem", "subdev", "stateless", "hw interface", "output",
	}
	for _, token := range rejectTokens {
		if strings.Contains(text, token) {
			return false
		}
	}

	acceptTokens := []string{"capture", "camera", "uvc", "webcam", "hdmi", "usb video", "video device"}
	for _, token := range acceptTokens {
		if strings.Contains(text, token) {
			return true
		}
	}

	// Если сервер не прислал понятное описание, оставляем обычный /dev/videoX как fallback.
	return true
}

func getVideoInfoData(usbClient *api.USBClient) (*models.VideoInfoData, error) {
	if usbClient == nil {
		return nil, fmt.Errorf(i18n.Current.ErrorNoConnection)
	}

	videoInfoCacheMu.Lock()
	if time.Since(videoInfoCachedAt) < 750*time.Millisecond {
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

	resp, err := usbClient.GetVideoInfo()
	if err != nil {
		videoInfoCacheMu.Lock()
		videoInfoCachedAt = time.Now()
		videoInfoCachedData = nil
		videoInfoCachedErr = err
		videoInfoCacheMu.Unlock()
		return nil, err
	}
	if resp == nil || !resp.Success || resp.Data == nil {
		err := fmt.Errorf("video info unavailable")
		videoInfoCacheMu.Lock()
		videoInfoCachedAt = time.Now()
		videoInfoCachedData = nil
		videoInfoCachedErr = err
		videoInfoCacheMu.Unlock()
		return nil, err
	}

	info, err := models.ParseVideoInfoData(resp.Data)
	videoInfoCacheMu.Lock()
	videoInfoCachedAt = time.Now()
	videoInfoCachedData = info
	videoInfoCachedErr = err
	videoInfoCacheMu.Unlock()
	return info, err
}

func resetVideoInfoCache() {
	videoInfoCacheMu.Lock()
	defer videoInfoCacheMu.Unlock()
	videoInfoCachedAt = time.Time{}
	videoInfoCachedData = nil
	videoInfoCachedErr = nil
}

func getAvailableVideoDevices(usbClient *api.USBClient) ([]models.SystemDevice, error) {
	if usbClient == nil {
		return nil, fmt.Errorf(i18n.Current.ErrorNoConnection)
	}

	merged := make(map[string]models.SystemDevice)

	if devices, err := usbClient.GetVideoDevices(); err == nil {
		for _, device := range devices {
			if isLikelyCaptureVideoDevice(device) {
				merged[device.Path] = device
			}
		}
	} else if devices, err := usbClient.GetSystemDevices(); err == nil {
		for _, device := range devices {
			if isLikelyCaptureVideoDevice(device) {
				merged[device.Path] = device
			}
		}
	} else {
		logrus.Debugf("system video devices unavailable: %v", err)
	}

	if info, err := getVideoInfoData(usbClient); err == nil && info != nil && strings.HasPrefix(info.Device, "/dev/video") {
		current := merged[info.Device]
		current.Path = info.Device
		if current.Name == "" {
			current.Name = filepath.Base(info.Device)
		}
		if current.Description == "" {
			current.Description = "Capture device"
		}
		current.Connected = info.Enabled || info.Streaming
		merged[info.Device] = current
	}

	if len(merged) == 0 {
		return nil, fmt.Errorf("capture video devices not found")
	}

	result := make([]models.SystemDevice, 0, len(merged))
	for _, device := range merged {
		result = append(result, device)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})
	return result, nil
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
		return models.VideoDeviceConfig{}, fmt.Errorf("capture video devices not found")
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
		return fmt.Errorf("video device is empty")
	}
	if !vw.beginVideoOperation() {
		return fmt.Errorf("video operation already in progress")
	}
	defer vw.endVideoOperation()

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

	if vw.isStreaming {
		vw.stopVideoInternal()
		time.Sleep(500 * time.Millisecond)
	}

	vw.startVideoWithParamsInternal(cfg.ToVideoStartRequest())
	return nil
}

func (vw *VideoWidget) StartConfiguredVideoAsync() {
	go func() {
		cfg, err := vw.resolvePreferredVideoConfig()
		if err != nil {
			logrus.Warnf("⚠️ cannot resolve preferred video config: %v", err)
			fyne.Do(func() {
				vw.statusLabel.SetText(fmt.Sprintf("❌ %v", err))
			})
			return
		}

		if err := vw.applyVideoDeviceConfig(cfg, true); err != nil {
			logrus.Warnf("⚠️ cannot start configured video: %v", err)
			fyne.Do(func() {
				vw.statusLabel.SetText(fmt.Sprintf("❌ %v", err))
			})
		}
	}()
}

func (vw *VideoWidget) StartVideoDeviceAsync(devicePath string) {
	go func() {
		devices, err := vw.GetAvailableVideoDevices()
		if err != nil {
			logrus.Warnf("⚠️ cannot load available video devices: %v", err)
			return
		}

		for _, device := range devices {
			if device.Path != devicePath {
				continue
			}
			cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
			cfg.DevicePath = device.Path
			cfg.DeviceName = device.Name
			if !hasSavedVideoDeviceConfig(device.Path) {
				if info, err := getVideoInfoData(vw.usbClient); err == nil && info != nil && info.Device == device.Path {
					cfg = mergeVideoConfigWithInfo(cfg, info)
				}
			}
			if err := vw.applyVideoDeviceConfig(cfg, true); err != nil {
				logrus.Warnf("⚠️ cannot start selected video device %s: %v", devicePath, err)
			}
			return
		}

		logrus.Warnf("⚠️ selected video device not found: %s", devicePath)
	}()
}

func (vw *VideoWidget) StopVideoAsync() {
	go vw.handleStopVideo()
}

func (vw *VideoWidget) ShowCurrentVideoSettings(showFullscreen bool) {
	cfg, err := vw.resolvePreferredVideoConfig()
	if err != nil {
		logrus.Warnf("⚠️ cannot open video settings: %v", err)
		return
	}
	vw.ShowVideoDeviceSettings(cfg.DevicePath, true, showFullscreen)
}

func (vw *VideoWidget) ShowVideoDeviceSettings(devicePath string, restartOnApply bool, showFullscreen bool) {
	if vw.usbClient == nil || vw.parentWindow == nil {
		return
	}

	go func() {
		devices, err := vw.GetAvailableVideoDevices()
		if err != nil {
			logrus.Warnf("⚠️ failed to load video devices: %v", err)
			return
		}

		var device models.SystemDevice
		for _, candidate := range devices {
			if candidate.Path == devicePath {
				device = candidate
				break
			}
		}
		if device.Path == "" {
			logrus.Warnf("⚠️ video device %s not found", devicePath)
			return
		}

		cfg := loadSavedVideoDeviceConfig(device.Path, device.Name)
		cfg.DevicePath = device.Path
		cfg.DeviceName = device.Name

		info := vw.fetchVideoInfoForStartDialog()
		if info != nil && info.Device == device.Path {
			cfg = mergeVideoConfigWithInfo(cfg, info)
		}

		fyne.Do(func() {
			if vw.startDialog == nil {
				vw.startDialog = view.NewVideoStartDialog(vw.parentWindow)
			}

			vw.startDialog.Configure(info, cfg.VideoWidth, cfg.VideoHeight, cfg.VideoFPS, cfg.VideoBitrate)
			vw.startDialog.SetDeviceLabel(fmt.Sprintf("%s\n%s", device.Name, device.Path))
			vw.startDialog.SetPrimaryAction("Применить")
			if showFullscreen && vw.isStreaming {
				vw.startDialog.SetExtraAction(i18n.Current.FullscreenButton, func() {
					vw.startDialog.Hide()
					vw.handleFullscreen()
				})
			} else {
				vw.startDialog.SetExtraAction("", nil)
			}

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

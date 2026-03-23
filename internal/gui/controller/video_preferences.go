package controller

import (
	"encoding/json"
	"strings"
	"time"

	"usbridge-client/internal/models"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

const videoPreferencesKey = "video_preferences_v1"

type videoPreferences struct {
	SelectedDevice string                              `json:"selected_device"`
	Devices        map[string]models.VideoDeviceConfig `json:"devices"`
}

func defaultVideoDeviceConfig(devicePath, deviceName string) models.VideoDeviceConfig {
	return models.VideoDeviceConfig{
		DevicePath:    devicePath,
		DeviceName:    deviceName,
		VideoWidth:    640,
		VideoHeight:   480,
		VideoFPS:      30,
		VideoQuality:  80,
		VideoBitrate:  "2000K",
		VideoMode:     models.VideoModeH264,
		LastAppliedAt: time.Now().Unix(),
	}
}

func loadVideoPreferences() videoPreferences {
	prefs := videoPreferences{
		Devices: make(map[string]models.VideoDeviceConfig),
	}

	app := fyne.CurrentApp()
	if app == nil {
		return prefs
	}

	raw := app.Preferences().StringWithFallback(videoPreferencesKey, "")
	if raw == "" {
		return prefs
	}

	if err := json.Unmarshal([]byte(raw), &prefs); err != nil {
		logrus.Warnf("⚠️ failed to parse saved video preferences: %v", err)
		prefs.Devices = make(map[string]models.VideoDeviceConfig)
		return prefs
	}

	if prefs.Devices == nil {
		prefs.Devices = make(map[string]models.VideoDeviceConfig)
	}

	return prefs
}

func saveVideoPreferences(prefs videoPreferences) {
	app := fyne.CurrentApp()
	if app == nil {
		return
	}

	if prefs.Devices == nil {
		prefs.Devices = make(map[string]models.VideoDeviceConfig)
	}

	data, err := json.Marshal(prefs)
	if err != nil {
		logrus.Warnf("⚠️ failed to save video preferences: %v", err)
		return
	}

	app.Preferences().SetString(videoPreferencesKey, string(data))
}

func loadSavedVideoDeviceConfig(devicePath, deviceName string) models.VideoDeviceConfig {
	prefs := loadVideoPreferences()
	if cfg, ok := prefs.Devices[devicePath]; ok {
		if cfg.DeviceName == "" {
			cfg.DeviceName = deviceName
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
		return cfg
	}
	return defaultVideoDeviceConfig(devicePath, deviceName)
}

func hasSavedVideoDeviceConfig(devicePath string) bool {
	if strings.TrimSpace(devicePath) == "" {
		return false
	}
	prefs := loadVideoPreferences()
	_, ok := prefs.Devices[devicePath]
	return ok
}

func saveVideoDeviceConfig(cfg models.VideoDeviceConfig) {
	if cfg.DevicePath == "" {
		return
	}

	prefs := loadVideoPreferences()
	if prefs.Devices == nil {
		prefs.Devices = make(map[string]models.VideoDeviceConfig)
	}

	cfg.LastAppliedAt = time.Now().Unix()
	prefs.SelectedDevice = cfg.DevicePath
	prefs.Devices[cfg.DevicePath] = cfg
	saveVideoPreferences(prefs)
}

func selectedVideoDevicePath() string {
	return loadVideoPreferences().SelectedDevice
}

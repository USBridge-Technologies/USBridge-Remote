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
		VideoWidth:    1280,
		VideoHeight:   720,
		VideoFPS:      60,
		VideoQuality:  100,
		VideoBitrate:  "20000K",
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

	// Remove stale server-alias entries (e.g. "auto") that are not real device paths.
	for path := range prefs.Devices {
		if !strings.Contains(path, "/") && !strings.Contains(path, ":") {
			delete(prefs.Devices, path)
		}
	}
	if prefs.SelectedDevice != "" && !strings.Contains(prefs.SelectedDevice, "/") && !strings.Contains(prefs.SelectedDevice, ":") {
		prefs.SelectedDevice = ""
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
			cfg.VideoQuality = 100
		}
		if cfg.VideoBitrate == "" {
			cfg.VideoBitrate = "20000K"
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
	logrus.Infof("🎯 [CODEC-TRACE] saveVideoDeviceConfig: wrote prefs.SelectedDevice=%q VideoMode=%q", prefs.SelectedDevice, cfg.VideoMode)
}

func selectedVideoDevicePath() string {
	return loadVideoPreferences().SelectedDevice
}

// correctSelectedVideoDevicePath updates prefs.SelectedDevice to newPath
// without touching any per-device saved config -- call this when
// resolvePreferredVideoConfig's device-list lookup falls back to a
// different device than the one saved as "selected".
//
// Without this, a stale SelectedDevice (e.g. a virtual display whose agent
// process has since restarted and forgotten it -- the agent only keeps
// virtual displays in memory, see server.go's virtualDisplayCreate) never
// self-heals: selectedVideoDevicePath() keeps returning the phantom path
// forever, so every future ShowCurrentVideoSettings (header/status-bar gear)
// opens the settings popup for a device that doesn't exist, any change the
// user makes there gets saved under that same phantom path, and the device
// that's actually streaming never sees it -- exactly the "picked H265, still
// streams H264" bug, confirmed live via [CODEC-TRACE] logging (2026-09-18).
func correctSelectedVideoDevicePath(newPath string) {
	if strings.TrimSpace(newPath) == "" {
		return
	}
	prefs := loadVideoPreferences()
	if prefs.SelectedDevice == newPath {
		return
	}
	logrus.Infof("🎯 [CODEC-TRACE] correctSelectedVideoDevicePath: prefs.SelectedDevice %q -> %q (previous device not in current device list)", prefs.SelectedDevice, newPath)
	prefs.SelectedDevice = newPath
	saveVideoPreferences(prefs)
}

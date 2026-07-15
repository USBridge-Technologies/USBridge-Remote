//go:build android

package service

import (
	"path/filepath"

	"usbridge-client/internal/api/moonlight"

	"github.com/sirupsen/logrus"
)

// initMoonlightConfigDir sets the persistent Android files dir for Moonlight identity storage
// so that client.key / client.pem survive app restarts and re-pairing is not needed every launch.
func initMoonlightConfigDir() {
	dir, err := getAndroidFilesDir()
	if err != nil || dir == "" {
		logrus.Warnf("⚠️ [Moonlight/Android] Cannot get files dir for identity storage: %v", err)
		return
	}
	configDir := filepath.Join(dir, "moonlight")
	logrus.Infof("🌕 [Moonlight/Android] Persistent identity dir: %s", configDir)
	moonlight.ConfigDirOverride = configDir
}

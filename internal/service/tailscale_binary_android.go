//go:build android

package service

import (
	"os"
	"path/filepath"
)

func getTailscaleBinaryPath() string {
	// On Android, we look in the app's native library directory
	// The binary is bundled as libtailscale.so to be extracted by Android
	libDir, err := getAndroidNativeLibraryDir()
	if err == nil && libDir != "" {
		localPath := filepath.Join(libDir, "libtailscale.so")
		if _, err := os.Stat(localPath); err == nil {
			return localPath
		}
	}
	return ""
}

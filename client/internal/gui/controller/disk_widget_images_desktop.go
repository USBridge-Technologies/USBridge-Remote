//go:build !android

package controller

import (
	"os"
	"strings"
)

// checkStoragePermission is usually true on desktop or handled by OS dialogs.
func (dw *DiskWidget) checkStoragePermission() bool {
	return true
}

// checkFileExists checks if a file exists on desktop.
func (dw *DiskWidget) checkFileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// convertAndroidURIToPath on desktop just handles file:// and raw paths.
func (dw *DiskWidget) convertAndroidURIToPath(uriString string, fileName string) string {
	if strings.HasPrefix(uriString, "file://") {
		return strings.TrimPrefix(uriString, "file://")
	}
	return uriString
}

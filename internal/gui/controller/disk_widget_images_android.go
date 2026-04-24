//go:build android

package controller

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
)

// checkStoragePermission checks for storage access on Android.
func (dw *DiskWidget) checkStoragePermission() bool {
	// SAF doesn't need legacy storage permissions
	return true 
}

// checkFileExists checks if a file exists using SAF for content URIs or os.Stat for regular paths.
func (dw *DiskWidget) checkFileExists(path string) bool {
	if strings.HasPrefix(path, "content://") {
		isGoogleDrive := strings.Contains(path, "com.google.android.apps.docs.storage")
		if isGoogleDrive {
			// Files from Google Drive might not be immediately available, but we add them anyway
			return true
		}
		if dw.safHelper != nil {
			file, err := dw.safHelper.OpenFileDescriptor(path, "r")
			if err == nil && file != nil {
				file.Close()
				return true
			}
		}
		return false
	}
	
	_, err := os.Stat(path)
	return err == nil
}

// convertAndroidURIToPath converts an Android document URI to a real file path.
func (dw *DiskWidget) convertAndroidURIToPath(uriString string, fileName string) string {
	if strings.HasPrefix(uriString, "content://com.android.externalstorage.documents/document/primary") {
		parts := strings.Split(uriString, "primary")
		if len(parts) >= 2 {
			relativePath := strings.TrimPrefix(parts[1], "%3A")
			relativePath = strings.TrimPrefix(relativePath, ":")
			relativePath = strings.ReplaceAll(relativePath, "%20", " ")
			relativePath = strings.ReplaceAll(relativePath, "%2F", "/")

			possiblePaths := []string{
				filepath.Join("/storage/emulated/0", relativePath),
				filepath.Join("/sdcard", relativePath),
				filepath.Join("/mnt/sdcard", relativePath),
				filepath.Join(os.Getenv("EXTERNAL_STORAGE"), relativePath),
			}

			for _, path := range possiblePaths {
				if path == "" {
					continue
				}
				if _, err := os.Stat(path); err == nil {
					logrus.Infof("✅ Found file at path: %s", path)
					return path
				}
			}

			return possiblePaths[0]
		}
	}

	if strings.HasPrefix(uriString, "content://") && strings.Contains(uriString, "/document/") {
		searchPaths := []string{
			"/storage/emulated/0",
			"/sdcard",
			"/mnt/sdcard",
			os.Getenv("EXTERNAL_STORAGE"),
		}

		for _, basePath := range searchPaths {
			if basePath == "" {
				continue
			}

			fullPath := filepath.Join(basePath, fileName)
			if _, err := os.Stat(fullPath); err == nil {
				logrus.Infof("✅ Found file in root: %s", fullPath)
				return fullPath
			}

			fullPath = filepath.Join(basePath, "Download", fileName)
			if _, err := os.Stat(fullPath); err == nil {
				logrus.Infof("✅ Found file in Download: %s", fullPath)
				return fullPath
			}
		}
	}

	if strings.HasPrefix(uriString, "file://") {
		return strings.TrimPrefix(uriString, "file://")
	}

	if strings.HasPrefix(uriString, "/") {
		return uriString
	}

	logrus.Warnf("⚠️ Could not convert URI, using as-is: %s", uriString)
	return uriString
}

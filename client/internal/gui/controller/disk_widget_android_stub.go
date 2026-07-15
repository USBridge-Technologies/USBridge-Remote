//go:build !android

package controller

import "github.com/sirupsen/logrus"

// handleAddImageAndroid is a stub for non-Android platforms
func (dw *DiskWidget) handleAddImageAndroid() {
	logrus.Warn("handleAddImageAndroid called on non-android platform")
}

// pickImageForDiskList is a stub for non-Android platforms
func (dw *DiskWidget) pickImageForDiskList() {
	logrus.Warn("pickImageForDiskList called on non-android platform")
}

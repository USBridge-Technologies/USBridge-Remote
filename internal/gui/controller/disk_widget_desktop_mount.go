//go:build !android
// +build !android

package controller

// handleAndroidNBDMount заглушка для desktop
func (dw *DiskWidget) handleAndroidNBDMount() {
	// На desktop не используется
}

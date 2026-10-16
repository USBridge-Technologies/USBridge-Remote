//go:build !android

package gui

// syncAndroidWiFiLock: Android only (wlan_android.go).
func (mw *MainWindow) syncAndroidWiFiLock(isStreaming bool) {}

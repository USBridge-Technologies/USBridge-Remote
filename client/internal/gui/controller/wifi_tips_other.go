//go:build !android

package controller

// The Wi-Fi tips are Android only (wifi_tips.go).

func androidWiFiScanAlwaysOn() int { return -1 }

func openAndroidSettings(actions []string) bool { return false }

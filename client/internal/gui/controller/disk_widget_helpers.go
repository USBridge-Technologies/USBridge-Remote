//go:build !android
// +build !android

package controller

import "runtime"

// isAndroid returns true if running on Android
func isAndroid() bool {
	return runtime.GOOS == "android"
}

// canUseDirectFileAccess returns true if os.Open() can be used
// On Android 10+ this is false - SAF is required
func canUseDirectFileAccess() bool {
	return !isAndroid()
}

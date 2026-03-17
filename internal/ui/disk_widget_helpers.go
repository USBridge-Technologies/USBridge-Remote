// +build !android

package ui

import "runtime"

// isAndroid возвращает true если запущено на Android
func isAndroid() bool {
	return runtime.GOOS == "android"
}

// canUseDirectFileAccess возвращает true если можно использовать os.Open()
// На Android 10+ это false - нужен SAF
func canUseDirectFileAccess() bool {
	return !isAndroid()
}

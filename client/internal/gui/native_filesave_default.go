//go:build !windows && !darwin && (!linux || android)

package gui

func nativeSaveAvailable() bool { return false }

func nativeSaveFile(uintptr, string, string, saveFilter) (string, error) {
	return "", nil
}

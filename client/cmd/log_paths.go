package main

import (
	"os"
	"path/filepath"
	"runtime"
)

// ensureLogDir falls back to the user cache dir before os.TempDir: when the
// binary's own dir isn't writable (an AppImage's read-only squashfs mount,
// a root-owned install) the old straight-to-TempDir fallback put app.log on
// /tmp -- tmpfs, i.e. RAM, on Arch/Fedora -- and lost it on every reboot.
func ensureLogDir() string {
	logDir := resolveLogDir()
	if dirWritable(logDir) {
		return logDir
	}
	if cache, err := os.UserCacheDir(); err == nil {
		if dir := filepath.Join(cache, "usbridge-client", "logs"); dirWritable(dir) {
			return dir
		}
	}

	fallbackDir := filepath.Join(os.TempDir(), "usbridge-client-logs")
	_ = os.MkdirAll(fallbackDir, 0755)
	return fallbackDir
}

// dirWritable creates dir and proves a file can be created in it --
// MkdirAll alone succeeds on an existing read-only dir.
func dirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	_ = os.Remove(name)
	return true
}

func getAppRootDir(exePath string) string {
	dir := filepath.Dir(exePath)
	if filepath.Base(dir) == "bin" {
		return filepath.Dir(dir)
	}
	return dir
}

func resolveLogDir() string {
	if logDir := os.Getenv("USBRIDGE_LOG_DIR"); logDir != "" {
		return logDir
	}

	if runtime.GOOS == "windows" {
		if exePath, err := os.Executable(); err == nil {
			return filepath.Join(getAppRootDir(exePath), "logs")
		}
		if wd, err := os.Getwd(); err == nil {
			return filepath.Join(wd, "logs")
		}
		return filepath.Join(".", "logs")
	}

	if runtime.GOOS == "darwin" {
		if homeDir, err := os.UserHomeDir(); err == nil && homeDir != "" {
			return filepath.Join(homeDir, "Library", "Logs", "USBridgeClient")
		}
	}

	if exePath, err := os.Executable(); err == nil {
		return filepath.Join(getAppRootDir(exePath), "logs")
	}
	if wd, err := os.Getwd(); err == nil {
		return filepath.Join(wd, "logs")
	}
	return filepath.Join(".", "logs")
}

func appLogPath() string {
	return filepath.Join(ensureLogDir(), "app.log")
}

package main

import (
	"os"
	"path/filepath"
	"runtime"
)

func ensureLogDir() string {
	logDir := resolveLogDir()
	if err := os.MkdirAll(logDir, 0755); err == nil {
		return logDir
	}

	fallbackDir := filepath.Join(os.TempDir(), "usbridge-client-logs")
	_ = os.MkdirAll(fallbackDir, 0755)
	return fallbackDir
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

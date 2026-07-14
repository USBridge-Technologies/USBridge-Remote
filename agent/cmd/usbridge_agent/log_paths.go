package main

import (
	"os"
	"path/filepath"
)

func ensureLogDir() string {
	logDir := resolveLogDir()
	if err := os.MkdirAll(logDir, 0o755); err == nil {
		return logDir
	}

	fallbackDir := filepath.Join(os.TempDir(), "usbridge-agent-logs")
	_ = os.MkdirAll(fallbackDir, 0o755)
	return fallbackDir
}

func resolveLogDir() string {
	if logDir := os.Getenv("USBRIDGE_LOG_DIR"); logDir != "" {
		return logDir
	}
	return resolvePlatformLogDir()
}

func appLogPath() string {
	return filepath.Join(ensureLogDir(), "app.log")
}

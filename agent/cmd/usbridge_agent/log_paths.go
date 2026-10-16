package main

import (
	"os"
	"path/filepath"
)

func ensureLogDir() string {
	logDir := resolveLogDir()
	if dirWritable(logDir) {
		return logDir
	}
	// On Windows the default is <install dir>\logs, which only the
	// LocalSystem service may write under Program Files: a window the user
	// opened there used to log nowhere at all.
	if cache, err := os.UserCacheDir(); err == nil {
		if dir := filepath.Join(cache, "usbridge-agent", "logs"); dirWritable(dir) {
			return dir
		}
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

// dirWritable creates dir if needed and checks a file can actually be
// created in it -- MkdirAll alone succeeds on an existing read-only dir.
func dirWritable(dir string) bool {
	if err := os.MkdirAll(dir, 0o755); err != nil {
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

//go:build windows
// +build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func init() {
	writeStartupTrace("env_windows: init entered")

	exePath, err := os.Executable()
	if err != nil {
		writeStartupTrace("env_windows: os.Executable failed: %v", err)
		return
	}
	exeDir := filepath.Dir(exePath)
	writeStartupTrace("env_windows: exeDir=%s", exeDir)
	_ = os.Chdir(exeDir)

	prependPath(exeDir)
	prependPath(filepath.Join(exeDir, "bin"))

	setIfEmpty("GST_PLUGIN_PATH", filepath.Join(exeDir, "lib", "gstreamer-1.0"))
	setIfEmpty("GST_PLUGIN_SYSTEM_PATH", filepath.Join(exeDir, "lib", "gstreamer-1.0"))
	writeStartupTrace("env_windows: init done")
}

func prependPath(dir string) {
	if dir == "" {
		return
	}
	current := os.Getenv("PATH")
	parts := strings.Split(current, string(os.PathListSeparator))
	for _, p := range parts {
		if strings.EqualFold(p, dir) {
			return
		}
	}
	if current == "" {
		_ = os.Setenv("PATH", dir)
		return
	}
	_ = os.Setenv("PATH", dir+string(os.PathListSeparator)+current)
}

func setIfEmpty(key, value string) {
	if os.Getenv(key) == "" && value != "" {
		_ = os.Setenv(key, value)
	}
}

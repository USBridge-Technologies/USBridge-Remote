//go:build windows
// +build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

func init() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}
	exeDir := filepath.Dir(exePath)
	_ = os.Chdir(exeDir)

	prependPath(exeDir)
	prependPath(filepath.Join(exeDir, "bin"))

	setIfEmpty("GST_PLUGIN_PATH", filepath.Join(exeDir, "lib", "gstreamer-1.0"))
	setIfEmpty("GST_PLUGIN_SYSTEM_PATH", filepath.Join(exeDir, "lib", "gstreamer-1.0"))
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

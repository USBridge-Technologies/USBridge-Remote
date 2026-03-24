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

	pluginDir := filepath.Join(exeDir, "lib", "gstreamer-1.0")
	setIfEmpty("GST_PLUGIN_PATH", pluginDir)
	setIfEmpty("GST_PLUGIN_SYSTEM_PATH", pluginDir)
	setIfEmpty("GST_REGISTRY", filepath.Join(os.TempDir(), "usbridge-gstreamer-registry.bin"))

	for _, scannerPath := range []string{
		filepath.Join(exeDir, "libexec", "gstreamer-1.0", "gst-plugin-scanner.exe"),
		filepath.Join(exeDir, "libexec", "gstreamer-1.0", "gst-plugin-scanner"),
		filepath.Join(exeDir, "bin", "gst-plugin-scanner.exe"),
		filepath.Join(exeDir, "bin", "gst-plugin-scanner"),
		filepath.Join(exeDir, "gst-plugin-scanner.exe"),
	} {
		if _, err := os.Stat(scannerPath); err == nil {
			setIfEmpty("GST_PLUGIN_SCANNER", scannerPath)
			writeStartupTrace("env_windows: GST_PLUGIN_SCANNER=%s", scannerPath)
			break
		}
	}
	writeStartupTrace("env_windows: GST_PLUGIN_PATH=%s", os.Getenv("GST_PLUGIN_PATH"))
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

//go:build linux

package video

import (
	"strings"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/capture"
)

func sourceFormatForPlatform() string {
	return "yuv420p"
}

func buildPlatformArgs(cfg config.Config, req api.VideoStartRequest, mode, codec string) []string {
	env := capture.GetLinuxEnv()
	if env == "Wayland" {
		return buildPlatformArgsWayland(cfg, req, mode, codec)
	}
	return buildPlatformArgsX11(cfg, req, mode, codec)
}

func detectPlatformVideoAdapters() []string {
	return nil
}

func preferredCodecsForAdapters(_ []string) []string {
	return nil
}

func captureModesForPlatform(configured string) []string {
	mode := strings.ToLower(strings.TrimSpace(configured))
	if mode == "" || mode == "auto" || mode == "dxgi" || (mode == "x11grab" && capture.GetLinuxEnv() == "Wayland") {
		if capture.GetLinuxEnv() == "Wayland" {
			mode = "pipewire"
		} else {
			mode = "x11grab"
		}
	}
	return []string{mode}
}

func platformCodecFallbacks() []string {
	return []string{"libx264", "h264_nvenc", "h264_qsv"}
}

func platformAutoCodec(_ string) string {
	return ""
}

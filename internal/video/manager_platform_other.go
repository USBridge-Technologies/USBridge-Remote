//go:build !darwin && !windows

package video

import (
	"fmt"
	"strings"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
)

func sourceFormatForPlatform() string {
	return "BGRA"
}

func buildPlatformArgs(cfg config.Config, req api.VideoStartRequest, mode, codec string) []string {
	input := []string{"-f", "gdigrab", "-framerate", fmt.Sprintf("%d", req.VideoFPS), "-draw_mouse", "1", "-i", "desktop"}
	videoFilter := softwareFilter(req.VideoWidth, req.VideoHeight, codec)
	if strings.EqualFold(mode, "dxgi") {
		input = []string{"-f", "lavfi", "-i", fmt.Sprintf("ddagrab=framerate=%d:draw_mouse=1", req.VideoFPS)}
		videoFilter = dxgiFilter(req.VideoWidth, req.VideoHeight, codec)
	}

	args := append(input,
		"-probesize", "32",
		"-analyzeduration", "0",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-fps_mode", "passthrough",
		"-an",
		"-vf", videoFilter,
		"-c:v", codec,
	)
	args = append(args, codecArgs(codec)...)
	args = append(args,
		"-sc_threshold", "0",
		"-g", fmt.Sprintf("%d", req.VideoFPS),
		"-keyint_min", fmt.Sprintf("%d", req.VideoFPS),
		"-bf", "0",
		"-bsf:v", "dump_extra=freq=keyframe",
		"-flush_packets", "1",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-b:v", firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate),
		"-maxrate", firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate),
		"-bufsize", firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate),
		"-payload_type", "96",
		"-f", "rtp",
		fmt.Sprintf("rtp://%s:%d?config-interval=1&pkt_size=1200", req.ClientHost, req.ClientPort),
	)
	return args
}

func detectPlatformVideoAdapters() []string {
	return nil
}

func preferredCodecsForAdapters(_ []string) []string {
	return nil
}

func captureModesForPlatform(configured string) []string {
	mode := strings.ToLower(strings.TrimSpace(configured))
	if mode == "" {
		mode = "dxgi"
	}
	return []string{mode}
}

func platformCodecFallbacks() []string {
	return []string{"h264_amf", "h264_nvenc", "h264_qsv", "libx264"}
}

func platformAutoCodec(_ string) string {
	return ""
}

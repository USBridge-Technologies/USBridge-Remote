//go:build windows

package video

import (
	"fmt"
	"log"
	"os/exec"
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
		"-b:v", firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate),
		"-maxrate", firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate),
		"-bufsize", firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate),
		"-payload_type", "96",
		"-f", "rtp",
		fmt.Sprintf("rtp://127.0.0.1:%d?pkt_size=1200", cfg.VideoUDPPort),
	)
	return args
}

func detectPlatformVideoAdapters() []string {
	cmd := exec.Command(
		"powershell",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		"Get-CimInstance Win32_VideoController | Select-Object -ExpandProperty Name",
	)
	output, err := cmd.Output()
	if err != nil {
		log.Printf("[video] adapter probe skipped: %v", err)
		return nil
	}

	lines := strings.Split(string(output), "\n")
	adapters := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		adapters = append(adapters, name)
	}
	return adapters
}

func preferredCodecsForAdapters(adapters []string) []string {
	order := make([]string, 0, 4)
	for _, adapter := range adapters {
		switch {
		case containsAny(adapter, "amd", "radeon"):
			order = append(order, "h264_amf")
		case containsAny(adapter, "nvidia", "geforce", "quadro", "rtx", "gtx"):
			order = append(order, "h264_nvenc")
		case containsAny(adapter, "intel", "iris", "uhd", "arc"):
			order = append(order, "h264_qsv")
		}
	}
	return appendUnique(append(order, platformCodecFallbacks()...)...)
}

func captureModesForPlatform(configured string) []string {
	mode := strings.ToLower(strings.TrimSpace(configured))
	if mode == "" {
		mode = "dxgi"
	}
	modes := []string{mode}
	if mode == "dxgi" {
		modes = append(modes, "gdigrab")
	}
	return modes
}

func platformCodecFallbacks() []string {
	return []string{"h264_amf", "h264_nvenc", "h264_qsv", "libx264"}
}

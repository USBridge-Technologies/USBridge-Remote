//go:build linux

package video

import (
	"fmt"
	"strings"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
)

func buildPlatformArgsX11(cfg config.Config, req api.VideoStartRequest, mode, codec string) []string {
	input := []string{"-f", "x11grab", "-framerate", fmt.Sprintf("%d", req.VideoFPS), "-i", ":0.0"}
	
	if strings.HasPrefix(req.VideoDevice, "display:") {
		idx := strings.TrimPrefix(req.VideoDevice, "display:")
		input = []string{"-f", "x11grab", "-framerate", fmt.Sprintf("%d", req.VideoFPS), "-i", fmt.Sprintf(":0.0+%s", idx)}
	}

	videoFilter := softwareFilter(req.VideoWidth, req.VideoHeight, codec)

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

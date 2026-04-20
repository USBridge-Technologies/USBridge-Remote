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

	if strings.EqualFold(codec, "h264_vaapi") {
		return buildVaapiFFmpegArgs(input, req, cfg)
	}

	videoFilter := softwareFilter(req.VideoWidth, req.VideoHeight, codec)
	return appendCommonFFmpegArgs(input, req, cfg, videoFilter, codec)
}

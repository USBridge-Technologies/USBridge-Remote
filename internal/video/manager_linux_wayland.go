//go:build linux

package video

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
)

func hasPipewireDemuxer(ffmpegPath string) bool {
	out, err := exec.Command(ffmpegPath, "-demuxers").Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), "pipewire")
}

func buildPlatformArgsWayland(cfg config.Config, req api.VideoStartRequest, mode, codec string) []string {
	fd := capture.GetPortalPipeWireFD()
	nodeID := capture.GetPortalPipeWireNodeID()

	if fd > 0 {
		// GStreamer pipewiresrc — child process receives fd=3 via ExtraFiles[0]
		return buildGstreamerArgs(cfg, req, 3, nodeID)
	}

	if hasPipewireDemuxer(cfg.FFmpegPath) && nodeID > 0 {
		videoFilter := softwareFilter(req.VideoWidth, req.VideoHeight, codec)
		args := []string{"-f", "pipewire", "-i", fmt.Sprintf("%d", nodeID)}
		args = appendCommonFFmpegArgs(args, req, cfg, videoFilter, codec)
		return args
	}

	// Fallback: x11grab via XWayland
	videoFilter := softwareFilter(req.VideoWidth, req.VideoHeight, codec)
	args := []string{"-f", "x11grab", "-framerate", fmt.Sprintf("%d", req.VideoFPS), "-i", ":0.0"}
	args = appendCommonFFmpegArgs(args, req, cfg, videoFilter, codec)
	return args
}

func buildGstreamerArgs(cfg config.Config, req api.VideoStartRequest, childFD int, nodeID uint32) []string {
	bitrateKbps := parseBitrateKbps(firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate))

	args := []string{"gst-launch-1.0", "-e"}

	// pipewiresrc element
	args = append(args, "pipewiresrc", fmt.Sprintf("fd=%d", childFD), "do-timestamp=true")
	if nodeID > 0 {
		args = append(args, fmt.Sprintf("path=%d", nodeID))
	}

	args = append(args,
		"!", "videoconvert",
		"!", "videoscale",
		"!", fmt.Sprintf("video/x-raw,width=%d,height=%d,format=I420", req.VideoWidth, req.VideoHeight),
		"!", "videorate",
		"!", fmt.Sprintf("video/x-raw,framerate=%d/1", req.VideoFPS),
		"!", "x264enc",
		"tune=zerolatency",
		fmt.Sprintf("bitrate=%d", bitrateKbps),
		"speed-preset=ultrafast",
		fmt.Sprintf("key-int-max=%d", req.VideoFPS),
		"bframes=0",
		"byte-stream=true",
		"!", "video/x-h264,profile=constrained-baseline",
		"!", "rtph264pay", "config-interval=1", "pt=96", "mtu=1200",
		"!", "udpsink",
		fmt.Sprintf("host=%s", req.ClientHost),
		fmt.Sprintf("port=%d", req.ClientPort),
		"sync=false",
	)

	return args
}

// parseBitrateKbps converts strings like "4000K", "4M", "4000" to an integer kbps value.
func parseBitrateKbps(s string) int {
	s = strings.TrimSpace(s)
	if strings.HasSuffix(s, "M") {
		if n, err := strconv.Atoi(strings.TrimSuffix(s, "M")); err == nil {
			return n * 1000
		}
	}
	if strings.HasSuffix(s, "K") || strings.HasSuffix(s, "k") {
		if n, err := strconv.Atoi(s[:len(s)-1]); err == nil {
			return n
		}
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 4000
}

func appendCommonFFmpegArgs(args []string, req api.VideoStartRequest, cfg config.Config, videoFilter, codec string) []string {
	args = append(args,
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

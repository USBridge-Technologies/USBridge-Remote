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
		return buildGstreamerArgs(cfg, req, 3, nodeID, codec)
	}

	if hasPipewireDemuxer(cfg.FFmpegPath) && nodeID > 0 {
		input := []string{"-f", "pipewire", "-i", fmt.Sprintf("%d", nodeID)}
		if strings.EqualFold(codec, "h264_vaapi") {
			return buildVaapiFFmpegArgs(input, req, cfg)
		}
		return appendCommonFFmpegArgs(input, req, cfg, softwareFilter(req.VideoWidth, req.VideoHeight, codec), codec)
	}

	// Fallback: x11grab via XWayland
	input := []string{"-f", "x11grab", "-framerate", fmt.Sprintf("%d", req.VideoFPS), "-i", ":0.0"}
	if strings.EqualFold(codec, "h264_vaapi") {
		return buildVaapiFFmpegArgs(input, req, cfg)
	}
	return appendCommonFFmpegArgs(input, req, cfg, softwareFilter(req.VideoWidth, req.VideoHeight, codec), codec)
}

func buildGstreamerArgs(cfg config.Config, req api.VideoStartRequest, childFD int, nodeID uint32, codec string) []string {
	bitrateKbps := parseBitrateKbps(firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate))
	gstEncoder := gstreamerEncoderForCodec(codec)

	args := []string{"gst-launch-1.0", "-e"}
	args = append(args, "pipewiresrc", fmt.Sprintf("fd=%d", childFD), "do-timestamp=true")
	if nodeID > 0 {
		args = append(args, fmt.Sprintf("path=%d", nodeID))
	}

	tail := []string{
		"!", "rtph264pay", "config-interval=1", "pt=96", "mtu=1200",
		"!", "udpsink",
		fmt.Sprintf("host=%s", req.ClientHost),
		fmt.Sprintf("port=%d", req.ClientPort),
		"sync=false",
	}

	switch gstEncoder {
	case "nvenc":
		args = append(args,
			"!", "videoconvert",
			"!", "videoscale",
			"!", fmt.Sprintf("video/x-raw,width=%d,height=%d", req.VideoWidth, req.VideoHeight),
			"!", "videorate",
			"!", fmt.Sprintf("video/x-raw,framerate=%d/1", req.VideoFPS),
			"!", "nvh264enc",
			fmt.Sprintf("bitrate=%d", bitrateKbps),
			"preset=low-latency-hq",
			"rc-mode=cbr-ld-hq",
			fmt.Sprintf("gop-size=%d", req.VideoFPS),
			"!", "video/x-h264,profile=baseline",
		)
	case "vaapi", "qsv":
		args = append(args,
			"!", "videoconvert",
			"!", "videoscale",
			"!", fmt.Sprintf("video/x-raw,width=%d,height=%d,format=NV12", req.VideoWidth, req.VideoHeight),
			"!", "videorate",
			"!", fmt.Sprintf("video/x-raw,framerate=%d/1", req.VideoFPS),
			"!", "vaapih264enc",
			"rate-control=cbr",
			fmt.Sprintf("bitrate=%d", bitrateKbps),
			fmt.Sprintf("keyframe-period=%d", req.VideoFPS),
			"!", "video/x-h264,profile=constrained-baseline",
		)
	default: // software x264enc
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
		)
	}

	return append(args, tail...)
}

// buildVaapiFFmpegArgs builds FFmpeg args for h264_vaapi encoding.
// VAAPI requires -vaapi_device before the input and a special hwupload filter.
func buildVaapiFFmpegArgs(input []string, req api.VideoStartRequest, cfg config.Config) []string {
	bitrateStr := firstNonEmpty(req.VideoBitrate, cfg.VideoBitrate)
	device := detectVaapiDevice()
	args := []string{"-vaapi_device", device}
	args = append(args, input...)
	args = append(args,
		"-probesize", "32",
		"-analyzeduration", "0",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-fps_mode", "passthrough",
		"-an",
		"-vf", fmt.Sprintf("scale=%d:%d,format=nv12,hwupload", req.VideoWidth, req.VideoHeight),
		"-c:v", "h264_vaapi",
		"-profile:v", "constrained_baseline",
		"-level", "3.2",
		"-sc_threshold", "0",
		"-g", fmt.Sprintf("%d", req.VideoFPS),
		"-keyint_min", fmt.Sprintf("%d", req.VideoFPS),
		"-bf", "0",
		"-bsf:v", "dump_extra=freq=keyframe",
		"-flush_packets", "1",
		"-muxdelay", "0",
		"-muxpreload", "0",
		"-b:v", bitrateStr,
		"-maxrate", bitrateStr,
		"-bufsize", bitrateStr,
		"-payload_type", "96",
		"-f", "rtp",
		fmt.Sprintf("rtp://%s:%d?config-interval=1&pkt_size=1200", req.ClientHost, req.ClientPort),
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

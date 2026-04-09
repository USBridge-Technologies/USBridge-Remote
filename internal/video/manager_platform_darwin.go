//go:build darwin

package video

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
)

func sourceFormatForPlatform() string {
	return "BGR0"
}

func buildPlatformArgs(cfg config.Config, req api.VideoStartRequest, _ string, codec string) []string {
	devices := detectAVFoundationVideoDevices(cfg.FFmpegPath)
	device := resolveDarwinCaptureDeviceFromList(devices, req.VideoDevice)
	log.Printf("[video/darwin] capture selection requested=%q resolved=%q devices=%s", req.VideoDevice, device, describeAVFoundationDevices(devices))
	args := []string{
		"-f", "avfoundation",
		"-framerate", fmt.Sprintf("%d", req.VideoFPS),
		"-capture_cursor", "1",
		"-i", fmt.Sprintf("%s:none", device),
		"-probesize", "32",
		"-analyzeduration", "0",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-fps_mode", "passthrough",
		"-an",
		"-vf", softwareFilter(req.VideoWidth, req.VideoHeight, codec),
		"-c:v", codec,
	}
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
		fmt.Sprintf("rtp://%s:%d?pkt_size=1200", req.ClientHost, req.ClientPort),
	)
	return args
}

func detectPlatformVideoAdapters() []string {
	return nil
}

func preferredCodecsForAdapters(_ []string) []string {
	return []string{"libx264", "h264_videotoolbox"}
}

func captureModesForPlatform(configured string) []string {
	mode := strings.ToLower(strings.TrimSpace(configured))
	if mode == "" || mode == "dxgi" || mode == "gdigrab" {
		mode = "avfoundation"
	}
	return []string{mode}
}

func platformCodecFallbacks() []string {
	return []string{"libx264", "h264_videotoolbox"}
}

func resolveDarwinCaptureDevice(ffmpegPath, requested string) string {
	devices := detectAVFoundationVideoDevices(ffmpegPath)
	return resolveDarwinCaptureDeviceFromList(devices, requested)
}

func resolveDarwinCaptureDeviceFromList(devices []avfoundationDevice, requested string) string {
	if len(devices) == 0 {
		return "Capture screen 0"
	}

	requested = strings.TrimSpace(requested)
	if requested == "" {
		for _, device := range devices {
			if device.screen {
				return strconv.Itoa(device.index)
			}
		}
		return strconv.Itoa(devices[0].index)
	}

	if strings.HasPrefix(requested, "display:") {
		if idx, err := strconv.Atoi(strings.TrimPrefix(requested, "display:")); err == nil {
			screenIndex := 0
			for _, device := range devices {
				if !device.screen {
					continue
				}
				if screenIndex == idx {
					return strconv.Itoa(device.index)
				}
				screenIndex++
			}
		}
	}

	if strings.HasPrefix(requested, "avfoundation:") {
		requested = strings.TrimSpace(strings.TrimPrefix(requested, "avfoundation:"))
	}

	for _, device := range devices {
		if requested == device.name || requested == strconv.Itoa(device.index) {
			return strconv.Itoa(device.index)
		}
	}

	for _, device := range devices {
		if device.screen {
			return strconv.Itoa(device.index)
		}
	}
	return strconv.Itoa(devices[0].index)
}

func describeAVFoundationDevices(devices []avfoundationDevice) string {
	if len(devices) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(devices))
	for _, device := range devices {
		kind := "camera"
		if device.screen {
			kind = "screen"
		}
		parts = append(parts, fmt.Sprintf("%d:%s:%q", device.index, kind, device.name))
	}
	return strings.Join(parts, ", ")
}

type avfoundationDevice struct {
	index  int
	name   string
	screen bool
}

func detectAVFoundationVideoDevices(ffmpegPath string) []avfoundationDevice {
	cmd := exec.Command(ffmpegPath, "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "")
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) == 0 {
		log.Printf("[video] avfoundation device probe skipped: %v", err)
		return nil
	}

	lines := strings.Split(string(output), "\n")
	devices := make([]avfoundationDevice, 0, 4)
	inVideoSection := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "AVFoundation video devices") {
			inVideoSection = true
			continue
		}
		if strings.Contains(line, "AVFoundation audio devices") {
			inVideoSection = false
			continue
		}
		if !inVideoSection {
			continue
		}
		end := strings.LastIndex(line, "]")
		start := strings.LastIndex(line[:end], "[")
		if start < 0 || end <= start+1 {
			continue
		}
		index, err := strconv.Atoi(line[start+1 : end])
		if err != nil {
			continue
		}
		name := strings.TrimSpace(line[end+1:])
		if name == "" {
			continue
		}
		devices = append(devices, avfoundationDevice{
			index:  index,
			name:   name,
			screen: strings.Contains(strings.ToLower(name), "capture screen"),
		})
	}
	return devices
}

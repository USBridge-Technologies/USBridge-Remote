//go:build darwin

package video

/*
#cgo LDFLAGS: -framework CoreGraphics -framework CoreFoundation
#include <ApplicationServices/ApplicationServices.h>
#include <CoreFoundation/CoreFoundation.h>
#include <stdlib.h>
#include <stdio.h>

int usbridge_preflight_screen_capture_access() {
    if (__builtin_available(macOS 10.15, *)) {
        return CGPreflightScreenCaptureAccess() ? 1 : 0;
    }
    return -1;
}

char* usbridge_active_displays_summary() {
    uint32_t count = 0;
    if (CGGetActiveDisplayList(0, NULL, &count) != kCGErrorSuccess || count == 0) {
        char* out = (char*)malloc(32);
        if (out) snprintf(out, 32, "none");
        return out;
    }

    CGDirectDisplayID *displays = (CGDirectDisplayID*)calloc(count, sizeof(CGDirectDisplayID));
    if (!displays) {
        char* out = (char*)malloc(32);
        if (out) snprintf(out, 32, "alloc-failed");
        return out;
    }
    if (CGGetActiveDisplayList(count, displays, &count) != kCGErrorSuccess) {
        free(displays);
        char* out = (char*)malloc(32);
        if (out) snprintf(out, 32, "query-failed");
        return out;
    }

    size_t cap = 128 + (size_t)count * 96;
    char* out = (char*)malloc(cap);
    if (!out) {
        free(displays);
        return NULL;
    }

    size_t used = 0;
    used += snprintf(out + used, cap - used, "count=%u", count);
    for (uint32_t i = 0; i < count && used < cap; i++) {
        CGRect bounds = CGDisplayBounds(displays[i]);
        used += snprintf(
            out + used,
            cap - used,
            "%s#%u:%ux%u@%.0f,%.0f",
            i == 0 ? " " : ", ",
            displays[i],
            (unsigned)bounds.size.width,
            (unsigned)bounds.size.height,
            bounds.origin.x,
            bounds.origin.y
        );
    }

    free(displays);
    return out;
}
*/
import "C"

import (
	"fmt"
	"log"
	"os/exec"
	"strconv"
	"strings"
	"unsafe"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
)

func sourceFormatForPlatform() string {
	return strings.ToUpper(darwinInputPixelFormat())
}

func darwinInputPixelFormat() string {
	return "uyvy422"
}

func darwinSoftwareFilter(width, height int, codec string) string {
	if prefersNV12(codec) {
		return fmt.Sprintf("format=bgra,scale=%d:%d,format=nv12", width, height)
	}
	return fmt.Sprintf("format=bgra,scale=%d:%d,format=yuv420p", width, height)
}

func buildPlatformArgs(cfg config.Config, req api.VideoStartRequest, _ string, codec string) []string {
	devices := detectAVFoundationVideoDevices(cfg.FFmpegPath)
	device := resolveDarwinCaptureDeviceFromList(devices, req.VideoDevice)
	log.Printf(
		"[video/darwin] capture diagnostics permission=%s displays=%s requested=%q resolved=%q input_format=%s devices=%s",
		darwinScreenCapturePermission(),
		darwinActiveDisplaysSummary(),
		req.VideoDevice,
		device,
		darwinInputPixelFormat(),
		describeAVFoundationDevices(devices),
	)
	args := []string{
		"-f", "avfoundation",
		"-framerate", fmt.Sprintf("%d", req.VideoFPS),
		"-i", device,
		"-probesize", "32",
		"-analyzeduration", "0",
		"-use_wallclock_as_timestamps", "1",
		"-fflags", "nobuffer",
		"-flags", "low_delay",
		"-an",
		"-vf", darwinSoftwareFilter(req.VideoWidth, req.VideoHeight, codec),
		"-r", fmt.Sprintf("%d", req.VideoFPS),
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
		fmt.Sprintf("rtp://%s:%d?config-interval=1&pkt_size=1200", req.ClientHost, req.ClientPort),
	)
	return args
}

func detectPlatformVideoAdapters() []string {
	return nil
}

func preferredCodecsForAdapters(_ []string) []string {
	return []string{"h264_videotoolbox", "libx264"}
}

func captureModesForPlatform(configured string) []string {
	mode := strings.ToLower(strings.TrimSpace(configured))
	if mode == "" || mode == "dxgi" || mode == "gdigrab" {
		mode = "avfoundation"
	}
	return []string{mode}
}

func platformCodecFallbacks() []string {
	return []string{"h264_videotoolbox", "libx264"}
}

func attachPipeWireFD(_ *exec.Cmd) {}

func platformAutoCodec(_ string) string {
	return ""
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

func darwinScreenCapturePermission() string {
	switch int(C.usbridge_preflight_screen_capture_access()) {
	case 1:
		return "granted"
	case 0:
		return "denied"
	default:
		return "unknown"
	}
}

func darwinActiveDisplaysSummary() string {
	raw := C.usbridge_active_displays_summary()
	if raw == nil {
		return "unavailable"
	}
	defer C.free(unsafe.Pointer(raw))
	summary := strings.TrimSpace(C.GoString(raw))
	if summary == "" {
		return "empty"
	}
	return summary
}

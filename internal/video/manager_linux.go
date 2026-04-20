//go:build linux

package video

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
)

func attachPipeWireFD(cmd *exec.Cmd) {
	if capture.GetLinuxEnv() != "Wayland" {
		return
	}
	fd := capture.GetPortalPipeWireFD()
	if fd <= 0 {
		return
	}
	dupFD, err := syscall.Dup(fd)
	if err != nil {
		log.Printf("[video] failed to dup PipeWire FD: %v", err)
		return
	}
	f := os.NewFile(uintptr(dupFD), "pipewire-fd")
	cmd.ExtraFiles = append(cmd.ExtraFiles, f)
	log.Printf("[video] Wayland PipeWire FD attached to child process as ExtraFiles[%d] (Child FD: %d)", len(cmd.ExtraFiles)-1, 2+len(cmd.ExtraFiles))
}

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
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	// Order matters: we'll preserve insertion order via a slice
	var order []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "card") || strings.Contains(name, "-") {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/sys/class/drm/%s/device/vendor", name))
		if err != nil {
			continue
		}
		vendor := strings.TrimSpace(string(data))
		var v string
		switch vendor {
		case "0x10de":
			v = "nvidia"
		case "0x8086":
			v = "intel"
		case "0x1002":
			v = "amd"
		}
		if v != "" && !seen[v] {
			seen[v] = true
			order = append(order, v)
		}
	}
	return order
}

func preferredCodecsForAdapters(adapters []string) []string {
	var codecs []string
	for _, a := range adapters {
		switch strings.ToLower(a) {
		case "nvidia":
			codecs = append(codecs, "h264_nvenc")
		case "intel":
			codecs = append(codecs, "h264_qsv", "h264_vaapi")
		case "amd":
			codecs = append(codecs, "h264_vaapi")
		}
	}
	return codecs
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
	return []string{"h264_nvenc", "h264_qsv", "h264_vaapi", "libx264"}
}

func platformAutoCodec(_ string) string {
	return ""
}

// detectVaapiDevice returns the first available /dev/dri/renderD* device.
func detectVaapiDevice() string {
	for i := 128; i < 132; i++ {
		path := fmt.Sprintf("/dev/dri/renderD%d", i)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "/dev/dri/renderD128"
}

// detectGStreamerElement returns true if the named GStreamer element is available.
func detectGStreamerElement(name string) bool {
	return exec.Command("gst-inspect-1.0", "--exists", name).Run() == nil
}

// gstreamerEncoderForCodec maps an FFmpeg codec name to a GStreamer encoder type.
// Returns "nvenc", "vaapi", or "software".
func gstreamerEncoderForCodec(codec string) string {
	switch strings.ToLower(codec) {
	case "h264_nvenc":
		if detectGStreamerElement("nvh264enc") {
			return "nvenc"
		}
	case "h264_qsv":
		if detectGStreamerElement("qsvh264enc") {
			return "qsv"
		}
		// fall through to vaapi check
		if detectGStreamerElement("vaapih264enc") {
			return "vaapi"
		}
	case "h264_vaapi":
		if detectGStreamerElement("vaapih264enc") {
			return "vaapi"
		}
	}
	return "software"
}

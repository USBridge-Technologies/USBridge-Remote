//go:build linux

package video

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"

	"usbridge_agent/internal/api"
	"usbridge_agent/internal/capture"
	"usbridge_agent/internal/config"
)

func attachPipeWireFD(cmd *exec.Cmd) {
	if capture.GetLinuxEnv() != "Wayland" {
		return
	}
	portal := capture.GetPortal()
	if !portal.IsInitialized() {
		return
	}
	
	fd, err := portal.OpenPipeWireFD()
	if err != nil {
		log.Printf("[video/wayland] failed to open PipeWire FD for child process: %v", err)
		return
	}

	f := os.NewFile(uintptr(fd), "pipewire-fd")
	// cmd.ExtraFiles takes ownership of the FD and will close it when the command starts/finishes.
	cmd.ExtraFiles = append(cmd.ExtraFiles, f)
	log.Printf("[video/wayland] attached PipeWire FD %d as ExtraFiles[%d] (will be FD %d in child)", fd, len(cmd.ExtraFiles)-1, 3+len(cmd.ExtraFiles)-1)
}

func sourceFormatForPlatform() string {
	return "yuv420p"
}

func buildPlatformArgs(cfg config.Config, req api.VideoStartRequest, mode, codec string) []string {
	env := capture.GetLinuxEnv()
	if env == "Wayland" {
		if req.ShowMouse {
			log.Println("[video/wayland] show_mouse requested; cursor is rendered via separate client overlay")
		}
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
	if mode == "" || mode == "auto" || mode == "dxgi" {
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

func detectVaapiDevice() string {
	for i := 128; i < 132; i++ {
		path := fmt.Sprintf("/dev/dri/renderD%d", i)
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return "/dev/dri/renderD128"
}

func detectGStreamerElement(name string) bool {
	return exec.Command("gst-inspect-1.0", "--exists", name).Run() == nil
}

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

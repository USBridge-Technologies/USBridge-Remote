//go:build windows

package video

import (
	"fmt"
	"log"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"usbridge_agent/internal/api"
	"usbridge_agent/internal/config"
)

const (
	displayDeviceAttachedToDesktop = 0x00000001
	displayDeviceMirroringDriver   = 0x00000008
)

type displayDevice struct {
	cb           uint32
	deviceName   [32]uint16
	deviceString [128]uint16
	stateFlags   uint32
	deviceID     [128]uint16
	deviceKey    [128]uint16
}

var (
	user32Windows           = windows.NewLazySystemDLL("user32.dll")
	procEnumDisplayDevicesW = user32Windows.NewProc("EnumDisplayDevicesW")
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
	adapters := make([]string, 0, 4)
	seen := make(map[string]struct{}, 4)
	for idx := uint32(0); ; idx++ {
		dev := displayDevice{cb: uint32(unsafe.Sizeof(displayDevice{}))}
		r1, _, err := procEnumDisplayDevicesW.Call(0, uintptr(idx), uintptr(unsafe.Pointer(&dev)), 0)
		if r1 == 0 {
			if idx == 0 {
				log.Printf("[video] adapter probe skipped: %v", err)
			}
			break
		}
		if dev.stateFlags&displayDeviceMirroringDriver != 0 {
			continue
		}
		name := strings.TrimSpace(windows.UTF16ToString(dev.deviceString[:]))
		if name == "" || dev.stateFlags&displayDeviceAttachedToDesktop == 0 {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
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
	return []string{mode}
}

func platformCodecFallbacks() []string {
	return []string{"h264_amf", "h264_nvenc", "h264_qsv", "libx264"}
}

func platformAutoCodec(_ string) string {
	adapters := detectPlatformVideoAdapters()
	order := preferredCodecsForAdapters(adapters)
	for _, codec := range order {
		if strings.TrimSpace(codec) == "" {
			continue
		}
		log.Printf("[video] windows auto codec selected codec=%s adapters=%s", codec, strings.Join(adapters, ", "))
		return codec
	}
	log.Printf("[video] windows auto codec fallback codec=libx264 adapters=%s", strings.Join(adapters, ", "))
	return "libx264"
}

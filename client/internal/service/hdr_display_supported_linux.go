//go:build linux && !android && cgo

package service

/*
extern int vk_video_hdr_supported(void);
*/
import "C"

import "sync"

var (
	linuxHdrOnce      sync.Once
	linuxHdrSupported bool
)

// HdrDisplaySupported reports whether this Linux display and Vulkan driver
// can present an HDR10 (HEVC/AV1 Main10, BT.2020 + PQ) swapchain via
// VK_EXT_swapchain_colorspace + VK_COLOR_SPACE_HDR10_ST2084_EXT.
func HdrDisplaySupported() bool {
	r := VideoRenderer()
	if r != VideoRendererAuto && r != VideoRendererVulkan {
		return false
	}
	linuxHdrOnce.Do(func() {
		linuxHdrSupported = C.vk_video_hdr_supported() != 0
	})
	return linuxHdrSupported
}

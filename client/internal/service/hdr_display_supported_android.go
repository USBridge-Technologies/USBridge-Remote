//go:build android

package service

/*
extern int android_hdr_display_supported(void);
*/
import "C"

// HdrDisplaySupported: the display lists HDR10 and a hardware HEVC decoder
// takes Main10 (VideoSurfaceBridge.hdrDisplaySupported). The Vulkan renderer
// then presents the PQ picture on an HDR10 swapchain.
func HdrDisplaySupported() bool {
	return C.android_hdr_display_supported() != 0
}

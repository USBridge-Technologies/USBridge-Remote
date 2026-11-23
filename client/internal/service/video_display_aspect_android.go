//go:build android && cgo

package service

/*
// Implemented in vk_video_impl_android.c.
extern void vk_video_set_display_aspect(float a);
extern void vk_video_set_content_crop(float x, float y, float w, float h);
*/
import "C"

const nativeDisplayAspectSupported = true

func setNativeDisplayAspect(aspect float32) {
	C.vk_video_set_display_aspect(C.float(aspect))
}

// nativeContentCropSupported: the Vulkan renderer shows only the picture part
// of a frame the host letterboxed its desktop into.
const nativeContentCropSupported = true

func setNativeContentCrop(x, y, w, h float32) {
	C.vk_video_set_content_crop(C.float(x), C.float(y), C.float(w), C.float(h))
}

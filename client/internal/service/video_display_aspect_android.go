//go:build android && cgo

package service

/*
// Implemented in vk_video_impl_android.c.
extern void vk_video_set_display_aspect(float a);
*/
import "C"

const nativeDisplayAspectSupported = true

func setNativeDisplayAspect(aspect float32) {
	C.vk_video_set_display_aspect(C.float(aspect))
}

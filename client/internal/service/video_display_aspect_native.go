//go:build cgo && (windows || (linux && !android))

package service

/*
// Implemented in vk_video_impl_{linux,windows}.c and gl_video_impl_{linux,windows}.c.
extern void vk_video_set_display_aspect(float a);
extern void gl_video_set_display_aspect(float a);
*/
import "C"

const nativeDisplayAspectSupported = true

func setNativeDisplayAspect(aspect float32) {
	C.vk_video_set_display_aspect(C.float(aspect))
	C.gl_video_set_display_aspect(C.float(aspect))
}

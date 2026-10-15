//go:build darwin && !ios

package controller

/*
extern void goMetalMouseEvent(int typ, float x, float y, int btn);
extern void metal_video_set_mouse_callback(void (*cb)(int typ, float x, float y, int btn));

static void register_metal_mouse_callback(void) {
	metal_video_set_mouse_callback(goMetalMouseEvent);
}
*/
import "C"

// init hands USBridgeMetalView (internal/service/metal_video_impl_darwin.m)
// the goMetalMouseEvent export. The service only holds a callback pointer, so
// binaries without this package (its tests, cmd tools) still link.
func init() {
	C.register_metal_mouse_callback()
}

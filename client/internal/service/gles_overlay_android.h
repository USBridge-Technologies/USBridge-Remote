// gles_overlay_android.h — see gles_overlay_android.c.
#ifndef GLES_OVERLAY_ANDROID_H
#define GLES_OVERLAY_ANDROID_H

#include <android/hardware_buffer.h>
#include <android/native_window.h>
#include <stdint.h>

// One frame for gles_ov_draw, laid out by vk_video_impl_android.c. All
// rects are in surface pixels, top-left origin.
typedef struct {
    int surf_w, surf_h;
    AHardwareBuffer *ahb;      // decoder image, or
    const uint8_t   *rgba;     // tightly packed RGBA (tex_w x tex_h)
    int tex_w, tex_h;          // buffer size
    int sx0, sy0, sx1, sy1;    // visible source rect within the buffer
    int dx, dy, dw, dh;        // where it goes on the surface
    int cursor_visible, cursor_x, cursor_y;
    const int *holes;          // nholes x {x, y, w, h}
    int nholes;
    int hud_visible, hud[4];   // Net Graph HUD rect {x, y, w, h}
} gles_ov_frame;

int  gles_ov_init(ANativeWindow *win);   // any thread; leaves nothing current
int  gles_ov_make_current(void);         // render thread, before drawing
int  gles_ov_images_supported(void);
int  gles_ov_surface_size(int *w, int *h);
int  gles_ov_draw(const gles_ov_frame *f);
void gles_ov_set_cursor(const uint8_t *rgba, int w, int h);
void gles_ov_set_hud(const uint8_t *rgba, int w, int h); // Net Graph canvas
int  gles_ov_hud_ready(void);
void gles_ov_wait_idle(void);            // GPU done with every drawn frame
void gles_ov_drop_images(void);          // releases cached decoder buffers
void gles_ov_gpu_name(char *out, int out_len);
void gles_ov_destroy(void);              // render thread or after it exited

#endif

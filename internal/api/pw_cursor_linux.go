//go:build linux && cgo

package api

/*
#cgo pkg-config: libpipewire-0.3

#include <errno.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <pthread.h>

#include <pipewire/pipewire.h>
#include <spa/buffer/buffer.h>
#include <spa/buffer/meta.h>
#include <spa/param/video/format-utils.h>
#include <spa/pod/builder.h>

typedef struct {
	struct pw_thread_loop *loop;
	struct pw_stream *stream;
	uint32_t node_id;
	int fd;

	pthread_mutex_t mu;
	double x, y;
	int visible;
	int width, height;
	int hotspot_x, hotspot_y;
	int have_cursor;

	struct spa_video_info_raw video_info;
} usbridge_pw_cursor_watcher;

static void usbridge_pw_stream_process(void *data) {
	usbridge_pw_cursor_watcher *w = (usbridge_pw_cursor_watcher *)data;
	struct pw_buffer *b;
	struct spa_meta_cursor *cursor;

	while ((b = pw_stream_dequeue_buffer(w->stream)) != NULL) {
		if (b->buffer != NULL) {
			cursor = spa_buffer_find_meta_data(b->buffer, SPA_META_Cursor, sizeof(*cursor));
			if (cursor != NULL) {
				pthread_mutex_lock(&w->mu);
				w->x = (double)cursor->position.x;
				w->y = (double)cursor->position.y;
				w->visible = (cursor->id != 0);
				w->hotspot_x = cursor->hotspot.x;
				w->hotspot_y = cursor->hotspot.y;
				w->have_cursor = 1;
				pthread_mutex_unlock(&w->mu);
			}
		}
		pw_stream_queue_buffer(w->stream, b);
	}
}

static void usbridge_pw_stream_state_changed(void *data, enum pw_stream_state old, enum pw_stream_state state, const char *error) {
	usbridge_pw_cursor_watcher *w = (usbridge_pw_cursor_watcher *)data;
	if (state == PW_STREAM_STATE_PAUSED) {
		pw_stream_set_active(w->stream, true);
	}
}

static void usbridge_pw_stream_param_changed(void *data, uint32_t id, const struct spa_pod *param) {
	usbridge_pw_cursor_watcher *w = (usbridge_pw_cursor_watcher *)data;
	if (param == NULL || id != SPA_PARAM_Format) return;

	struct spa_video_info video_info = { 0 };
	if (spa_format_video_parse(param, &video_info) == 0) {
		pthread_mutex_lock(&w->mu);
		w->width = video_info.info.raw.size.width;
		w->height = video_info.info.raw.size.height;
		pthread_mutex_unlock(&w->mu);
	}
}

static const struct pw_stream_events usbridge_pw_stream_events = {
	PW_VERSION_STREAM_EVENTS,
	.state_changed = usbridge_pw_stream_state_changed,
	.param_changed = usbridge_pw_stream_param_changed,
	.process = usbridge_pw_stream_process,
};

static usbridge_pw_cursor_watcher* usbridge_pw_cursor_create(uint32_t node_id, int fd) {
	usbridge_pw_cursor_watcher *w = calloc(1, sizeof(usbridge_pw_cursor_watcher));
	w->node_id = node_id;
	w->fd = dup(fd);
	pthread_mutex_init(&w->mu, NULL);
	return w;
}

static int usbridge_pw_cursor_start(usbridge_pw_cursor_watcher *w) {
	pw_init(NULL, NULL);

	w->loop = pw_thread_loop_new("usbridge-cursor", NULL);
	if (!w->loop) return -1;

	pw_thread_loop_lock(w->loop);
	pw_thread_loop_start(w->loop);

	struct pw_context *context = pw_context_new(pw_thread_loop_get_loop(w->loop), NULL, 0);
	w->stream = pw_stream_new_simple(pw_thread_loop_get_loop(w->loop), "cursor-meta",
		pw_properties_new(PW_KEY_MEDIA_TYPE, "Video", PW_KEY_MEDIA_CATEGORY, "Capture", NULL),
		&usbridge_pw_stream_events, w);

	uint8_t pod_buf[1024];
	struct spa_pod_builder b = SPA_POD_BUILDER_INIT(pod_buf, sizeof(pod_buf));
	const struct spa_pod *params[2];

	params[0] = spa_pod_builder_add_object(&b,
		SPA_TYPE_OBJECT_Format, SPA_PARAM_EnumFormat,
		SPA_FORMAT_mediaType, SPA_POD_Id(SPA_MEDIA_TYPE_video),
		SPA_FORMAT_mediaSubtype, SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
		SPA_FORMAT_VIDEO_format, SPA_POD_CHOICE_ENUM_Id(2, SPA_VIDEO_FORMAT_BGRx, SPA_VIDEO_FORMAT_RGBx));

	params[1] = spa_pod_builder_add_object(&b,
		SPA_TYPE_OBJECT_ParamMeta, SPA_PARAM_Meta,
		SPA_PARAM_META_type, SPA_POD_Id(SPA_META_Cursor),
		SPA_PARAM_META_size, SPA_POD_Int(sizeof(struct spa_meta_cursor) + 512));

	int res = pw_stream_connect(w->stream, PW_DIRECTION_INPUT, w->node_id,
		PW_STREAM_FLAG_AUTOCONNECT, params, 2);

	pw_thread_loop_unlock(w->loop);
	return res;
}

static void usbridge_pw_cursor_destroy(usbridge_pw_cursor_watcher *w) {
	if (!w) return;
	if (w->loop) {
		pw_thread_loop_stop(w->loop);
		pw_thread_loop_destroy(w->loop);
	}
	if (w->fd >= 0) close(w->fd);
	pthread_mutex_destroy(&w->mu);
	free(w);
}

static int usbridge_pw_cursor_snapshot(usbridge_pw_cursor_watcher *w, double *x, double *y, int *vis, int *width, int *height, int *hx, int *hy) {
	if (!w->have_cursor) return 0;
	pthread_mutex_lock(&w->mu);
	*x = w->x;
	*y = w->y;
	*vis = w->visible;
	*width = w->width;
	*height = w->height;
	*hx = w->hotspot_x;
	*hy = w->hotspot_y;
	pthread_mutex_unlock(&w->mu);
	return 1;
}
*/
import "C"

import (
	"fmt"
	"sync"
)

type pwCursorWatcher struct {
	ptr  *C.usbridge_pw_cursor_watcher
	once sync.Once
}

func newPWCursorWatcher(nodeID uint32, fd int) (*pwCursorWatcher, error) {
	ptr := C.usbridge_pw_cursor_create(C.uint32_t(nodeID), C.int(fd))
	if ptr == nil {
		return nil, fmt.Errorf("failed to create pw cursor watcher")
	}

	if res := C.usbridge_pw_cursor_start(ptr); res < 0 {
		C.usbridge_pw_cursor_destroy(ptr)
		return nil, fmt.Errorf("failed to start pw stream (rc=%d)", res)
	}

	return &pwCursorWatcher{ptr: ptr}, nil
}

func (w *pwCursorWatcher) stop() {
	w.once.Do(func() {
		if w != nil && w.ptr != nil {
			C.usbridge_pw_cursor_destroy(w.ptr)
			w.ptr = nil
		}
	})
}

func (w *pwCursorWatcher) snapshot(fallbackWidth, fallbackHeight int) *CursorState {
	if w == nil || w.ptr == nil {
		return nil
	}

	var x, y C.double
	var vis, width, height, hx, hy C.int

	if C.usbridge_pw_cursor_snapshot(w.ptr, &x, &y, &vis, &width, &height, &hx, &hy) == 0 {
		return nil
	}

	fW, fH := int(width), int(height)
	if fW <= 0 || fH <= 0 {
		fW = fallbackWidth
		fH = fallbackHeight
	}

	return &CursorState{
		Visible:  vis != 0,
		X:        float64(x),
		Y:        float64(y),
		Width:    fW,
		Height:   fH,
		HotspotX: int(hx),
		HotspotY: int(hy),
		Source:   "wayland-meta",
	}
}

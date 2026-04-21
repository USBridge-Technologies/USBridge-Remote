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
#include <spa/param/buffers.h>
#include <spa/param/video/format-utils.h>
#include <spa/param/video/raw-utils.h>
#include <spa/pod/builder.h>
#include <spa/utils/result.h>

typedef struct {
	struct pw_thread_loop *loop;
	struct pw_context *context;
	struct pw_core *core;
	struct pw_stream *stream;
	struct spa_hook stream_listener;
	uint32_t node_id;
	int fd;

	pthread_mutex_t mu;
	int visible;
	int have_cursor;
	double x;
	double y;
	int width;
	int height;
	int hotspot_x;
	int hotspot_y;
	int event_count;
} usbridge_pw_cursor_watcher;

static void usbridge_pw_stream_state_changed(void *data,
	enum pw_stream_state old_state, enum pw_stream_state new_state, const char *error)
{
	fprintf(stderr, "[cursor-cgo] stream state: %s -> %s%s%s\n",
		pw_stream_state_as_string(old_state),
		pw_stream_state_as_string(new_state),
		error ? " error=" : "",
		error ? error : "");
	fflush(stderr);
}

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
				w->hotspot_x = cursor->hotspot.x;
				w->hotspot_y = cursor->hotspot.y;
				w->visible = (cursor->id != 0);
				w->have_cursor = 1;
				w->event_count++;

				if (w->event_count % 30 == 1) {
					fprintf(stderr, "[cursor-cgo] #%d POS:%d,%d HOT:%d,%d VIS:%d SPACE:%dx%d\n",
						w->event_count, cursor->position.x, cursor->position.y,
						cursor->hotspot.x, cursor->hotspot.y, w->visible, w->width, w->height);
					fflush(stderr);
				}
				pthread_mutex_unlock(&w->mu);
			}
		}
		pw_stream_queue_buffer(w->stream, b);
	}
}

static void usbridge_pw_stream_param_changed(void *data, uint32_t id, const struct spa_pod *param) {
	usbridge_pw_cursor_watcher *w = (usbridge_pw_cursor_watcher *)data;
	if (id == SPA_PARAM_Format && param != NULL) {
		struct spa_video_info video_info = { 0 };
		if (spa_format_video_parse(param, &video_info) == 0) {
			pthread_mutex_lock(&w->mu);
			w->width = video_info.info.raw.size.width;
			w->height = video_info.info.raw.size.height;
			fprintf(stderr, "[cursor-cgo] ✅ format negotiated: %dx%d\n", w->width, w->height);
			fflush(stderr);
			pthread_mutex_unlock(&w->mu);
		} else {
			fprintf(stderr, "[cursor-cgo] ⚠️ spa_format_video_parse failed (id=%u)\n", id);
			fflush(stderr);
		}
	}
}

static const struct pw_stream_events usbridge_pw_stream_events = {
	PW_VERSION_STREAM_EVENTS,
	.state_changed = usbridge_pw_stream_state_changed,
	.process       = usbridge_pw_stream_process,
	.param_changed = usbridge_pw_stream_param_changed,
};

static usbridge_pw_cursor_watcher *usbridge_pw_cursor_create(uint32_t node_id, int fd) {
	usbridge_pw_cursor_watcher *w = calloc(1, sizeof(*w));
	if (!w) return NULL;
	pthread_mutex_init(&w->mu, NULL);
	w->node_id = node_id;
	w->fd = dup(fd);
	fprintf(stderr, "[cursor-cgo] created: node=%u orig_fd=%d dup_fd=%d\n", node_id, fd, w->fd);
	fflush(stderr);
	return w;
}

static int usbridge_pw_cursor_start(usbridge_pw_cursor_watcher *w) {
	pw_init(NULL, NULL);

	fprintf(stderr, "[cursor-cgo] start: node=%u fd=%d\n", w->node_id, w->fd);
	fflush(stderr);

	w->loop = pw_thread_loop_new("usbridge-cursor", NULL);
	if (!w->loop) {
		fprintf(stderr, "[cursor-cgo] ❌ step=thread_loop_new errno=%s\n", strerror(errno));
		fflush(stderr);
		return -1;
	}

	w->context = pw_context_new(pw_thread_loop_get_loop(w->loop), NULL, 0);
	if (!w->context) {
		fprintf(stderr, "[cursor-cgo] ❌ step=context_new errno=%s\n", strerror(errno));
		fflush(stderr);
		return -1;
	}

	int rc = pw_thread_loop_start(w->loop);
	if (rc < 0) {
		fprintf(stderr, "[cursor-cgo] ❌ step=thread_loop_start rc=%d err=%s\n", rc, spa_strerror(rc));
		fflush(stderr);
		return -1;
	}

	pw_thread_loop_lock(w->loop);

	fprintf(stderr, "[cursor-cgo] step=connect_fd fd=%d\n", w->fd);
	fflush(stderr);

	w->core = pw_context_connect_fd(w->context, w->fd, NULL, 0);
	w->fd = -1;
	if (!w->core) {
		fprintf(stderr, "[cursor-cgo] ❌ step=connect_fd errno=%s\n", strerror(errno));
		fflush(stderr);
		pw_thread_loop_unlock(w->loop);
		return -1;
	}
	fprintf(stderr, "[cursor-cgo] ✅ step=connect_fd ok\n");
	fflush(stderr);

	w->stream = pw_stream_new_simple(
		pw_thread_loop_get_loop(w->loop),
		"cursor-meta",
		pw_properties_new(
			PW_KEY_MEDIA_TYPE,     "Video",
			PW_KEY_MEDIA_CATEGORY, "Capture",
			PW_KEY_MEDIA_ROLE,     "Screen",
			NULL),
		&usbridge_pw_stream_events, w);
	if (!w->stream) {
		fprintf(stderr, "[cursor-cgo] ❌ step=stream_new errno=%s\n", strerror(errno));
		fflush(stderr);
		pw_thread_loop_unlock(w->loop);
		return -1;
	}
	fprintf(stderr, "[cursor-cgo] ✅ step=stream_new ok\n");
	fflush(stderr);

	uint8_t pod_buf[4096];
	struct spa_pod_builder b = SPA_POD_BUILDER_INIT(pod_buf, sizeof(pod_buf));
	const struct spa_pod *params[2];

	// Request any raw video format — cursor metadata lives in spa_buffer.metas which
	// is always CPU-accessible regardless of the buffer data type (DMA-BUF/VAAPI safe).
	// PW_STREAM_FLAG_MAP_BUFFERS is intentionally omitted because it requires CPU-mappable
	// pixel data and fails when the upstream node provides DMA-BUF (VAAPI pipeline).
	params[0] = spa_pod_builder_add_object(&b,
		SPA_TYPE_OBJECT_Format, SPA_PARAM_EnumFormat,
		SPA_FORMAT_mediaType,    SPA_POD_Id(SPA_MEDIA_TYPE_video),
		SPA_FORMAT_mediaSubtype, SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
		SPA_FORMAT_VIDEO_format, SPA_POD_CHOICE_ENUM_Id(5,
			SPA_VIDEO_FORMAT_BGRA,
			SPA_VIDEO_FORMAT_BGRA,
			SPA_VIDEO_FORMAT_RGBA,
			SPA_VIDEO_FORMAT_BGRx,
			SPA_VIDEO_FORMAT_NV12));

	params[1] = spa_pod_builder_add_object(&b,
		SPA_TYPE_OBJECT_ParamMeta, SPA_PARAM_Meta,
		SPA_PARAM_META_type, SPA_POD_Id(SPA_META_Cursor),
		SPA_PARAM_META_size, SPA_POD_Int(
			(int)(sizeof(struct spa_meta_cursor) + sizeof(struct spa_meta_bitmap) + 64 * 64 * 4)));

	int res = pw_stream_connect(w->stream, PW_DIRECTION_INPUT, w->node_id,
		PW_STREAM_FLAG_AUTOCONNECT, params, 2);

	if (res < 0) {
		fprintf(stderr, "[cursor-cgo] ❌ step=stream_connect rc=%d err=%s node=%u\n",
			res, spa_strerror(res), w->node_id);
		fflush(stderr);
		pw_thread_loop_unlock(w->loop);
		return -1;
	}
	fprintf(stderr, "[cursor-cgo] ✅ step=stream_connect ok node=%u — awaiting negotiation\n",
		w->node_id);
	fflush(stderr);

	pw_thread_loop_unlock(w->loop);
	return 0;
}

static void usbridge_pw_cursor_destroy(usbridge_pw_cursor_watcher *w) {
	if (!w) return;
	if (w->loop) pw_thread_loop_stop(w->loop);
	if (w->stream) pw_stream_destroy(w->stream);
	if (w->core) pw_core_disconnect(w->core);
	if (w->context) pw_context_destroy(w->context);
	if (w->loop) pw_thread_loop_destroy(w->loop);
	if (w->fd >= 0) close(w->fd);
	pthread_mutex_destroy(&w->mu);
	free(w);
}

static int usbridge_pw_cursor_snapshot(usbridge_pw_cursor_watcher *w,
	double *x, double *y, int *visible, int *width, int *height,
	int *hotspot_x, int *hotspot_y)
{
	if (!w) return 0;
	pthread_mutex_lock(&w->mu);
	int ok = w->have_cursor;
	if (ok) {
		*x = w->x; *y = w->y; *visible = w->visible;
		*width = w->width; *height = w->height;
		*hotspot_x = w->hotspot_x; *hotspot_y = w->hotspot_y;
	}
	pthread_mutex_unlock(&w->mu);
	return ok;
}
*/
import "C"

import (
	"fmt"
	"log"
	"sync"
)

type pwCursorWatcher struct {
	ptr  *C.usbridge_pw_cursor_watcher
	once sync.Once
}

func newPWCursorWatcher(nodeID uint32, fd int) (*pwCursorWatcher, error) {
	log.Printf("[cursor-cgo] newPWCursorWatcher: node=%d fd=%d", nodeID, fd)
	ptr := C.usbridge_pw_cursor_create(C.uint32_t(nodeID), C.int(fd))
	if ptr == nil {
		return nil, fmt.Errorf("failed to allocate cursor watcher struct")
	}
	if C.usbridge_pw_cursor_start(ptr) < 0 {
		C.usbridge_pw_cursor_destroy(ptr)
		return nil, fmt.Errorf("pw cursor stream failed to start (see [cursor-cgo] stderr lines above)")
	}
	log.Printf("[cursor-cgo] newPWCursorWatcher: started successfully node=%d", nodeID)
	return &pwCursorWatcher{ptr: ptr}, nil
}

func (w *pwCursorWatcher) stop() {
	if w != nil && w.ptr != nil {
		w.once.Do(func() {
			C.usbridge_pw_cursor_destroy(w.ptr)
			w.ptr = nil
		})
	}
}

func (w *pwCursorWatcher) snapshot(fallbackWidth, fallbackHeight int) *CursorState {
	if w == nil || w.ptr == nil {
		return nil
	}

	var (
		x, y               C.double
		vis, width, height C.int
		hotspotX, hotspotY C.int
	)

	if C.usbridge_pw_cursor_snapshot(w.ptr, &x, &y, &vis, &width, &height, &hotspotX, &hotspotY) == 0 {
		return nil
	}

	fW, fH := int(width), int(height)
	if fW <= 0 || fH <= 0 {
		// Stream format not yet negotiated — coordinate space unknown.
		// Return nil until param_changed fires and confirms dimensions.
		return nil
	}

	return &CursorState{
		Visible:  vis != 0,
		X:        float64(x),
		Y:        float64(y),
		Width:    fW,
		Height:   fH,
		HotspotX: int(hotspotX),
		HotspotY: int(hotspotY),
		Source:   "wayland-meta",
	}
}

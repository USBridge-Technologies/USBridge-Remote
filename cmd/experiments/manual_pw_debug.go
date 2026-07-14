package main

/*
#cgo pkg-config: libpipewire-0.3

#include <pipewire/pipewire.h>
#include <spa/buffer/buffer.h>
#include <spa/buffer/meta.h>
#include <spa/param/video/format-utils.h>
#include <spa/pod/builder.h>
#include <pthread.h>
#include <unistd.h>
#include <errno.h>

typedef struct {
    struct pw_thread_loop *loop;
    struct pw_stream *stream;
    struct pw_context *context;
    struct pw_core *core;
    uint32_t node_id;
    int fd;
    int buffer_count;
} test_watcher;

static void on_stream_process(void *data) {
    test_watcher *w = (test_watcher *)data;
    struct pw_buffer *b;
    struct spa_meta_cursor *cursor;
    
    while ((b = pw_stream_dequeue_buffer(w->stream)) != NULL) {
        w->buffer_count++;
        
        if (w->buffer_count == 1) {
            fprintf(stderr, ">> First buffer: n_metas=%d\n", b->buffer->n_metas);
            for (uint32_t i = 0; i < b->buffer->n_metas; i++) {
                fprintf(stderr, "   - Meta[%d] type=%d size=%d\n", i, b->buffer->metas[i].type, b->buffer->metas[i].size);
            }
        }

        cursor = spa_buffer_find_meta_data(b->buffer, SPA_META_Cursor, sizeof(*cursor));
        if (cursor != NULL) {
            fprintf(stderr, ">> CURSOR: x=%d y=%d vis=%d id=%d hot=%d,%d\n", 
                cursor->position.x, cursor->position.y, cursor->flags, cursor->id,
                cursor->hotspot.x, cursor->hotspot.y);
        } else if (w->buffer_count % 100 == 1) {
             fprintf(stderr, ">> Buffers processed: %d (no cursor)\n", w->buffer_count);
        }
        pw_stream_queue_buffer(w->stream, b);
    }
}

static void on_stream_state_changed(void *data, enum pw_stream_state old, enum pw_stream_state state, const char *error) {
    fprintf(stderr, ">> Stream state: %s -> %s (err: %s)\n", 
        pw_stream_state_as_string(old), pw_stream_state_as_string(state), error ? error : "none");
    if (state == PW_STREAM_STATE_PAUSED) {
        pw_stream_set_active(((test_watcher*)data)->stream, true);
    }
}

static const struct pw_stream_events stream_events = {
    PW_VERSION_STREAM_EVENTS,
    .state_changed = on_stream_state_changed,
    .process = on_stream_process,
};

static int run_pw_test(uint32_t node_id, int fd) {
    pw_init(NULL, NULL);
    test_watcher *w = calloc(1, sizeof(test_watcher));
    w->node_id = node_id;
    w->fd = dup(fd);
    
    w->loop = pw_thread_loop_new("test-loop", NULL);
    pw_thread_loop_lock(w->loop);
    pw_thread_loop_start(w->loop);
    
    w->context = pw_context_new(pw_thread_loop_get_loop(w->loop), NULL, 0);
    w->core = pw_context_connect_fd(w->context, w->fd, NULL, 0);
    
    w->stream = pw_stream_new_simple(pw_thread_loop_get_loop(w->loop), "cursor-debug",
        pw_properties_new(PW_KEY_MEDIA_TYPE, "Video", PW_KEY_MEDIA_CATEGORY, "Capture", NULL),
        &stream_events, w);
        
    uint8_t buffer[2048];
    struct spa_pod_builder b = SPA_POD_BUILDER_INIT(buffer, sizeof(buffer));
    const struct spa_pod *params[4];
    
    params[0] = spa_pod_builder_add_object(&b,
        SPA_TYPE_OBJECT_Format, SPA_PARAM_EnumFormat,
        SPA_FORMAT_mediaType, SPA_POD_Id(SPA_MEDIA_TYPE_video),
        SPA_FORMAT_mediaSubtype, SPA_POD_Id(SPA_MEDIA_SUBTYPE_raw),
        SPA_FORMAT_VIDEO_format, SPA_POD_CHOICE_ENUM_Id(2, SPA_VIDEO_FORMAT_BGRx, SPA_VIDEO_FORMAT_RGBx));
        
    params[1] = spa_pod_builder_add_object(&b,
        SPA_TYPE_OBJECT_ParamMeta, SPA_PARAM_Meta,
        SPA_PARAM_META_type, SPA_POD_Id(SPA_META_Header),
        SPA_PARAM_META_size, SPA_POD_Int(sizeof(struct spa_meta_header)));

    params[2] = spa_pod_builder_add_object(&b,
        SPA_TYPE_OBJECT_ParamMeta, SPA_PARAM_Meta,
        SPA_PARAM_META_type, SPA_POD_Id(SPA_META_Cursor),
        SPA_PARAM_META_size, SPA_POD_Int(sizeof(struct spa_meta_cursor) + 2048));

    params[3] = spa_pod_builder_add_object(&b,
        SPA_TYPE_OBJECT_ParamMeta, SPA_PARAM_Meta,
        SPA_PARAM_META_type, SPA_POD_Id(SPA_META_VideoDamage),
        SPA_PARAM_META_size, SPA_POD_Int(sizeof(struct spa_meta_region) * 16));

    pw_stream_connect(w->stream, PW_DIRECTION_INPUT, w->node_id, 
        PW_STREAM_FLAG_AUTOCONNECT, params, 4);
    
    pw_thread_loop_unlock(w->loop);
    
    fprintf(stderr, ">> Debug waiting for data. Move mouse!\n");
    sleep(10);
    
    pw_thread_loop_lock(w->loop);
    pw_thread_loop_stop(w->loop);
    fprintf(stderr, ">> Test finished.\n");
    pw_thread_loop_unlock(w->loop);
    return 0;
}
*/
import "C"
import (
	"fmt"
	"log"
	"time"
	"usbridge_agent/internal/capture"
)

func main() {
	fmt.Println("Starting Deep PipeWire Meta Debug Test...")
	err := capture.InitPortalSession()
	if err != nil {
		log.Fatalf("Portal init failed: %v", err)
	}

	fmt.Println("ALLOW screen access...")
	time.Sleep(5 * time.Second)
	
	nodeID := capture.GetPortalPipeWireNodeID()
	fd := capture.GetPortalPipeWireFD()
	if nodeID == 0 || fd <= 0 {
		log.Fatalf("Bad PipeWire info: nodeID=%d, fd=%d", nodeID, fd)
	}
	C.run_pw_test(C.uint32_t(nodeID), C.int(fd))
}

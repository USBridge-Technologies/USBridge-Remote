// video_latency_android.c — per-stage video latency on Android, logged every
// ~2 s (logcat tag USBridge/VideoLat) and fed to Net Graph's DEC row.
//
// Stages of one frame, all on CLOCK_MONOTONIC_RAW (moonlight-common-c's
// clock, so receiveTimeUs ages line up):
//   net  receive of the frame's first packet -> dr_submit (reassembly + queue)
//   dec  queueInputBuffer -> its output dequeued from MediaCodec
//   gl   output dequeued -> handed to Vulkan: the decoder image arriving
//        from AImageReader, or (zerocopy off / CPU paths) the GL copy
//   vk   handed to Vulkan -> vkQueuePresentKHR returned
// "total" is their sum: receive -> present queued. SurfaceFlinger still adds
// its latch/scanout on top, which no app-side clock sees.
#ifdef __ANDROID__

#include <android/log.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <time.h>

#define LTAG "USBridge/VideoLat"
#define LLOGI(...) __android_log_print(ANDROID_LOG_INFO, LTAG, __VA_ARGS__)

static uint64_t vlat_now_us(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC_RAW, &ts);
    return (uint64_t)ts.tv_sec * 1000000ull + (uint64_t)ts.tv_nsec / 1000ull;
}

// Submit times by presentation timestamp, so the output thread can match a
// decoded buffer to its input.
#define VLAT_RING 64
typedef struct { uint64_t pts, submit_us, net_us; } vlat_in;
static vlat_in         g_in[VLAT_RING];
static pthread_mutex_t g_in_mu = PTHREAD_MUTEX_INITIALIZER;

// The frame most recently handed to Vulkan, waiting for its present.
typedef struct { uint64_t handoff_us, net_us, dec_us, gl_us; int valid; } vlat_frame;
static vlat_frame      g_pending;
static pthread_mutex_t g_pend_mu = PTHREAD_MUTEX_INITIALIZER;

// Output-thread scratch for the frame being decoded/copied.
static uint64_t g_cur_out_us, g_cur_net_us, g_cur_dec_us;
static int      g_cur_valid;

// 2 s window sums (render thread only).
static uint64_t g_win_t0;
static int      g_win_n;
static uint64_t g_sum[5], g_max[5];
static int      g_dropped;
static atomic_int g_dropped_pending;
static atomic_int g_submitted_pending; // frames into the decoder this window
static atomic_int g_output_pending;    // frames out of the decoder
static atomic_int g_handoff_pending;   // frames handed to Vulkan
static int        g_shown;             // frames presented (render thread)
static _Atomic double g_last_total_ms;
static _Atomic double g_last_decode_ms; // dec + gl + vk: submit -> present

void vlat_note_submit(uint64_t pts, uint64_t receive_us, uint64_t now_moonlight_us) {
    uint64_t net = now_moonlight_us > receive_us ? now_moonlight_us - receive_us : 0;
    atomic_fetch_add(&g_submitted_pending, 1);
    pthread_mutex_lock(&g_in_mu);
    vlat_in *e = &g_in[pts % VLAT_RING];
    e->pts = pts; e->submit_us = vlat_now_us(); e->net_us = net;
    pthread_mutex_unlock(&g_in_mu);
}

void vlat_note_dropped(void) { atomic_fetch_add(&g_dropped_pending, 1); }

// vlat_note_output: the buffer with this pts just left the decoder.
void vlat_note_output(uint64_t pts) {
    atomic_fetch_add(&g_output_pending, 1);
    uint64_t now = vlat_now_us();
    g_cur_valid = 0;
    pthread_mutex_lock(&g_in_mu);
    vlat_in *e = &g_in[pts % VLAT_RING];
    if (e->pts == pts && e->submit_us) {
        g_cur_out_us = now;
        g_cur_net_us = e->net_us;
        g_cur_dec_us = now - e->submit_us;
        g_cur_valid = 1;
    }
    pthread_mutex_unlock(&g_in_mu);
}

// vlat_note_handoff: the frame from the last vlat_note_output was handed to
// Vulkan (GL copy done).
void vlat_note_handoff(void) {
    atomic_fetch_add(&g_handoff_pending, 1);
    if (!g_cur_valid) return;
    uint64_t now = vlat_now_us();
    pthread_mutex_lock(&g_pend_mu);
    g_pending.handoff_us = now;
    g_pending.net_us = g_cur_net_us;
    g_pending.dec_us = g_cur_dec_us;
    g_pending.gl_us  = now - g_cur_out_us;
    g_pending.valid  = 1;
    pthread_mutex_unlock(&g_pend_mu);
    g_cur_valid = 0;
}

// vlat_note_present: Vulkan presented a new video frame (render thread).
void vlat_note_present(void) {
    uint64_t now = vlat_now_us();
    if (!g_win_t0) g_win_t0 = now;
    g_shown++;
    vlat_frame f;
    pthread_mutex_lock(&g_pend_mu);
    f = g_pending;
    g_pending.valid = 0;
    pthread_mutex_unlock(&g_pend_mu);
    // Stats only for frames whose handoff this present consumed; a newer
    // handoff can take the slot first, so g_shown is the frame count.
    if (f.valid) {
        uint64_t v[5];
        v[0] = f.net_us; v[1] = f.dec_us; v[2] = f.gl_us; v[3] = now > f.handoff_us ? now - f.handoff_us : 0;
        v[4] = v[0] + v[1] + v[2] + v[3];
        for (int i = 0; i < 5; i++) {
            g_sum[i] += v[i];
            if (v[i] > g_max[i]) g_max[i] = v[i];
        }
        g_win_n++;
    }
    if (now - g_win_t0 < 2000000 || g_win_n == 0) return;

    g_dropped += atomic_exchange(&g_dropped_pending, 0);
    int in = atomic_exchange(&g_submitted_pending, 0);
    int out = atomic_exchange(&g_output_pending, 0);
    int handed = atomic_exchange(&g_handoff_pending, 0);
    double secs = (double)(now - g_win_t0) / 1e6;
    double n = (double)g_win_n;
    atomic_store(&g_last_total_ms, (double)g_sum[4] / n / 1000.0);
    atomic_store(&g_last_decode_ms, (double)(g_sum[1] + g_sum[2] + g_sum[3]) / n / 1000.0);
    LLOGI("fps in %.1f decoded %.1f handed %.1f shown %.1f, avg/max ms: net %.1f/%.1f dec %.1f/%.1f gl %.1f/%.1f vk %.1f/%.1f total %.1f/%.1f dropped=%d",
          in / secs, out / secs, handed / secs, g_shown / secs,
          g_sum[0] / n / 1000.0, g_max[0] / 1000.0,
          g_sum[1] / n / 1000.0, g_max[1] / 1000.0,
          g_sum[2] / n / 1000.0, g_max[2] / 1000.0,
          g_sum[3] / n / 1000.0, g_max[3] / 1000.0,
          g_sum[4] / n / 1000.0, g_max[4] / 1000.0,
          g_dropped);
    g_win_t0 = now; g_win_n = 0; g_shown = 0; g_dropped = 0;
    for (int i = 0; i < 5; i++) { g_sum[i] = 0; g_max[i] = 0; }
}

// vlat_last_total_ms: average receive->present of the last window, 0 before
// the first one.
double vlat_last_total_ms(void) { return atomic_load(&g_last_total_ms); }

// vlat_last_decode_ms: average decoder-submit -> present of the last window
// (Net Graph's DEC row, same span as Metal's), 0 before the first one.
double vlat_last_decode_ms(void) { return atomic_load(&g_last_decode_ms); }

void vlat_reset(void) {
    pthread_mutex_lock(&g_in_mu);
    for (int i = 0; i < VLAT_RING; i++) g_in[i].submit_us = 0, g_in[i].pts = 0;
    pthread_mutex_unlock(&g_in_mu);
    pthread_mutex_lock(&g_pend_mu);
    g_pending.valid = 0;
    pthread_mutex_unlock(&g_pend_mu);
    g_cur_valid = 0;
    atomic_store(&g_last_total_ms, 0.0);
    atomic_store(&g_last_decode_ms, 0.0);
}

#endif // __ANDROID__

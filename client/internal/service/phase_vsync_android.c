// Android half of the phase lock reports (control message 0x5530, see phase_report.h).
//
// The phone only knows its refresh grid: PhaseBridge.kt (a Choreographer callback while a
// stream is on screen) hands the latest vsync time and the refresh period over, and the
// decoder's output thread calls phase_android_frame_ready() as each frame comes out of
// MediaCodec. How long that frame has until the next refresh is what the host times its
// frames by (as macOS does from its display link).
#include <jni.h>
#include <stdatomic.h>
#include <stdint.h>
#include <time.h>
#include <android/log.h>

#include "phase_report.h"

// How long before the refresh SurfaceFlinger still takes a frame queued by the app.
#define ANDROID_LATCH_US 3000

static _Atomic int64_t g_vsync_ns;  // a Choreographer frame time, CLOCK_MONOTONIC
static _Atomic int64_t g_period_ns; // refresh period
static _Atomic int64_t g_ready_ns;  // when the decoder last had a frame

// Returns 0 once the decoder had frames and none has come out for 3 s (the stream is over), 2
// while it has not produced any yet (the host may still be locking on its source), else 1, so
// PhaseBridge can stop asking the display for every vsync when nothing is streaming.
__attribute__((used)) JNIEXPORT jint JNICALL
Java_io_usbridge_client_PhaseBridge_onVsync(JNIEnv *env, jclass clazz, jlong frame_time_ns, jlong period_ns) {
    (void)env;
    (void)clazz;
    atomic_store_explicit(&g_period_ns, (int64_t)period_ns, memory_order_relaxed);
    atomic_store_explicit(&g_vsync_ns, (int64_t)frame_time_ns, memory_order_release);
    int64_t ready = atomic_load_explicit(&g_ready_ns, memory_order_relaxed);
    if (ready <= 0) return 2;
    return (int64_t)frame_time_ns - ready < 3000000000LL ? 1 : 0;
}

// A decoded frame is ready to be shown.
void phase_android_frame_ready(void) {
    struct timespec t0;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    int64_t t0ns = (int64_t)t0.tv_sec * 1000000000LL + t0.tv_nsec;
    atomic_store_explicit(&g_ready_ns, t0ns, memory_order_relaxed);
    // Once every 5 s: is the chain alive (frames, vsync grid, reports)?
    static _Atomic int64_t log_at;
    static _Atomic int frames;
    atomic_fetch_add(&frames, 1);
    if (t0ns - atomic_load(&log_at) >= 5000000000LL) {
        atomic_store(&log_at, t0ns);
        int64_t v = atomic_load_explicit(&g_vsync_ns, memory_order_relaxed);
        __android_log_print(ANDROID_LOG_INFO, "USBridge/Phase", "frames=%d vsync_age_ms=%lld period_us=%lld enabled=%d reports_sent=%d",
                            atomic_exchange(&frames, 0), v > 0 ? (long long)((t0ns - v) / 1000000) : -1LL,
                            (long long)(atomic_load_explicit(&g_period_ns, memory_order_relaxed) / 1000), phase_report_enabled(), phase_report_sent_count());
    }
    if (!phase_report_enabled()) return;
    int64_t vsync = atomic_load_explicit(&g_vsync_ns, memory_order_acquire);
    int64_t period = atomic_load_explicit(&g_period_ns, memory_order_relaxed);
    if (vsync <= 0 || period <= 0) return;
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    int64_t now = (int64_t)ts.tv_sec * 1000000000LL + ts.tv_nsec;
    int64_t since = now - vsync;
    // No vsync for a while (the callback stopped): the grid can't be trusted.
    if (since < 0 || since > 2000000000LL) return;
    int64_t rem = period - since % period;
    phase_report_ready(rem / 1000, period / 1000, ANDROID_LATCH_US, now / 1000);
}

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

#include "phase_report.h"

// How long before the refresh SurfaceFlinger still takes a frame queued by the app.
#define ANDROID_LATCH_US 3000

static _Atomic int64_t g_vsync_ns;  // a Choreographer frame time, CLOCK_MONOTONIC
static _Atomic int64_t g_period_ns; // refresh period
static _Atomic int64_t g_ready_ns;  // when the decoder last had a frame

// Returns false once no frame has come out of the decoder for 3 s, so PhaseBridge can stop
// asking the display for every vsync.
__attribute__((used)) JNIEXPORT jboolean JNICALL
Java_io_usbridge_client_PhaseBridge_onVsync(JNIEnv *env, jclass clazz, jlong frame_time_ns, jlong period_ns) {
    (void)env;
    (void)clazz;
    atomic_store_explicit(&g_period_ns, (int64_t)period_ns, memory_order_relaxed);
    atomic_store_explicit(&g_vsync_ns, (int64_t)frame_time_ns, memory_order_release);
    int64_t ready = atomic_load_explicit(&g_ready_ns, memory_order_relaxed);
    return ready > 0 && (int64_t)frame_time_ns - ready < 3000000000LL;
}

// A decoded frame is ready to be shown.
void phase_android_frame_ready(void) {
    struct timespec t0;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    atomic_store_explicit(&g_ready_ns, (int64_t)t0.tv_sec * 1000000000LL + t0.tv_nsec, memory_order_relaxed);
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

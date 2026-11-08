//go:build cgo && (linux || darwin || ios || windows || android)

// Per-frame benchmark recorder -- see bench_frames.h.
//
// Single producer (the thread running dr_submit) and single consumer (the
// Go drain loop in bench_frames_cgo.go), so a lock-free ring with
// acquire/release indices is enough.

#include "bench_frames.h"

#include <time.h>
#ifdef _WIN32
#include <windows.h>
#endif

#define BENCH_FRAMES_CAP 16384 // ~4.5 min at 60fps; drained every 100ms

static bench_frame_t     g_bench_frames[BENCH_FRAMES_CAP];
static volatile uint32_t g_bench_head    = 0; // written by producer
static volatile uint32_t g_bench_tail    = 0; // written by consumer
static volatile int      g_bench_enabled = 0;
static volatile uint32_t g_bench_dropped = 0; // ring full

void bench_frames_note(const DECODE_UNIT *du) {
    if (!__atomic_load_n(&g_bench_enabled, __ATOMIC_ACQUIRE)) return;
    uint32_t head = __atomic_load_n(&g_bench_head, __ATOMIC_RELAXED);
    uint32_t tail = __atomic_load_n(&g_bench_tail, __ATOMIC_ACQUIRE);
    if (head - tail >= BENCH_FRAMES_CAP) {
        __atomic_fetch_add(&g_bench_dropped, 1, __ATOMIC_RELAXED);
        return;
    }
    bench_frame_t *f = &g_bench_frames[head % BENCH_FRAMES_CAP];
    f->frame_number           = du->frameNumber;
    f->frame_type             = du->frameType;
    f->size_bytes             = (uint32_t)du->fullLength;
    f->host_latency_tenths_ms = du->frameHostProcessingLatency;
    f->receive_us             = du->receiveTimeUs;
    f->enqueue_us             = du->enqueueTimeUs;
    f->submit_us              = LiGetMicroseconds();
    f->presentation_us        = du->presentationTimeUs;
    __atomic_store_n(&g_bench_head, head + 1, __ATOMIC_RELEASE);
}

// bench_frames_enable starts (1) or stops (0) recording. Starting discards
// anything left over, so a run only ever sees its own frames.
void bench_frames_enable(int on) {
    if (on) {
        __atomic_store_n(&g_bench_tail, __atomic_load_n(&g_bench_head, __ATOMIC_ACQUIRE), __ATOMIC_RELEASE);
        __atomic_store_n(&g_bench_dropped, 0, __ATOMIC_RELAXED);
    }
    __atomic_store_n(&g_bench_enabled, on ? 1 : 0, __ATOMIC_RELEASE);
}

// bench_frames_drain copies up to max recorded frames into out and returns
// how many were copied.
int bench_frames_drain(bench_frame_t *out, int max) {
    uint32_t tail = __atomic_load_n(&g_bench_tail, __ATOMIC_RELAXED);
    uint32_t head = __atomic_load_n(&g_bench_head, __ATOMIC_ACQUIRE);
    int n = 0;
    while (tail != head && n < max) {
        out[n++] = g_bench_frames[tail % BENCH_FRAMES_CAP];
        tail++;
    }
    __atomic_store_n(&g_bench_tail, tail, __ATOMIC_RELEASE);
    return n;
}

uint32_t bench_frames_dropped(void) {
    return __atomic_load_n(&g_bench_dropped, __ATOMIC_RELAXED);
}

uint64_t bench_now_us(void) { return LiGetMicroseconds(); }

// ---- Time barcode -------------------------------------------------------------

#define BENCH_LATENCY_CAP 4096 // far more than a run takes between drains
#define BENCH_LATENCY_EVERY_US 200000 // five frames a second is plenty for percentiles

typedef struct {
    int64_t  wall_us;
    uint32_t bar;
} bench_latency_t;

static bench_latency_t   g_bench_latency[BENCH_LATENCY_CAP];
static volatile uint32_t g_latency_head = 0;
static volatile uint32_t g_latency_tail = 0;
static volatile uint64_t g_latency_next_us = 0;

int64_t bench_wall_us(void) {
#ifdef _WIN32
    FILETIME ft;
    GetSystemTimePreciseAsFileTime(&ft);
    uint64_t t = ((uint64_t)ft.dwHighDateTime << 32) | ft.dwLowDateTime;
    return (int64_t)(t / 10) - 11644473600000000LL;
#else
    struct timespec ts;
    clock_gettime(CLOCK_REALTIME, &ts);
    return (int64_t)ts.tv_sec * 1000000 + ts.tv_nsec / 1000;
#endif
}

int bench_latency_due(void) {
    if (!__atomic_load_n(&g_bench_enabled, __ATOMIC_ACQUIRE)) return 0;
    uint64_t now = bench_now_us();
    if (now < __atomic_load_n(&g_latency_next_us, __ATOMIC_RELAXED)) return 0;
    __atomic_store_n(&g_latency_next_us, now + BENCH_LATENCY_EVERY_US, __ATOMIC_RELAXED);
    return 1;
}

// Reads the barcode from one row of luma; see agent/internal/benchrender. Returns 0
// on a picture without one (the desktop before the test window is up, a cut-off row).
int bench_barcode_decode_row(const uint8_t *row, int width, int bytes_per_sample, uint32_t *out) {
    enum { CELLS = 37, BRIGHT = 190, DARK = 70 };
    int off = bytes_per_sample == 2 ? 1 : 0; // P010: the high byte
    int step = bytes_per_sample == 2 ? 2 : 1;
    int first = -1, last = -1;
    for (int x = 0; x < width; x++) {
        if (row[x * step + off] >= BRIGHT) {
            if (first < 0) first = x;
            last = x;
        }
    }
    if (first < 0 || last - first + 1 < CELLS * 3) return 0;
    double cell = (double)(last - first + 1) / CELLS;
    uint64_t bits = 0;
    for (int i = 0; i < CELLS; i++) {
        uint8_t p = row[(int)(first + (i + 0.5) * cell) * step + off];
        if (p >= BRIGHT) bits = bits << 1 | 1;
        else if (p <= DARK) bits <<= 1;
        else return 0;
    }
    // Guards: 1010 ... 1.
    if ((bits >> 33) != 0xA || (bits & 1) != 1) return 0;
    *out = (uint32_t)(bits >> 1);
    return 1;
}

void bench_latency_note(const uint8_t *luma, int stride, int width, int height, int bytes_per_sample, int64_t wall_us) {
    if (!luma || width <= 0 || height <= 0) return;
    uint32_t bar;
    if (!bench_barcode_decode_row(luma + (size_t)(height / 2) * stride, width, bytes_per_sample, &bar)) return;
    uint32_t head = __atomic_load_n(&g_latency_head, __ATOMIC_RELAXED);
    uint32_t tail = __atomic_load_n(&g_latency_tail, __ATOMIC_ACQUIRE);
    if (head - tail >= BENCH_LATENCY_CAP) return;
    g_bench_latency[head % BENCH_LATENCY_CAP] = (bench_latency_t){ wall_us, bar };
    __atomic_store_n(&g_latency_head, head + 1, __ATOMIC_RELEASE);
}

int bench_latency_drain(int64_t *wall_us, uint32_t *bar, int max) {
    uint32_t tail = __atomic_load_n(&g_latency_tail, __ATOMIC_RELAXED);
    uint32_t head = __atomic_load_n(&g_latency_head, __ATOMIC_ACQUIRE);
    int n = 0;
    while (tail != head && n < max) {
        wall_us[n] = g_bench_latency[tail % BENCH_LATENCY_CAP].wall_us;
        bar[n] = g_bench_latency[tail % BENCH_LATENCY_CAP].bar;
        n++; tail++;
    }
    __atomic_store_n(&g_latency_tail, tail, __ATOMIC_RELEASE);
    return n;
}

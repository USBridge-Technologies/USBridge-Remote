// Phase lock on a Wayland client: when each decoded frame really reached the
// screen (wp_presentation), and how long before the compositor's latch it was
// ready, sent as the 0x5530 report: a host with a source faster than the stream times its
// frames by it, any other ignores it (phase_report.c). USBRIDGE_PHASE_PROBE keeps a per-frame
// record for the benchmark.
#if defined(__linux__) && !defined(__ANDROID__)

#define _GNU_SOURCE
#include <pthread.h>
#include <stdatomic.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <wayland-client.h>
#include "presentation_time_protocol.h"
#include "phase_report.h"


typedef struct {
    int64_t ready_us, present_us;
    int32_t refresh_ns;
    uint32_t bar;
    uint8_t status; // 1 presented, 2 discarded, 3 replaced before it was drawn
    uint8_t arm;
    uint8_t bar_ok;
} phase_rec;

enum { REC_CAP = 200000 };
static phase_rec *g_rec;
static atomic_int g_nrec;
static struct wl_display *g_dpy;
static struct wl_event_queue *g_queue;
static struct wl_surface *g_surface_wrap;
static struct wp_presentation *g_pres;
static atomic_int g_reporting, g_arm;
static int64_t g_aim_us;

static pthread_mutex_t g_cur_mu = PTHREAD_MUTEX_INITIALIZER;
static int64_t g_cur_ready;
static uint32_t g_cur_bar;
static int g_cur_bar_ok, g_cur_valid;

static int64_t mono_us(void) {
    struct timespec t; clock_gettime(CLOCK_MONOTONIC, &t);
    return (int64_t)t.tv_sec * 1000000 + t.tv_nsec / 1000;
}

static phase_rec *rec_add(void) {
    int i = atomic_fetch_add(&g_nrec, 1);
    return (g_rec && i < REC_CAP) ? &g_rec[i] : NULL;
}

typedef struct { phase_rec r; } fb_ctx;

static void fb_sync_output(void *d, struct wp_presentation_feedback *f, struct wl_output *o) {
    (void)d; (void)f; (void)o;
}
static void fb_presented(void *d, struct wp_presentation_feedback *f, uint32_t sec_hi, uint32_t sec_lo,
                         uint32_t nsec, uint32_t refresh, uint32_t seq_hi, uint32_t seq_lo, uint32_t flags) {
    (void)seq_hi; (void)seq_lo; (void)flags;
    fb_ctx *c = d;
    c->r.present_us = (((int64_t)sec_hi << 32 | sec_lo) * 1000000) + nsec / 1000;
    c->r.refresh_ns = (int32_t)refresh;
    c->r.status = 1;
    phase_rec *out = rec_add();
    if (out) *out = c->r;
    if (atomic_load(&g_reporting)) phase_report_presented(c->r.ready_us, c->r.present_us, refresh / 1000, g_aim_us);
    wp_presentation_feedback_destroy(f);
    free(c);
}
static void fb_discarded(void *d, struct wp_presentation_feedback *f) {
    fb_ctx *c = d;
    c->r.status = 2;
    phase_rec *out = rec_add();
    if (out) *out = c->r;
    wp_presentation_feedback_destroy(f);
    free(c);
}
static const struct wp_presentation_feedback_listener fb_listener = { fb_sync_output, fb_presented, fb_discarded };

static void registry_global(void *d, struct wl_registry *r, uint32_t name, const char *iface, uint32_t ver) {
    (void)d; (void)ver;
    if (!strcmp(iface, wp_presentation_interface.name))
        g_pres = wl_registry_bind(r, name, &wp_presentation_interface, 1);
}
static void registry_remove(void *d, struct wl_registry *r, uint32_t n) { (void)d; (void)r; (void)n; }
static const struct wl_registry_listener registry_listener = { registry_global, registry_remove };

int phase_probe_init(struct wl_display *dpy, struct wl_surface *surface) {
    int probe = getenv("USBRIDGE_PHASE_PROBE") != NULL;
    int lock = phase_report_enabled();
    if (!probe && !lock) return 0;
    if (g_pres) {
        // A renderer made again (the benchmark does, per run) brings a new surface.
        if (!g_surface_wrap && surface) g_surface_wrap = wl_proxy_create_wrapper(surface);
        return 1;
    }
    // KWin's latch sits this far before the vblank (measured 4-6 ms at 60 Hz).
    const char *aim = getenv("USBRIDGE_PHASE_AIM_US");
    g_aim_us = aim ? atoll(aim) : 5000;
    if (lock) atomic_store(&g_reporting, 1);
    if (probe && !g_rec) g_rec = calloc(REC_CAP, sizeof(phase_rec));
    g_dpy = dpy;
    g_queue = wl_display_create_queue(dpy);
    struct wl_display *wrap = wl_proxy_create_wrapper(dpy);
    wl_proxy_set_queue((struct wl_proxy *)wrap, g_queue);
    struct wl_registry *reg = wl_display_get_registry(wrap);
    wl_registry_add_listener(reg, &registry_listener, NULL);
    wl_display_roundtrip_queue(dpy, g_queue);
    wl_registry_destroy(reg);
    wl_proxy_wrapper_destroy(wrap);
    if (surface) g_surface_wrap = wl_proxy_create_wrapper(surface);
    return g_pres != NULL;
}

// The surface is about to be destroyed: no feedback is asked for it from here on.
// Its id goes to the next object made, and a feedback request naming that one
// (a wp_viewporter, after the benchmark's renderer restart) is a protocol error
// the compositor answers by dropping the whole connection: the app closed.
void phase_probe_forget_surface(void) {
    if (g_surface_wrap) {
        wl_proxy_wrapper_destroy(g_surface_wrap);
        g_surface_wrap = NULL;
    }
}

// A decoded frame is ready. Its picture carries the host's draw time as a barcode
// when the benchmark's probe renderer is on screen.
void phase_probe_note_submit(const uint8_t *y, int stride, int w, int h) {
    if (!g_pres) return;
    int64_t now = mono_us();
    const uint8_t *row = y + (size_t)(h / 2) * stride;
    uint32_t v = 0;
    int ok = 1;
    for (int i = 0; i < 32; i++) {
        uint8_t p = row[(int)((120.0 + i * 40.0 + 20.0) * w / 1920.0)];
        if (p > 190) v = v << 1 | 1;
        else if (p < 70) v <<= 1;
        else ok = 0;
    }
    pthread_mutex_lock(&g_cur_mu);
    if (g_cur_valid) { // the previous one never got drawn
        phase_rec *out = rec_add();
        if (out) *out = (phase_rec){ .ready_us = g_cur_ready, .bar = g_cur_bar, .bar_ok = (uint8_t)g_cur_bar_ok,
                                     .status = 3, .arm = (uint8_t)atomic_load(&g_arm) };
    }
    g_cur_ready = now; g_cur_bar = v; g_cur_bar_ok = ok; g_cur_valid = 1;
    pthread_mutex_unlock(&g_cur_mu);
}

// Right before the commit that shows the frame noted last.
void phase_probe_before_commit(void) {
    if (!g_pres || !g_surface_wrap) return;
    pthread_mutex_lock(&g_cur_mu);
    int valid = g_cur_valid;
    phase_rec r = { .ready_us = g_cur_ready, .bar = g_cur_bar, .bar_ok = (uint8_t)g_cur_bar_ok, .arm = (uint8_t)atomic_load(&g_arm) };
    g_cur_valid = 0;
    pthread_mutex_unlock(&g_cur_mu);
    if (!valid) return;
    fb_ctx *c = calloc(1, sizeof(*c));
    c->r = r;
    struct wp_presentation_feedback *f = wp_presentation_feedback(g_pres, g_surface_wrap);
    wp_presentation_feedback_add_listener(f, &fb_listener, c);
}

void phase_probe_after_commit(void) {
    if (!g_pres) return;
    wl_display_flush(g_dpy);
    wl_display_dispatch_queue_pending(g_dpy, g_queue);
}

void phase_probe_set(int reporting, int arm) {
    atomic_store(&g_reporting, reporting);
    atomic_store(&g_arm, arm);
}

int phase_probe_dump(const char *path) {
    FILE *f = fopen(path, "w");
    if (!f) return -1;
    struct timespec w, m;
    clock_gettime(CLOCK_REALTIME, &w); clock_gettime(CLOCK_MONOTONIC, &m);
    int64_t wall_minus_mono = ((int64_t)w.tv_sec - m.tv_sec) * 1000000 + (w.tv_nsec - m.tv_nsec) / 1000;
    fprintf(f, "# wall_minus_mono_us=%lld aim_us=%lld\narm,status,ready_us,present_us,refresh_ns,bar_ok,bar\n",
            (long long)wall_minus_mono, (long long)g_aim_us);
    int n = atomic_load(&g_nrec);
    if (n > REC_CAP) n = REC_CAP;
    for (int i = 0; g_rec && i < n; i++) {
        phase_rec *r = &g_rec[i];
        fprintf(f, "%d,%d,%lld,%lld,%d,%d,%u\n", r->arm, r->status, (long long)r->ready_us,
                (long long)r->present_us, r->refresh_ns, r->bar_ok, r->bar);
    }
    fclose(f);
    return n;
}

#endif // defined(__linux__) && !defined(__ANDROID__)

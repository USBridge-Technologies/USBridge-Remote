//go:build cgo && !android && !ios && (linux || darwin || windows)

// See phase_report.h.

#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>

#include "phase_report.h"

extern int LiSendPhaseReport(const uint8_t *data, uint16_t length);

enum { WINDOW = 256, WINDOW_US = 500000, MIN_FRAMES = 10 };

static atomic_int g_enabled = 1;
static atomic_flag g_busy = ATOMIC_FLAG_INIT; // a platform's callbacks may come from any thread
static int64_t g_slack[WINDOW];
static int g_count;
static int64_t g_window_start_us;

void phase_report_set_enabled(int on) { atomic_store(&g_enabled, on); }
int phase_report_enabled(void) { return atomic_load(&g_enabled); }

static int cmp_i64(const void *a, const void *b) {
    int64_t x = *(const int64_t *)a, y = *(const int64_t *)b;
    return x < y ? -1 : x > y;
}

// One frame's slack before the latch, from which the 500 ms reports are made: frames, the
// refresh period, the 10th percentile (or ~3rd percentile via report_over when frames fill
// >=90% of refreshes, as in Rubylight's SlackWindow::report_over) and the 10th..90th spread.
static int64_t rem_euclid_i64(int64_t a, int64_t b) {
    int64_t r = a % b;
    return r < 0 ? r + b : r;
}

static int64_t circular_mean_us(const int64_t *samples, int n, int64_t period_us) {
    int64_t sorted[WINDOW];
    memcpy(sorted, samples, (size_t)n * sizeof(int64_t));
    qsort(sorted, (size_t)n, sizeof(int64_t), cmp_i64);
    int start = 0;
    int64_t widest = sorted[0] + period_us - sorted[n - 1];
    for (int i = 1; i < n; i++) {
        int64_t gap = sorted[i] - sorted[i - 1];
        if (gap > widest) {
            widest = gap;
            start = i;
        }
    }
    int64_t origin = sorted[start];
    int64_t total = 0;
    for (int i = 0; i < n; i++) {
        total += rem_euclid_i64(sorted[i] - origin, period_us);
    }
    return rem_euclid_i64(origin + total / n, period_us);
}

static void note_slack(int64_t slack_us, int64_t period_us, int64_t now_us) {
    if (!atomic_load(&g_enabled) || period_us <= 0) return;
    if (atomic_flag_test_and_set(&g_busy)) return;
    if (g_count < WINDOW) g_slack[g_count++] = rem_euclid_i64(slack_us, period_us);
    if (!g_window_start_us) g_window_start_us = now_us;
    int64_t elapsed_us = now_us - g_window_start_us;
    if (elapsed_us >= WINDOW_US) {
        g_window_start_us = now_us;
        int n = g_count;
        g_count = 0;
        if (n >= MIN_FRAMES) {
            int64_t centre = circular_mean_us(g_slack, n, period_us);
            for (int i = 0; i < n; i++) {
                int64_t dev = rem_euclid_i64(g_slack[i] - centre, period_us);
                if (dev > period_us / 2) dev -= period_us;
                g_slack[i] = dev;
            }
            qsort(g_slack, (size_t)n, sizeof(g_slack[0]), cmp_i64);
            int64_t low = g_slack[(n - 1) * 10 / 100];
            int64_t high = g_slack[(n - 1) * 90 / 100];
            int64_t lead = rem_euclid_i64(centre + low, period_us);
            if (lead > period_us / 2) lead -= period_us;
            int64_t spread = high - low;
            if (spread < 0) spread = 0;
            // Rubylight SlackWindow::report_over: at 60 fps on 60 Hz (>=90% of refreshes filled),
            // shift the reported early edge down by spread/4 so the ~3rd percentile sits at the
            // host's safety margin instead of the 10th.
            if (elapsed_us > 0 && (int64_t)n * period_us * 10 >= elapsed_us * 9) {
                int64_t target = 1500 > (spread / 4) ? 1500 : (spread / 4);
                int64_t room = period_us / 2 - spread - target;
                if (room < 0) room = 0;
                int64_t extra = (spread / 4) < room ? (spread / 4) : room;
                lead -= extra;
                if (lead < -(period_us / 2)) lead = -(period_us / 2);
            }
            uint8_t m[16] = {1};
            uint16_t frames = (uint16_t)n;
            uint32_t period = (uint32_t)(period_us * 1000), spread_ns = (uint32_t)(spread * 1000);
            int32_t lead_ns = (int32_t)(lead * 1000);
            memcpy(m + 2, &frames, 2);
            memcpy(m + 4, &period, 4);
            memcpy(m + 8, &lead_ns, 4);
            memcpy(m + 12, &spread_ns, 4);
            LiSendPhaseReport(m, sizeof(m));
        }
    }
    atomic_flag_clear(&g_busy);
}

// Slack is circular: a frame just past the latch has nearly a refresh until the next one,
// yet belongs next to the frames that just made it.
static int64_t wrapped(int64_t slack_us, int64_t period_us) {
    int64_t v = slack_us % period_us;
    if (v < 0) v += period_us;
    return v > period_us * 3 / 4 ? v - period_us : v;
}

void phase_report_presented(int64_t ready_us, int64_t present_us, int64_t period_us, int64_t latch_us) {
    if (period_us <= 0) return;
    int64_t raw = present_us - ready_us - latch_us;
    // Waited past a whole refresh: it missed the latch before the one that showed it. Modulo
    // the period that reads as a comfortable margin, and the lock would stay there, a
    // refresh late for good (seen live: 24 ms instead of 8).
    note_slack(raw >= period_us ? -(raw - period_us) - 1000 : wrapped(raw, period_us), period_us, present_us);
}

void phase_report_ready(int64_t next_refresh_in_us, int64_t period_us, int64_t latch_us, int64_t now_us) {
    if (period_us <= 0) return;
    note_slack(wrapped(next_refresh_in_us - latch_us, period_us), period_us, now_us);
}

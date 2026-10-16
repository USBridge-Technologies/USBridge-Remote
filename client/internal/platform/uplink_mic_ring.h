// The microphone ring shared by the platforms whose audio API calls back
// with captured PCM (WASAPI, AudioQueue, AAudio): the callback pushes
// 48kHz mono S16 samples, uplink_mic_ring.go pops whole frames. Everything
// is static inline so each cgo preamble that includes it gets its own copy.
#ifndef USBRIDGE_UPLINK_MIC_RING_H
#define USBRIDGE_UPLINK_MIC_RING_H

#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <errno.h>

typedef struct {
    pthread_mutex_t mu;
    pthread_cond_t cv;
    int16_t *buf;
    int cap, head, len;
    int failed; // the device went away: pops return -1 once drained
} mic_ring;

// A platform's open microphone starts with its ring (so a mic_ring* is
// the platform's own struct as well): plat_mic_open/plat_mic_close below.
extern mic_ring *plat_mic_open(char *err, int errlen);
extern void plat_mic_close(mic_ring *r);

static inline int mic_ring_init(mic_ring *r, int cap) {
    memset(r, 0, sizeof(*r));
    r->buf = (int16_t *)calloc((size_t)cap, sizeof(int16_t));
    if (!r->buf) return -1;
    r->cap = cap;
    pthread_mutex_init(&r->mu, NULL);
    pthread_cond_init(&r->cv, NULL);
    return 0;
}

static inline void mic_ring_destroy(mic_ring *r) {
    pthread_mutex_destroy(&r->mu);
    pthread_cond_destroy(&r->cv);
    free(r->buf);
    r->buf = NULL;
}

// mic_ring_push appends n samples (NULL: silence); on overflow the oldest
// go, so a reader that fell behind hears the latest audio.
static inline void mic_ring_push(mic_ring *r, const int16_t *pcm, int n) {
    pthread_mutex_lock(&r->mu);
    for (int i = 0; i < n; i++) {
        int tail = (r->head + r->len) % r->cap;
        r->buf[tail] = pcm ? pcm[i] : 0;
        if (r->len < r->cap) r->len++;
        else r->head = (r->head + 1) % r->cap;
    }
    pthread_cond_signal(&r->cv);
    pthread_mutex_unlock(&r->mu);
}

static inline void mic_ring_fail(mic_ring *r) {
    pthread_mutex_lock(&r->mu);
    r->failed = 1;
    pthread_cond_signal(&r->cv);
    pthread_mutex_unlock(&r->mu);
}

// mic_ring_pop takes exactly n samples: 1 when it did, 0 when they didn't
// come within timeout_ms, -1 when the device failed.
static inline int mic_ring_pop(mic_ring *r, int16_t *out, int n, int timeout_ms) {
    struct timespec until;
    clock_gettime(CLOCK_REALTIME, &until);
    until.tv_sec += timeout_ms / 1000;
    until.tv_nsec += (long)(timeout_ms % 1000) * 1000000L;
    if (until.tv_nsec >= 1000000000L) { until.tv_sec++; until.tv_nsec -= 1000000000L; }

    pthread_mutex_lock(&r->mu);
    while (r->len < n && !r->failed) {
        if (pthread_cond_timedwait(&r->cv, &r->mu, &until) == ETIMEDOUT) break;
    }
    int rc;
    if (r->len >= n) {
        for (int i = 0; i < n; i++) out[i] = r->buf[(r->head + i) % r->cap];
        r->head = (r->head + n) % r->cap;
        r->len -= n;
        rc = 1;
    } else {
        rc = r->failed ? -1 : 0;
    }
    pthread_mutex_unlock(&r->mu);
    return rc;
}

#endif

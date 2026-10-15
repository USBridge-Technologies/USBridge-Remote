// pyrowave_decode_linux.c
// PyroWave decode for the Linux client: the intra-only wavelet codec a USBridge host
// (rust-shine, Punktfunk) sends when the session negotiated VIDEO_FORMAT_PYROWAVE.
//
// The inverse wavelet transform runs as Vulkan compute on a device PyroWave owns
// (third_party/pyrowave, built by scripts/build_pyrowave.sh). The decoded planes are
// read back and handed to the Vulkan renderer's NV12 upload path
// (vk_video_try_submit_nv12) -- the decoder's device and the renderer's device are
// separate, so one readback and one upload per frame is the price of not sharing them.
//
// A decode unit is one whole PyroWave packet: every frame is a keyframe, and a frame
// with a lost shard never reaches here (moonlight-common-c drops it).
//
// Decoding runs on its own thread. moonlight-common-c submits decode units on the
// thread that drains the video socket; a few milliseconds of decode there is enough
// for a 100+ Mb/s stream to overflow the socket buffer, and every later frame then
// arrives with holes. The receive side only copies the unit into a one-frame slot,
// latest wins: with no inter-frame references, a skipped frame costs nothing.

//go:build linux && !android && cgo

#include <vulkan/vulkan.h>
#include <pyrowave.h>
#include "pyrowave_framing.h"

#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>

extern void goVTLog(char *msg);
// Hands a decoded frame to the renderer (moonlight_cgo_linux.go).
extern void pyrowave_linux_deliver(const uint8_t *y, const uint8_t *uv, int w, int h, int full_range);

static pthread_mutex_t  g_pw_mu  = PTHREAD_MUTEX_INITIALIZER;
static pyrowave_device  g_pw_dev = NULL;
static pyrowave_decoder g_pw_dec = NULL;
static int              g_pw_w = 0, g_pw_h = 0;
// 0 = not tried, 1 = usable, -1 = no Vulkan device PyroWave can run on (don't retry).
static int              g_pw_dev_state = 0;
static uint8_t         *g_pw_planes = NULL; // Y, then Cb, then Cr, then interleaved CbCr
static size_t           g_pw_planes_cap = 0;
static uint64_t         g_pw_frames = 0;
static uint64_t         g_pw_decode_us = 0;

static void pw_log(const char *fmt, int a, int b) {
    char msg[160];
    snprintf(msg, sizeof(msg), fmt, a, b);
    goVTLog(msg);
}

// pw_fail logs why a unit was not decoded: the first few, then every 300th.
static uint64_t g_pw_failures = 0;
static void pw_fail(const char *why, int value) {
    g_pw_failures++;
    if (g_pw_failures > 5 && g_pw_failures % 300 != 0) return;
    char msg[160];
    snprintf(msg, sizeof(msg), "pyrowave: frame not decoded (%s, %d); %d so far", why, value, (int)g_pw_failures);
    goVTLog(msg);
}

static void pw_destroy_decoder(void) {
    if (g_pw_dec) { pyrowave_decoder_destroy(g_pw_dec); g_pw_dec = NULL; }
    g_pw_w = g_pw_h = 0;
}

// pw_pick_gpu finds the GPU to decode on: the first discrete one, else the first
// integrated one. pyrowave_create_default_device takes whichever device the loader lists
// first, which can be a CPU implementation (llvmpipe) -- an order of magnitude too slow.
static int pw_pick_gpu(uint32_t *vid, uint32_t *pid, char *name, size_t name_len) {
    VkApplicationInfo app = { VK_STRUCTURE_TYPE_APPLICATION_INFO };
    app.apiVersion = VK_API_VERSION_1_1;
    VkInstanceCreateInfo ci = { VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
    ci.pApplicationInfo = &app;
    VkInstance inst = VK_NULL_HANDLE;
    if (vkCreateInstance(&ci, NULL, &inst) != VK_SUCCESS) return 0;
    uint32_t n = 0;
    vkEnumeratePhysicalDevices(inst, &n, NULL);
    VkPhysicalDevice devs[16];
    if (n > 16) n = 16;
    vkEnumeratePhysicalDevices(inst, &n, devs);
    int found = 0;
    for (int want = 0; want < 2 && !found; want++) {
        VkPhysicalDeviceType type = want == 0 ? VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU
                                              : VK_PHYSICAL_DEVICE_TYPE_INTEGRATED_GPU;
        for (uint32_t i = 0; i < n; i++) {
            VkPhysicalDeviceProperties pr;
            vkGetPhysicalDeviceProperties(devs[i], &pr);
            if (pr.deviceType != type || pr.apiVersion < VK_API_VERSION_1_3) continue;
            *vid = pr.vendorID;
            *pid = pr.deviceID;
            snprintf(name, name_len, "%s", pr.deviceName);
            found = 1;
            break;
        }
    }
    vkDestroyInstance(inst, NULL);
    return found;
}

static int pw_ensure_device(void) {
    if (g_pw_dev_state) return g_pw_dev_state > 0;
    uint32_t vid = 0, pid = 0;
    char name[VK_MAX_PHYSICAL_DEVICE_NAME_SIZE] = "";
    pyrowave_result r;
    if (pw_pick_gpu(&vid, &pid, name, sizeof(name))) {
        r = pyrowave_create_device_by_compat(vid, pid, NULL, NULL, NULL, &g_pw_dev);
        char msg[320];
        snprintf(msg, sizeof(msg), "pyrowave: decoding on %s (%04x:%04x), result %d", name, vid, pid, (int)r);
        goVTLog(msg);
    } else {
        goVTLog((char*)"pyrowave: no Vulkan 1.3 GPU found, trying the loader's default device");
        r = pyrowave_create_default_device(&g_pw_dev);
    }
    if (r != PYROWAVE_SUCCESS || !g_pw_dev) {
        g_pw_dev = NULL;
        g_pw_dev_state = -1;
        pw_log("pyrowave: no usable Vulkan device (result %d) -- cannot decode this stream", (int)r, 0);
        return 0;
    }
    g_pw_dev_state = 1;
    return 1;
}

static int pw_ensure_decoder(int w, int h) {
    if (g_pw_dec && g_pw_w == w && g_pw_h == h) return 1;
    pw_destroy_decoder();
    if (!pw_ensure_device()) return 0;

    pyrowave_decoder_create_info info;
    memset(&info, 0, sizeof(info));
    info.device = g_pw_dev;
    info.width = w;
    info.height = h;
    info.chroma = PYROWAVE_CHROMA_SUBSAMPLING_420;
    info.fragment_path = false;
    pyrowave_result r = pyrowave_decoder_create(&info, &g_pw_dec);
    if (r != PYROWAVE_SUCCESS || !g_pw_dec) {
        g_pw_dec = NULL;
        pw_log("pyrowave: decoder create failed for %dx%d", w, h);
        return 0;
    }

    size_t luma = (size_t)w * (size_t)h;
    size_t chroma = (size_t)(w / 2) * (size_t)(h / 2);
    size_t need = luma + chroma * 4;
    if (g_pw_planes_cap < need) {
        free(g_pw_planes);
        g_pw_planes = malloc(need);
        g_pw_planes_cap = g_pw_planes ? need : 0;
        if (!g_pw_planes) { pw_destroy_decoder(); return 0; }
    }
    g_pw_w = w; g_pw_h = h;
    pw_log("pyrowave: Vulkan compute decoder ready %dx%d", w, h);
    return 1;
}

// pw_decode decodes one access unit. On success (return 1) *y points at a
// tightly packed w*h luma plane and *uv at an interleaved CbCr plane of (w/2)*2 bytes
// per row and h/2 rows, both valid until the next call; *full_range reports whether
// the stream signals full-range YCbCr. Returns 0 for a frame that cannot be decoded.
static int pw_decode_dense(const uint8_t *au, size_t len,
                     const uint8_t **y, const uint8_t **uv,
                     int *w_out, int *h_out, int *full_range) {
    // BitstreamSequenceHeader (pyrowave_common.hpp), little endian: width-1 in bits
    // 0..13, height-1 in 14..27, `extended` in bit 31 of the first word; chroma
    // resolution in bit 26 and the YCbCr range in bit 30 of the second.
    if (len < 8) { pw_fail("too short", (int)len); return 0; }
    uint32_t w0 = (uint32_t)au[0] | (uint32_t)au[1] << 8 | (uint32_t)au[2] << 16 | (uint32_t)au[3] << 24;
    uint32_t w1 = (uint32_t)au[4] | (uint32_t)au[5] << 8 | (uint32_t)au[6] << 16 | (uint32_t)au[7] << 24;
    if (!(w0 >> 31)) { pw_fail("no sequence header", (int)len); return 0; }
    int w = (int)(w0 & 0x3fff) + 1;
    int h = (int)((w0 >> 14) & 0x3fff) + 1;
    if ((w1 >> 26) & 1) { pw_fail("4:4:4 stream", (int)len); return 0; } // not negotiated here
    if ((w | h) & 1) { pw_fail("odd size", w); return 0; }

    int ok = 0;
    pthread_mutex_lock(&g_pw_mu);
    if (!pw_ensure_decoder(w, h)) goto out;

    // Anything still queued belongs to a frame that never completed.
    pyrowave_decoder_clear(g_pw_dec);
    if (pyrowave_decoder_push_packet(g_pw_dec, au, len) != PYROWAVE_SUCCESS) { pw_fail("packet rejected", (int)len); goto out; }
    if (!pyrowave_decoder_decode_is_ready(g_pw_dec, false)) { pw_fail("frame incomplete", (int)len); goto out; }

    int cw = w / 2, ch = h / 2;
    size_t luma = (size_t)w * (size_t)h;
    size_t chroma = (size_t)cw * (size_t)ch;
    uint8_t *py = g_pw_planes, *pcb = py + luma, *pcr = pcb + chroma, *puv = pcr + chroma;

    pyrowave_cpu_buffer buf;
    memset(&buf, 0, sizeof(buf));
    buf.format = PYROWAVE_CPU_BUFFER_FORMAT_YUV420P;
    buf.width = w;
    buf.height = h;
    buf.data[0] = py;  buf.row_stride_in_bytes[0] = (size_t)w;  buf.plane_size_in_bytes[0] = luma;
    buf.data[1] = pcb; buf.row_stride_in_bytes[1] = (size_t)cw; buf.plane_size_in_bytes[1] = chroma;
    buf.data[2] = pcr; buf.row_stride_in_bytes[2] = (size_t)cw; buf.plane_size_in_bytes[2] = chroma;
    struct timespec t0, t1;
    clock_gettime(CLOCK_MONOTONIC, &t0);
    if (pyrowave_decoder_decode_cpu_buffer_synchronous(g_pw_dec, &buf) != PYROWAVE_SUCCESS) { pw_fail("decode failed", (int)len); goto out; }
    clock_gettime(CLOCK_MONOTONIC, &t1);
    g_pw_decode_us += (uint64_t)((t1.tv_sec - t0.tv_sec) * 1000000 + (t1.tv_nsec - t0.tv_nsec) / 1000);

    if ((w1 >> 30) & 1) {
        for (size_t i = 0; i < chroma; i++) {
            puv[i * 2]     = pcb[i];
            puv[i * 2 + 1] = pcr[i];
        }
    } else {
        // Full-range stream (rust-shine's scaled encode): the renderer samples limited
        // range, so compress the levels here, fused with the interleave.
        static uint8_t ly[256], lc[256];
        static int luts = 0;
        if (!luts) { pw_range_luts(ly, lc); luts = 1; }
        for (size_t i = 0; i < luma; i++) py[i] = ly[py[i]];
        for (size_t i = 0; i < chroma; i++) {
            puv[i * 2]     = lc[pcb[i]];
            puv[i * 2 + 1] = lc[pcr[i]];
        }
    }
    *y = py;
    *uv = puv;
    *w_out = w;
    *h_out = h;
    *full_range = 0; // levels are limited range by now (compressed above if the stream was full)
    if (++g_pw_frames == 1) pw_log("pyrowave: first frame decoded (%dx%d)", w, h);
    if (g_pw_frames % 600 == 0) {
        pw_log("pyrowave: decode + readback %d us per frame (average of the last %d)",
               (int)(g_pw_decode_us / 600), 600);
        g_pw_decode_us = 0;
    }
    ok = 1;
out:
    pthread_mutex_unlock(&g_pw_mu);
    return ok;
}

// pw_decode takes the frame payload as it came off the wire (dense, record framed or
// length-prefixed -- see pyrowave_framing.h) and decodes it.
static int pw_decode(const uint8_t *au, size_t len,
                     const uint8_t **y, const uint8_t **uv,
                     int *w_out, int *h_out, int *full_range) {
    uint8_t *dense = (uint8_t *)malloc(len ? len : 1);
    if (!dense) return 0;
    size_t n = pw_unframe(au, len, dense);
    int ok = 0;
    if (n) ok = pw_decode_dense(dense, n, y, uv, w_out, h_out, full_range);
    else pw_fail("malformed framing", (int)len);
    free(dense);
    return ok;
}

// ── decode thread ──────────────────────────────────────────────────────────────

static pthread_mutex_t g_pwq_mu = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t  g_pwq_cv = PTHREAD_COND_INITIALIZER;
static pthread_t       g_pwq_thread;
static int             g_pwq_running = 0, g_pwq_stop = 0, g_pwq_ready = 0;
static uint8_t        *g_pwq_slot = NULL, *g_pwq_work = NULL; // swapped under g_pwq_mu
static size_t          g_pwq_slot_len = 0, g_pwq_slot_cap = 0, g_pwq_work_cap = 0;
static uint64_t        g_pwq_skipped = 0;

static void *pw_worker(void *unused) {
    (void)unused;
    for (;;) {
        pthread_mutex_lock(&g_pwq_mu);
        while (!g_pwq_ready && !g_pwq_stop) pthread_cond_wait(&g_pwq_cv, &g_pwq_mu);
        if (g_pwq_stop) { pthread_mutex_unlock(&g_pwq_mu); break; }
        uint8_t *t = g_pwq_work; g_pwq_work = g_pwq_slot; g_pwq_slot = t;
        size_t tc = g_pwq_work_cap; g_pwq_work_cap = g_pwq_slot_cap; g_pwq_slot_cap = tc;
        size_t len = g_pwq_slot_len;
        g_pwq_ready = 0;
        pthread_mutex_unlock(&g_pwq_mu);

        const uint8_t *y = NULL, *uv = NULL;
        int w = 0, h = 0, full_range = 0;
        if (pw_decode(g_pwq_work, len, &y, &uv, &w, &h, &full_range))
            pyrowave_linux_deliver(y, uv, w, h, full_range);
    }
    return NULL;
}

// pyrowave_linux_slot_begin locks the decode thread's one-unit slot and returns room for
// a unit of `total` bytes (NULL when it cannot be allocated, slot unlocked). The caller
// copies the unit in and calls pyrowave_linux_slot_commit, which replaces any unit the
// decode thread has not started on yet.
uint8_t *pyrowave_linux_slot_begin(size_t total) {
    pthread_mutex_lock(&g_pwq_mu);
    if (!g_pwq_running) {
        g_pwq_stop = 0;
        if (pthread_create(&g_pwq_thread, NULL, pw_worker, NULL) != 0) {
            pthread_mutex_unlock(&g_pwq_mu);
            return NULL;
        }
        g_pwq_running = 1;
    }
    if (g_pwq_slot_cap < total) {
        free(g_pwq_slot);
        g_pwq_slot = malloc(total);
        g_pwq_slot_cap = g_pwq_slot ? total : 0;
        if (!g_pwq_slot) { pthread_mutex_unlock(&g_pwq_mu); return NULL; }
    }
    // A unit the decode thread never picked up is about to be overwritten.
    if (g_pwq_ready) { g_pwq_skipped++; g_pwq_ready = 0; }
    return g_pwq_slot;
}

void pyrowave_linux_slot_commit(size_t total) {
    g_pwq_slot_len = total;
    g_pwq_ready = 1;
    pthread_cond_signal(&g_pwq_cv);
    pthread_mutex_unlock(&g_pwq_mu);
}

static void pw_stop_worker(void) {
    pthread_mutex_lock(&g_pwq_mu);
    int running = g_pwq_running;
    g_pwq_stop = 1;
    pthread_cond_signal(&g_pwq_cv);
    pthread_mutex_unlock(&g_pwq_mu);
    if (running) pthread_join(g_pwq_thread, NULL);
    pthread_mutex_lock(&g_pwq_mu);
    if (g_pwq_skipped) pw_log("pyrowave: %d frames skipped while the decoder was busy", (int)g_pwq_skipped, 0);
    g_pwq_running = 0;
    g_pwq_ready = 0;
    g_pwq_skipped = 0;
    pthread_mutex_unlock(&g_pwq_mu);
}

// pyrowave_linux_unframe reduces a frame payload as it came off the wire (dense, record
// framed or length-prefixed, see pyrowave_framing.h) to the dense access unit; `out` needs
// room for `len` bytes. Returns the dense length, 0 for a malformed payload. Used by the
// GPU path (vk_video_try_submit_pyrowave), which decodes inside the renderer.
size_t pyrowave_linux_unframe(const uint8_t *in, size_t len, uint8_t *out) {
    return pw_unframe(in, len, out);
}

extern unsigned long long vk_video_pyrowave_frames(void);

// pyrowave_linux_frames counts the frames decoded since the stream started: those of the CPU
// path plus those the renderer decoded on the GPU.
uint64_t pyrowave_linux_frames(void) {
    pthread_mutex_lock(&g_pw_mu);
    uint64_t n = g_pw_frames;
    pthread_mutex_unlock(&g_pw_mu);
    n += (uint64_t)vk_video_pyrowave_frames();
    return n;
}

// pyrowave_linux_teardown drops the decoder and its device. Called once the stream's
// threads are joined (platform_post_stop).
void pyrowave_linux_teardown(void) {
    pw_stop_worker();
    pthread_mutex_lock(&g_pw_mu);
    pw_destroy_decoder();
    if (g_pw_dev) { pyrowave_device_destroy(g_pw_dev); g_pw_dev = NULL; }
    g_pw_dev_state = 0;
    g_pw_frames = 0;
    g_pw_decode_us = 0;
    g_pw_failures = 0;
    pthread_mutex_unlock(&g_pw_mu);
}

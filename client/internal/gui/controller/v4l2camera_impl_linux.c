//go:build linux && !android

// Minimal V4L2 webcam capture used by the desktop QR scanner on Linux.
// Replaces the previous gstreamer autovideosrc pipeline (qr_camera_scanner.go,
// now removed) so the Linux build no longer needs to link or bundle
// GStreamer at all — capture goes straight through the kernel's V4L2 ioctl
// interface, mirroring avcamera_impl_darwin.m (AVFoundation) and
// mfcamera_impl_windows.c (Media Foundation).
//
// Camera access on desktop Linux needs no portal/permission dialog (unlike
// screen-cast on Wayland) — a plain /dev/videoN open is standard as long as
// the user is in the "video" group, which is the default on every mainstream
// distro. Portal/PipeWire plumbing exists in this repo for Wayland
// screen-cast (agent/internal/capture/portal_linux_wayland.go) but that's a
// different, sandboxing-oriented mechanism not needed for a plain webcam.
//
// Only ever one camera session is open at a time (the QR scan popup), so
// global state is enough — no handle table.

#include <linux/videodev2.h>
#include <sys/ioctl.h>
#include <sys/mman.h>
#include <fcntl.h>
#include <unistd.h>
#include <poll.h>
#include <errno.h>
#include <string.h>
#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>

#include "v4l2camera_impl_linux.h"

extern void goV4L2CameraLog(char *msg, int level);

static void v4l2_log(int level, const char *fmt, ...) {
    char buf[512];
    va_list args;
    va_start(args, fmt);
    vsnprintf(buf, sizeof(buf), fmt, args);
    va_end(args);
    goV4L2CameraLog(buf, level);
}

#define V4L2_NUM_BUFFERS 4

struct mapped_buffer {
    void *start;
    size_t length;
};

static int g_fd = -1;
static int g_width = 0;
static int g_height = 0;
static struct mapped_buffer g_buffers[V4L2_NUM_BUFFERS];
static int g_buffer_count = 0;
static int g_streaming = 0;

static int xioctl(int fd, unsigned long request, void *arg) {
    int r;
    do {
        r = ioctl(fd, request, arg);
    } while (r == -1 && errno == EINTR);
    return r;
}

static inline uint8_t clamp_u8(int v) {
    if (v < 0) return 0;
    if (v > 255) return 255;
    return (uint8_t)v;
}

// yuyv_to_rgba converts packed YUYV 4:2:2 (2 pixels per 4 bytes: Y0 U Y1 V)
// to top-down packed RGBA, using fixed-point BT.601 coefficients. Accuracy
// only needs to be good enough for gozxing to find the QR pattern and for
// the live preview to look right, not colorimetrically exact.
static void yuyv_to_rgba(const uint8_t *src, uint8_t *dst, int width, int height) {
    int numPixelPairs = (width * height) / 2;
    for (int i = 0; i < numPixelPairs; i++) {
        int y0 = src[i * 4 + 0];
        int u  = src[i * 4 + 1] - 128;
        int y1 = src[i * 4 + 2];
        int v  = src[i * 4 + 3] - 128;

        int r0 = y0 + ((91881 * v) >> 16);
        int g0 = y0 - ((22554 * u + 46802 * v) >> 16);
        int b0 = y0 + ((116130 * u) >> 16);

        int r1 = y1 + ((91881 * v) >> 16);
        int g1 = y1 - ((22554 * u + 46802 * v) >> 16);
        int b1 = y1 + ((116130 * u) >> 16);

        uint8_t *out0 = dst + (size_t)i * 8;
        out0[0] = clamp_u8(r0);
        out0[1] = clamp_u8(g0);
        out0[2] = clamp_u8(b0);
        out0[3] = 0xFF;
        out0[4] = clamp_u8(r1);
        out0[5] = clamp_u8(g1);
        out0[6] = clamp_u8(b1);
        out0[7] = 0xFF;
    }
}

// try_open_device opens path if it's a V4L2 video-capture *streaming*
// device — not a metadata-only or output-only node. Modern webcams often
// expose multiple /dev/videoN nodes for the same physical camera, so this
// capability check is what tells them apart.
static int try_open_device(const char *path) {
    int fd = open(path, O_RDWR | O_NONBLOCK, 0);
    if (fd < 0) {
        return -1;
    }

    struct v4l2_capability cap;
    memset(&cap, 0, sizeof(cap));
    if (xioctl(fd, VIDIOC_QUERYCAP, &cap) == -1) {
        close(fd);
        return -1;
    }

    uint32_t caps = (cap.capabilities & V4L2_CAP_DEVICE_CAPS) ? cap.device_caps : cap.capabilities;
    if (!(caps & V4L2_CAP_VIDEO_CAPTURE) || !(caps & V4L2_CAP_STREAMING)) {
        close(fd);
        return -1;
    }

    return fd;
}

// find_and_open_camera scans /dev/video0../dev/video15 for the first node
// that's an actual streaming capture device.
static int find_and_open_camera(char *pathOut, size_t pathOutLen) {
    char path[32];
    for (int i = 0; i < 16; i++) {
        snprintf(path, sizeof(path), "/dev/video%d", i);
        int fd = try_open_device(path);
        if (fd >= 0) {
            if (pathOut) {
                snprintf(pathOut, pathOutLen, "%s", path);
            }
            return fd;
        }
    }
    return -1;
}

// v4l2_camera_open finds the first usable capture device and negotiates a
// YUYV output close to (reqWidth x reqHeight) — the one raw pixel format
// virtually every UVC webcam supports at this resolution. On success the
// actual negotiated size is written to outWidth/outHeight.
//
// MJPEG-only devices (some higher-megapixel webcams that only offer
// compressed output, uncommon at 640x480) aren't decoded here and will fail
// to open with a clear log message rather than being treated as YUYV and
// producing garbage frames.
int v4l2_camera_open(int reqWidth, int reqHeight, int *outWidth, int *outHeight) {
    if (g_fd >= 0) {
        *outWidth = g_width;
        *outHeight = g_height;
        return 1;
    }

    char path[32];
    int fd = find_and_open_camera(path, sizeof(path));
    if (fd < 0) {
        v4l2_log(1, "No V4L2 capture device found");
        return 0;
    }

    struct v4l2_format fmt;
    memset(&fmt, 0, sizeof(fmt));
    fmt.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    fmt.fmt.pix.width = reqWidth;
    fmt.fmt.pix.height = reqHeight;
    fmt.fmt.pix.pixelformat = V4L2_PIX_FMT_YUYV;
    fmt.fmt.pix.field = V4L2_FIELD_ANY;

    if (xioctl(fd, VIDIOC_S_FMT, &fmt) == -1) {
        v4l2_log(2, "%s: VIDIOC_S_FMT failed: %s", path, strerror(errno));
        close(fd);
        return 0;
    }
    if (fmt.fmt.pix.pixelformat != V4L2_PIX_FMT_YUYV) {
        v4l2_log(1, "%s: camera only offers pixel format 0x%x, not YUYV — unsupported for now",
                  path, fmt.fmt.pix.pixelformat);
        close(fd);
        return 0;
    }

    struct v4l2_requestbuffers req;
    memset(&req, 0, sizeof(req));
    req.count = V4L2_NUM_BUFFERS;
    req.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    req.memory = V4L2_MEMORY_MMAP;
    if (xioctl(fd, VIDIOC_REQBUFS, &req) == -1 || req.count < 2) {
        v4l2_log(2, "%s: VIDIOC_REQBUFS failed: %s", path, strerror(errno));
        close(fd);
        return 0;
    }

    memset(g_buffers, 0, sizeof(g_buffers));
    int bufferCount = (int)req.count;
    if (bufferCount > V4L2_NUM_BUFFERS) {
        bufferCount = V4L2_NUM_BUFFERS;
    }

    for (int i = 0; i < bufferCount; i++) {
        struct v4l2_buffer buf;
        memset(&buf, 0, sizeof(buf));
        buf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
        buf.memory = V4L2_MEMORY_MMAP;
        buf.index = i;
        if (xioctl(fd, VIDIOC_QUERYBUF, &buf) == -1) {
            v4l2_log(2, "%s: VIDIOC_QUERYBUF failed: %s", path, strerror(errno));
            goto fail_unmap;
        }
        void *start = mmap(NULL, buf.length, PROT_READ | PROT_WRITE, MAP_SHARED, fd, buf.m.offset);
        if (start == MAP_FAILED) {
            v4l2_log(2, "%s: mmap failed: %s", path, strerror(errno));
            goto fail_unmap;
        }
        g_buffers[i].start = start;
        g_buffers[i].length = buf.length;

        if (xioctl(fd, VIDIOC_QBUF, &buf) == -1) {
            v4l2_log(2, "%s: VIDIOC_QBUF failed: %s", path, strerror(errno));
            goto fail_unmap;
        }
    }
    g_buffer_count = bufferCount;

    {
        enum v4l2_buf_type type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
        if (xioctl(fd, VIDIOC_STREAMON, &type) == -1) {
            v4l2_log(2, "%s: VIDIOC_STREAMON failed: %s", path, strerror(errno));
            goto fail_unmap;
        }
    }

    g_fd = fd;
    g_width = (int)fmt.fmt.pix.width;
    g_height = (int)fmt.fmt.pix.height;
    g_streaming = 1;
    *outWidth = g_width;
    *outHeight = g_height;
    v4l2_log(0, "%s: camera opened %dx%d YUYV", path, g_width, g_height);
    return 1;

fail_unmap:
    for (int i = 0; i < bufferCount; i++) {
        if (g_buffers[i].start) {
            munmap(g_buffers[i].start, g_buffers[i].length);
            g_buffers[i].start = NULL;
        }
    }
    g_buffer_count = 0;
    close(fd);
    return 0;
}

// v4l2_camera_read_frame waits (with a timeout) for the next queued buffer,
// converts its YUYV data to top-down packed RGBA into buf, then re-queues
// the V4L2 buffer for reuse.
// Returns: 1 = frame written, 0 = end of stream / fatal error (caller must
// stop), 2 = no sample this call (caller should just call again).
int v4l2_camera_read_frame(uint8_t *buf, int bufLen) {
    if (g_fd < 0) {
        return 0;
    }
    if (bufLen < g_width * g_height * 4) {
        v4l2_log(1, "Frame buffer too small: bufLen=%d needed=%d", bufLen, g_width * g_height * 4);
        return 2;
    }

    struct pollfd pfd;
    pfd.fd = g_fd;
    pfd.events = POLLIN;
    pfd.revents = 0;
    int pr = poll(&pfd, 1, 500); // 500ms timeout, matches the darwin/windows contract
    if (pr < 0) {
        if (errno == EINTR) {
            return 2;
        }
        v4l2_log(2, "poll failed: %s", strerror(errno));
        return 0;
    }
    if (pr == 0) {
        return 2; // timeout, no frame yet
    }

    struct v4l2_buffer v4lbuf;
    memset(&v4lbuf, 0, sizeof(v4lbuf));
    v4lbuf.type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
    v4lbuf.memory = V4L2_MEMORY_MMAP;
    if (xioctl(g_fd, VIDIOC_DQBUF, &v4lbuf) == -1) {
        if (errno == EAGAIN) {
            return 2;
        }
        v4l2_log(2, "VIDIOC_DQBUF failed: %s", strerror(errno));
        return 0;
    }

    if (v4lbuf.index < (unsigned)g_buffer_count) {
        yuyv_to_rgba((const uint8_t *)g_buffers[v4lbuf.index].start, buf, g_width, g_height);
    }

    if (xioctl(g_fd, VIDIOC_QBUF, &v4lbuf) == -1) {
        v4l2_log(1, "VIDIOC_QBUF (re-queue) failed: %s", strerror(errno));
    }

    return 1;
}

void v4l2_camera_close(void) {
    if (g_fd < 0) {
        return;
    }
    if (g_streaming) {
        enum v4l2_buf_type type = V4L2_BUF_TYPE_VIDEO_CAPTURE;
        xioctl(g_fd, VIDIOC_STREAMOFF, &type);
        g_streaming = 0;
    }
    for (int i = 0; i < g_buffer_count; i++) {
        if (g_buffers[i].start) {
            munmap(g_buffers[i].start, g_buffers[i].length);
            g_buffers[i].start = NULL;
        }
    }
    g_buffer_count = 0;
    close(g_fd);
    g_fd = -1;
    g_width = 0;
    g_height = 0;
}

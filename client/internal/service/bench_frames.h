// Per-frame recorder for the streamer benchmark (bench_recorder.go).
//
// Every DECODE_UNIT moonlight-common-c hands to dr_submit is one frame the
// host actually delivered; bench_frames_note copies the fields that let the
// benchmark separate *where* a stall came from:
//   - presentation_us: the host's own capture timestamp (from the RTP
//     timestamp) -- its spacing is the host's capture/encode cadence, with
//     no network in it at all;
//   - host_latency: the host's capture+encode time for this frame
//     (frameHostProcessingLatency, 1/10 ms);
//   - receive_us/enqueue_us: first packet in / frame fully reassembled on
//     this client (LiGetMicroseconds clock) -- enqueue spacing minus
//     presentation spacing is pure network-added jitter;
//   - frame_number: gaps are frames that never made it (lost, or dropped by
//     moonlight-common-c while waiting for recovery), and frame_type of the
//     first frame after a gap says how the host recovered (IDR vs RFI).
//
// Storage and bodies live in bench_frames.c: a non-static definition in a
// cgo preamble of a file with //export directives gets duplicated into
// _cgo_export.c (see moonlight_cgo_windows.go's note on
// g_last_host_latency_tenths_ms).
#ifndef USBRIDGE_BENCH_FRAMES_H
#define USBRIDGE_BENCH_FRAMES_H

#include <stdint.h>
#include <Limelight.h>

typedef struct {
    int32_t  frame_number;
    int32_t  frame_type;
    uint32_t size_bytes;
    uint16_t host_latency_tenths_ms;
    uint16_t _pad;
    uint64_t receive_us;
    uint64_t enqueue_us;
    uint64_t submit_us;
    uint64_t presentation_us;
} bench_frame_t;

void     bench_frames_note(const DECODE_UNIT *du);
void     bench_frames_enable(int on);
int      bench_frames_drain(bench_frame_t *out, int max);
uint32_t bench_frames_dropped(void);
uint64_t bench_now_us(void);

// Time barcode sampling (bench_frames.c). bench_latency_due is one atomic load and a
// clock read, true a few times a second while the benchmark records. A decoder then
// stamps the frame with bench_wall_us, gets its luma plane to the CPU if it has to
// (bytes_per_sample 2: P010), and calls bench_latency_note.
int      bench_latency_due(void);
int      bench_recording(void);
int64_t  bench_wall_us(void);
void     bench_latency_note(const uint8_t *luma, int stride, int width, int height, int bytes_per_sample, int64_t wall_us);
void     bench_latency_note_bgra(const uint8_t *bgra, int stride, int width, int height, int64_t wall_us);
void     bench_latency_note_rgba_row(const uint8_t *rgba, int width, int height, int64_t wall_us);

#endif

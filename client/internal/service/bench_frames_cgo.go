//go:build cgo && (linux || darwin || ios || windows || android)

package service

/*
#include <stdint.h>

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

// Bodies in bench_frames.c (declared again here rather than including
// bench_frames.h, which pulls in Limelight.h and its include path).
extern void     bench_frames_enable(int on);
extern int      bench_frames_drain(bench_frame_t *out, int max);
extern uint32_t bench_frames_dropped(void);
extern uint64_t bench_now_us(void);
extern int      bench_latency_drain(int64_t *wall_us, uint32_t *bar, int max);
extern int      bench_barcode_decode_row(const uint8_t *row, int width, int bytes_per_sample, uint32_t *out);
*/
import "C"

import "unsafe"

func init() {
	benchFramesEnableFn = func(on bool) {
		v := C.int(0)
		if on {
			v = 1
		}
		C.bench_frames_enable(v)
	}
	benchFramesDrainFn = benchDrainCgo
	benchFramesDroppedFn = func() uint32 { return uint32(C.bench_frames_dropped()) }
	benchNowUsFn = func() uint64 { return uint64(C.bench_now_us()) }
	benchLatencyDrainFn = benchLatencyDrainCgo
}

const benchDrainBatch = 1024

func benchDrainCgo(dst []BenchFrame) []BenchFrame {
	var buf [benchDrainBatch]C.bench_frame_t
	for {
		n := int(C.bench_frames_drain((*C.bench_frame_t)(unsafe.Pointer(&buf[0])), benchDrainBatch))
		for i := 0; i < n; i++ {
			f := &buf[i]
			dst = append(dst, BenchFrame{
				Number:        int32(f.frame_number),
				IDR:           f.frame_type == 1, // FRAME_TYPE_IDR
				Size:          uint32(f.size_bytes),
				HostLatencyMs: float64(f.host_latency_tenths_ms) / 10,
				ReceiveUs:     uint64(f.receive_us),
				EnqueueUs:     uint64(f.enqueue_us),
				SubmitUs:      uint64(f.submit_us),
				PtsUs:         uint64(f.presentation_us),
			})
		}
		if n < benchDrainBatch {
			return dst
		}
	}
}

func benchLatencyDrainCgo(dst []BenchLatencySample) []BenchLatencySample {
	const batch = 256
	var wall [batch]C.int64_t
	var bar [batch]C.uint32_t
	for {
		n := int(C.bench_latency_drain(&wall[0], &bar[0], batch))
		for i := 0; i < n; i++ {
			dst = append(dst, BenchLatencySample{WallUs: int64(wall[i]), Bar: uint32(bar[i])})
		}
		if n < batch {
			return dst
		}
	}
}

// benchDecodeBarcodeRow is bench_barcode_decode_row for the tests (a _test.go file can't use cgo).
func benchDecodeBarcodeRow(row []byte, bytesPerSample int) (uint32, bool) {
	var out C.uint32_t
	width := len(row) / bytesPerSample
	ok := C.bench_barcode_decode_row((*C.uint8_t)(unsafe.Pointer(&row[0])), C.int(width), C.int(bytesPerSample), &out) != 0
	return uint32(out), ok
}

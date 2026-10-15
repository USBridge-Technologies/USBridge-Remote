//go:build darwin

// metalspike is a throwaway manual validation harness for the Metal FSR
// spike (see internal/service/metal_video_impl_darwin.m's "SPIKE:" section).
// It opens a real on-screen window, submits a synthetic BGRA test-pattern
// frame (no live host/stream needed), pumps the AppKit run loop so
// CADisplayLink actually fires, prints the window number (for `screencapture
// -l<n>`) and FPS/latency stats, then exits. Delete this directory once the
// spike is validated -- it exists only to answer "does a real Metal
// compute+present loop work on this thread without regressing render
// throughput" before building FSR/bicubic on top of it.
//
// Usage: go run ./cmd/metalspike
package main

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreVideo -framework CoreFoundation

#import <AppKit/AppKit.h>
#import <CoreVideo/CoreVideo.h>
#include <stdint.h>

// Implemented in internal/service/metal_video_impl_darwin.m -- called
// directly via cgo rather than through any Go wrapper, since
// internal/service doesn't export one (real callers reach it from
// moonlight_cgo_apple.go's C-side vt_callback).
extern int metal_video_try_submit(CVImageBufferRef img);

static uintptr_t spike_make_window(void) {
    __block NSWindow *win = nil;
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
        NSRect frame = NSMakeRect(100, 100, 960, 540);
        win = [[NSWindow alloc] initWithContentRect:frame
                                           styleMask:(NSWindowStyleMaskTitled | NSWindowStyleMaskClosable)
                                             backing:NSBackingStoreBuffered
                                               defer:NO];
        [win setTitle:@"metal-fsr-spike"];
        [win makeKeyAndOrderFront:nil];
        [NSApp activateIgnoringOtherApps:YES];
    }
    return (uintptr_t)(__bridge void *)win;
}

// Builds an IOSurface-backed BGRA CVPixelBuffer with a simple test pattern
// (vertical color bands + a diagonal line) so a bilinear resize is visually
// obvious (band edges/the diagonal will look different if resized wrong).
static CVPixelBufferRef spike_make_test_frame(int w, int h) {
    NSDictionary *attrs = @{
        (NSString *)kCVPixelBufferIOSurfacePropertiesKey: @{},
        (NSString *)kCVPixelBufferMetalCompatibilityKey: @YES,
    };
    CVPixelBufferRef buf = NULL;
    CVReturn r = CVPixelBufferCreate(kCFAllocatorDefault, w, h, kCVPixelFormatType_32BGRA,
                                      (__bridge CFDictionaryRef)attrs, &buf);
    if (r != kCVReturnSuccess || !buf) return NULL;

    CVPixelBufferLockBaseAddress(buf, 0);
    uint8_t *base = (uint8_t *)CVPixelBufferGetBaseAddress(buf);
    size_t stride = CVPixelBufferGetBytesPerRow(buf);
    for (int y = 0; y < h; y++) {
        uint8_t *row = base + y * stride;
        for (int x = 0; x < w; x++) {
            int band = (x * 6) / w; // 6 vertical color bands
            uint8_t r8=0,g8=0,b8=0;
            switch (band % 6) {
                case 0: r8=255; g8=0;   b8=0;   break;
                case 1: r8=0;   g8=255; b8=0;   break;
                case 2: r8=0;   g8=0;   b8=255; break;
                case 3: r8=255; g8=255; b8=0;   break;
                case 4: r8=255; g8=0;   b8=255; break;
                case 5: r8=0;   g8=255; b8=255; break;
            }
            // Diagonal white line for a sharpness/alignment sanity check.
            if (abs((x * h / w) - y) < 2) { r8=255; g8=255; b8=255; }
            row[x*4+0]=b8; row[x*4+1]=g8; row[x*4+2]=r8; row[x*4+3]=255;
        }
    }
    CVPixelBufferUnlockBaseAddress(buf, 0);
    return buf;
}

static void spike_submit_test_frame(int w, int h) {
    CVPixelBufferRef buf = spike_make_test_frame(w, h);
    if (!buf) return;
    metal_video_try_submit(buf); // takes its own retain; release ours
    CVPixelBufferRelease(buf);
}

// Builds a kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange test frame with
// the SAME 6-color-band + diagonal-line pattern as spike_make_test_frame,
// but encoded through the FORWARD BT.2020 limited-range RGB->YCbCr matrix
// (the exact inverse of fsr_metal.metal's yuv2020_to_rgb kernel) -- a
// closed-loop test: if the kernel's decode matrix is correct, the rendered
// picture should look identical to the plain BGRA version (same colors,
// same band edges, same crisp diagonal), entirely independent of any real
// HDR source/decoder.
static CVPixelBufferRef spike_make_test_frame_hdr(int w, int h) {
    NSDictionary *attrs = @{
        (NSString *)kCVPixelBufferIOSurfacePropertiesKey: @{},
        (NSString *)kCVPixelBufferMetalCompatibilityKey: @YES,
    };
    CVPixelBufferRef buf = NULL;
    CVReturn r = CVPixelBufferCreate(kCFAllocatorDefault, w, h, kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange,
                                      (__bridge CFDictionaryRef)attrs, &buf);
    if (r != kCVReturnSuccess || !buf) return NULL;

    CVBufferSetAttachment(buf, kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_2020, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferTransferFunctionKey, kCVImageBufferTransferFunction_SMPTE_ST_2084_PQ, kCVAttachmentMode_ShouldPropagate);
    CVBufferSetAttachment(buf, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_2020, kCVAttachmentMode_ShouldPropagate);

    CVPixelBufferLockBaseAddress(buf, 0);
    uint16_t *yBase = (uint16_t *)CVPixelBufferGetBaseAddressOfPlane(buf, 0);
    size_t yStride = CVPixelBufferGetBytesPerRowOfPlane(buf, 0) / 2;
    uint16_t *cBase = (uint16_t *)CVPixelBufferGetBaseAddressOfPlane(buf, 1);
    size_t cStride = CVPixelBufferGetBytesPerRowOfPlane(buf, 1) / 2; // interleaved Cb,Cr pairs

    // colorAt: the same 6-band + diagonal pattern as spike_make_test_frame.
    void (^colorAt)(int, int, float*, float*, float*) = ^(int x, int y, float *rr, float *gg, float *bb) {
        int band = (x * 6) / w;
        float r8f=0, g8f=0, b8f=0;
        switch (band % 6) {
            case 0: r8f=1; g8f=0; b8f=0; break;
            case 1: r8f=0; g8f=1; b8f=0; break;
            case 2: r8f=0; g8f=0; b8f=1; break;
            case 3: r8f=1; g8f=1; b8f=0; break;
            case 4: r8f=1; g8f=0; b8f=1; break;
            case 5: r8f=0; g8f=1; b8f=1; break;
        }
        if (abs((x * h / w) - y) < 2) { r8f=1; g8f=1; b8f=1; }
        *rr=r8f; *gg=g8f; *bb=b8f;
    };

    for (int y = 0; y < h; y++) {
        for (int x = 0; x < w; x++) {
            float r8f, g8f, b8f;
            colorAt(x, y, &r8f, &g8f, &b8f);
            // Forward BT.2020 RGB->Y'CbCr' (matches yuv2020_to_rgb's inverse exactly).
            float yp = 0.2627f*r8f + 0.6780f*g8f + 0.0593f*b8f;
            float yCode = yp*876.0f + 64.0f;
            yBase[y*yStride + x] = (uint16_t)(yCode * 64.0f + 0.5f);
        }
    }
    // Chroma plane: one Cb,Cr sample per 2x2 luma block (nearest, from the
    // block's top-left pixel -- good enough for wide, flat-color test bands).
    for (int cy = 0; cy < h/2; cy++) {
        for (int cx = 0; cx < w/2; cx++) {
            float r8f, g8f, b8f;
            colorAt(cx*2, cy*2, &r8f, &g8f, &b8f);
            float yp = 0.2627f*r8f + 0.6780f*g8f + 0.0593f*b8f;
            float cb = (b8f - yp) / 1.8814f;
            float cr = (r8f - yp) / 1.4746f;
            float cbCode = cb*896.0f + 512.0f;
            float crCode = cr*896.0f + 512.0f;
            cBase[cy*cStride + cx*2 + 0] = (uint16_t)(cbCode * 64.0f + 0.5f);
            cBase[cy*cStride + cx*2 + 1] = (uint16_t)(crCode * 64.0f + 0.5f);
        }
    }
    CVPixelBufferUnlockBaseAddress(buf, 0);
    return buf;
}

static void spike_submit_test_frame_hdr(int w, int h) {
    CVPixelBufferRef buf = spike_make_test_frame_hdr(w, h);
    if (!buf) return;
    metal_video_try_submit(buf);
    CVPixelBufferRelease(buf);
}

static void spike_pump_runloop(double seconds) {
    CFRunLoopRunInMode(kCFRunLoopDefaultMode, seconds, false);
}

// CGWindowListCreateImage is obsoleted on macOS 15+ (ScreenCaptureKit only,
// which is async) -- print the window number instead so the caller can
// shell out to the `screencapture` CLI (-l<windowNumber>), which still works.
static long spike_window_number(uintptr_t winPtr) {
    NSWindow *win = (__bridge NSWindow *)(void *)winPtr;
    return (long)win.windowNumber;
}
*/
import "C"

import (
	"fmt"
	"os"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

func main() {
	// Exercises the REAL end-to-end wiring (service.SetUpscaleMode -> the
	// upscaleModeMetalSet hook -> MetalVideoSetUpscaleMode -> cgo), the same
	// path video_start_dialog.go's picker uses -- not the METAL_UPSCALE_MODE
	// env var fallback, which is only a dev-convenience default read once at
	// first use. METALSPIKE_MODE selects which: bilinear (default) | bicubic
	// | lanczos | fsr1.
	if mode := os.Getenv("METALSPIKE_MODE"); mode != "" {
		m := models.UpscaleModeBilinear
		switch mode {
		case "bicubic":
			m = models.UpscaleModeBicubic
		case "lanczos":
			m = models.UpscaleModeLanczos
		case "fsr1":
			m = models.UpscaleModeFSR1
		}
		service.SetUpscaleMode(m)
	}

	win := uintptr(C.spike_make_window())
	if win == 0 {
		fmt.Fprintln(os.Stderr, "spike_make_window failed")
		os.Exit(1)
	}
	if !service.MetalVideoCreate(win, 0, 0, 0, 0) {
		fmt.Fprintln(os.Stderr, "MetalVideoCreate failed")
		os.Exit(1)
	}

	// A source resolution smaller than the 960x540 window so the resize is
	// a real upscale, matching the FSR use case ("stream at lower res").
	// METALSPIKE_HDR=1 submits the HDR (10-bit BT.2020 biplanar) test frame
	// instead of the plain BGRA one -- see spike_make_test_frame_hdr's own
	// doc comment for why this is a meaningful closed-loop correctness test
	// without needing a real HDR decoder/source.
	if os.Getenv("METALSPIKE_HDR") == "1" {
		C.spike_submit_test_frame_hdr(480, 270)
	} else {
		C.spike_submit_test_frame(480, 270)
	}

	wn := int(C.spike_window_number(C.uintptr_t(win)))
	fmt.Fprintf(os.Stderr, "SPIKE_WINDOW_NUMBER=%d\n", wn)
	fmt.Fprintln(os.Stderr, "pumping run loop for 10s -- capture with: screencapture -l<N> -x <path>")
	C.spike_pump_runloop(10.0)

	fmt.Fprintf(os.Stderr, "done -- fps=%.1f decodeMs=%.2f\n", service.MetalVideoLastFPS(), service.MetalVideoLastDecodeMs())
	service.MetalVideoDestroy()
}

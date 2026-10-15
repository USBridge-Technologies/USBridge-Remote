// metal_video_impl_darwin.m — ObjC implementation compiled as a separate translation unit.
// CGO picks up .m files automatically; keeping implementation here avoids
// duplicate-symbol errors that occur when C code is inlined in multiple
// Go files that all do `import "C"` in the same package.
#include <TargetConditionals.h>
#if !TARGET_OS_IPHONE

#import <AppKit/AppKit.h>
#import <CoreVideo/CoreVideo.h>
#import <QuartzCore/QuartzCore.h>
#import <Metal/Metal.h>
#import <AVFoundation/AVFoundation.h>
#import <CoreMedia/CoreMedia.h>
#include <stdatomic.h>
#include <pthread.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <time.h>

// Forward declaration — CGO generates this export from metal_video_darwin.go.
// Must use char* (not const char*) to match the CGO-generated signature.
extern void goMetalLog(char *msg, int level);

// ─────────────────────────────────────────────────────────────────────────────
// Global singleton state (one Metal overlay at a time).
// All NSView/CALayer access happens exclusively on the main thread.
// CVPixelBufferRef pending frame is protected by g_mu.
// ─────────────────────────────────────────────────────────────────────────────
static NSView  *g_view   = nil;
static CALayer *g_layer  = nil;

// AVSampleBufferDisplayLayer video path -- sibling of g_layer/g_metal_layer,
// takes over as the actual on-screen video presentation (see
// metal_video_submit_compressed_sample below). Unlike g_layer's
// VTDecompressionSession + CVPixelBuffer + CADisplayLink "whatever's newest
// wins" polling, this hands the still-*compressed* CMSampleBuffer straight
// to AVFoundation, which decodes AND schedules presentation itself -- the
// same approach the official Moonlight client uses (moonlight-ios's
// VideoDecoderRenderer.m), instead of a hand-rolled decode+present pipeline.
// No controlTimebase is attached (official doesn't set one either), AND
// every sample fed in via metal_video_submit_compressed_sample carries the
// kCMSampleAttachmentKey_DisplayImmediately attachment (set in
// moonlight_cgo_apple.go's platform_dr_submit, see its own doc comment) --
// without that attachment, AVSampleBufferDisplayLayer still paces display
// against each sample's presentationTimeStamp via its own internally
// created timebase even with no controlTimebase attached, which reproduces
// the host's encode/network jitter as on-screen judder. The attachment is
// what actually makes it display each sample the instant it's ready, the
// right behavior for low-latency game streaming, not a video player's "play
// back at the recorded rate".
static AVSampleBufferDisplayLayer *g_avsbdl = nil;

// Set whenever a *fresh* g_avsbdl is created (metal_video_create), cleared
// once a real IDR has been fed to it. Needed because this overlay is only
// created reactively -- off the Go side's frameNum==1 bootstrap, itself
// only reachable once a frame has already been decoded (see
// platform_dr_submit's own goVTFrame call) -- so by the time g_avsbdl
// actually exists, the stream's *real* opening IDR is long gone: every
// frame that arrived during the create race (dispatch_sync to the main
// thread, a goroutine hop, ...) was silently dropped (g_avsbdl was still
// nil), and what's arriving now is an ordinary delta frame referencing
// decoder state this brand-new layer never had. Unlike the old
// VTDecompressionSession path -- which decoded continuously regardless of
// whether a CALayer existed to show the result, so by the time one did,
// its reference-frame state was already caught up -- AVSampleBufferDisplayLayer
// only starts decoding once *we* feed it something, cold, so its first
// sample must actually be a valid IDR. Without requesting one, the fix was
// to wait for whatever periodic refresh/RFI cadence the host happened to
// be running -- confirmed live as the "took several minutes to show a
// picture" report this flag exists to fix.
static _Atomic int g_avsbdl_needs_idr = 0;

// AI Vision overlay layer, stacked directly above g_layer (the video
// IOSurface layer) and sharing its frame/gravity so a box drawn at pixel
// (x,y) of the detected frame lands on the exact same screen pixel the
// video content does, regardless of aspect-fit scaling. Populated via
// metal_video_set_overlay (see ai_vision.go's pushAIVisionOverlayToMetal)
// with a mostly-transparent RGBA image -- Core Animation composites it on
// the GPU for free every frame, so unlike the CPU-fallback decode path
// (moonlight_cgo_apple.go's vt_callback, which burns boxes directly into
// pixels via goAIVisionOverlay) this needs no per-frame CPU work at all:
// the image only changes once per completed detection pass.
static CALayer *g_overlay_layer = nil;

// Net Graph HUD layer, stacked above g_overlay_layer -- a small FIXED-size
// canvas anchored to the bottom-right corner of the view via its own frame
// (set directly in metal_video_set_hud_overlay), deliberately NOT sharing
// g_overlay_layer's full-video-frame sizing/gravity: AI Vision's overlay is
// sized to match the video content exactly so a box lands on the right
// pixel, but a net_graph HUD is a fixed small box independent of the video's
// resolution/aspect/scaling. Kept as its own layer rather than reusing
// g_overlay_layer so pushing it at 10-20Hz never means re-uploading a
// full-frame-sized (megabyte-class) image -- see net_graph.go's own doc
// comment on cost.
static CALayer *g_hud_layer = nil;

// ─────────────────────────────────────────────────────────────────────────────
// Real Metal compute upscale pipeline (FSR1 EASU+RCAS, plus a plain bilinear
// fallback), running on a CAMetalLayer sibling of g_layer -- see
// metal_video_create. Validated (spike, since removed) to run from the SAME
// call site as today's direct IOSurface->CALayer.contents assignment
// (metal_render_main_with_buf, itself called from displayLinkFired on the
// main thread) without regressing render throughput: steady 60Hz held for a
// 10s synthetic-frame run, only a one-time ~636ms stall on the very first
// ever shader compile (see metal_fsr_ensure_pipeline's warm-up comment for
// the fix). See VideoDepacketizer.c's playoutDelayForFrame doc comment and
// this file's own g_hud_pending_* doc comment for the two documented
// regression classes on this exact render path (decode/render moved off its
// expected thread, or work routed through dispatch_async instead of piggy-
// backing on displayLinkFired) -- this stays on that same thread/call site
// on purpose, for the same reason.
//
// Mode selection: metal_video_set_upscale_mode (called from Go --
// service.SetUpscaleMode, wired to video_start_dialog.go's upscale-quality
// picker) is the real path. METAL_UPSCALE_MODE ("bilinear" default,
// "bicubic", "lanczos", or "fsr1") env var is a secondary DEV-ONLY override,
// read once at first use as the starting value -- convenient for the
// standalone cmd/metalspike harness, overridden the moment Go calls the
// setter. UPSCALE_MODE_BILINEAR is the only mode that skips this whole
// pipeline (g_layer's original direct-assign path, untouched) -- selecting
// any other mode is what "enables" it, there's no separate on/off flag.
// ─────────────────────────────────────────────────────────────────────────────

typedef enum {
    UPSCALE_MODE_BILINEAR = 0,
    UPSCALE_MODE_BICUBIC  = 1,
    UPSCALE_MODE_LANCZOS  = 2,
    UPSCALE_MODE_FSR1     = 3,
} UpscaleMode;
static UpscaleMode g_upscale_mode = UPSCALE_MODE_BILINEAR;
static int g_upscale_mode_read = 0;

static id<MTLDevice>               g_mtl_device       = nil;
static id<MTLCommandQueue>         g_mtl_queue        = nil;
static CVMetalTextureCacheRef      g_mtl_tex_cache    = NULL;
static id<MTLComputePipelineState> g_mtl_bilinear_pso = nil;
static id<MTLComputePipelineState> g_mtl_bicubic_pso  = nil;
static id<MTLComputePipelineState> g_mtl_lanczos_pso  = nil;
static id<MTLComputePipelineState> g_mtl_easu_pso     = nil;
static id<MTLComputePipelineState> g_mtl_rcas_pso     = nil;
static id<MTLComputePipelineState> g_mtl_yuv2rgb_pso  = nil;
static CAMetalLayer               *g_metal_layer      = nil;

// EASU's output (== RCAS's input) intermediate texture, sized to the current
// drawable and recreated only when that size changes (not every frame).
// RGBA16Float (not BGRA8Unorm) so PQ-encoded HDR values round-trip through
// EASU/RCAS without 8-bit banding -- costs nothing extra for the SDR case,
// same numeric content just stored wider.
static id<MTLTexture> g_mtl_easu_out_tex = nil;
static NSUInteger      g_mtl_easu_out_w   = 0, g_mtl_easu_out_h = 0;

// HDR-only: yuv2020_to_rgb's output, at SOURCE resolution (not drawable
// size) -- becomes every mode's `inTex` for the HDR path, exactly like the
// SDR path's direct BGRA import, so EASU/RCAS/bilinear/bicubic/Lanczos never
// need to know or care whether the frame started out as YCbCr or BGRA.
static id<MTLTexture> g_mtl_yuv_rgb_tex = nil;
static NSUInteger      g_mtl_yuv_rgb_w   = 0, g_mtl_yuv_rgb_h = 0;

// Populated once at process start by metal_fsr_set_shader_source (see
// metal_fsr_shader_darwin.go's init(), which go:embeds fsr/fsr_metal.metal)
// -- long before any stream connects, so newLibraryWithSource's runtime
// compile below always happens during metal_fsr_ensure_pipeline's warm-up,
// never on a frame's own critical path.
static NSString *g_fsr_shader_source = nil;

void metal_fsr_set_shader_source(const char *src) {
    g_fsr_shader_source = src ? [NSString stringWithUTF8String:src] : nil;
}

static UpscaleMode metal_upscale_mode(void) {
    if (!g_upscale_mode_read) {
        g_upscale_mode_read = 1;
        const char *v = getenv("METAL_UPSCALE_MODE");
        if (v && strcmp(v, "fsr1") == 0) g_upscale_mode = UPSCALE_MODE_FSR1;
        else if (v && strcmp(v, "bicubic") == 0) g_upscale_mode = UPSCALE_MODE_BICUBIC;
        else if (v && strcmp(v, "lanczos") == 0) g_upscale_mode = UPSCALE_MODE_LANCZOS;
    }
    return g_upscale_mode;
}

// Forward declaration -- metal_fsr_ensure_pipeline is defined further down,
// used by metal_video_set_upscale_mode above that definition.
static int metal_fsr_ensure_pipeline(void);

// Forward declaration -- defined further down (near metal_video_set_hdr),
// used by metal_spike_render above that definition.
static void apply_dynamic_range(CALayer *layer, BOOL hdr);

// Forward declaration -- mono_sec is defined further down (shared by the fps/
// decode-latency counters), used by metal_spike_render's periodic
// CVMetalTextureCacheFlush throttle above that definition.
static double mono_sec(void);

// metal_video_set_upscale_mode is called from Go (service.SetUpscaleMode,
// wired to video_start_dialog.go's upscale-quality picker) as soon as the
// user's choice is known/changed -- no codec-negotiation dependency, unlike
// metal_video_set_hdr. mode: 0=bilinear 1=bicubic 2=lanczos 3=fsr1 (see
// models.UpscaleMode* on the Go side, which must stay in sync by hand -- no
// shared enum across the cgo boundary).
void metal_video_set_upscale_mode(int mode) {
    if (mode < UPSCALE_MODE_BILINEAR || mode > UPSCALE_MODE_FSR1) return;
    g_upscale_mode_read = 1; // an explicit call always wins over the env var default
    dispatch_block_t blk = ^{
        g_upscale_mode = (UpscaleMode)mode;
        // Warm up the shader pipeline right away rather than waiting for the
        // next frame to discover it needs it -- see metal_fsr_ensure_pipeline's
        // own doc comment for the ~636ms first-compile stall this avoids.
        // Harmless (and cheap: an early-return) if already warm, or if
        // called before any window/session exists yet.
        if (g_upscale_mode != UPSCALE_MODE_BILINEAR) metal_fsr_ensure_pipeline();
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// Lazily creates the device/queue/texture-cache/pipeline-state singletons on
// first use of ANY non-default mode -- normal sessions never touch this.
// Deliberately called once from metal_video_create (see its own call site)
// rather than truly lazily on the first frame: an earlier spike measured a
// ~636ms main-thread stall on the first-ever newLibraryWithSource runtime
// shader compile, which would otherwise land on a real frame's render call.
// Warming it up at session-create time instead (still off the video
// socket/decode thread's critical path -- metal_video_create runs on the
// main thread via dispatch_sync/dispatch_block_t, before any frame exists)
// means only the FIRST session of a process pays that cost, and it never
// competes with a frame deadline. Returns 1 on success (or if already
// initialized), 0 on failure -- callers must fall back to the plain g_layer
// path on failure, never show a stale/blank drawable.
static int metal_fsr_ensure_pipeline(void) {
    if (g_mtl_bilinear_pso && g_mtl_bicubic_pso && g_mtl_lanczos_pso && g_mtl_easu_pso && g_mtl_rcas_pso && g_mtl_yuv2rgb_pso) return 1;
    if (!g_fsr_shader_source) { goMetalLog("metal fsr: no shader source set (metal_fsr_set_shader_source never called?)", 2); return 0; }

    if (!g_mtl_device) {
        g_mtl_device = MTLCreateSystemDefaultDevice();
        if (!g_mtl_device) { goMetalLog("metal fsr: MTLCreateSystemDefaultDevice failed", 2); return 0; }
    }
    if (!g_mtl_queue) {
        g_mtl_queue = [g_mtl_device newCommandQueue];
        if (!g_mtl_queue) { goMetalLog("metal fsr: newCommandQueue failed", 2); return 0; }
    }
    if (!g_mtl_tex_cache) {
        CVReturn cvr = CVMetalTextureCacheCreate(kCFAllocatorDefault, NULL, g_mtl_device, NULL, &g_mtl_tex_cache);
        if (cvr != kCVReturnSuccess || !g_mtl_tex_cache) {
            char msg[64]; snprintf(msg, sizeof(msg), "metal fsr: CVMetalTextureCacheCreate failed (%d)", cvr);
            goMetalLog(msg, 2);
            return 0;
        }
    }

    NSError *err = nil;
    id<MTLLibrary> lib = [g_mtl_device newLibraryWithSource:g_fsr_shader_source options:nil error:&err];
    if (!lib) {
        NSString *m = [NSString stringWithFormat:@"metal fsr: shader compile failed: %@", err];
        goMetalLog((char *)m.UTF8String, 2);
        return 0;
    }

    struct { NSString *name; id<MTLComputePipelineState> __strong *slot; } kernels[] = {
        {@"resample_bilinear", &g_mtl_bilinear_pso},
        {@"resample_bicubic",  &g_mtl_bicubic_pso},
        {@"resample_lanczos",  &g_mtl_lanczos_pso},
        {@"fsr_easu",          &g_mtl_easu_pso},
        {@"fsr_rcas",          &g_mtl_rcas_pso},
        {@"yuv2020_to_rgb",    &g_mtl_yuv2rgb_pso},
    };
    for (size_t i = 0; i < sizeof(kernels)/sizeof(kernels[0]); i++) {
        id<MTLFunction> fn = [lib newFunctionWithName:kernels[i].name];
        if (!fn) {
            NSString *m = [NSString stringWithFormat:@"metal fsr: newFunctionWithName(%@) failed", kernels[i].name];
            goMetalLog((char *)m.UTF8String, 2);
            return 0;
        }
        id<MTLComputePipelineState> pso = [g_mtl_device newComputePipelineStateWithFunction:fn error:&err];
        if (!pso) {
            NSString *m = [NSString stringWithFormat:@"metal fsr: pipeline state (%@) failed: %@", kernels[i].name, err];
            goMetalLog((char *)m.UTF8String, 2);
            return 0;
        }
        *kernels[i].slot = pso;
    }

    goMetalLog("metal fsr: pipeline ready", 0);
    return 1;
}

// Standard "aspect fit, centered" rect -- same visual result as the plain
// video layer's kCAGravityResizeAspect, computed by hand because a
// CAMetalLayer's drawable has no gravity/contents-scaling equivalent: the
// compute kernel fills the ENTIRE drawable 1:1, so the letterboxing has to
// happen by sizing/positioning the layer itself to this rect instead.
static CGRect metal_spike_aspect_fit_rect(CGRect container, CGFloat contentW, CGFloat contentH) {
    if (contentW <= 0 || contentH <= 0 || container.size.width <= 0 || container.size.height <= 0) {
        return container;
    }
    CGFloat containerAspect = container.size.width / container.size.height;
    CGFloat contentAspect   = contentW / contentH;
    CGFloat w, h;
    if (contentAspect > containerAspect) {
        w = container.size.width;
        h = w / contentAspect;
    } else {
        h = container.size.height;
        w = h * contentAspect;
    }
    CGFloat x = container.origin.x + (container.size.width  - w) * 0.5;
    CGFloat y = container.origin.y + (container.size.height - h) * 0.5;
    return CGRectMake(x, y, w, h);
}

// Builds FSR1's EASU constants (Const0..3), matching ffx_fsr1.h's
// FsrEasuCon() exactly (full-viewport form -- inputViewport == inputSize,
// this app never renders into a sub-rect of a larger resource). Each
// float is bit-packed into the uint the GPU-side struct expects (see
// fsr_metal.metal's fsr_easu kernel, which un-bitcasts them right back).
typedef struct { uint32_t x, y, z, w; } FsrUint4;
static void fsr_easu_con(FsrUint4 *con0, FsrUint4 *con1, FsrUint4 *con2, FsrUint4 *con3,
                          float inW, float inH, float outW, float outH) {
    union { float f; uint32_t u; } c;
#define PACK(v) (c.f = (v), c.u)
    con0->x = PACK(inW / outW);
    con0->y = PACK(inH / outH);
    con0->z = PACK(0.5f * inW / outW - 0.5f);
    con0->w = PACK(0.5f * inH / outH - 0.5f);

    con1->x = PACK(1.0f / inW);
    con1->y = PACK(1.0f / inH);
    con1->z = PACK( 1.0f / inW);
    con1->w = PACK(-1.0f / inH);

    con2->x = PACK(-1.0f / inW);
    con2->y = PACK( 2.0f / inH);
    con2->z = PACK( 1.0f / inW);
    con2->w = PACK( 2.0f / inH);

    con3->x = PACK(0.0f / inW);
    con3->y = PACK(4.0f / inH);
    con3->z = 0;
    con3->w = 0;
#undef PACK
}

// FsrRcasCon(): sharpness is given in stops (0 = max sharpness); transform
// to a linear value CPU-side, exactly like ffx_fsr1.h does, so the GPU
// kernel just un-bitcasts a single already-transformed float.
static uint32_t fsr_rcas_con(float sharpnessStops) {
    union { float f; uint32_t u; } c;
    c.f = exp2f(-sharpnessStops);
    return c.u;
}

// Renders one CVPixelBuffer (SDR BGRA, or HDR 10-bit 4:2:0 biplanar YCbCr)
// through the active upscale mode onto g_metal_layer, sized/positioned to
// aspect-fit `container`. Returns 1 on success (caller should hide g_layer
// and show g_metal_layer), 0 on any failure (caller should fall back to the
// normal g_layer path for this frame rather than showing a stale/blank
// drawable). FSR1 only makes sense when actually upscaling (EASU is
// documented for 1x-4x area scaling); when the output isn't larger than the
// input, this silently runs the bilinear kernel instead regardless of the
// requested mode.
static int metal_spike_render(CVPixelBufferRef buf, CGRect container) {
    if (!metal_fsr_ensure_pipeline() || !g_metal_layer) return 0;

    size_t srcW = CVPixelBufferGetWidth(buf), srcH = CVPixelBufferGetHeight(buf);
    if (srcW == 0 || srcH == 0) return 0;
    BOOL isHDR = (CVPixelBufferGetPixelFormatType(buf) == kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange);

    CGRect fit = metal_spike_aspect_fit_rect(container, (CGFloat)srcW, (CGFloat)srcH);
    CGFloat scale = NSScreen.mainScreen.backingScaleFactor;
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    g_metal_layer.frame = fit;
    g_metal_layer.contentsScale = scale;
    [CATransaction commit];
    MTLSize drawableSize = MTLSizeMake((NSUInteger)(fit.size.width * scale), (NSUInteger)(fit.size.height * scale), 1);
    if (drawableSize.width == 0 || drawableSize.height == 0) return 0;
    g_metal_layer.drawableSize = CGSizeMake(drawableSize.width, drawableSize.height);

    // Tag the layer's colorspace to match what's being written into it this
    // frame -- kCGColorSpaceITUR_2100_PQ tells the OS compositor these RGB
    // values are PQ-encoded BT.2020 (see yuv2020_to_rgb's own doc comment:
    // this kernel does NOT linearize/tone-map, that stays the OS's job,
    // same as the pre-existing g_layer/Core-Animation path). Cheap to set
    // every frame; only actually costs anything on the rare frame the value
    // changes.
    // -1 (not 0/NO): forces the branch below to run on this function's very
    // first call regardless of whether that first frame is SDR or HDR. A
    // BOOL(NO) sentinel here would silently skip tagging entirely for any
    // session that's SDR from start to finish and never has an SDR<->HDR
    // transition (e.g. no HDR entitlement/display) -- g_metal_layer's
    // colorspace would then sit at its untagged default (nil) for the whole
    // session, which is exactly the washed-out/overexposed picture this was
    // meant to fix (see the sRGB branch's own comment below).
    static int sLastColorspaceWasHDR = -1; // not thread-shared: always called from the main thread (see displayLinkFired)
    if ((int)isHDR != sLastColorspaceWasHDR) {
        if (isHDR) {
            CGColorSpaceRef cs = CGColorSpaceCreateWithName(kCGColorSpaceITUR_2100_PQ);
            g_metal_layer.colorspace = cs;
            if (cs) CGColorSpaceRelease(cs);
        } else {
            // Explicit sRGB, NOT nil. g_metal_layer.pixelFormat is
            // MTLPixelFormatRGBA16Float (set once, unconditionally, for both
            // SDR and HDR -- see metal_video_create). For an 8-bit backing a
            // nil colorspace defaults to sRGB and this wouldn't matter, but
            // for a float16 backing nil defaults to *extended linear sRGB*
            // -- the compositor then treats these still-gamma-encoded
            // BGRA8Unorm-sampled values (see the non-HDR branch below: the
            // resample kernels write them straight through, no linearize/
            // delinearize step) as linear light, which washes the picture
            // out exactly like HDR/EDR being on with the OS-level toggle
            // off. Tagging it sRGB explicitly tells the compositor to apply
            // the normal gamma curve, matching g_layer's own untagged
            // (implicitly sRGB, 8-bit) behavior.
            CGColorSpaceRef cs = CGColorSpaceCreateWithName(kCGColorSpaceSRGB);
            g_metal_layer.colorspace = cs;
            if (cs) CGColorSpaceRelease(cs);
        }
        apply_dynamic_range(g_metal_layer, isHDR);
        sLastColorspaceWasHDR = (int)isHDR;
    }

    BOOL isUpscale = (drawableSize.width > srcW && drawableSize.height > srcH);
    BOOL useFsr1 = (metal_upscale_mode() == UPSCALE_MODE_FSR1) && isUpscale;

    id<CAMetalDrawable> drawable = [g_metal_layer nextDrawable];
    if (!drawable) return 0;

    id<MTLTexture> inTex = nil;
    CVMetalTextureRef cvTexA = NULL, cvTexB = NULL; // release both at the end, regardless of path

    if (isHDR) {
        size_t chromaW = (srcW + 1) / 2, chromaH = (srcH + 1) / 2;
        CVReturn cvrY = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, g_mtl_tex_cache, buf, NULL,
            MTLPixelFormatR16Unorm, srcW, srcH, 0, &cvTexA);
        CVReturn cvrC = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, g_mtl_tex_cache, buf, NULL,
            MTLPixelFormatRG16Unorm, chromaW, chromaH, 1, &cvTexB);
        if (cvrY != kCVReturnSuccess || !cvTexA || cvrC != kCVReturnSuccess || !cvTexB) {
            if (cvTexA) CFRelease(cvTexA);
            if (cvTexB) CFRelease(cvTexB);
            return 0;
        }
        id<MTLTexture> yTex = CVMetalTextureGetTexture(cvTexA);
        id<MTLTexture> cbcrTex = CVMetalTextureGetTexture(cvTexB);
        if (!yTex || !cbcrTex) { CFRelease(cvTexA); CFRelease(cvTexB); return 0; }

        if (g_mtl_yuv_rgb_tex == nil || g_mtl_yuv_rgb_w != srcW || g_mtl_yuv_rgb_h != srcH) {
            MTLTextureDescriptor *desc = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA16Float
                                                                                              width:srcW height:srcH mipmapped:NO];
            desc.usage = MTLTextureUsageShaderRead | MTLTextureUsageShaderWrite;
            desc.storageMode = MTLStorageModePrivate;
            g_mtl_yuv_rgb_tex = [g_mtl_device newTextureWithDescriptor:desc];
            g_mtl_yuv_rgb_w = srcW;
            g_mtl_yuv_rgb_h = srcH;
        }
        if (!g_mtl_yuv_rgb_tex) { CFRelease(cvTexA); CFRelease(cvTexB); return 0; }

        id<MTLCommandBuffer> yuvCmd = [g_mtl_queue commandBuffer];
        id<MTLComputeCommandEncoder> yuvEnc = [yuvCmd computeCommandEncoder];
        [yuvEnc setComputePipelineState:g_mtl_yuv2rgb_pso];
        [yuvEnc setTexture:yTex atIndex:0];
        [yuvEnc setTexture:cbcrTex atIndex:1];
        [yuvEnc setTexture:g_mtl_yuv_rgb_tex atIndex:2];
        MTLSize yuvTgSize = MTLSizeMake(16, 16, 1);
        MTLSize yuvTgCount = MTLSizeMake((srcW + 15) / 16, (srcH + 15) / 16, 1);
        [yuvEnc dispatchThreadgroups:yuvTgCount threadsPerThreadgroup:yuvTgSize];
        [yuvEnc endEncoding];
        [yuvCmd commit];

        inTex = g_mtl_yuv_rgb_tex;
    } else {
        CVReturn cvr = CVMetalTextureCacheCreateTextureFromImage(
            kCFAllocatorDefault, g_mtl_tex_cache, buf, NULL,
            MTLPixelFormatBGRA8Unorm, srcW, srcH, 0, &cvTexA);
        if (cvr != kCVReturnSuccess || !cvTexA) return 0;
        inTex = CVMetalTextureGetTexture(cvTexA);
        if (!inTex) { CFRelease(cvTexA); return 0; }
    }

    id<MTLCommandBuffer> cmd = [g_mtl_queue commandBuffer];

    if (useFsr1) {
        if (g_mtl_easu_out_tex == nil || g_mtl_easu_out_w != drawableSize.width || g_mtl_easu_out_h != drawableSize.height) {
            MTLTextureDescriptor *desc = [MTLTextureDescriptor texture2DDescriptorWithPixelFormat:MTLPixelFormatRGBA16Float
                                                                                              width:drawableSize.width
                                                                                             height:drawableSize.height
                                                                                          mipmapped:NO];
            desc.usage = MTLTextureUsageShaderRead | MTLTextureUsageShaderWrite;
            desc.storageMode = MTLStorageModePrivate;
            g_mtl_easu_out_tex = [g_mtl_device newTextureWithDescriptor:desc];
            g_mtl_easu_out_w = drawableSize.width;
            g_mtl_easu_out_h = drawableSize.height;
        }
        if (!g_mtl_easu_out_tex) { CFRelease(cvTexA); if (cvTexB) CFRelease(cvTexB); return 0; }

        FsrUint4 con0, con1, con2, con3;
        fsr_easu_con(&con0, &con1, &con2, &con3, (float)srcW, (float)srcH, (float)drawableSize.width, (float)drawableSize.height);
        struct { FsrUint4 c0, c1, c2, c3; } easuCB = { con0, con1, con2, con3 };

        id<MTLComputeCommandEncoder> easuEnc = [cmd computeCommandEncoder];
        [easuEnc setComputePipelineState:g_mtl_easu_pso];
        [easuEnc setTexture:inTex atIndex:0];
        [easuEnc setTexture:g_mtl_easu_out_tex atIndex:1];
        [easuEnc setBytes:&easuCB length:sizeof(easuCB) atIndex:0];
        MTLSize tgSize = MTLSizeMake(16, 16, 1);
        MTLSize tgCount = MTLSizeMake((drawableSize.width + 15) / 16, (drawableSize.height + 15) / 16, 1);
        [easuEnc dispatchThreadgroups:tgCount threadsPerThreadgroup:tgSize];
        [easuEnc endEncoding];

        uint32_t rcasCon0x = fsr_rcas_con(0.0f); // 0 stops = max sharpness for now; UI-tunable later
        struct { uint32_t c0x, c0y, c0z, c0w; } rcasCB = { rcasCon0x, 0, 0, 0 };

        id<MTLComputeCommandEncoder> rcasEnc = [cmd computeCommandEncoder];
        [rcasEnc setComputePipelineState:g_mtl_rcas_pso];
        [rcasEnc setTexture:g_mtl_easu_out_tex atIndex:0];
        [rcasEnc setTexture:drawable.texture atIndex:1];
        [rcasEnc setBytes:&rcasCB length:sizeof(rcasCB) atIndex:0];
        [rcasEnc dispatchThreadgroups:tgCount threadsPerThreadgroup:tgSize];
        [rcasEnc endEncoding];
    } else {
        // Non-FSR modes: plain single-pass resample kernels. FSR1 falls
        // back here too when the drawable isn't actually larger than the
        // source (see isUpscale/useFsr1 above) -- always via bilinear in
        // that case, regardless of the requested mode, since a non-upscale
        // resize is rare (usually exact-fit) and not worth a second
        // pipeline-state branch.
        UpscaleMode mode = (metal_upscale_mode() == UPSCALE_MODE_FSR1) ? UPSCALE_MODE_BILINEAR : metal_upscale_mode();
        id<MTLComputePipelineState> pso = g_mtl_bilinear_pso;
        if (mode == UPSCALE_MODE_BICUBIC) pso = g_mtl_bicubic_pso;
        else if (mode == UPSCALE_MODE_LANCZOS) pso = g_mtl_lanczos_pso;

        id<MTLComputeCommandEncoder> enc = [cmd computeCommandEncoder];
        [enc setComputePipelineState:pso];
        [enc setTexture:inTex atIndex:0];
        [enc setTexture:drawable.texture atIndex:1];
        MTLSize tgSize = MTLSizeMake(16, 16, 1);
        MTLSize tgCount = MTLSizeMake((drawableSize.width + 15) / 16, (drawableSize.height + 15) / 16, 1);
        [enc dispatchThreadgroups:tgCount threadsPerThreadgroup:tgSize];
        [enc endEncoding];
    }

    [cmd presentDrawable:drawable];
    [cmd commit];

    // Safe to release now that the encoder(s) have recorded their reference
    // to inTex -- Metal internally retains resources used by a command
    // buffer until that buffer completes, same idiom as AVFoundation's own
    // CVMetalTextureCache sample code.
    CFRelease(cvTexA);
    if (cvTexB) CFRelease(cvTexB);

    // CVMetalTextureCache does not free its internal per-source-CVPixelBuffer
    // texture wrappers just because every CVMetalTextureRef handed out this
    // frame was released above -- Apple's own AVFoundation/Metal camera
    // sample code flushes the cache periodically for exactly this reason
    // (see CVMetalTextureCacheFlush's header doc: "call ... periodically to
    // allow released textures to be purged"). VT's decode pool recycles a
    // bounded set of IOSurfaces (24, see vt_create_session's
    // kCVPixelBufferPoolMinimumBufferCountKey), but each one is still a
    // logically distinct CVPixelBuffer per decode, so without this the cache
    // would otherwise accumulate one stale wrapper per unique buffer for as
    // long as any non-bilinear UpscaleMode session runs. Throttled to once a
    // second -- cheap either way, but no reason to pay it every frame.
    static double sLastFlush = 0.0;
    double nowFlush = mono_sec();
    if (nowFlush - sLastFlush >= 1.0) {
        CVMetalTextureCacheFlush(g_mtl_tex_cache, 0);
        sLastFlush = nowFlush;
    }
    return 1;
}

// g_hud_pending_* hold the most recently pushed-but-not-yet-applied HUD
// image; g_hud_dirty marks that g_hud_pending_* has something newer than
// what's currently on g_hud_layer.
//
// IMPORTANT: this is deliberately NOT applied via dispatch_async(main
// queue, ...) -- that was the original design and measurement showed it
// was the actual bug behind the "HUD sometimes freezes, sometimes crawls
// smoothly" report. Logged evidence (see the "HUD apply gap" diagnostic
// below): with net_graph.go pushing at a steady 10Hz, dispatch_async'd
// blocks onto dispatch_get_main_queue() were only actually being run by
// this app roughly once every ~1.6s -- consistently, for over a minute
// straight -- while displayLinkFired below kept firing and rendering
// video at 42-60Hz on the very same main thread the whole time. That's
// not "the main thread is busy" (rendering proves it wasn't); it's this
// app's run loop not draining GCD's main-queue source at anywhere near
// the rate it services the CADisplayLink's own run-loop-mode source.
// Whatever the exact reason, the fix is to stop asking GCD to reach the
// main thread at all for this and instead piggyback directly on
// displayLinkFired -- the one callback already proven to land on the main
// thread at a real, consistent cadence -- which also directly satisfies
// "update the HUD at the same rate as the stream renders."
static pthread_mutex_t g_hud_pending_mu = PTHREAD_MUTEX_INITIALIZER;
static NSData *g_hud_pending_data = nil;
static int g_hud_pending_w = 0, g_hud_pending_h = 0, g_hud_pending_stride = 0;
static atomic_int g_hud_dirty = 0;
static atomic_int g_hud_scale_pct = 100;

static float metal_hud_scale(void) {
    int pct = atomic_load(&g_hud_scale_pct);
    if (pct < 25) pct = 25;
    if (pct > 200) pct = 200;
    return (float)pct / 100.0f;
}

// Diagnostics for the "HUD sometimes freezes, sometimes crawls smoothly"
// report. Now that applies happen inline from displayLinkFired (already on
// the main thread, no dispatch involved), a large gap here would mean
// displayLinkFired itself stopped firing for a while -- a genuine stall,
// not the GCD-scheduling-latency issue this replaced. Logged via
// goMetalLog so it lands in app.log next to the existing "AppKit/
// DisplayLink stalled" warnings. Remove once the report is fully closed
// out.
static double g_hud_last_apply_time = 0.0;

// HUD_MARGIN: device points between the HUD box and the bottom/right edges
// of the view. (0,0) in this layer's (unflipped, AppKit-default) coordinate
// space is already the bottom-left corner, so the Y side needs no flip
// math -- only the X side has to subtract the HUD's own width from the
// container's width to anchor it to the right instead, done in
// metal_video_set_hud_overlay itself (it needs the image width anyway).
#define HUD_MARGIN 12.0

static volatile atomic_int g_active           = 0;

// Display link — drives rendering at the display refresh rate, decoupled from VT decode timing.
// CADisplayLink (macOS 14+) fires on the main thread directly; no extra dispatch needed.
// Stored as id to avoid pulling in CoreVideo/CVDisplayLink headers here.
static CADisplayLink *g_display_link = nil;

// Test-only instrumentation: counts every CADisplayLink actually created vs.
// invalidated, so a regression test (metal_video_leak_darwin_test.go) can prove
// metal_video_create's "replace" path -- called back-to-back without an
// intervening metal_video_destroy, e.g. video_widget_ui.go's frameNum==1
// codec-restart bootstrap hitting an already-active overlay -- never leaves
// a prior CADisplayLink running forever in the background. Always compiled
// in: two int64 increments per session/replace is free.
static _Atomic int64_t g_display_link_created_count     = 0;
static _Atomic int64_t g_display_link_invalidated_count = 0;

static pthread_mutex_t g_mu = PTHREAD_MUTEX_INITIALIZER;
static CVPixelBufferRef g_pendingBuf = NULL;
// Timestamp (mono_sec) of the metal_video_try_submit call that produced
// g_pendingBuf, captured under the same g_mu as the buffer itself. Read back
// out when the display link drains g_pendingBuf so metal_render_main_with_buf
// can report "how long did this frame sit between decode-submit and actual
// display" -- a stand-in for true per-frame decode latency (see
// metal_video_last_decode_ms's doc comment) that needs no extra plumbing
// through the VT decode callback.
static double g_pendingBufSubmitTime = 0.0;

static int64_t g_submitCount = 0;
static int64_t g_renderCount = 0;
static int64_t g_fpsFrames      = 0;
static double  g_fpsStart       = 0.0;
static double  g_lastKnownFps   = 0.0; // last computed value, returned during reset gap

// Decode/render-latency rolling window -- same shape as the fps window
// above (accumulate, snapshot+reset every 2s, cache last value during the
// reset gap), just averaging submit-to-display latency (ms) instead of a
// frame count. Fed by metal_render_main_with_buf's latencyMs argument.
static double  g_decodeMsSum    = 0.0;
static int64_t g_decodeSamples  = 0;
static double  g_lastKnownDecodeMs = 0.0;
static int     g_lastW = 0, g_lastH = 0;
static int     g_fullWindow  = 0; // 1 when overlay covers the full contentView (fullscreen mode)

// Last rendered pixel buffer — retained for pause snapshot (read by metal_video_get_last_frame_rgba).
static CVPixelBufferRef g_lastRenderedBuf = NULL;

// Desired EDR presentation state, set by metal_video_set_hdr -- persisted
// independently of g_layer's own lifetime (metal_video_create tears down
// and rebuilds a fresh CALayer on every new session) so a call that arrives
// before metal_video_create runs isn't silently lost: metal_video_create
// applies this value to the freshly-created layer itself, and
// metal_video_set_hdr applies it directly whenever g_layer already exists.
static atomic_int g_hdr_enabled = 0;

// Forward declaration -- defined further down (Net Graph HUD section),
// used by MetalDisplayLinkTarget's displayLinkFired above that definition.
static void metal_video_apply_pending_hud_overlay(void);

static double mono_sec(void) {
    struct timespec ts;
    clock_gettime(CLOCK_MONOTONIC, &ts);
    return (double)ts.tv_sec + (double)ts.tv_nsec * 1e-9;
}

// ─────────────────────────────────────────────────────────────────────────────
// Render — called on main thread; OWNS buf and always releases it.
// ─────────────────────────────────────────────────────────────────────────────
static void metal_render_main_with_buf(CVPixelBufferRef buf, double latencyMs) {
    if (!atomic_load(&g_active) || !g_layer) {
        CVPixelBufferRelease(buf);
        return;
    }

    // Stutter Profiler: how long THIS call itself takes on the main thread.
    // Pinning CADisplayLink's preferredFrameRateRange didn't recover the
    // fire rate in the field (still ~23-29Hz with the pin in place), which
    // means the OS isn't throttling the link's own scheduling -- something
    // in this call (IOSurface import, metal_spike_render, or the
    // CATransaction commit below) is itself slow enough to push the next
    // tick out, which a fixed preferredFrameRateRange can't fix because it
    // only picks a target cadence, it can't make a slow handler return
    // faster. This brackets the same window the "AppKit/DisplayLink
    // stalled" check measures, so the two can be directly compared in the
    // log to tell "render call is slow" apart from "something else on the
    // main thread between calls is slow".
    double renderCallStart = mono_sec();

    int w = (int)CVPixelBufferGetWidth(buf);
    int h = (int)CVPixelBufferGetHeight(buf);

    // IOSurface path: CALayer compositor reads GPU memory directly — zero CPU copy.
    IOSurfaceRef surf = CVPixelBufferGetIOSurface(buf);

    // Only engages when a non-bilinear UpscaleMode is selected AND the frame
    // is one of the two formats metal_spike_render actually handles (SDR
    // BGRA, or HDR 10-bit 4:2:0 biplanar -- see moonlight_cgo_apple.go's
    // platform_set_video_format, the only two formats VideoToolbox ever
    // decodes to here). Any failure at any step (pipeline init, texture
    // import, no drawable) falls straight through to the untouched g_layer
    // path below, exactly as if the mode were still bilinear -- a failure
    // here must never blank the screen.
    OSType fmt = surf ? CVPixelBufferGetPixelFormatType(buf) : 0;
    int spikeRendered = 0;
    if (metal_upscale_mode() != UPSCALE_MODE_BILINEAR && surf &&
        (fmt == kCVPixelFormatType_32BGRA || fmt == kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange) &&
        g_view) {
        CGRect container = g_view.bounds;
        spikeRendered = metal_spike_render(buf, container);
    }

    // A decoded CVPixelBuffer means this session's frames don't go through
    // g_avsbdl (PyroWave, decoded by pyrowave_decode_darwin.m). That layer
    // is opaque black and sits above g_layer/g_metal_layer, so left visible
    // it hides every frame shown here. metal_video_create makes a fresh,
    // visible one each session, so H.264/H.265 sessions are unaffected.
    if (g_avsbdl && !g_avsbdl.hidden) g_avsbdl.hidden = YES;

    if (spikeRendered) {
        if (g_metal_layer.hidden) g_metal_layer.hidden = NO;
        if (!g_layer.hidden) g_layer.hidden = YES;
    } else {
        if (g_metal_layer && !g_metal_layer.hidden) g_metal_layer.hidden = YES;
        if (g_layer.hidden) g_layer.hidden = NO;
        if (surf) {
            [CATransaction begin];
            [CATransaction setDisableActions:YES];
            g_layer.contents = (__bridge id)surf;
            [CATransaction commit];
        }
    }

    // Save a retained copy for the pause snapshot (cheap retain; releases old).
    // Guarded by g_mu: metal_video_get_last_frame_rgba() reads g_lastRenderedBuf
    // from an arbitrary background goroutine (VideoWidget.clearVideo, via
    // getMetalLastFrame) with no thread-hopping of its own, concurrently with
    // this main-thread (CADisplayLink) write -- an unguarded read-then-retain
    // racing this reassign+release could retain/read a buffer mid-free
    // (use-after-free) the moment a stream stop/restart lands while frames
    // are still actively rendering, e.g. switching virtual monitor or codec
    // mid-stream.
    CVPixelBufferRetain(buf);
    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef old_last = g_lastRenderedBuf;
    g_lastRenderedBuf = buf;
    pthread_mutex_unlock(&g_mu);
    if (old_last) CVPixelBufferRelease(old_last);

    // Release the caller's ref (g_lastRenderedBuf holds its own).
    CVPixelBufferRelease(buf);

    double renderCallMs = (mono_sec() - renderCallStart) * 1000.0;
    if (renderCallMs > 20.0) {
        char msg[128];
        snprintf(msg, sizeof(msg),
                 "⚠️ [Profiler] Metal render call itself took %.1f ms (main-thread render stall)",
                 renderCallMs);
        goMetalLog(msg, 2); // warn
    }

    // ── Logging ──────────────────────────────────────────────────────────────
    int64_t n = ++g_renderCount;
    if (n == 1) {
        g_lastW = w; g_lastH = h;
        g_fpsStart  = mono_sec();
        g_fpsFrames = 0;
        char msg[128];
        snprintf(msg, sizeof(msg),
                 "first frame rendered — %dx%d  iosurface=%s",
                 w, h, surf ? "yes" : "no (fallback)");
        goMetalLog(msg, 0);
    }

    g_lastW = w; g_lastH = h;
    g_fpsFrames++;
    if (latencyMs >= 0.0) {
        g_decodeMsSum += latencyMs;
        g_decodeSamples++;
    }
    double now     = mono_sec();
    double elapsed = now - g_fpsStart;
    // Early snapshot after 30 frames to catch FPS issues quickly.
    if (n == 30 && g_fpsStart > 0.0) {
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "first 30 frames: fps=%.1f  submitted=%lld  size=%dx%d",
                 (double)g_fpsFrames / elapsed,
                 (long long)g_submitCount, g_lastW, g_lastH);
        goMetalLog(msg, 0);
    }
    if (elapsed >= 2.0 && g_fpsFrames > 0) {
        g_lastKnownFps = (double)g_fpsFrames / elapsed;
        if (g_decodeSamples > 0) {
            g_lastKnownDecodeMs = g_decodeMsSum / (double)g_decodeSamples;
        }
        char msg[192];
        snprintf(msg, sizeof(msg),
                 "fps=%.1f  rendered=%lld  submitted=%lld  size=%dx%d  decodeMs=%.1f",
                 g_lastKnownFps,
                 (long long)g_renderCount, (long long)g_submitCount,
                 g_lastW, g_lastH, g_lastKnownDecodeMs);
        goMetalLog(msg, 0);
        g_fpsStart      = now;
        g_fpsFrames     = 0;
        g_decodeMsSum   = 0.0;
        g_decodeSamples = 0;
    }
}

// ─────────────────────────────────────────────────────────────────────────────
// CADisplayLink target — fires on the main thread at the display refresh rate.
// Drains g_pendingBuf and renders directly; no extra dispatch needed.
// ─────────────────────────────────────────────────────────────────────────────
@interface MetalDisplayLinkTarget : NSObject
- (void)displayLinkFired:(CADisplayLink *)link;
@end
#include <mach/mach_time.h>

static uint64_t g_last_dl_time = 0;
static uint64_t g_last_submit_time = 0;

// TEMP DIAGNOSTIC (render-throughput regression investigation): counts every
// displayLinkFired call and every time it found a pending buffer, logged
// every ~2s -- distinguishes "CVDisplayLink itself isn't firing at the
// display refresh rate" from "it's firing fine but g_pendingBuf is usually
// empty/stale by the time it checks".
static uint64_t g_dl_fire_count = 0;
static uint64_t g_dl_hit_count = 0;
static double g_dl_diag_start = 0.0;

@implementation MetalDisplayLinkTarget
- (void)displayLinkFired:(CADisplayLink __unused *)link {
    if (!atomic_load(&g_active)) return;

    // Net Graph HUD: applied here, not via dispatch_async -- see
    // g_hud_dirty's doc comment for why. Checked first and unconditionally
    // (independent of whether this firing also has a new video frame
    // below), so the HUD updates at the display's own refresh cadence even
    // if, say, the video source itself is momentarily idle.
    if (atomic_exchange(&g_hud_dirty, 0)) {
        metal_video_apply_pending_hud_overlay();
    }

    // AVSampleBufferDisplayLayer health check: g_avsbdl can report
    // status=Rendering and a perfectly healthy submit fps while the LAYER
    // ITSELF is detached from the window (no superlayer) or has collapsed
    // to a zero-size frame -- decode succeeds independent of presentation
    // geometry, so neither condition trips the Failed-status recovery in
    // metal_video_submit_compressed_sample. Confirmed live: a rapid
    // benchmark-driven stop/switch-backend/start reconnect cycle left
    // exactly this state -- "AVSBDL: submit fps=60.0 status=1" logging
    // continuously while the actual screen stayed solid black. Checked
    // every tick (cheap: two property reads) and self-healed by
    // re-attaching/re-sizing rather than just logged, since by the time a
    // human notices the black screen the diagnostic window has long since
    // passed -- next time this fires, the fix should already be visible by
    // the time anyone looks.
    if (g_avsbdl && g_view) {
        BOOL detached = (g_avsbdl.superlayer == nil);
        BOOL collapsed = CGRectIsEmpty(g_avsbdl.frame);
        if (detached || collapsed) {
            char msg[160];
            snprintf(msg, sizeof(msg),
                     "AVSBDL: self-heal -- detached=%d collapsed=%d (frame=%.0fx%.0f) -- re-attaching",
                     detached, collapsed, g_avsbdl.frame.size.width, g_avsbdl.frame.size.height);
            goMetalLog(msg, 1); // warn
            if (detached) [g_view.layer addSublayer:g_avsbdl];
            g_avsbdl.frame = g_view.bounds;
        }
    }

    g_dl_fire_count++;

    // Stutter Profiler: DisplayLink stall detection, plus a gap histogram --
    // the 50ms-threshold warning below only catches a single dropped frame
    // outright; a sustained 60->45Hz degradation (what's actually reported)
    // is many *small* ~4-6ms-over-budget gaps, not occasional big ones, and
    // would never trip that threshold even once. Bucketed here so a 2s
    // window shows the actual shape of the jank instead of just an average.
    static uint64_t g_gap_bucket_ok = 0;    // <18ms: one healthy ~60Hz tick
    static uint64_t g_gap_bucket_1 = 0;     // 18-25ms: ~1 tick missed
    static uint64_t g_gap_bucket_2 = 0;     // 25-50ms: 2+ ticks missed
    uint64_t now = mach_absolute_time();
    if (g_last_dl_time != 0) {
        mach_timebase_info_data_t tb;
        mach_timebase_info(&tb);
        uint64_t elapsed_ns = (now - g_last_dl_time) * tb.numer / tb.denom;
        uint64_t elapsed_ms = elapsed_ns / 1000000;
        if (elapsed_ms > 50) {
            char msg[128];
            snprintf(msg, sizeof(msg), "⚠️ [Profiler] AppKit/DisplayLink stalled for %llu ms (UI freeze!)", (unsigned long long)elapsed_ms);
            goMetalLog(msg, 2); // warn
        } else if (elapsed_ms >= 25) {
            g_gap_bucket_2++;
        } else if (elapsed_ms >= 18) {
            g_gap_bucket_1++;
        } else {
            g_gap_bucket_ok++;
        }
    }
    g_last_dl_time = now;

    double diagNow = mono_sec();
    if (g_dl_diag_start == 0.0) g_dl_diag_start = diagNow;
    double diagElapsed = diagNow - g_dl_diag_start;
    if (diagElapsed >= 2.0) {
        char diagMsg[220];
        snprintf(diagMsg, sizeof(diagMsg),
                 "[DIAG] DisplayLink fire_rate=%.1fHz hit_rate=%.1fHz (fires=%llu hits=%llu window=%.1fs) gaps: ok=%llu 1tick(18-25ms)=%llu 2tick(25-50ms)=%llu",
                 (double)g_dl_fire_count / diagElapsed, (double)g_dl_hit_count / diagElapsed,
                 (unsigned long long)g_dl_fire_count, (unsigned long long)g_dl_hit_count, diagElapsed,
                 (unsigned long long)g_gap_bucket_ok, (unsigned long long)g_gap_bucket_1, (unsigned long long)g_gap_bucket_2);
        goMetalLog(diagMsg, 0);
        g_dl_fire_count = 0;
        g_dl_hit_count = 0;
        g_dl_diag_start = diagNow;
        g_gap_bucket_ok = 0; g_gap_bucket_1 = 0; g_gap_bucket_2 = 0;
    }

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef buf = g_pendingBuf;
    double submitTime = g_pendingBufSubmitTime;
    g_pendingBuf = NULL;
    pthread_mutex_unlock(&g_mu);
    if (!buf) return;
    g_dl_hit_count++;
    double latencyMs = submitTime > 0.0 ? (mono_sec() - submitTime) * 1000.0 : -1.0;
    metal_render_main_with_buf(buf, latencyMs); // already on main thread
}
@end
static MetalDisplayLinkTarget *g_dl_target = nil;

// ─────────────────────────────────────────────────────────────────────────────
// Public C API
// ─────────────────────────────────────────────────────────────────────────────

int metal_video_is_active(void) {
    return atomic_load(&g_active);
}

// Running count of frames actually presented (g_renderCount, incremented in
// metal_render_main_with_buf on the main thread) -- the streamer
// benchmark's client-side render-fps counter (see bench_recorder.go's
// benchRenderedFramesFn) diffs this between ticks instead of relying on
// metal_video_last_fps's own 2s window, same as the Linux/Windows
// VKVideoGetStats().Rendered equivalent. Read cross-thread without a lock,
// same tolerated race as g_submitCount above: a plain monotonic counter,
// never torn in practice on this platform's int64 alignment.
// Also incremented by metal_video_submit_compressed_sample further down
// (the AVSampleBufferDisplayLayer path) -- declared up here so
// metal_video_rendered_count below can read it; see that function's own
// doc comment for why the two counters are summed.
static _Atomic uint64_t g_avsbdl_submit_count = 0;
// g_avsbdl_fps_start/g_avsbdl_fps_frames/g_lastKnownAvsbdlFps: also declared
// up here (full definitions live with metal_video_submit_compressed_sample
// further down) so metal_video_last_fps above that function can read them
// too -- same forward-declare-for-an-earlier-reader reason as
// g_avsbdl_submit_count just above.
static _Atomic double   g_avsbdl_fps_start     = 0.0;
static _Atomic uint64_t g_avsbdl_fps_frames    = 0;
static _Atomic double   g_lastKnownAvsbdlFps   = 0.0;

int64_t metal_video_rendered_count(void) {
    // g_renderCount (the old CAMetalLayer/CADisplayLink path) stays 0 for
    // H.264/H.265 now that g_avsbdl handles that pipeline instead -- and
    // vice versa, g_avsbdl_submit_count stays 0 if that path is never
    // reached (e.g. PyroWave's own route, or iOS). Exactly one of the two
    // is live in a given build/session, so summing them is safe and keeps
    // this counter meaningful for both -- the streamer benchmark's
    // client-side render-fps stat (bench_recorder.go) reads this, and
    // reported a flat 0.0 fps for every AVSBDL-path run before this fix.
    return g_renderCount + (int64_t)atomic_load(&g_avsbdl_submit_count);
}

// Returns the Metal render FPS from the current measurement window.
// Falls back to the last known value during the brief reset gap so callers
// never see a spurious zero while frames are still being rendered.
//
// Checks the AVSBDL path first: g_fpsFrames/g_fpsStart (and therefore
// g_lastKnownFps) are only ever touched by metal_render_main_with_buf, the
// legacy CVPixelBuffer+CADisplayLink path -- which metal_video_rendered_count's
// own doc comment already established stays completely untouched for
// H.264/H.265 now that g_avsbdl handles that pipeline instead. Before this
// check, Net Graph's FPS line silently stayed frozen at 0 for the whole
// session on every AVSBDL-path stream (the common case on macOS) -- same
// bug class metal_video_rendered_count was fixed for, just never ported to
// this getter too.
double metal_video_last_fps(void) {
    if (!atomic_load(&g_active)) return 0.0;
    if (atomic_load(&g_avsbdl_submit_count) > 0) {
        double start = atomic_load(&g_avsbdl_fps_start);
        if (start == 0.0) return 0.0; // no frames at all yet
        double elapsed = mono_sec() - start;
        uint64_t frames = atomic_load(&g_avsbdl_fps_frames);
        if (frames == 0 || elapsed < 0.5) {
            return atomic_load(&g_lastKnownAvsbdlFps);
        }
        return (double)frames / elapsed;
    }
    if (g_fpsStart == 0.0) return 0.0; // no frames at all yet
    double elapsed = mono_sec() - g_fpsStart;
    if (g_fpsFrames == 0 || elapsed < 0.5) {
        // Just reset the window — return last known value to avoid a gap.
        return g_lastKnownFps;
    }
    return (double)g_fpsFrames / elapsed;
}

// Returns the rolling-average submit-to-display latency (ms) from the
// current measurement window (same window as metal_video_last_fps, see
// g_decodeMsSum's doc comment) -- a stand-in for true per-frame decode
// latency, cheap to compute since it needs no extra plumbing through the VT
// decode callback. Falls back to the last known value during the brief
// reset gap, same as metal_video_last_fps. 0 if no sample has ever landed
// (e.g. overlay inactive, or every submit happened before any pendingBuf
// timestamp was set).
//
// Meaningless on the AVSBDL path (see metal_video_decode_ms_available's doc
// comment for why there's no equivalent measurement there yet) -- callers
// MUST check that first, since this still returns g_lastKnownDecodeMs's
// frozen initial 0.0 in that case, not a real number.
double metal_video_last_decode_ms(void) {
    if (!atomic_load(&g_active)) return 0.0;
    return g_lastKnownDecodeMs;
}

// metal_video_decode_ms_available reports whether metal_video_last_decode_ms
// reflects a real measurement for the CURRENT session. The legacy
// CVPixelBuffer+CADisplayLink path (metal_render_main_with_buf) times
// submit-to-display directly; the newer AVSampleBufferDisplayLayer path
// (metal_video_submit_compressed_sample, now the main H.264/H.265 pipeline
// -- see g_avsbdl's doc comment) hands decode+presentation to AVFoundation
// internally with no per-sample completion callback this code hooks, so it
// has never fed g_decodeMsSum/g_lastKnownDecodeMs at all. Net Graph's DEC
// line used to show a flat, misleading "0.0ms" for the entire session on
// every AVSBDL-path stream (the common case) instead of admitting there's
// no measurement -- same root cause metal_video_last_fps was just fixed
// for, except there's no equivalent AVSBDL-side counter to fall back to
// here, since no timestamp is captured on that path at all.
//
// g_avsbdl_submit_count never resets mid-session (only at metal_video_create),
// and the two paths are mutually exclusive per session (see
// metal_video_rendered_count's doc comment), so count==0 reliably means
// "the legacy path is the one actually running."
int metal_video_decode_ms_available(void) {
    return atomic_load(&g_avsbdl_submit_count) == 0;
}

// metal_video_get_gpu_name copies the system default Metal device's name
// (e.g. "Apple M3 Pro") into out, NUL-terminated and truncated to out_len.
// Queried fresh rather than cached off g_mtl_device/ml.device (the FSR
// upscale and overlay layers' own devices, both only created once a stream
// starts) -- a plain MTLCreateSystemDefaultDevice() call is cheap and this
// way Net Graph's GPU line reads correctly even before the first stream of
// the session, same as macOS's automatic-graphics-switching Macs report
// whichever GPU is actually active right now. Empty if Metal is
// unavailable (never expected on a supported macOS version).
void metal_video_get_gpu_name(char *out, int out_len) {
    if (!out || out_len <= 0) return;
    out[0] = '\0';
    id<MTLDevice> dev = MTLCreateSystemDefaultDevice();
    if (!dev) return;
    const char *name = [dev.name UTF8String];
    if (name) snprintf(out, out_len, "%s", name);
}

// metal_video_submit_compressed_sample feeds one ready-to-decode
// CMSampleBuffer (built zero-copy in moonlight_cgo_apple.go's
// platform_dr_submit) to g_avsbdl -- see that global's own doc comment for
// why this replaces the VTDecompressionSession+CVPixelBuffer+CADisplayLink
// path below for the main H.264/H.265 video pipeline. Called from the
// DIRECT_SUBMIT thread (same thread that reads the video UDP socket), not
// the main thread -- intentional and the documented way to feed this layer
// from a real-time decode pipeline, same as official Moonlight does.
// Returns 1 if enqueued, 0 if dropped (overlay inactive/torn down -- caller
// treats this like any other dropped frame, not a fatal error), or 2 if the
// layer had failed and was just flushed to recover -- caller should request
// a fresh IDR (this sample was NOT enqueued) rather than just dropping it.
// (g_avsbdl_fps_start/g_avsbdl_fps_frames/g_lastKnownAvsbdlFps are declared
// up near g_avsbdl_submit_count, for the same earlier-reader reason.)

// metal_video_avsbdl_needs_fresh_idr: see g_avsbdl_needs_idr's own doc
// comment. Peeks (does not clear) the flag -- platform_dr_submit clears it
// itself, only once it actually has an IDR in hand to feed, via
// metal_video_avsbdl_clear_needs_idr below.
int metal_video_avsbdl_needs_fresh_idr(void) {
    return atomic_load(&g_avsbdl_needs_idr) != 0;
}
void metal_video_avsbdl_clear_needs_idr(void) {
    atomic_store(&g_avsbdl_needs_idr, 0);
}

// The layer's own enqueue/flush/status/error are deprecated since macOS 15
// in favour of the same calls on its sampleBufferRenderer (macOS 14+), which
// the layer's versions forward to. Older systems only have the layer's.
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
static AVQueuedSampleBufferRenderingStatus avsbdl_status(AVSampleBufferDisplayLayer *l) {
    if (@available(macOS 14.0, *)) return l.sampleBufferRenderer.status;
    return l.status;
}
static NSError *avsbdl_error(AVSampleBufferDisplayLayer *l) {
    if (@available(macOS 14.0, *)) return l.sampleBufferRenderer.error;
    return l.error;
}
static void avsbdl_flush(AVSampleBufferDisplayLayer *l) {
    if (@available(macOS 14.0, *)) [l.sampleBufferRenderer flush];
    else [l flush];
}
static void avsbdl_enqueue(AVSampleBufferDisplayLayer *l, CMSampleBufferRef sample) {
    if (@available(macOS 14.0, *)) [l.sampleBufferRenderer enqueueSampleBuffer:sample];
    else [l enqueueSampleBuffer:sample];
}
#pragma clang diagnostic pop

int metal_video_submit_compressed_sample(CMSampleBufferRef sample) {
    if (!atomic_load(&g_active) || !g_avsbdl) return 0;

    // Once AVSampleBufferDisplayLayer hits AVQueuedSampleBufferRenderingStatusFailed
    // (2), it silently ignores every further enqueueSampleBuffer: call -- no
    // crash, no exception, the frame just never appears -- until -flush is
    // called to reset it back to a working state (Apple's own documented
    // recovery for this status). Without this check, a single bad early
    // sample (e.g. a decode error on whatever happened to be the very first
    // frame) meant the screen stayed black for the rest of the session: every
    // later frame, including real IDRs, kept getting silently dropped here.
    // Confirmed live: exactly this (a report of "black screen for up to a
    // minute") with zero evidence in the log before this check existed.
    if (avsbdl_status(g_avsbdl) == AVQueuedSampleBufferRenderingStatusFailed) {
        NSError *err = avsbdl_error(g_avsbdl);
        char msg[160];
        snprintf(msg, sizeof(msg), "AVSBDL: status=Failed, flushing to recover (%s)",
                 err ? err.localizedDescription.UTF8String : "no error info");
        goMetalLog(msg, 1); // warn
        avsbdl_flush(g_avsbdl);
        // Don't also enqueue this sample: it's almost certainly not an IDR
        // (those are rare/periodic), so it'll just fail again immediately
        // post-flush. Tell the caller to request a fresh IDR instead of
        // waiting for whatever RFI/periodic-refresh cadence would otherwise
        // eventually send one -- recovers in roughly one round-trip instead
        // of up to several seconds.
        return 2;
    }

    avsbdl_enqueue(g_avsbdl, sample);

    if (atomic_fetch_add(&g_avsbdl_submit_count, 1) == 0) {
        char msg[96];
        snprintf(msg, sizeof(msg), "AVSBDL: first sample enqueued (status=%ld)", (long)avsbdl_status(g_avsbdl));
        goMetalLog(msg, 0);
    }
    double now = mono_sec();
    double start = atomic_load(&g_avsbdl_fps_start);
    if (start == 0.0) { atomic_store(&g_avsbdl_fps_start, now); return 1; }
    atomic_fetch_add(&g_avsbdl_fps_frames, 1);
    double elapsed = now - start;
    if (elapsed >= 2.0) {
        uint64_t frames = atomic_exchange(&g_avsbdl_fps_frames, 0);
        atomic_store(&g_avsbdl_fps_start, now);
        double fps = (double)frames / elapsed;
        atomic_store(&g_lastKnownAvsbdlFps, fps);
        char msg[128];
        snprintf(msg, sizeof(msg), "AVSBDL: submit fps=%.1f (frames=%llu window=%.1fs) status=%ld",
                 fps, (unsigned long long)frames, elapsed, (long)avsbdl_status(g_avsbdl));
        goMetalLog(msg, 0);
    }
    return 1;
}

// One-shot diagnostic for the HDR black-screen investigation (2026-09-14):
// logs into app.log (unlike metal_video_impl_ios.m's NSLog-only equivalent,
// which never reaches it) exactly which of the two early-out checks below
// -- inactive overlay vs. no IOSurface -- is actually responsible when every
// frame silently drops on the CPU-fallback-rejects-10-bit path in
// moonlight_cgo_apple.go's vt_callback. Safe to leave in: fires once per
// process, not per frame.
static _Atomic int g_submit_call_count = 0;
int metal_video_try_submit(CVImageBufferRef img) {
    if (atomic_fetch_add(&g_submit_call_count, 1) == 0) {
        char msg[128];
        snprintf(msg, sizeof(msg), "metal_video_try_submit: first call (active=%d has_iosurface=%d)",
                 atomic_load(&g_active), CVPixelBufferGetIOSurface(img) != NULL ? 1 : 0);
        goMetalLog(msg, 0);
    }
    if (!atomic_load(&g_active)) return 0;
    if (!CVPixelBufferGetIOSurface(img)) return 0;

    // Stutter Profiler: Decoder stall detection
    uint64_t now = mach_absolute_time();
    if (g_last_submit_time != 0) {
        mach_timebase_info_data_t tb;
        mach_timebase_info(&tb);
        uint64_t elapsed_ns = (now - g_last_submit_time) * tb.numer / tb.denom;
        if (elapsed_ns > 50000000) { // 50ms
            char msg[128];
            snprintf(msg, sizeof(msg), "⚠️ [Profiler] Moonlight Decoder/Network stalled for %llu ms (Dropped packets or host keyframe!)", elapsed_ns / 1000000);
            goMetalLog(msg, 2); // warn
        }
    }
    g_last_submit_time = now;

    CVPixelBufferRetain(img);

    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef old = g_pendingBuf;
    g_pendingBuf = (CVPixelBufferRef)img;
    g_pendingBufSubmitTime = mono_sec();
    g_submitCount++;
    pthread_mutex_unlock(&g_mu);

    if (old) CVPixelBufferRelease(old);
    // CVDisplayLink drains g_pendingBuf at the display refresh rate — no dispatch here.
    return 1;
}

// ─────────────────────────────────────────────────────────────────────────────
// AI Vision overlay — composited as its own CALayer (see g_overlay_layer's
// doc comment) instead of touching pixels, since metal_video_try_submit's
// IOSurface path never produces a CPU-writable frame buffer to draw into.
// rgba must be straight/premultiplied-equivalent RGBA (alpha 0 or 255 only,
// which both are the same thing) at stride*h bytes -- exactly what
// image.RGBA produces, see ai_vision.go's pushAIVisionOverlayToMetal.
// ─────────────────────────────────────────────────────────────────────────────

void metal_video_set_overlay(const uint8_t *rgba, int w, int h, int stride) {
    if (!atomic_load(&g_active) || !rgba || w <= 0 || h <= 0 || stride <= 0) return;

    // Copy now (NSData:dataWithBytes: copies) — the caller's buffer is a Go
    // slice only valid for the duration of this call.
    NSData *data = [NSData dataWithBytes:rgba length:(size_t)stride * (size_t)h];

    dispatch_block_t blk = ^{
        if (!g_overlay_layer) return;
        CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
        CGDataProviderRef provider = CGDataProviderCreateWithCFData((CFDataRef)data);
        CGImageRef img = CGImageCreate((size_t)w, (size_t)h, 8, 32, (size_t)stride, cs,
            kCGImageAlphaPremultipliedLast | kCGBitmapByteOrderDefault,
            provider, NULL, false, kCGRenderingIntentDefault);
        CGDataProviderRelease(provider);
        CGColorSpaceRelease(cs);
        if (!img) return;

        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_overlay_layer.contents = (__bridge id)img; // CALayer retains it internally
        [CATransaction commit];
        CGImageRelease(img);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_clear_overlay(void) {
    dispatch_block_t blk = ^{
        if (g_overlay_layer) g_overlay_layer.contents = nil;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// ─────────────────────────────────────────────────────────────────────────────
// Net Graph HUD overlay — its own small CALayer (see g_hud_layer's doc
// comment), anchored bottom-right via a fixed frame set on every apply
// (cheap: w/h rarely change once the HUD is built once in Go). Applied
// from displayLinkFired, NOT dispatch_async -- see g_hud_dirty's own doc
// comment for why. See net_graph.go's buildNetGraphHUD for what actually
// gets uploaded here.
// ─────────────────────────────────────────────────────────────────────────────

// metal_video_apply_pending_hud_overlay must be called already on the main
// thread (displayLinkFired, or inline from metal_video_set_hud_overlay
// when that happens to already be on the main thread) -- it does no
// thread-hopping of its own. Applies whatever is CURRENTLY in
// g_hud_pending_*, not necessarily what was there when g_hud_dirty was set.
static void metal_video_apply_pending_hud_overlay(void) {
    double now = mono_sec();
    if (g_hud_last_apply_time > 0.0) {
        double gapMs = (now - g_hud_last_apply_time) * 1000.0;
        // net_graph.go ticks every 100ms; this now runs straight out of
        // displayLinkFired, so a large gap here means THAT stopped firing
        // for a while (a genuine stall), not a GCD scheduling artifact.
        if (gapMs > 250.0) {
            char msg[160];
            snprintf(msg, sizeof(msg), "⚠️ [Net Graph] HUD apply gap %.0fms (expected ~100ms)", gapMs);
            goMetalLog(msg, 1);
        }
    }
    g_hud_last_apply_time = now;

    pthread_mutex_lock(&g_hud_pending_mu);
    NSData *data = g_hud_pending_data;
    int w = g_hud_pending_w, h = g_hud_pending_h, stride = g_hud_pending_stride;
    pthread_mutex_unlock(&g_hud_pending_mu);

    if (!g_hud_layer || !data) return;

    CGColorSpaceRef cs = CGColorSpaceCreateDeviceRGB();
    CGDataProviderRef provider = CGDataProviderCreateWithCFData((CFDataRef)data);
    CGImageRef img = CGImageCreate((size_t)w, (size_t)h, 8, 32, (size_t)stride, cs,
        kCGImageAlphaPremultipliedLast | kCGBitmapByteOrderDefault,
        provider, NULL, false, kCGRenderingIntentDefault);
    CGDataProviderRelease(provider);
    CGColorSpaceRelease(cs);
    if (!img) return;

    // Bottom-right anchor: (0,0) is already the bottom-left corner in this
    // unflipped coordinate space (see HUD_MARGIN's doc comment), so only X
    // needs adjusting -- push the box's right edge in from the container's
    // own right edge by HUD_MARGIN.
    CGFloat containerW = g_hud_layer.superlayer ? g_hud_layer.superlayer.bounds.size.width : (CGFloat)w;
    float scale = metal_hud_scale();
    CGFloat dw = (CGFloat)w * scale;
    CGFloat dh = (CGFloat)h * scale;
    CGFloat x = containerW - HUD_MARGIN - dw;
    if (x < HUD_MARGIN) x = HUD_MARGIN; // clamp: a window narrower than the HUD pins it left instead of going negative

    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    g_hud_layer.frame = CGRectMake(x, HUD_MARGIN, dw, dh);
    g_hud_layer.contents = (__bridge id)img;
    [CATransaction commit];
    CGImageRelease(img);
}

// metal_video_set_hud_overlay is called from net_graph.go's Go goroutine
// (a background thread) at 10Hz. It ONLY stores the pixels and flags them
// dirty -- see g_hud_dirty's doc comment for why this deliberately does
// NOT dispatch/queue anything to reach the main thread itself.
// displayLinkFired picks this up on its own next firing.
void metal_video_set_hud_overlay(const uint8_t *rgba, int w, int h, int stride) {
    if (!atomic_load(&g_active) || !rgba || w <= 0 || h <= 0 || stride <= 0) return;

    NSData *data = [NSData dataWithBytes:rgba length:(size_t)stride * (size_t)h];

    pthread_mutex_lock(&g_hud_pending_mu);
    g_hud_pending_data = data;
    g_hud_pending_w = w;
    g_hud_pending_h = h;
    g_hud_pending_stride = stride;
    pthread_mutex_unlock(&g_hud_pending_mu);

    atomic_store(&g_hud_dirty, 1);

    // displayLinkFired (main thread) is the normal path and will pick this
    // up on its own next firing -- but if we happen to already BE the main
    // thread (e.g. a future caller), apply immediately instead of waiting
    // an extra tick for no reason.
    if ([NSThread isMainThread] && atomic_exchange(&g_hud_dirty, 0)) {
        metal_video_apply_pending_hud_overlay();
    }
}

void metal_video_clear_hud_overlay(void) {
    dispatch_block_t blk = ^{
        if (g_hud_layer) g_hud_layer.contents = nil;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_set_hud_scale(float s) {
    if (s < 0.25f) s = 0.25f;
    if (s > 2.0f) s = 2.0f;
    atomic_store(&g_hud_scale_pct, (int)(s * 100.0f + 0.5f));
    dispatch_block_t blk = ^{
        if (!g_hud_layer) return;
        pthread_mutex_lock(&g_hud_pending_mu);
        int w = g_hud_pending_w, h = g_hud_pending_h;
        pthread_mutex_unlock(&g_hud_pending_mu);
        if (w <= 0 || h <= 0) return;
        float scale = metal_hud_scale();
        CGFloat dw = (CGFloat)w * scale;
        CGFloat dh = (CGFloat)h * scale;
        CGFloat containerW = g_hud_layer.superlayer ? g_hud_layer.superlayer.bounds.size.width : dw;
        CGFloat x = containerW - HUD_MARGIN - dw;
        if (x < HUD_MARGIN) x = HUD_MARGIN;
        [CATransaction begin];
        [CATransaction setDisableActions:YES];
        g_hud_layer.frame = CGRectMake(x, HUD_MARGIN, dw, dh);
        [CATransaction commit];
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// Pointer-event sink, registered by internal/gui/controller (its
// goMetalMouseEvent export) through metal_video_set_mouse_callback. A
// pointer, not a direct reference to that export, so binaries that link
// this package without the controller (tests, cmd tools) still link.
typedef void (*metal_mouse_cb)(int typ, float x, float y, int btn);
static _Atomic(metal_mouse_cb) g_mouse_cb = NULL;

void metal_video_set_mouse_callback(metal_mouse_cb cb) { atomic_store(&g_mouse_cb, cb); }

static void metal_push_mouse(int typ, float x, float y, int btn) {
    metal_mouse_cb cb = atomic_load(&g_mouse_cb);
    if (cb) cb(typ, x, y, btn);
}

// Menu holes: rounded rects (view points, top-left origin like Fyne; r is
// the corner radius) cut out of g_view so Fyne menus drawn by the GLFW
// content view underneath show through while the rest of the video keeps
// playing. The mask follows the cards' rounded corners, so video fills the
// corners a menu card leaves bare. Main thread only.
#define METAL_MAX_HOLES 64
static CGRect  g_holes[METAL_MAX_HOLES];
static CGFloat g_hole_radius[METAL_MAX_HOLES];
static int     g_hole_count = 0;

static int metal_point_in_hole(NSView *v, NSPoint pt) {
    if (g_hole_count == 0) return 0;
    CGPoint p = CGPointMake(pt.x, v.bounds.size.height - pt.y);
    for (int i = 0; i < g_hole_count; i++)
        if (CGRectContainsPoint(g_holes[i], p)) return 1;
    return 0;
}

// metal_apply_holes rebuilds g_view's mask from g_holes against its current
// bounds; no holes removes the mask.
static void metal_apply_holes(void) {
    if (!g_view) return;
    CALayer *root = g_view.layer;
    if (!root) return;
    [CATransaction begin];
    [CATransaction setDisableActions:YES];
    if (g_hole_count == 0) {
        root.mask = nil;
    } else {
        CGRect b = root.bounds;
        CGMutablePathRef cut = CGPathCreateMutable();
        for (int i = 0; i < g_hole_count; i++) {
            // Fyne's top-left rect -> the layer's bottom-left geometry.
            CGRect r = g_holes[i];
            r.origin.y = b.size.height - r.origin.y - r.size.height;
            CGFloat rad = g_hole_radius[i];
            rad = MIN(rad, MIN(r.size.width, r.size.height) / 2);
            if (rad > 0) CGPathAddRoundedRect(cut, NULL, r, rad, rad);
            else         CGPathAddRect(cut, NULL, r);
        }
        CAShapeLayer *mask = [root.mask isKindOfClass:[CAShapeLayer class]]
                                 ? (CAShapeLayer *)root.mask : [CAShapeLayer layer];
        mask.frame = b;
        mask.fillColor = CGColorGetConstantColor(kCGColorBlack);
        CGPathRef shape = NULL;
        if (@available(macOS 13.0, *)) {
            // Non-zero subtraction: overlapping holes stay cut out.
            CGPathRef full = CGPathCreateWithRect(b, NULL);
            shape = CGPathCreateCopyBySubtractingPath(full, cut, false);
            CGPathRelease(full);
            mask.fillRule = kCAFillRuleNonZero;
        }
        if (!shape) {
            CGMutablePathRef eo = CGPathCreateMutable();
            CGPathAddRect(eo, NULL, b);
            CGPathAddPath(eo, NULL, cut);
            shape = eo;
            mask.fillRule = kCAFillRuleEvenOdd;
        }
        mask.path = shape;
        CGPathRelease(shape);
        root.mask = mask;
    }
    [CATransaction commit];
}

// metal_video_set_holes replaces the hole list; xywhr holds n holes as
// x,y,w,h,radius quintuples in view points. n == 0 clears every hole.
void metal_video_set_holes(const float *xywhr, int n) {
    if (n < 0) n = 0;
    if (n > METAL_MAX_HOLES) n = METAL_MAX_HOLES;
    NSData *d = [NSData dataWithBytes:xywhr length:(NSUInteger)n * 5 * sizeof(float)];
    dispatch_block_t blk = ^{
        const float *v = d.bytes;
        int k = 0;
        for (int i = 0; i < n; i++) {
            const float *h = v + i * 5;
            if (h[2] <= 0 || h[3] <= 0) continue;
            g_holes[k] = CGRectMake(h[0], h[1], h[2], h[3]);
            g_hole_radius[k] = h[4];
            k++;
        }
        if (k == 0 && g_hole_count == 0) return;
        g_hole_count = k;
        metal_apply_holes();
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// ─────────────────────────────────────────────────────────────────────────────
// USBridgeMetalView: captures all pointer events and sends them directly to Go.
// ─────────────────────────────────────────────────────────────────────────────
@interface USBridgeMetalView : NSView
@end
@implementation USBridgeMetalView

- (BOOL)acceptsFirstResponder { return YES; }
- (BOOL)acceptsFirstMouse:(NSEvent __unused *)event { return YES; }

// Clicks inside a menu hole belong to the Fyne view underneath.
- (NSView *)hitTest:(NSPoint)p {
    NSView *hit = [super hitTest:p];
    if (hit == self && metal_point_in_hole(self, [self convertPoint:p fromView:self.superview]))
        return nil;
    return hit;
}

// The mask is cut against the bounds; re-cut it whenever they change.
- (void)setFrameSize:(NSSize)size {
    [super setFrameSize:size];
    if (self == g_view && g_hole_count > 0) metal_apply_holes();
}

// Tracking area: deliver mouseMoved: (and drag events) even without a held button.
- (void)updateTrackingAreas {
    [super updateTrackingAreas];
    for (NSTrackingArea *ta in self.trackingAreas.copy) [self removeTrackingArea:ta];
    NSTrackingAreaOptions opts = NSTrackingActiveInKeyWindow
                               | NSTrackingMouseMoved
                               | NSTrackingInVisibleRect;
    [self addTrackingArea:[[NSTrackingArea alloc]
        initWithRect:NSZeroRect options:opts owner:self userInfo:nil]];
}

// ── coordinate helper ──────────────────────────────────────────────────────
// NSView coordinates have origin at bottom-left; Fyne expects top-left.
- (void)pushMoveEvent:(NSEvent *)e {
    NSPoint pt = [self convertPoint:e.locationInWindow fromView:nil];
    metal_push_mouse(1, (float)pt.x, (float)(self.bounds.size.height - pt.y), 0);
}
- (void)pushButtonEvent:(int)type btn:(int)btn event:(NSEvent *)e {
    NSPoint pt = [self convertPoint:e.locationInWindow fromView:nil];
    metal_push_mouse(type, (float)pt.x, (float)(self.bounds.size.height - pt.y), btn);
}

// ── mouse movement (no button held) ───────────────────────────────────────
- (void)mouseMoved:(NSEvent *)e {
    // Hovering an open menu must not drive the remote cursor.
    if (metal_point_in_hole(self, [self convertPoint:e.locationInWindow fromView:nil])) return;
    [self pushMoveEvent:e];
}
// ── drag (button held) ────────────────────────────────────────────────────
- (void)mouseDragged:(NSEvent *)e     { [self pushMoveEvent:e]; }
- (void)rightMouseDragged:(NSEvent *)e{ [self pushMoveEvent:e]; }
- (void)otherMouseDragged:(NSEvent *)e{ [self pushMoveEvent:e]; }
// ── buttons ───────────────────────────────────────────────────────────────
- (void)mouseDown:(NSEvent *)e        { [self pushButtonEvent:2 btn:1 event:e]; }
- (void)mouseUp:(NSEvent *)e          { [self pushButtonEvent:3 btn:1 event:e]; }
- (void)rightMouseDown:(NSEvent *)e   { [self pushButtonEvent:2 btn:3 event:e]; }
- (void)rightMouseUp:(NSEvent *)e     { [self pushButtonEvent:3 btn:3 event:e]; }
- (void)otherMouseDown:(NSEvent *)e   { [self pushButtonEvent:2 btn:2 event:e]; }
- (void)otherMouseUp:(NSEvent *)e     { [self pushButtonEvent:3 btn:2 event:e]; }
// ── scroll wheel ──────────────────────────────────────────────────────────
- (void)scrollWheel:(NSEvent *)e {
    int btn = (e.scrollingDeltaY >= 0) ? 4 : 5; // 4=up 5=down
    NSPoint pt = [self convertPoint:e.locationInWindow fromView:nil];
    metal_push_mouse(2, (float)pt.x, (float)(self.bounds.size.height - pt.y), btn);
}

@end

static NSRect fyne_to_nsrect(CGFloat cvH, float x, float y, float w, float h) {
    return NSMakeRect((CGFloat)x, cvH - (CGFloat)y - (CGFloat)h,
                      (CGFloat)w, (CGFloat)h);
}

int metal_video_create(uintptr_t nsWinPtr, float x, float y, float w, float h) {
    if (!nsWinPtr) return 0;
    __block int ok = 0;
    dispatch_block_t blk = ^{
        NSWindow *win = (__bridge NSWindow *)((void *)nsWinPtr);
        NSView   *cv  = win.contentView;
        if (!cv) { goMetalLog("metal_video_create: no contentView", 2); return; }

        if (g_view) {
            [g_view removeFromSuperview];
            g_view  = nil;
            g_layer = nil;
            g_overlay_layer = nil;
            g_hud_layer = nil;
            g_metal_layer = nil;
            g_avsbdl = nil;
        }

        // Replace path: metal_video_create can be called again while a
        // previous overlay is still active (see video_widget_ui.go's
        // frameNum==1 bootstrap, which deliberately calls this as a
        // "safe no-op/replace" when a codec-restart's new session starts
        // before the old one's stopMetalVideo() ran). Without invalidating
        // the OLD display link here, it keeps firing forever -- it's a
        // strong ref held by NSRunLoop, not by g_display_link, so simply
        // overwriting the global below drops our only handle to it while it
        // keeps calling into g_dl_target's displayLinkFired: at full display
        // refresh rate alongside the new one. Confirmed via
        // TestMetalVideoCreateReplaceInvalidatesPriorDisplayLink: each
        // leaked link doubles (triples, ...) the per-frame render/diagnostic
        // work on the main thread, which is exactly the slow, compounding
        // memory/CPU growth and stutter reported over a long session with
        // multiple codec restarts.
        if (g_display_link) {
            [g_display_link invalidate];
            g_display_link = nil;
            atomic_fetch_add(&g_display_link_invalidated_count, 1);
        }

        g_fullWindow = (w <= 0 || h <= 0);
        CGFloat cvH = cv.bounds.size.height;
        NSRect frame = g_fullWindow ? cv.bounds
                                    : fyne_to_nsrect(cvH, x, y, w, h);

        USBridgeMetalView *ov = [[USBridgeMetalView alloc] initWithFrame:frame];
        ov.wantsLayer = YES;
        ov.layer.backgroundColor = CGColorGetConstantColor(kCGColorBlack);

        CALayer *vl = [CALayer layer];
        vl.frame = ov.bounds;
        vl.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        vl.contentsGravity = kCAGravityResizeAspect;
        vl.backgroundColor = CGColorGetConstantColor(kCGColorBlack);
        vl.contentsScale   = NSScreen.mainScreen.backingScaleFactor;
        // Applies whatever metal_video_set_hdr's most recent call requested
        // -- see g_hdr_enabled's own doc comment: this layer is rebuilt
        // fresh every session, so the desired state has to be re-applied
        // here rather than assumed to survive from a previous session's
        // (now-destroyed) layer.
        apply_dynamic_range(vl, atomic_load(&g_hdr_enabled) != 0);
        [ov.layer addSublayer:vl];

        // Upscale-pipeline video layer (g_metal_layer) -- sibling of vl,
        // hidden by default, shown instead of vl only when a non-bilinear
        // UpscaleMode is active (see metal_render_main_with_buf). Added at
        // the same sublayer depth as vl (both inserted before ol/hl below)
        // so whichever one is visible, the AI Vision/Net Graph overlays stay
        // on top of it. RGBA16Float (not BGRA8Unorm) so it can carry
        // PQ-encoded HDR values without 8-bit banding; colorspace/EDR mode
        // is set per-frame in metal_spike_render based on the source
        // format, not fixed here.
        CAMetalLayer *ml = [CAMetalLayer layer];
        ml.hidden = YES;
        ml.device = MTLCreateSystemDefaultDevice();
        ml.pixelFormat = MTLPixelFormatRGBA16Float;
        ml.framebufferOnly = NO; // compute kernel writes the drawable's texture directly
        ml.backgroundColor = CGColorGetConstantColor(kCGColorBlack);
        [ov.layer addSublayer:ml];

        // AVSampleBufferDisplayLayer -- the actual video presentation path
        // now (see g_avsbdl's own doc comment). Sibling of vl/ml at the same
        // depth, so AI Vision/Net Graph overlays (ol/hl below) stay on top
        // of it the same way they do for vl/ml.
        AVSampleBufferDisplayLayer *sl = [AVSampleBufferDisplayLayer new];
        sl.frame = ov.bounds;
        sl.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        sl.videoGravity = AVLayerVideoGravityResizeAspect;
        sl.backgroundColor = CGColorGetConstantColor(kCGColorBlack);
        sl.contentsScale = NSScreen.mainScreen.backingScaleFactor;
        [ov.layer addSublayer:sl];

        CALayer *ol = [CALayer layer];
        ol.frame = ov.bounds;
        ol.autoresizingMask = kCALayerWidthSizable | kCALayerHeightSizable;
        ol.contentsGravity = kCAGravityResizeAspect;
        ol.contentsScale   = NSScreen.mainScreen.backingScaleFactor;
        [ov.layer addSublayer:ol]; // above vl -> boxes render on top of video

        CALayer *hl = [CALayer layer];
        hl.contentsScale = NSScreen.mainScreen.backingScaleFactor;
        // No frame yet -- metal_video_set_hud_overlay sets it (bottom-left
        // anchored, HUD_MARGIN) the first time net_graph.go pushes an image;
        // no autoresizingMask either, since this is a fixed-size box, unlike
        // vl/ol which track the whole view.
        [ov.layer addSublayer:hl]; // above ol -> HUD renders on top of everything

        [cv addSubview:ov];

        g_view  = ov;
        g_hole_count = 0; // the Go side re-sends holes for the new view
        g_layer = vl;
        g_overlay_layer = ol;
        g_hud_layer = hl;
        g_metal_layer = ml;
        g_avsbdl = sl;
        atomic_store(&g_avsbdl_needs_idr, 1);
        // Per session, like g_renderCount below: a count left over from an
        // earlier H.264/H.265 session made metal_video_last_fps and
        // metal_video_decode_ms_available read this (idle) path for a
        // PyroWave one -- FPS 0 and DEC -- in the Net Graph.
        atomic_store(&g_avsbdl_submit_count, 0);
        atomic_store(&g_avsbdl_fps_start, 0.0);
        atomic_store(&g_avsbdl_fps_frames, 0);
        atomic_store(&g_lastKnownAvsbdlFps, 0.0);

        g_submitCount = 0; g_renderCount = 0;
        g_fpsFrames = 0;   g_fpsStart = 0;   g_lastKnownFps = 0.0;
        g_decodeMsSum = 0.0; g_decodeSamples = 0; g_lastKnownDecodeMs = 0.0;
        g_pendingBufSubmitTime = 0.0;
        atomic_store(&g_hud_dirty, 0);
        g_hud_last_apply_time = 0.0;
        pthread_mutex_lock(&g_hud_pending_mu);
        g_hud_pending_data = nil;
        pthread_mutex_unlock(&g_hud_pending_mu);
        g_lastW = 0;       g_lastH = 0;
        atomic_store(&g_submit_call_count, 0); // re-arm the one-shot try_submit diagnostic for this session
        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
        pthread_mutex_unlock(&g_mu);
        if (old) CVPixelBufferRelease(old);

        // Warm up the Metal upscale pipeline's shader compile HERE (main
        // thread, before any frame exists) rather than lazily on the first
        // frame that needs it -- see metal_fsr_ensure_pipeline's own doc
        // comment for the measured ~636ms stall this avoids. Also done from
        // metal_video_set_upscale_mode itself (covers the case where that's
        // called AFTER this one, e.g. the mode changes mid-session). No-op
        // (and cheap to call) when the mode is still the bilinear default.
        if (metal_upscale_mode() != UPSCALE_MODE_BILINEAR) metal_fsr_ensure_pipeline();

        atomic_store(&g_active, 1);

        // Start a CADisplayLink that fires on the main thread at the display refresh
        // rate (60/120 Hz). This decouples rendering from VT decode timing and gives
        // vsync-aligned frames regardless of network or decode jitter.
        if (!g_dl_target) g_dl_target = [MetalDisplayLinkTarget new];
        g_display_link = [ov displayLinkWithTarget:g_dl_target
                                          selector:@selector(displayLinkFired:)];
        atomic_fetch_add(&g_display_link_created_count, 1);

        // Pin min==max==preferred to the display's own refresh rate. Without
        // this, CADisplayLink's adaptive duty-cycle picks its own rate from
        // observed commit cadence -- and a long gap with nothing to present
        // (e.g. a slow host-switch leaving the stream frame-less for 2+
        // seconds, as seen with a Punktfunk backend switch) makes it latch
        // onto a low fire rate (observed: ~23Hz) that never climbs back to
        // 60Hz even once frames resume arriving at full rate. A fixed range
        // leaves the OS nothing to adapt, so it can't get stuck low again.
        CGFloat maxFPS = 60.0;
        if (ov.window && ov.window.screen) {
            NSInteger screenMax = ov.window.screen.maximumFramesPerSecond;
            if (screenMax > 0) maxFPS = (CGFloat)screenMax;
        }
        g_display_link.preferredFrameRateRange =
            (CAFrameRateRange){.minimum = (float)maxFPS, .maximum = (float)maxFPS, .preferred = (float)maxFPS};

        [g_display_link addToRunLoop:[NSRunLoop mainRunLoop]
                             forMode:NSRunLoopCommonModes];

        ok = 1;

        char msg[192];
        if (g_fullWindow) {
            snprintf(msg, sizeof(msg),
                     "overlay created (full-window %.0fx%.0f) NSWindow=%p",
                     cv.bounds.size.width, cv.bounds.size.height, (void*)nsWinPtr);
        } else {
            snprintf(msg, sizeof(msg),
                     "overlay created %.0fx%.0f at (%.0f,%.0f)  NSWindow=%p",
                     (double)w, (double)h, (double)x, (double)y, (void*)nsWinPtr);
        }
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_sync(dispatch_get_main_queue(), blk);
    return ok;
}

void metal_video_update_frame(float x, float y, float w, float h) {
    if (!atomic_load(&g_active) || !g_view) return;
    // Full-window (fullscreen) overlay is pinned to the contentView — don't resize it.
    if (g_fullWindow) return;
    dispatch_block_t blk = ^{
        if (!g_view) return;
        NSView *cv = g_view.superview;
        if (!cv) return;
        CGFloat cvH = cv.bounds.size.height;
        NSRect nr = fyne_to_nsrect(cvH, x, y, w, h);
        if (!NSEqualRects(g_view.frame, nr)) g_view.frame = nr;
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// metal_buffer_to_rgba converts a locked CVPixelBuffer to w*h RGBA: BGRA (what
// VideoToolbox decodes SDR H.26x to), or biplanar YCbCr -- 8-bit or 16-bit
// samples, 4:2:0 or 4:4:4, full or video range: the formats PyroWave
// (pyrowave_decode_darwin.m) and HEVC Main10 frames arrive in. The matrix
// follows the buffer's own YCbCr attachment (BT.2020 or BT.709); PQ is not
// tone mapped, it is a still for the pause screen. Returns 0 for any other
// format -- reading a planar buffer as BGRA through its base address walks
// off the end of the surface and crashed the client on stop.
static int metal_buffer_to_rgba(CVPixelBufferRef buf, int w, int h, uint8_t *rgba) {
    OSType fmt = CVPixelBufferGetPixelFormatType(buf);
    if (fmt == kCVPixelFormatType_32BGRA) {
        uint8_t *src = (uint8_t *)CVPixelBufferGetBaseAddress(buf);
        size_t srcStride = CVPixelBufferGetBytesPerRow(buf);
        if (!src || srcStride < (size_t)w * 4) return 0;
        // Swap B<->R to produce RGBA.
        for (int y = 0; y < h; y++) {
            uint8_t *s = src  + (size_t)y * srcStride;
            uint8_t *d = rgba + (size_t)y * (size_t)w * 4;
            for (int x = 0; x < w; x++, s += 4, d += 4) {
                d[0] = s[2]; d[1] = s[1]; d[2] = s[0]; d[3] = s[3];
            }
        }
        return 1;
    }

    int full;  // range
    int bps;   // bytes per sample
    int c444;
    switch (fmt) {
    case kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange:   full = 0; bps = 1; c444 = 0; break;
    case kCVPixelFormatType_420YpCbCr8BiPlanarFullRange:    full = 1; bps = 1; c444 = 0; break;
    case kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange:  full = 0; bps = 2; c444 = 0; break;
    case kCVPixelFormatType_420YpCbCr10BiPlanarFullRange:   full = 1; bps = 2; c444 = 0; break;
    case kCVPixelFormatType_444YpCbCr8BiPlanarVideoRange:   full = 0; bps = 1; c444 = 1; break;
    case kCVPixelFormatType_444YpCbCr8BiPlanarFullRange:    full = 1; bps = 1; c444 = 1; break;
    case kCVPixelFormatType_444YpCbCr10BiPlanarVideoRange:  full = 0; bps = 2; c444 = 1; break;
    case kCVPixelFormatType_444YpCbCr10BiPlanarFullRange:   full = 1; bps = 2; c444 = 1; break;
    default: return 0;
    }
    if (CVPixelBufferGetPlaneCount(buf) != 2) return 0;
    const uint8_t *yp = (const uint8_t *)CVPixelBufferGetBaseAddressOfPlane(buf, 0);
    const uint8_t *cp = (const uint8_t *)CVPixelBufferGetBaseAddressOfPlane(buf, 1);
    size_t ys = CVPixelBufferGetBytesPerRowOfPlane(buf, 0);
    size_t cs = CVPixelBufferGetBytesPerRowOfPlane(buf, 1);
    int cw = c444 ? w : (w + 1) / 2, ch = c444 ? h : (h + 1) / 2;
    if (!yp || !cp || ys < (size_t)w * bps || cs < (size_t)cw * 2 * bps ||
        (int)CVPixelBufferGetHeightOfPlane(buf, 1) < ch) return 0;

    // Kr/Kb of the matrix the frame is tagged with.
    float kr = 0.2126f, kb = 0.0722f;
    CFTypeRef m = CVBufferCopyAttachment(buf, kCVImageBufferYCbCrMatrixKey, NULL);
    if (m) {
        if (CFEqual(m, kCVImageBufferYCbCrMatrix_ITU_R_2020)) { kr = 0.2627f; kb = 0.0593f; }
        CFRelease(m);
    }
    const float kg = 1.0f - kr - kb;
    // Samples as 0..1, the 16-bit ones through their top byte's scale.
    const float ymul = full ? 1.0f : 255.0f / 219.0f, yoff = full ? 0.0f : 16.0f / 255.0f;
    const float cmul = full ? 1.0f : 255.0f / 224.0f;
    for (int y = 0; y < h; y++) {
        const uint8_t *yr = yp + (size_t)y * ys;
        const uint8_t *cr = cp + (size_t)(c444 ? y : y / 2) * cs;
        uint8_t *d = rgba + (size_t)y * (size_t)w * 4;
        for (int x = 0; x < w; x++, d += 4) {
            int cx = c444 ? x : x / 2;
            float Y, Cb, Cr;
            if (bps == 1) {
                Y  = yr[x] / 255.0f;
                Cb = cr[2 * cx] / 255.0f;
                Cr = cr[2 * cx + 1] / 255.0f;
            } else {
                const uint16_t *y16 = (const uint16_t *)yr, *c16 = (const uint16_t *)cr;
                Y  = y16[x] / 65535.0f;
                Cb = c16[2 * cx] / 65535.0f;
                Cr = c16[2 * cx + 1] / 65535.0f;
            }
            Y = (Y - yoff) * ymul;
            Cb = (Cb - 0.5f) * cmul;
            Cr = (Cr - 0.5f) * cmul;
            float r = Y + 2.0f * (1.0f - kr) * Cr;
            float b = Y + 2.0f * (1.0f - kb) * Cb;
            float g = (Y - kr * r - kb * b) / kg;
            #define PW_U8(v) ((uint8_t)lrintf(fminf(fmaxf((v), 0.0f), 1.0f) * 255.0f))
            d[0] = PW_U8(r); d[1] = PW_U8(g); d[2] = PW_U8(b); d[3] = 255;
            #undef PW_U8
        }
    }
    return 1;
}

// Copies the last rendered frame to a caller-owned RGBA buffer.
// Returns 1 on success; caller must free(*out) with free().
// Safe to call from any thread: the read-then-retain of g_lastRenderedBuf is
// guarded by g_mu against metal_render_main_with_buf's main-thread
// reassign+release of the same pointer (see that function's doc comment).
int metal_video_get_last_frame_rgba(int *outW, int *outH, uint8_t **out) {
    pthread_mutex_lock(&g_mu);
    CVPixelBufferRef buf = g_lastRenderedBuf;
    if (buf) CVPixelBufferRetain(buf);
    pthread_mutex_unlock(&g_mu);
    if (!buf) return 0;

    int w = (int)CVPixelBufferGetWidth(buf);
    int h = (int)CVPixelBufferGetHeight(buf);
    if (w <= 0 || h <= 0) { CVPixelBufferRelease(buf); return 0; }

    uint8_t *rgba = (uint8_t *)malloc((size_t)w * (size_t)h * 4);
    if (!rgba) {
        CVPixelBufferRelease(buf);
        return 0;
    }

    CVPixelBufferLockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
    int ok = metal_buffer_to_rgba(buf, w, h, rgba);
    CVPixelBufferUnlockBaseAddress(buf, kCVPixelBufferLock_ReadOnly);
    if (!ok) {
        free(rgba);
        CVPixelBufferRelease(buf);
        return 0;
    }
    CVPixelBufferRelease(buf);

    *outW = w; *outH = h; *out = rgba;
    return 1;
}

void metal_video_set_hidden(int hidden) {
    if (!atomic_load(&g_active)) return;
    dispatch_block_t blk = ^{
        if (g_view) g_view.hidden = (hidden != 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

// apply_dynamic_range sets a layer's EDR presentation mode via whichever API
// this OS actually has: `preferredDynamicRange` (macOS 26+) is the
// non-deprecated replacement for `wantsExtendedDynamicRangeContent`
// (deprecated in the same release, still the only option before it) -- see
// CALayer.h's own API_AVAILABLE/API_DEPRECATED annotations. This project's
// floor is macOS 14 (see this file's own CADisplayLink usage), well below
// 26, so the deprecated property is still the only thing that actually
// exists at runtime on most machines this ships to today; branching at
// runtime rather than picking one gets both "works everywhere this ships"
// and "no deprecated-API warning/behavior on the OS that already moved on".
static void apply_dynamic_range(CALayer *layer, BOOL hdr) {
    // @available only guards the runtime branch -- the compiler still needs
    // preferredDynamicRange/CADynamicRangeHigh/CADynamicRangeStandard
    // *declared* by the SDK doing the compiling, regardless of which OS
    // this ends up running on. A build machine on an Xcode/SDK that
    // predates macOS 26 doesn't have those declarations at all, so the
    // whole branch has to be compiled out (not just skipped at runtime) or
    // it fails with "undeclared identifier" there -- confirmed live.
#if defined(MAC_OS_VERSION_26_0) && __MAC_OS_X_VERSION_MAX_ALLOWED >= MAC_OS_VERSION_26_0
    if (@available(macOS 26.0, *)) {
        layer.preferredDynamicRange = hdr ? CADynamicRangeHigh : CADynamicRangeStandard;
    } else
#endif
    {
        // Deliberate: this is the guarded pre-26 fallback for a property
        // deprecated exactly at 26 -- there is no other API to use here on
        // an OS this old, so the deprecation warning is expected noise, not
        // a real "you should update this" signal.
        #pragma clang diagnostic push
        #pragma clang diagnostic ignored "-Wdeprecated-declarations"
        layer.wantsExtendedDynamicRangeContent = hdr;
        #pragma clang diagnostic pop
    }
}

// metal_video_set_hdr toggles g_layer's EDR (extended dynamic range)
// presentation mode -- called from moonlight_cgo_apple.go's
// platform_set_video_format the moment the negotiated codec is known (see
// that function's own comment), before the first frame ever reaches
// metal_render_main_with_buf. Deliberately just this one property, no
// custom shader/texture pipeline: the video layer here is a PLAIN CALayer
// (not CAMetalLayer, despite this file's name -- see
// metal_render_main_with_buf's own "IOSurface path" comment) whose
// `contents` is set directly to the decoded frame's IOSurface every frame;
// Core Animation's own compositor already does the accurate YUV(BT.2020,PQ)
// -> display conversion using the color primaries/transfer function/matrix
// VideoToolbox tagged onto that IOSurface from the stream's own signaled
// colorimetry (see rust-shine's video-encode::videotoolbox module, which
// sets those tags explicitly for an HDR session) -- letting the system do
// this rather than hand-rolling a PQ EOTF conversion in a shader is both
// the more correct (accurate, tested-by-Apple color science) and the
// faster (zero extra GPU work beyond what SDR frames already do) choice.
// See apply_dynamic_range's own doc comment for which actual CALayer
// property this ends up touching on a given OS version.
void metal_video_set_hdr(int enabled) {
    atomic_store(&g_hdr_enabled, enabled != 0);
    dispatch_block_t blk = ^{
        if (!g_layer) return; // metal_video_create (see its own comment) applies g_hdr_enabled once it exists
        BOOL want = (atomic_load(&g_hdr_enabled) != 0);
        apply_dynamic_range(g_layer, want);
        char msg[64];
        snprintf(msg, sizeof(msg), "HDR presentation %s", want ? "enabled" : "disabled");
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_async(dispatch_get_main_queue(), blk);
}

void metal_video_destroy(void) {
    if (!atomic_load(&g_active)) return;
    atomic_store(&g_active, 0);
    g_fullWindow = 0;

    // Invalidate the display link before teardown so no more callbacks fire.
    // -invalidate is safe from any thread and is idempotent.
    if (g_display_link) {
        [g_display_link invalidate];
        g_display_link = nil;
        atomic_fetch_add(&g_display_link_invalidated_count, 1);
    }

    dispatch_block_t blk = ^{
        if (g_view) {
            [g_view removeFromSuperview];
            g_view  = nil;
            g_layer = nil;
            g_overlay_layer = nil;
            g_hud_layer = nil;
            g_metal_layer = nil;
            g_avsbdl = nil;
        }
        g_hole_count = 0;
        atomic_store(&g_hud_dirty, 0);
        pthread_mutex_lock(&g_hud_pending_mu);
        g_hud_pending_data = nil;
        pthread_mutex_unlock(&g_hud_pending_mu);
        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old = g_pendingBuf;
        g_pendingBuf = NULL;
        g_pendingBufSubmitTime = 0.0;
        pthread_mutex_unlock(&g_mu);
        if (old) CVPixelBufferRelease(old);

        pthread_mutex_lock(&g_mu);
        CVPixelBufferRef old_last_render = g_lastRenderedBuf;
        g_lastRenderedBuf = NULL;
        pthread_mutex_unlock(&g_mu);
        if (old_last_render) CVPixelBufferRelease(old_last_render);

        char msg[192];
        snprintf(msg, sizeof(msg),
                 "overlay destroyed — rendered=%lld  submitted=%lld",
                 (long long)g_renderCount, (long long)g_submitCount);
        goMetalLog(msg, 0);
    };
    if ([NSThread isMainThread]) blk(); else dispatch_sync(dispatch_get_main_queue(), blk);
}

// metal_video_debug_link_counts exposes g_display_link_created_count /
// g_display_link_invalidated_count (see their own doc comment) for
// metal_video_leak_darwin_test.go's leak regression test. Test-only in practice,
// but harmless to leave callable in production builds -- two atomic loads.
void metal_video_debug_link_counts(int64_t *created, int64_t *invalidated) {
    if (created)     *created     = atomic_load(&g_display_link_created_count);
    if (invalidated) *invalidated = atomic_load(&g_display_link_invalidated_count);
}

#endif // !TARGET_OS_IPHONE

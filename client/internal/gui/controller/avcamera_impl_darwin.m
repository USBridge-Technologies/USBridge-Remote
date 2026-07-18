// Minimal AVFoundation webcam capture used by the desktop QR scanner on
// macOS. Replaces the previous gstreamer avfvideosrc pipeline
// (qr_camera_scanner.go, now Linux-only) so the macOS build no longer needs
// to link or bundle GStreamer at all — capture goes straight through
// AVFoundation, mirroring how qr_camera_scanner_windows.go/
// mfcamera_impl_windows.c goes straight through Media Foundation.
//
// Only ever one camera session is open at a time (the QR scan popup), so
// global state protected by a single mutex is enough — no handle table.

#include <TargetConditionals.h>
#if !TARGET_OS_IPHONE

#import <AVFoundation/AVFoundation.h>
#import <CoreVideo/CoreVideo.h>
#include <pthread.h>
#include <stdio.h>
#include <stdarg.h>
#include <string.h>

#include "avcamera_impl_darwin.h"

extern void goAVCameraLog(char *msg, int level);

static void av_log(int level, const char *fmt, ...) {
    char buf[512];
    va_list args;
    va_start(args, fmt);
    vsnprintf(buf, sizeof(buf), fmt, args);
    va_end(args);
    goAVCameraLog(buf, level);
}

@interface _USBAVCameraDelegate : NSObject <AVCaptureVideoDataOutputSampleBufferDelegate>
@end

static pthread_mutex_t g_lock = PTHREAD_MUTEX_INITIALIZER;
static AVCaptureSession *g_session = nil;
static _USBAVCameraDelegate *g_delegate = nil;
static dispatch_queue_t g_queue = NULL;
static dispatch_semaphore_t g_frameReady = NULL;

static uint8_t *g_latestFrame = NULL; // width*height*4, RGBA, protected by g_lock
static int g_width = 0;
static int g_height = 0;
static int g_ended = 0;

@implementation _USBAVCameraDelegate

- (void)captureOutput:(AVCaptureOutput *)output
didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
       fromConnection:(AVCaptureConnection *)connection {
    CVImageBufferRef pixelBuffer = CMSampleBufferGetImageBuffer(sampleBuffer);
    if (!pixelBuffer) {
        return;
    }

    CVPixelBufferLockBaseAddress(pixelBuffer, kCVPixelBufferLock_ReadOnly);

    size_t width = CVPixelBufferGetWidth(pixelBuffer);
    size_t height = CVPixelBufferGetHeight(pixelBuffer);
    size_t bytesPerRow = CVPixelBufferGetBytesPerRow(pixelBuffer);
    const uint8_t *base = (const uint8_t *)CVPixelBufferGetBaseAddress(pixelBuffer);

    pthread_mutex_lock(&g_lock);
    if (base && g_latestFrame != NULL && (int)width == g_width && (int)height == g_height) {
        // kCVPixelFormatType_32BGRA is BGRA in memory; convert to RGBA while copying.
        for (int y = 0; y < g_height; y++) {
            const uint8_t *srcRow = base + (size_t)y * bytesPerRow;
            uint8_t *dstRow = g_latestFrame + (size_t)y * g_width * 4;
            for (int x = 0; x < g_width; x++) {
                uint8_t b = srcRow[x * 4 + 0];
                uint8_t g = srcRow[x * 4 + 1];
                uint8_t r = srcRow[x * 4 + 2];
                dstRow[x * 4 + 0] = r;
                dstRow[x * 4 + 1] = g;
                dstRow[x * 4 + 2] = b;
                dstRow[x * 4 + 3] = 0xFF;
            }
        }
    }
    pthread_mutex_unlock(&g_lock);

    CVPixelBufferUnlockBaseAddress(pixelBuffer, kCVPixelBufferLock_ReadOnly);

    dispatch_semaphore_signal(g_frameReady);
}

@end

// av_camera_open requests camera authorization (blocking on the OS prompt if
// not yet determined), opens the default video capture device and
// negotiates a BGRA output close to (reqWidth x reqHeight) via the closest
// available session preset, letting AVFoundation's own scaler handle the
// rest. On success the actual negotiated size is written to
// outWidth/outHeight (may differ from the request if the device can't
// scale exactly).
int av_camera_open(int reqWidth, int reqHeight, int *outWidth, int *outHeight) {
    pthread_mutex_lock(&g_lock);
    if (g_session != nil) {
        *outWidth = g_width;
        *outHeight = g_height;
        pthread_mutex_unlock(&g_lock);
        return 1;
    }
    pthread_mutex_unlock(&g_lock);

    AVAuthorizationStatus authStatus = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
    if (authStatus == AVAuthorizationStatusNotDetermined) {
        dispatch_semaphore_t authSem = dispatch_semaphore_create(0);
        __block BOOL granted = NO;
        [AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL grantedResult) {
            granted = grantedResult;
            dispatch_semaphore_signal(authSem);
        }];
        dispatch_semaphore_wait(authSem, DISPATCH_TIME_FOREVER);
        authStatus = granted ? AVAuthorizationStatusAuthorized : AVAuthorizationStatusDenied;
    }
    if (authStatus != AVAuthorizationStatusAuthorized) {
        av_log(2, "Camera permission not granted (status=%ld)", (long)authStatus);
        return 0;
    }

    AVCaptureDevice *device = [AVCaptureDevice defaultDeviceWithMediaType:AVMediaTypeVideo];
    if (!device) {
        av_log(1, "No video capture device found");
        return 0;
    }

    NSError *error = nil;
    AVCaptureDeviceInput *input = [AVCaptureDeviceInput deviceInputWithDevice:device error:&error];
    if (!input) {
        av_log(2, "AVCaptureDeviceInput failed: %s", error.localizedDescription.UTF8String ?: "unknown error");
        return 0;
    }

    AVCaptureSession *session = [[AVCaptureSession alloc] init];
    if (![session canAddInput:input]) {
        av_log(2, "Session cannot add camera input");
        return 0;
    }
    [session addInput:input];

    AVCaptureVideoDataOutput *output = [[AVCaptureVideoDataOutput alloc] init];
    output.videoSettings = @{ (id)kCVPixelBufferPixelFormatTypeKey: @(kCVPixelFormatType_32BGRA) };
    output.alwaysDiscardsLateVideoFrames = YES;

    g_delegate = [[_USBAVCameraDelegate alloc] init];
    g_queue = dispatch_queue_create("com.usbridge.client.qrcamera", DISPATCH_QUEUE_SERIAL);
    [output setSampleBufferDelegate:g_delegate queue:g_queue];

    if (![session canAddOutput:output]) {
        av_log(2, "Session cannot add video data output");
        g_delegate = nil;
        g_queue = NULL;
        return 0;
    }
    [session addOutput:output];

    NSString *preset = AVCaptureSessionPresetMedium;
    if (reqWidth <= 640 && reqHeight <= 480 && [session canSetSessionPreset:AVCaptureSessionPreset640x480]) {
        preset = AVCaptureSessionPreset640x480;
    }
    if ([session canSetSessionPreset:preset]) {
        session.sessionPreset = preset;
    }

    [session startRunning];

    CMVideoDimensions dims = CMVideoFormatDescriptionGetDimensions(device.activeFormat.formatDescription);
    int width = dims.width > 0 ? (int)dims.width : reqWidth;
    int height = dims.height > 0 ? (int)dims.height : reqHeight;

    pthread_mutex_lock(&g_lock);
    g_width = width;
    g_height = height;
    free(g_latestFrame);
    g_latestFrame = (uint8_t *)calloc(1, (size_t)width * height * 4);
    g_ended = 0;
    pthread_mutex_unlock(&g_lock);

    if (!g_latestFrame) {
        av_log(2, "Failed to allocate frame buffer (%dx%d)", width, height);
        [session stopRunning];
        return 0;
    }

    g_frameReady = dispatch_semaphore_create(0);
    g_session = session;

    *outWidth = width;
    *outHeight = height;
    av_log(0, "Camera opened: %dx%d", width, height);
    return 1;
}

// av_camera_read_frame blocks (with a timeout) until the next frame is
// available, returning top-down packed RGBA in buf.
// Returns: 1 = frame written, 0 = end of stream / fatal error (caller must
// stop), 2 = no sample this call (caller should just call again).
int av_camera_read_frame(uint8_t *buf, int bufLen) {
    pthread_mutex_lock(&g_lock);
    int ended = g_ended;
    int width = g_width;
    int height = g_height;
    pthread_mutex_unlock(&g_lock);

    if (g_session == nil || ended) {
        return 0;
    }
    if (bufLen < width * height * 4) {
        av_log(1, "Frame buffer too small: bufLen=%d needed=%d", bufLen, width * height * 4);
        return 2;
    }

    dispatch_time_t timeout = dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.5 * NSEC_PER_SEC));
    if (dispatch_semaphore_wait(g_frameReady, timeout) != 0) {
        return 2; // no new frame within the timeout; try again
    }

    pthread_mutex_lock(&g_lock);
    if (g_latestFrame != NULL) {
        memcpy(buf, g_latestFrame, (size_t)width * height * 4);
    }
    pthread_mutex_unlock(&g_lock);
    return 1;
}

void av_camera_close(void) {
    pthread_mutex_lock(&g_lock);
    g_ended = 1;
    pthread_mutex_unlock(&g_lock);

    if (g_session != nil) {
        [g_session stopRunning];
        g_session = nil;
    }
    g_delegate = nil;
    g_queue = NULL;
    g_frameReady = NULL;

    pthread_mutex_lock(&g_lock);
    free(g_latestFrame);
    g_latestFrame = NULL;
    g_width = 0;
    g_height = 0;
    pthread_mutex_unlock(&g_lock);
}

#endif // !TARGET_OS_IPHONE

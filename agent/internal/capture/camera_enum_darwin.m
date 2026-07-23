#import <AVFoundation/AVFoundation.h>
#include <math.h>
#include <stdlib.h>
#include <string.h>

// usbridge_enumerate_cameras mirrors platf::enumerate_camera_devices() in
// Sunshine's own display.mm (see camera_darwin.go) so the agent's device list
// matches exactly what Sunshine can actually open by unique ID. Returns a
// malloc'd, tab/newline-delimited "<uniqueID>\t<localizedName>\n..." string;
// caller must free() it.
char *usbridge_enumerate_cameras(void) {
    @autoreleasepool {
        NSMutableArray<AVCaptureDeviceType> *deviceTypes = [NSMutableArray arrayWithObject:AVCaptureDeviceTypeBuiltInWideAngleCamera];
        if (@available(macOS 14.0, *)) {
            [deviceTypes addObject:AVCaptureDeviceTypeExternal];
        }

        AVCaptureDeviceDiscoverySession *discovery =
            [AVCaptureDeviceDiscoverySession discoverySessionWithDeviceTypes:deviceTypes
                                                                    mediaType:AVMediaTypeVideo
                                                                     position:AVCaptureDevicePositionUnspecified];

        NSMutableString *result = [NSMutableString string];
        for (AVCaptureDevice *device in discovery.devices) {
            [result appendFormat:@"%@\t%@\n", device.uniqueID, device.localizedName];
        }

        return strdup([result UTF8String]);
    }
}

// usbridge_camera_formats enumerates every AVCaptureDeviceFormat a camera
// actually supports (real width/height + real discrete frame rates from
// AVFrameRateRange, not a guessed/synthetic ladder) -- the camera equivalent
// of GetDisplayModes for monitors (see capture/common.go). Many UVC devices
// report several *discrete* frame rates as separate min==max ranges (e.g.
// this fork's frame-rate clamp fix found one reporting 60/50/30/20/10 as 5
// separate ranges) rather than one continuous span, so each range is kept
// distinct instead of collapsed into a single min-max.
//
// Returns a malloc'd string, one format per line:
//   <width>\t<height>\t<fps1>,<fps2>,...\n
// deduplicated by width/height (merging fps lists across formats that share
// the same dimensions but differ only in pixel format/binning). Caller must
// free() it. Empty string (not NULL) if the device isn't found.
char *usbridge_camera_formats(const char *uniqueID) {
    @autoreleasepool {
        NSString *uid = [NSString stringWithUTF8String:uniqueID];
        AVCaptureDevice *device = [AVCaptureDevice deviceWithUniqueID:uid];
        if (!device) {
            return strdup("");
        }

        // Preserve first-seen order of dimensions while merging fps sets for
        // repeated width/height pairs (e.g. same size offered at multiple
        // pixel formats/binning modes).
        NSMutableOrderedSet<NSString *> *dimKeys = [NSMutableOrderedSet orderedSet];
        NSMutableDictionary<NSString *, NSMutableOrderedSet<NSNumber *> *> *fpsByDim = [NSMutableDictionary dictionary];

        for (AVCaptureDeviceFormat *format in device.formats) {
            CMVideoDimensions dims = CMVideoFormatDescriptionGetDimensions(format.formatDescription);
            NSString *key = [NSString stringWithFormat:@"%dx%d", dims.width, dims.height];
            [dimKeys addObject:key];

            NSMutableOrderedSet<NSNumber *> *fpsSet = fpsByDim[key];
            if (!fpsSet) {
                fpsSet = [NSMutableOrderedSet orderedSet];
                fpsByDim[key] = fpsSet;
            }
            for (AVFrameRateRange *range in format.videoSupportedFrameRateRanges) {
                int lo = (int) lround(range.minFrameRate);
                int hi = (int) lround(range.maxFrameRate);
                if (lo == hi) {
                    // The common UVC case: a discrete rate reported as its
                    // own min==max range (e.g. this fork's frame-rate clamp
                    // fix found one device reporting 60/50/30/20/10 fps as 5
                    // separate ranges like this).
                    [fpsSet addObject:@(lo)];
                } else {
                    // A genuinely continuous range (common for built-in
                    // ISP-backed cameras) -- offer it via a standard fps
                    // ladder rather than every integer in [lo, hi], mirroring
                    // GetCommonModes()'s standardFPS list for monitors.
                    static const int kStandardFPS[] = {24, 25, 30, 50, 60, 120, 144, 165, 240};
                    for (size_t i = 0; i < sizeof(kStandardFPS) / sizeof(kStandardFPS[0]); i++) {
                        if (kStandardFPS[i] >= lo && kStandardFPS[i] <= hi) {
                            [fpsSet addObject:@(kStandardFPS[i])];
                        }
                    }
                    [fpsSet addObject:@(lo)];
                    [fpsSet addObject:@(hi)];
                }
            }
        }

        NSMutableString *result = [NSMutableString string];
        for (NSString *key in dimKeys) {
            NSArray<NSString *> *parts = [key componentsSeparatedByString:@"x"];
            NSMutableArray<NSString *> *fpsStrings = [NSMutableArray array];
            for (NSNumber *fps in fpsByDim[key]) {
                [fpsStrings addObject:[fps stringValue]];
            }
            [result appendFormat:@"%@\t%@\t%@\n", parts[0], parts[1], [fpsStrings componentsJoinedByString:@","]];
        }

        return strdup([result UTF8String]);
    }
}

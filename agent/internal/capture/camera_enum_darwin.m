#import <AVFoundation/AVFoundation.h>
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

#import <AVFoundation/AVFoundation.h>
#include <dispatch/dispatch.h>

int usbridge_camera_authorized(void) {
    return [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo] == AVAuthorizationStatusAuthorized;
}

// usbridge_request_camera blocks until the user answers the system prompt (or
// immediately, if a decision already exists from a prior run). TCC attributes
// this request to USBridgeAgent (see entitlements-macos.plist's
// com.apple.security.device.camera) even though it's Sunshine that actually
// opens the camera, so requesting from here — not Sunshine's own process —
// is what actually surfaces the system dialog.
int usbridge_request_camera(void) {
    AVAuthorizationStatus status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
    if (status == AVAuthorizationStatusNotDetermined) {
        dispatch_semaphore_t sem = dispatch_semaphore_create(0);
        [AVCaptureDevice requestAccessForMediaType:AVMediaTypeVideo completionHandler:^(BOOL granted) {
            dispatch_semaphore_signal(sem);
        }];
        dispatch_semaphore_wait(sem, DISPATCH_TIME_FOREVER);
        status = [AVCaptureDevice authorizationStatusForMediaType:AVMediaTypeVideo];
    }
    return status == AVAuthorizationStatusAuthorized;
}

#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <UIKit/UIKit.h>

extern void deliverViewportGestureStateFromObjC(int active);
extern void deliverViewportGestureUpdateFromObjC(float scaleFactor, float focusX, float focusY, float panDx, float panDy);
extern void deliverScrollGestureFromObjC(float dy);

// Two-finger touch distance, as a fraction of the smaller screen dimension, that separates
// "scroll wheel" gestures (fingers close together) from "pan+zoom / resize" gestures (fingers
// far apart). The two are mutually exclusive: whichever mode is decided when the gesture
// starts is the only one that delivers events for the rest of that gesture.
static const float kPanZoomThresholdFraction = 0.30f;

static float USBridgeTwoTouchDistance(UIGestureRecognizer *recognizer) {
    if (recognizer.numberOfTouches < 2) return 0;
    CGPoint p0 = [recognizer locationOfTouch:0 inView:recognizer.view];
    CGPoint p1 = [recognizer locationOfTouch:1 inView:recognizer.view];
    float dx = p1.x - p0.x, dy = p1.y - p0.y;
    return sqrtf(dx * dx + dy * dy);
}

@interface USBridgeGestureObserver : NSObject <UIGestureRecognizerDelegate>
@property (nonatomic, strong) UIPinchGestureRecognizer *pinchRecognizer;
@property (nonatomic, strong) UIPanGestureRecognizer *panRecognizer;
@property (nonatomic, assign) float initialPinchScale;
@property (nonatomic, assign) float lastScale;
@property (nonatomic, assign) BOOL modeDecided;
@property (nonatomic, assign) BOOL modeIsPanZoom;
@end

@implementation USBridgeGestureObserver

+ (instancetype)sharedInstance {
    static USBridgeGestureObserver *shared = nil;
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        shared = [[USBridgeGestureObserver alloc] init];
    });
    return shared;
}

- (void)setupGestures {
    dispatch_async(dispatch_get_main_queue(), ^{
        UIWindow *window = nil;
        if (@available(iOS 13.0, *)) {
            for (UIScene *scene in [UIApplication sharedApplication].connectedScenes) {
                if ([scene isKindOfClass:[UIWindowScene class]]) {
                    UIWindowScene *ws = (UIWindowScene *)scene;
                    for (UIWindow *win in ws.windows) {
                        if (win.isKeyWindow) { window = win; break; }
                    }
                }
                if (window) break;
            }
        } else {
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
            window = [UIApplication sharedApplication].keyWindow;
#pragma clang diagnostic pop
        }
        
        UIView *view = window.rootViewController.view;
        if (!view) return;

        self.pinchRecognizer = [[UIPinchGestureRecognizer alloc] initWithTarget:self action:@selector(handlePinch:)];
        self.pinchRecognizer.delegate = self;
        self.pinchRecognizer.cancelsTouchesInView = NO;
        [view addGestureRecognizer:self.pinchRecognizer];
        
        self.panRecognizer = [[UIPanGestureRecognizer alloc] initWithTarget:self action:@selector(handlePan:)];
        self.panRecognizer.delegate = self;
        self.panRecognizer.minimumNumberOfTouches = 2;
        self.panRecognizer.maximumNumberOfTouches = 2;
        self.panRecognizer.cancelsTouchesInView = NO;
        [view addGestureRecognizer:self.panRecognizer];
    });
}

- (BOOL)gestureRecognizer:(UIGestureRecognizer *)gestureRecognizer shouldRecognizeSimultaneouslyWithGestureRecognizer:(UIGestureRecognizer *)otherGestureRecognizer {
    return YES;
}

// Decides the gesture mode once, the first time either recognizer reports two touches down.
// Idempotent within a single gesture (guarded by modeDecided) so it doesn't matter which
// recognizer's Began fires first.
- (void)beginTwoFingerGestureFrom:(UIGestureRecognizer *)recognizer {
    if (self.modeDecided) return;
    self.modeDecided = YES;
    CGSize screen = [UIScreen mainScreen].bounds.size;
    float threshold = MIN(screen.width, screen.height) * kPanZoomThresholdFraction;
    self.modeIsPanZoom = USBridgeTwoTouchDistance(recognizer) >= threshold;
    deliverViewportGestureStateFromObjC(1);
}

- (void)endTwoFingerGestureIfNeeded {
    if (!self.modeDecided) return;
    self.modeDecided = NO;
    deliverViewportGestureStateFromObjC(0);
}

- (void)handlePinch:(UIPinchGestureRecognizer *)recognizer {
    if (recognizer.state == UIGestureRecognizerStateBegan) {
        [self beginTwoFingerGestureFrom:recognizer];
        self.initialPinchScale = 1.0;
        self.lastScale = recognizer.scale;
    } else if (recognizer.state == UIGestureRecognizerStateChanged) {
        if (!self.modeIsPanZoom) return; // scroll mode: pinch/zoom is ignored entirely
        CGPoint location = [recognizer locationInView:recognizer.view];
        float scaleFactor = recognizer.scale / self.lastScale;
        self.lastScale = recognizer.scale;
        deliverViewportGestureUpdateFromObjC(scaleFactor, location.x, location.y, 0, 0);
    } else if (recognizer.state == UIGestureRecognizerStateEnded || recognizer.state == UIGestureRecognizerStateCancelled) {
        [self endTwoFingerGestureIfNeeded];
    }
}

- (void)handlePan:(UIPanGestureRecognizer *)recognizer {
    if (recognizer.state == UIGestureRecognizerStateBegan) {
        [self beginTwoFingerGestureFrom:recognizer];
    } else if (recognizer.state == UIGestureRecognizerStateChanged) {
        CGPoint translation = [recognizer translationInView:recognizer.view];
        [recognizer setTranslation:CGPointMake(0, 0) inView:recognizer.view];

        if (self.modeIsPanZoom) {
            CGPoint location = [recognizer locationInView:recognizer.view];
            deliverViewportGestureUpdateFromObjC(1.0f, location.x, location.y, translation.x, translation.y);
        } else {
            deliverScrollGestureFromObjC(-translation.y);
        }
    } else if (recognizer.state == UIGestureRecognizerStateEnded || recognizer.state == UIGestureRecognizerStateCancelled) {
        [self endTwoFingerGestureIfNeeded];
    }
}

@end

void initVideoGesturesObserver(void) {
    [[USBridgeGestureObserver sharedInstance] setupGestures];
}

#endif // TARGET_OS_IPHONE

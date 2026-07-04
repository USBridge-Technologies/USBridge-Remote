#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <UIKit/UIKit.h>

extern void deliverViewportGestureStateFromObjC(int active);
extern void deliverViewportGestureUpdateFromObjC(float scaleFactor, float focusX, float focusY, float panDx, float panDy);
extern void deliverScrollGestureFromObjC(float dy);

@interface USBridgeGestureObserver : NSObject <UIGestureRecognizerDelegate>
@property (nonatomic, strong) UIPinchGestureRecognizer *pinchRecognizer;
@property (nonatomic, strong) UIPanGestureRecognizer *panRecognizer;
@property (nonatomic, assign) float initialPinchScale;
@property (nonatomic, assign) float lastScale;
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

- (void)handlePinch:(UIPinchGestureRecognizer *)recognizer {
    if (recognizer.state == UIGestureRecognizerStateBegan) {
        deliverViewportGestureStateFromObjC(1);
        self.initialPinchScale = 1.0;
        self.lastScale = recognizer.scale;
    } else if (recognizer.state == UIGestureRecognizerStateChanged) {
        CGPoint location = [recognizer locationInView:recognizer.view];
        float scaleFactor = recognizer.scale / self.lastScale;
        self.lastScale = recognizer.scale;
        
        deliverViewportGestureUpdateFromObjC(scaleFactor, location.x, location.y, 0, 0);
    } else if (recognizer.state == UIGestureRecognizerStateEnded || recognizer.state == UIGestureRecognizerStateCancelled) {
        deliverViewportGestureStateFromObjC(0);
    }
}

- (void)handlePan:(UIPanGestureRecognizer *)recognizer {
    // Fyne can also use two fingers to scroll. We'll send it via deliverScrollGestureFromObjC
    // and let Go handle panning. Wait, we'll send it to deliverViewportGestureUpdate if pinch is active.
    
    if (recognizer.state == UIGestureRecognizerStateBegan) {
        deliverViewportGestureStateFromObjC(1);
    } else if (recognizer.state == UIGestureRecognizerStateChanged) {
        CGPoint translation = [recognizer translationInView:recognizer.view];
        [recognizer setTranslation:CGPointMake(0, 0) inView:recognizer.view];
        
        CGPoint location = [recognizer locationInView:recognizer.view];
        deliverViewportGestureUpdateFromObjC(1.0f, location.x, location.y, translation.x, translation.y);
        
        // Optionally send scroll gesture for Fyne standard scroll
        deliverScrollGestureFromObjC(-translation.y);
    } else if (recognizer.state == UIGestureRecognizerStateEnded || recognizer.state == UIGestureRecognizerStateCancelled) {
        deliverViewportGestureStateFromObjC(0);
    }
}

@end

void initVideoGesturesObserver(void) {
    [[USBridgeGestureObserver sharedInstance] setupGestures];
}

#endif // TARGET_OS_IPHONE

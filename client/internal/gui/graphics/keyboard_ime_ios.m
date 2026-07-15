#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <Foundation/Foundation.h>
#import <UIKit/UIKit.h>

extern void deliverIMEHeightFromObjC(int imeHeightPx, int screenHeightPx);

@interface USBridgeKeyboardObserver : NSObject
+ (instancetype)sharedInstance;
- (void)startObserving;
@end

@implementation USBridgeKeyboardObserver

+ (instancetype)sharedInstance {
    static USBridgeKeyboardObserver *shared = nil;
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        shared = [[USBridgeKeyboardObserver alloc] init];
    });
    return shared;
}

- (void)startObserving {
    [[NSNotificationCenter defaultCenter] addObserver:self
                                             selector:@selector(keyboardWillChangeFrame:)
                                                 name:UIKeyboardWillChangeFrameNotification
                                               object:nil];
}

- (void)keyboardWillChangeFrame:(NSNotification *)notification {
    NSDictionary *userInfo = notification.userInfo;
    NSValue *endFrameValue = userInfo[UIKeyboardFrameEndUserInfoKey];
    if (!endFrameValue) return;

    CGRect endFrame = [endFrameValue CGRectValue];
    CGSize screenSize = [UIScreen mainScreen].bounds.size;
    
    // If the keyboard is off-screen (y >= screen height), height is 0.
    int imeHeight = 0;
    if (endFrame.origin.y < screenSize.height) {
        imeHeight = (int)endFrame.size.height;
    }
    
    int screenHeight = (int)screenSize.height;
    
    // Call back to Go
    deliverIMEHeightFromObjC(imeHeight, screenHeight);
}

@end

void initKeyboardObserver(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [[USBridgeKeyboardObserver sharedInstance] startObserving];
    });
}

#endif // TARGET_OS_IPHONE

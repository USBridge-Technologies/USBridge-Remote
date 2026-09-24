#include <TargetConditionals.h>
#if TARGET_OS_IPHONE

#import <Foundation/Foundation.h>
#import <UIKit/UIKit.h>

extern void deliverIMEHeightFromObjC(int imeHeightPx, int screenHeightPx);
extern void deliverIMETextFromObjC(int deleteCount, char* text);

@interface USBridgeStickyIMEField : UITextField
@end

@implementation USBridgeStickyIMEField
// Keep first-responder ownership: Fyne touchpads must not dismiss us.
- (BOOL)resignFirstResponder {
    // Only resign when sticky mode is off (see setStickyIMEEnabled).
    extern BOOL usbridgeStickyIMEEnabled(void);
    if (usbridgeStickyIMEEnabled()) {
        return NO;
    }
    return [super resignFirstResponder];
}
@end

@interface USBridgeKeyboardObserver : NSObject <UITextFieldDelegate>
@property (nonatomic, strong) USBridgeStickyIMEField *stickyField;
@property (nonatomic, copy) NSString *lastStickyText;
@property (nonatomic, assign) BOOL stickyEnabled;
@property (nonatomic, assign) BOOL ignoreText;
+ (instancetype)sharedInstance;
- (void)startObserving;
- (void)setStickyEnabled:(BOOL)enabled;
@end

static USBridgeKeyboardObserver *g_kbObserver = nil;

BOOL usbridgeStickyIMEEnabled(void) {
    return g_kbObserver != nil && g_kbObserver.stickyEnabled;
}

@implementation USBridgeKeyboardObserver

+ (instancetype)sharedInstance {
    static dispatch_once_t onceToken;
    dispatch_once(&onceToken, ^{
        g_kbObserver = [[USBridgeKeyboardObserver alloc] init];
    });
    return g_kbObserver;
}

- (void)startObserving {
    [[NSNotificationCenter defaultCenter] addObserver:self
                                             selector:@selector(keyboardWillChangeFrame:)
                                                 name:UIKeyboardWillChangeFrameNotification
                                               object:nil];
}

- (UIWindow *)keyWindow {
    for (UIScene *scene in UIApplication.sharedApplication.connectedScenes) {
        if (![scene isKindOfClass:[UIWindowScene class]]) {
            continue;
        }
        for (UIWindow *w in ((UIWindowScene *)scene).windows) {
            if (w.isKeyWindow) {
                return w;
            }
        }
    }
    return UIApplication.sharedApplication.windows.firstObject;
}

- (void)ensureStickyField {
    if (self.stickyField != nil) {
        return;
    }
    USBridgeStickyIMEField *field = [[USBridgeStickyIMEField alloc] initWithFrame:CGRectMake(0, 0, 1, 1)];
    field.hidden = YES;
    field.autocorrectionType = UITextAutocorrectionTypeNo;
    field.autocapitalizationType = UITextAutocapitalizationTypeNone;
    field.spellCheckingType = UITextSpellCheckingTypeNo;
    field.smartDashesType = UITextSmartDashesTypeNo;
    field.smartQuotesType = UITextSmartQuotesTypeNo;
    field.smartInsertDeleteType = UITextSmartInsertDeleteTypeNo;
    field.keyboardType = UIKeyboardTypeDefault;
    field.returnKeyType = UIReturnKeyDefault;
    field.delegate = self;
    // Seed with a sentinel space so backspace into empty still yields diffs.
    self.ignoreText = YES;
    field.text = @" ";
    self.ignoreText = NO;
    self.lastStickyText = @" ";
    self.stickyField = field;
    UIWindow *win = [self keyWindow];
    if (win != nil) {
        [win addSubview:field];
    }
}

- (void)setStickyEnabled:(BOOL)enabled {
    _stickyEnabled = enabled;
    dispatch_block_t blk = ^{
        [self ensureStickyField];
        UIWindow *win = [self keyWindow];
        if (win != nil && self.stickyField.superview != win) {
            [self.stickyField removeFromSuperview];
            [win addSubview:self.stickyField];
        }
        if (enabled) {
            self.ignoreText = YES;
            self.stickyField.text = @" ";
            self.lastStickyText = @" ";
            self.ignoreText = NO;
            [self.stickyField becomeFirstResponder];
        } else if (self.stickyField != nil) {
            [self.stickyField resignFirstResponder];
        }
    };
    if ([NSThread isMainThread]) {
        blk();
    } else {
        dispatch_async(dispatch_get_main_queue(), blk);
    }
}

- (void)reassertStickyFirstResponder {
    if (!self.stickyEnabled || self.stickyField == nil) {
        return;
    }
    dispatch_async(dispatch_get_main_queue(), ^{
        if (!self.stickyEnabled) {
            return;
        }
        if (![self.stickyField isFirstResponder]) {
            [self.stickyField becomeFirstResponder];
        }
    });
}

- (void)keyboardWillChangeFrame:(NSNotification *)notification {
    NSDictionary *userInfo = notification.userInfo;
    NSValue *endFrameValue = userInfo[UIKeyboardFrameEndUserInfoKey];
    if (!endFrameValue) return;

    CGRect endFrame = [endFrameValue CGRectValue];
    CGSize screenSize = [UIScreen mainScreen].bounds.size;

    int imeHeight = 0;
    if (endFrame.origin.y < screenSize.height) {
        imeHeight = (int)endFrame.size.height;
    }

    int screenHeight = (int)screenSize.height;
    deliverIMEHeightFromObjC(imeHeight, screenHeight);

    // If sticky and keyboard collapsed unexpectedly, pull it back.
    if (self.stickyEnabled && imeHeight < 50) {
        [self reassertStickyFirstResponder];
    }
}

- (void)emitDiffFrom:(NSString *)prev to:(NSString *)cur {
    NSUInteger i = 0;
    NSUInteger lim = MIN(prev.length, cur.length);
    while (i < lim && [prev characterAtIndex:i] == [cur characterAtIndex:i]) {
        i++;
    }
    int del = (int)(prev.length - i);
    NSString *ins = [cur substringFromIndex:i];
    if (del == 0 && ins.length == 0) {
        return;
    }
    const char *cIns = [ins UTF8String];
    deliverIMETextFromObjC(del, (char *)(cIns ? cIns : ""));
}

- (BOOL)textField:(UITextField *)textField shouldChangeCharactersInRange:(NSRange)range replacementString:(NSString *)string {
    if (self.ignoreText || !self.stickyEnabled) {
        return YES;
    }
    NSString *prev = textField.text ?: @"";
    NSString *cur = [prev stringByReplacingCharactersInRange:range withString:string ?: @""];
    if (cur.length < 1) {
        // Soft IME deleted the sentinel — restore and emit one backspace.
        self.ignoreText = YES;
        textField.text = @" ";
        self.lastStickyText = @" ";
        self.ignoreText = NO;
        deliverIMETextFromObjC(1, "");
        return NO;
    }
    [self emitDiffFrom:self.lastStickyText ?: @" " to:cur];
    self.lastStickyText = cur;
    // Cap buffer like Android sticky EditText.
    if (cur.length > 80) {
        self.ignoreText = YES;
        textField.text = @" ";
        self.lastStickyText = @" ";
        self.ignoreText = NO;
    }
    return YES;
}

- (BOOL)textFieldShouldReturn:(UITextField *)textField {
    if (self.stickyEnabled) {
        deliverIMETextFromObjC(0, "\n");
    }
    return NO;
}

@end

void initKeyboardObserver(void) {
    dispatch_async(dispatch_get_main_queue(), ^{
        [[USBridgeKeyboardObserver sharedInstance] startObserving];
    });
}

void setStickyIMEEnabled(int enabled) {
    [[USBridgeKeyboardObserver sharedInstance] setStickyEnabled:enabled != 0];
}

void reassertStickyIME(void) {
    [[USBridgeKeyboardObserver sharedInstance] reassertStickyFirstResponder];
}

#endif // TARGET_OS_IPHONE

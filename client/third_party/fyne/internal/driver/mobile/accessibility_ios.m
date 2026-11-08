//go:build ios

#import <UIKit/UIKit.h>

static NSMutableArray<UIAccessibilityElement*> *pendingElements = nil;

static UIView* getContainerView(void) {
    UIWindow *window = nil;
    NSArray<UIWindow*> *windows = [[UIApplication sharedApplication] windows];
    for (UIWindow *w in windows) {
        if (w.isKeyWindow) {
            window = w;
            break;
        }
    }
    if (window == nil && windows.count > 0) {
        window = windows[0];
    }
    if (window == nil) {
        return nil;
    }

    UIViewController *rootVC = window.rootViewController;
    if (rootVC == nil) {
        return nil;
    }

    return rootVC.view;
}

void clearAccessibilityNodesIOS(void) {
    @autoreleasepool {
        pendingElements = [[NSMutableArray alloc] init];
    }
}

void addAccessibilityNodeIOS(int role, const char *label,
    float x, float y, float width, float height) {
    @autoreleasepool {
        UIView *container = getContainerView();
        if (container == nil) {
            return;
        }

        UIAccessibilityElement *elem = [[UIAccessibilityElement alloc]
            initWithAccessibilityContainer:container];

        elem.accessibilityLabel = label ? [NSString stringWithUTF8String:label] : @"";

        // Map Fyne roles to UIAccessibilityTraits.
        // role values: 1=button, 2=text, 3=link, 4=container
        switch (role) {
            case 1: // button
                elem.accessibilityTraits = UIAccessibilityTraitButton;
                break;
            case 2: // text
                elem.accessibilityTraits = UIAccessibilityTraitStaticText;
                break;
            case 3: // link
                elem.accessibilityTraits = UIAccessibilityTraitLink;
                break;
            default:
                elem.accessibilityTraits = UIAccessibilityTraitNone;
                break;
        }

        // Convert from native pixel coordinates to point coordinates for the
        // accessibility frame, which UIKit expects in screen coordinates.
        CGFloat scale = [UIScreen mainScreen].nativeScale;
        CGRect frame = CGRectMake(x / scale, y / scale, width / scale, height / scale);
        elem.accessibilityFrame = UIAccessibilityConvertFrameToScreenCoordinates(frame, container);

        [pendingElements addObject:elem];
    }
}

void commitAccessibilityNodesIOS(void) {
    @autoreleasepool {
        UIView *container = getContainerView();
        if (container == nil) {
            return;
        }

        container.accessibilityElements = [pendingElements copy];
        UIAccessibilityPostNotification(UIAccessibilityLayoutChangedNotification, nil);
    }
}

void setupAccessibilityIOS(void) {
    // No-op: the container view is looked up dynamically each time.
}

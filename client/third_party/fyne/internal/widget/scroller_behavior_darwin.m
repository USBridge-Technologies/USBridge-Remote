//go:build !ci && darwin && !ios && !iossimulator

#import <Foundation/Foundation.h>
#import <AppKit/NSScroller.h>

extern void scrollerStyleChanged();

int getScrollerPagingBehavior() {
    return [[NSUserDefaults standardUserDefaults] boolForKey:@"AppleScrollerPagingBehavior"];
}

int getScrollerStyle() {
    return [NSScroller preferredScrollerStyle];
}

void watchScrollerStyle() {
    [[NSNotificationCenter defaultCenter] addObserverForName:NSPreferredScrollerStyleDidChangeNotification
        object:nil queue:nil
        usingBlock:^(NSNotification *note) {
            scrollerStyleChanged(); // calls back into Go
        }];
}

//go:build !ci && ios && !mobile

#import <UIKit/UIKit.h>

void openURL(char *urlStr) {
    UIApplication *app = [UIApplication sharedApplication];
    NSURL *url = [NSURL URLWithString:[NSString stringWithUTF8String:urlStr]];
    [app openURL:url options:@{} completionHandler:nil];
}


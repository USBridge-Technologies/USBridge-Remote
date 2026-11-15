//go:build ios && cgo

package service

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AuthenticationServices -framework UIKit

#import <AuthenticationServices/AuthenticationServices.h>
#import <UIKit/UIKit.h>
#include <stdlib.h>

// The Tailscale sign-in in an ASWebAuthenticationSession: a sheet over the
// app that shares Safari's cookies (so the user is already signed in to
// Google), instead of switching to Safari. Tailscale never redirects back,
// so the app closes it itself (usb_ios_close_signin) once tsnet reports the
// node signed in. The callback scheme is only there because the API needs
// one.

API_AVAILABLE(ios(13.0))
@interface USBSignInAnchor : NSObject <ASWebAuthenticationPresentationContextProviding>
@end

static UIWindow *usb_signin_key_window(void) {
	if (@available(iOS 13.0, *)) {
		for (UIScene *scene in UIApplication.sharedApplication.connectedScenes) {
			if (![scene isKindOfClass:[UIWindowScene class]]) continue;
			for (UIWindow *w in ((UIWindowScene *)scene).windows) {
				if (w.isKeyWindow) return w;
			}
		}
	}
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
	return UIApplication.sharedApplication.keyWindow;
#pragma clang diagnostic pop
}

@implementation USBSignInAnchor
- (ASPresentationAnchor)presentationAnchorForWebAuthenticationSession:(ASWebAuthenticationSession *)session API_AVAILABLE(ios(13.0)) {
	return usb_signin_key_window();
}
@end

static ASWebAuthenticationSession *usb_signin_session API_AVAILABLE(ios(12.0));
static id usb_signin_anchor;
static int usb_signin_gen;

static void usb_ios_open_signin(const char *curl) {
	NSString *s = [NSString stringWithUTF8String:curl];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSURL *url = [NSURL URLWithString:s];
		if (url == nil) return;
		if (@available(iOS 13.0, *)) {
			if (usb_signin_session != nil) [usb_signin_session cancel];
			int gen = ++usb_signin_gen;
			ASWebAuthenticationSession *session = [[ASWebAuthenticationSession alloc]
				initWithURL:url
				callbackURLScheme:@"usbridge"
				completionHandler:^(NSURL *callbackURL, NSError *error) {
					dispatch_async(dispatch_get_main_queue(), ^{
						if (gen == usb_signin_gen) usb_signin_session = nil;
					});
				}];
			if (usb_signin_anchor == nil) usb_signin_anchor = [USBSignInAnchor new];
			session.presentationContextProvider = usb_signin_anchor;
			session.prefersEphemeralWebBrowserSession = NO;
			usb_signin_session = session;
			if ([session start]) return;
			usb_signin_session = nil;
			NSLog(@"[Tailscale] sign-in sheet did not start, opening Safari");
		}
		[UIApplication.sharedApplication openURL:url options:@{} completionHandler:nil];
	});
}

static void usb_ios_close_signin(void) {
	dispatch_async(dispatch_get_main_queue(), ^{
		if (@available(iOS 13.0, *)) {
			if (usb_signin_session != nil) {
				[usb_signin_session cancel];
				usb_signin_session = nil;
			}
		}
	});
}
*/
import "C"

import "unsafe"

// openIOSSignIn shows the sign-in in the in-app sheet (Safari if it can't).
func openIOSSignIn(url string) {
	c := C.CString(url)
	defer C.free(unsafe.Pointer(c))
	C.usb_ios_open_signin(c)
}

// closeIOSSignIn closes the sheet once the sign-in is done.
func closeIOSSignIn() { C.usb_ios_close_signin() }

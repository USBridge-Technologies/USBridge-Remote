package service

import "runtime"

// OpenSignInPage shows a sign-in page (Tailscale, the USBridge account) over
// the app on mobile, in the user's own browser session so they are already
// signed in to Google: a Custom Tab on Android (kept alive meanwhile, see
// SignInKeepAliveService), an ASWebAuthenticationSession sheet on iOS.
// Returns false on desktop: the caller opens the default browser (Chrome,
// Safari, ... with the user's own sign-ins; the sheet would be Safari-only
// on macOS) and the app raises itself once the sign-in is done.
func OpenSignInPage(url string) bool {
	switch runtime.GOOS {
	case "android":
		ok, err := openAndroidExternalUrl(url)
		return err == nil && ok
	case "ios":
		openIOSSignIn(url)
		return true
	}
	return false
}

// CloseSignInPage returns to the app once the sign-in is done: Android
// brings it back over the Custom Tab, iOS closes the sheet.
func CloseSignInPage() {
	switch runtime.GOOS {
	case "android":
		bringAndroidAppToFront()
	case "ios":
		closeIOSSignIn()
	}
}

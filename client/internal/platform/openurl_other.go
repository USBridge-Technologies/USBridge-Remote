//go:build !darwin && !android

package platform

// TakeOpenedURL: macOS, iOS and Android deliver links to a running app as events
// (see openurl_darwin.go, openurl_android.go);
// elsewhere they come through GetIntentDataURI.
func TakeOpenedURL() string { return "" }

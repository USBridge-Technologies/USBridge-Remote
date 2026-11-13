//go:build !darwin || ios

package platform

// TakeOpenedURL: only macOS delivers links as events (see openurl_darwin.go);
// elsewhere they come through GetIntentDataURI.
func TakeOpenedURL() string { return "" }

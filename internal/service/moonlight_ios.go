//go:build ios

package service

// initMoonlightConfigDir is a no-op on iOS: the app stores Moonlight identity
// in the default config directory or it doesn't need to be overridden.
func initMoonlightConfigDir() {}

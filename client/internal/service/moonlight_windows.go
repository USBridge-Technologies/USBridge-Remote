//go:build windows

package service

// initMoonlightConfigDir is a no-op on Windows: the app stores Moonlight identity
// in the default OS config dir (%APPDATA%\usbridge-client\moonlight).
func initMoonlightConfigDir() {}

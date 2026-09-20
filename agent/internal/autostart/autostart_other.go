//go:build !linux && !darwin && !windows

package autostart

func IsEnabled() bool       { return false }
func Enable() error         { return nil }
func Disable() error        { return nil }
func NeedsReboot() bool     { return false }
func RefreshX11SessionEnv() {}
func EnsureDisplayActive()  {}
func Location() string        { return "" }

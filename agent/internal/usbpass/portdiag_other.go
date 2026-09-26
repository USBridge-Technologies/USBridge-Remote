//go:build !windows && !linux

package usbpass

// whatHoldsPort has no implementation outside Windows/Linux (this package's
// only supported platforms -- see Service.Status's Available field); always
// "" here, same as any failed lookup on the platforms that do implement it.
func whatHoldsPort(int) string { return "" }

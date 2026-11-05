//go:build !darwin || ios

package update

// LeaveTranslocation is macOS-only (see translocation_darwin.go).
func LeaveTranslocation() bool { return false }

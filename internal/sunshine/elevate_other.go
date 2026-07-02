//go:build !windows

package sunshine

import "errors"

var errElevationNotSupported = errors.New("elevation not supported on this platform")

func launchElevated(_ string) (uintptr, error) { return 0, errElevationNotSupported }
func terminateElevated(_ uintptr)              {}
func waitElevatedAsync(_ uintptr, _ func())    {}
func elevatedRunning(_ uintptr) bool           { return false }

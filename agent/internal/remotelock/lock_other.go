//go:build !windows && !linux && !darwin

package remotelock

func setHookEnabled(bool) {}

func setX11Window(uintptr) {}

func hookInstalled() bool { return false }

//go:build !linux

package app

func disconnectedPinFallback(string) (string, bool) { return "", false }

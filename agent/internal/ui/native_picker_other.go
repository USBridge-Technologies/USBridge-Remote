//go:build !windows && !darwin && !linux

package ui

func pickNativePath(folder bool, title string) (path string, shown bool) { return "", false }

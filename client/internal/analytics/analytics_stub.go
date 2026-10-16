//go:build !android && !windows && !linux && !darwin

package analytics

func emit(name string, a, b param) {}

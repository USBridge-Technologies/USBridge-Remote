//go:build !android

package analytics

func emit(name string, a, b param) {}

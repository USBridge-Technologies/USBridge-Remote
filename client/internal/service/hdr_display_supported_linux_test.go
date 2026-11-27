//go:build linux && !android && cgo

package service

import "testing"

func TestHdrDisplaySupportedLinux(t *testing.T) {
	got := HdrDisplaySupported()
	t.Logf("HdrDisplaySupported() = %v", got)
}

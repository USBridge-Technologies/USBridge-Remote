//go:build linux && !android && cgo

package service

import "testing"

func TestHdrDisplaySupportedLinux(t *testing.T) {
	t.Logf("HdrDisplaySupported() = %v", HdrDisplaySupported())
	t.Logf("PyroWaveColorDecodeSupported(444=true, hdr=false) = %v", PyroWaveColorDecodeSupported(true, false))
	t.Logf("PyroWaveColorDecodeSupported(444=false, hdr=true) = %v", PyroWaveColorDecodeSupported(false, true))
	t.Logf("PyroWaveColorDecodeSupported(444=true, hdr=true)  = %v", PyroWaveColorDecodeSupported(true, true))
}

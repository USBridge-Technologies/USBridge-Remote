//go:build !android

package service

// MaxHardwareDecodeResolution returns the hardware video decoder ceiling on
// desktop platforms (4K UHD; individual GPUs/displays may exceed this, and
// callers treat (0, 0) as unrestricted).
func MaxHardwareDecodeResolution(videoMode string) (int, int) {
	_ = videoMode
	return 3840, 2160
}

//go:build !android

package service

// Color444DecodeSupported: see color444_decode_supported_android.go. The
// desktop decode paths (D3D11VA, VideoToolbox, VAAPI/software) handle HEVC
// 4:4:4 themselves or fall back in their own decoder setup.
func Color444DecodeSupported() bool {
	return true
}

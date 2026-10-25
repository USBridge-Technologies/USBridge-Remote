//go:build !(darwin && !ios && cgo) && !(windows && cgo) && !android

package service

// HdrDisplaySupported: see hdr_display_supported_darwin.go's and
// hdr_display_supported_windows.go's doc comments.
// No HDR render path on this platform yet.
func HdrDisplaySupported() bool {
	return false
}

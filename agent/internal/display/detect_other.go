//go:build !linux

package display

// MaxResolution is not yet implemented outside Linux; callers fall back to
// their own default.
func MaxResolution() (width, height int, ok bool) {
	return 0, 0, false
}

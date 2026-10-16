//go:build !(darwin && cgo) && !(android && cgo)

package platform

import (
	"net/url"
	"runtime"

	"fyne.io/fyne/v2"
)

// MicPermission: Linux and Windows don't ask per app; Windows' privacy
// switch shows up as ErrMicAccessDenied when the microphone opens.
func MicPermission() MicAuth { return MicAuthGranted }

// RequestMicPermission: see MicPermission.
func RequestMicPermission(done func(granted bool)) { done(true) }

// OpenMicSettings opens the page where the microphone is allowed again.
func OpenMicSettings() {
	if runtime.GOOS != "windows" {
		return
	}
	if u, err := url.Parse("ms-settings:privacy-microphone"); err == nil {
		if app := fyne.CurrentApp(); app != nil {
			_ = app.OpenURL(u)
		}
	}
}

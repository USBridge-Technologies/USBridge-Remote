//go:build !android && !ios

package controller

// HandleAppBackgrounded / HandleAppForegrounded are no-ops on desktop.
// See video_widget_android.go's HandleAppBackgrounded doc comment for the
// mobile-specific race (a stray video frame from a superseded connection
// attempt popping the native overlay up over the wrong screen after the OS
// throttles/freezes background work) these exist to close on Android/iOS —
// desktop platforms don't get backgrounded/foregrounded the same way, and
// have no equivalent OS-level network/goroutine freezing to race against.
func (vw *VideoWidget) HandleAppBackgrounded() {}
func (vw *VideoWidget) HandleAppForegrounded() {}

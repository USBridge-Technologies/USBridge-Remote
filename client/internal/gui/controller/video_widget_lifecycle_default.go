//go:build !android && !ios

package controller

// HandleAppBackgrounded: on desktop Fyne calls it when no window of the
// client has focus any more (Alt+Tab, a click into another app). Keys held
// at that moment never get their KeyUp in this process, so the host would
// keep them pressed and auto-repeat them: release everything sent. (See
// video_widget_android.go for the mobile-only video race the mobile
// versions close; desktop has none.)
func (vw *VideoWidget) HandleAppBackgrounded() {
	vw.releaseAllMoonlightKeys()
}

func (vw *VideoWidget) HandleAppForegrounded()           {}
func (vw *VideoWidget) prepareNativeVideoBeforeConnect() {}

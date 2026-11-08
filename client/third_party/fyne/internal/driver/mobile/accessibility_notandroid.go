//go:build !accessibility || (!android && !ios)

package mobile

// Stub implementations for platforms without accessibility bridges.

func (w *window) updateAccessibility() {
}

func (w *window) initAccessibilityForWindow() {
}

func (w *window) cleanupAccessibilityForWindow() {
}

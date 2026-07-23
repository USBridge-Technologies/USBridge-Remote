//go:build !android

package gui

import "usbridge-client/internal/gui/view"

// vulkanPostprocessMenuItem is a no-op stub on every platform except
// Android (see main_window_vulkan_android.go) — the Vulkan post-processing
// popup is Android-only for now.
func (mw *MainWindow) vulkanPostprocessMenuItem() *view.StyledMenuItem {
	return nil
}

//go:build android

package gui

import (
	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
)

// vulkanPostprocessMenuItem adds the "Vulkan" entry to the video icon's menu
// (right below "Settings") on Android, where the native renderer is Vulkan
// and can run the GPU post-processing compute pass. Other platforms get the
// stub in main_window_vulkan_stub.go, which returns nil (no menu item).
func (mw *MainWindow) vulkanPostprocessMenuItem() *view.StyledMenuItem {
	return &view.StyledMenuItem{
		Label: i18n.Current.VulkanAction,
		OnTap: mw.showVulkanPostprocessDialog,
	}
}

func (mw *MainWindow) showVulkanPostprocessDialog() {
	if mw.window == nil {
		return
	}
	app := mw.app
	current := controller.LoadVulkanPostprocessSettings(app)
	view.ShowVulkanPostprocessDialog(mw.window, view.VulkanPostprocessValues(current),
		func(v view.VulkanPostprocessValues) {
			controller.ApplyVulkanPostprocessSettings(app, controller.VulkanPostprocessSettings(v))
		},
		func() view.VulkanPostprocessValues {
			defaults := controller.DefaultVulkanPostprocessSettings()
			controller.ApplyVulkanPostprocessSettings(app, defaults)
			return view.VulkanPostprocessValues(defaults)
		},
	)
}

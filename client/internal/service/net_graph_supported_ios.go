//go:build ios

package service

// NetGraphSupported reports whether this build has a working Net Graph HUD
// push path -- see net_graph_supported_darwin.go's doc comment for the
// general contract. iOS gets its own native HUD CALayer, mirroring macOS's
// (see metal_video_impl_ios.m's g_hud_layer and metal_video_ios.go's init()).
func NetGraphSupported() bool {
	return true
}

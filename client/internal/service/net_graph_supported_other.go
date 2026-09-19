//go:build !darwin && !ios && !linux && !windows

package service

// NetGraphSupported is false on every platform without a push
// implementation yet (js/wasm, etc.) -- see net_graph_supported_darwin.go's
// doc comment. macOS/iOS/Linux/Windows/Android each have their own file.
func NetGraphSupported() bool {
	return false
}

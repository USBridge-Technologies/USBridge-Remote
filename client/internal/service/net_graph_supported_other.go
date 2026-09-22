//go:build !darwin && !ios && !linux && !windows && !(js && wasm)

package service

// NetGraphSupported is false on every remaining platform without a push
// implementation (js/wasm has its own file, net_graph_supported_wasm.go,
// with a real getStats()-backed implementation -- see that file's doc
// comment) -- see net_graph_supported_darwin.go's doc comment.
// macOS/iOS/Linux/Windows/Android/wasm each have their own file.
func NetGraphSupported() bool {
	return false
}

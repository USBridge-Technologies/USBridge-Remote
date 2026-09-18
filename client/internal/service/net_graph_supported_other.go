//go:build !darwin && !ios && !linux && !windows

package service

// NetGraphSupported is false on every platform without a push
// implementation yet (e.g. Android) -- see net_graph_supported_darwin.go's
// doc comment. macOS/iOS/Linux/Windows each have their own dedicated file.
func NetGraphSupported() bool {
	return false
}

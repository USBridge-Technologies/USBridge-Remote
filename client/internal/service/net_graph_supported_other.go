//go:build !darwin || ios

package service

// NetGraphSupported is false on every platform without a push
// implementation yet -- see net_graph_supported_darwin.go's doc comment.
func NetGraphSupported() bool {
	return false
}

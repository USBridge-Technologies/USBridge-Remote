//go:build ci || !darwin || ios || iossimulator

package widget

func isScrollerPageOnTap() bool {
	return false
}

func scrollBarAlwaysVisible() bool {
	return true
}

func subscribeScrollerStyle(_ func()) uint64 { return 0 }
func unsubscribeScrollerStyle(_ uint64)      {}

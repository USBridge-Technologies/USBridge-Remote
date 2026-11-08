//go:build !ci && darwin && !ios && !iossimulator

package widget

/*
#cgo LDFLAGS: -framework AppKit

int getScrollerPagingBehavior();
int getScrollerStyle();
void watchScrollerStyle();
*/
import "C"

import (
	"fyne.io/fyne/v2"
)

func isScrollerPageOnTap() bool {
	return C.getScrollerPagingBehavior() == 0
}

// scrollBarAlwaysVisible returns true when the macOS "Show scroll bars" preference
// is set to "Always" (NSScrollerStyleLegacy = 0).
// When set to "When scrolling" or "Automatically" (NSScrollerStyleOverlay = 1),
// scroll bars should only appear on hover or while scrolling.
func scrollBarAlwaysVisible() bool {
	return C.getScrollerStyle() == 0
}

var (
	isWatchingMacScrollerStyle bool
	scrollerStyleSubscribers   = map[uint64]func(){}
	scrollerStyleNextID        uint64
)

func subscribeScrollerStyle(fn func()) uint64 {
	if !isWatchingMacScrollerStyle {
		isWatchingMacScrollerStyle = true
		C.watchScrollerStyle()
	}
	id := scrollerStyleNextID
	scrollerStyleNextID++
	scrollerStyleSubscribers[id] = fn
	return id
}

func unsubscribeScrollerStyle(id uint64) {
	delete(scrollerStyleSubscribers, id)
}

//export scrollerStyleChanged
func scrollerStyleChanged() {
	fyne.Do(func() {
		for _, fn := range scrollerStyleSubscribers {
			fn()
		}
	})
}

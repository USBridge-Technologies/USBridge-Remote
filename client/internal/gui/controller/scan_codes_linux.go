//go:build linux && !android

package controller

import (
	"sync"

	"usbridge-client/internal/input"
)

var scanCodeNumberingOnce sync.Once

// syncScanCodeNumbering tells the input package which scan code numbering GLFW
// uses in this process: a key event means GLFW is up, and its platform never changes.
func syncScanCodeNumbering() {
	scanCodeNumberingOnce.Do(func() {
		input.SetLinuxScanCodesAreEvdev(vkOnWaylandPlatform())
	})
}

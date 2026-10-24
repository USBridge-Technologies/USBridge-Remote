package service

import "sync/atomic"

// The two-finger scroll speed on touch screens: how many wheel notches the
// remote PC gets for a two-finger swipe (Windows scrolls 3 lines a notch).
// The video settings dialog's slider on phones and tablets; level
// ScrollSpeedDefaultLevel is a notch every 10dp.
var scrollSpeedLevel atomic.Int32

const (
	ScrollSpeedMinLevel     = 1
	ScrollSpeedMaxLevel     = 10
	ScrollSpeedDefaultLevel = 4
	// scrollDPAtLevel1 is a notch's travel at the slowest level; level n
	// takes 1/n of it.
	scrollDPAtLevel1 = 40
)

func init() { scrollSpeedLevel.Store(ScrollSpeedDefaultLevel) }

// SetScrollSpeedLevel sets the slider's level (clamped).
func SetScrollSpeedLevel(level int) { scrollSpeedLevel.Store(int32(ClampScrollSpeedLevel(level))) }

// ScrollSpeedLevel reports the slider's level.
func ScrollSpeedLevel() int { return int(scrollSpeedLevel.Load()) }

// ClampScrollSpeedLevel bounds a stored or dragged level.
func ClampScrollSpeedLevel(level int) int {
	return max(ScrollSpeedMinLevel, min(ScrollSpeedMaxLevel, level))
}

// ScrollDPPerNotch is how far two fingers move, in dp, for one notch.
func ScrollDPPerNotch() float32 {
	return float32(scrollDPAtLevel1) / float32(ScrollSpeedLevel())
}

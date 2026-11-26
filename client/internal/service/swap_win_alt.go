package service

import "sync/atomic"

// swapWinAlt (Client settings, off by default): the Win and Alt keys trade
// places on the way to the host, left with left and right with right, in
// the key code and in the modifier mask. For a PC keyboard on a Mac host
// that puts Command next to the space bar, where a Mac keyboard has it.
var swapWinAlt atomic.Bool

// SetSwapWinAlt turns the swap on or off.
func SetSwapWinAlt(on bool) { swapWinAlt.Store(on) }

// SwapWinAlt reports whether the swap is on.
func SwapWinAlt() bool { return swapWinAlt.Load() }

// Moonlight modifier bits (MODIFIER_ALT / MODIFIER_META).
const (
	moonlightModAlt  int8 = 0x04
	moonlightModMeta int8 = 0x08
)

// applySwapWinAlt translates a key for the host; every SendMoonlightKey
// calls it. The dialog that flips the switch releases held keys first, so a
// key-up is never translated differently from its key-down.
func applySwapWinAlt(vk int16, mods int8) (int16, int8) {
	if !swapWinAlt.Load() {
		return vk, mods
	}
	switch vk {
	case 0x5B: // VK_LWIN
		vk = 0xA4 // VK_LMENU
	case 0x5C: // VK_RWIN
		vk = 0xA5 // VK_RMENU
	case 0xA4:
		vk = 0x5B
	case 0xA5:
		vk = 0x5C
	case 0x12: // VK_MENU (unsided Alt)
		vk = 0x5B
	}
	alt, meta := mods&moonlightModAlt != 0, mods&moonlightModMeta != 0
	mods &^= moonlightModAlt | moonlightModMeta
	if alt {
		mods |= moonlightModMeta
	}
	if meta {
		mods |= moonlightModAlt
	}
	return vk, mods
}

package controller

import (
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/input"
	"usbridge-client/internal/service"
)

// A hardware KVM types into its target as a USB keyboard: it presses keys,
// and the target's layout decides the characters. Text mode used to press
// each character's US key, so on a Hungarian target "0" typed "ö" and there
// was no way to type 0. The target layout (by default the one this computer
// types with) now picks the key, Shift/AltGr, and Space after a dead key.

// SetRemoteKeyboardLayout sets the hardware KVM target's layout: a KLID from
// input.RemoteLayouts or input.RemoteLayoutAuto.
func (vw *VideoWidget) SetRemoteKeyboardLayout(setting string) {
	if setting == "" {
		setting = input.RemoteLayoutAuto
	}
	vw.remoteKeyboardLayout.Store(setting)
}

// RemoteKeyboardLayout returns the setting (input.RemoteLayoutAuto unless set).
func (vw *VideoWidget) RemoteKeyboardLayout() string {
	if s, _ := vw.remoteKeyboardLayout.Load().(string); s != "" {
		return s
	}
	return input.RemoteLayoutAuto
}

// TypesAsUSBKeyboard reports a session whose keyboard is a hardware KVM's
// emulated USB keyboard (any KVM, not only USBridge's): text is typed with
// the target layout setting there.
func (vw *VideoWidget) TypesAsUSBKeyboard() bool {
	return vw.kvmCaptureIsTheFrame()
}

const (
	vkSpace  = 0x20
	vkShift  = 0x10
	vkLShift = 0xA0
	vkRShift = 0xA1
	vkRAlt   = 0xA5 // AltGr
)

// sendRuneViaRemoteLayout types r with the key the target layout has it on.
// Modifiers go as their own key events, not as packet modifier bits: the
// host only adds missing ones from those, and a KVM target sees every
// press, so Shift is released for an unshifted character the user typed
// with Shift held (and pressed again after), not left to change it.
func (vw *VideoWidget) sendRuneViaRemoteLayout(r rune) bool {
	mi := vw.moonlightInput()
	if mi == nil {
		return false
	}
	layout := input.ResolveRemoteLayout(vw.RemoteKeyboardLayout())
	k, ok := input.RemoteLayoutKey(layout, r)
	if !ok {
		return false
	}
	vk := input.VKCodeFromPS2ScanCode(k.ScanCode)
	if vk == 0 {
		return false
	}
	logrus.Debugf("⌨️ [remote-layout] %q on %s: scan=0x%02X vk=0x%02X shift=%v altgr=%v dead=%v", r, layout, k.ScanCode, uint16(vk), k.Shift, k.AltGr, k.Dead)
	vw.enqueueSend(func() {
		for _, s := range remoteLayoutKeySteps(k, vk, vw.heldShiftVKs()) {
			action := service.LiKeyActionUp
			if s.down {
				action = service.LiKeyActionDown
			}
			mi.SendMoonlightKey(s.vk, action, 0)
		}
	})
	return true
}

type keyStep struct {
	vk   int16
	down bool
}

// remoteLayoutKeySteps is the press sequence for one character; heldShift
// are the Shift keys the user holds on the remote right now.
func remoteLayoutKeySteps(k input.RemoteKey, vk int16, heldShift []int16) []keyStep {
	var steps, after []keyStep
	switch {
	case k.Shift && len(heldShift) == 0:
		steps = append(steps, keyStep{vkLShift, true})
		after = append(after, keyStep{vkLShift, false})
	case !k.Shift:
		for _, s := range heldShift {
			steps = append(steps, keyStep{s, false})
			after = append(after, keyStep{s, true})
		}
	}
	if k.AltGr {
		steps = append(steps, keyStep{vkRAlt, true})
		after = append([]keyStep{{vkRAlt, false}}, after...)
	}
	steps = append(steps, keyStep{vk, true}, keyStep{vk, false})
	if k.Dead {
		// Modifiers off first: AltGr+Space is not a plain space everywhere.
		var restore []keyStep
		for _, s := range after {
			if s.down {
				restore = append(restore, s)
			} else {
				steps = append(steps, s)
			}
		}
		steps = append(steps, keyStep{vkSpace, true}, keyStep{vkSpace, false})
		return append(steps, restore...)
	}
	return append(steps, after...)
}

// heldShiftVKs returns the Shift VKs this client holds down on the remote.
func (vw *VideoWidget) heldShiftVKs() []int16 {
	vw.moonlightKeyMu.Lock()
	defer vw.moonlightKeyMu.Unlock()
	var out []int16
	for _, vk := range []int16{vkLShift, vkRShift, vkShift} {
		if vw.moonlightHeldVKs[vk] {
			out = append(out, vk)
		}
	}
	return out
}

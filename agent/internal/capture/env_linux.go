//go:build linux

package capture

import (
	"os"
)

func GetLinuxEnv() string {
	waylandDisplay := os.Getenv("WAYLAND_DISPLAY")
	if waylandDisplay != "" {
		return "Wayland"
	}
	x11Display := os.Getenv("DISPLAY")
	if x11Display != "" {
		return "X11"
	}
	return "Unknown"
}

func GetOSInfo() string {
	return "Linux (" + GetLinuxEnv() + ")"
}

func GetDisplayServer() string {
	return GetLinuxEnv()
}

// AutoCaptureMode picks Sunshine's Linux capture backend from the live
// session alone — no user choice involved, so it can never be left pointed
// at a combination that's known not to work. Just two outcomes:
//
//   - X11 (GetLinuxEnv() == "X11", i.e. a live desktop session with DISPLAY
//     set): "x11", direct XShm capture. Deliberately never "kms" here: on
//     the NVIDIA proprietary driver — the common discrete-GPU case — KMS
//     capture is confirmed broken alongside a running Xorg, because Xorg
//     already holds the DRM master and NVIDIA leaves the legacy
//     CRTC/plane/encoder fields Sunshine's KMS backend reads unpopulated for
//     any other client (see the matching comment in internal/ui/window.go).
//     X11 capture needs no DRM master, so it's the one path guaranteed to
//     work here regardless of GPU vendor.
//   - Everything else — Wayland (GetLinuxEnv() == "Wayland") and no
//     graphical session at all in this process's environment
//     (GetLinuxEnv() == "Unknown": headless systemd autostart before login,
//     an SDDM/greeter screen, or a bare virtual console): "kms". Sunshine's
//     own KMS backend handles a live Wayland compositor directly (no
//     separate portal path needed), and it's the only backend that can
//     capture anything at all with no graphical session running yet.
func AutoCaptureMode() string {
	if GetLinuxEnv() == "X11" {
		return "x11"
	}
	return "kms"
}

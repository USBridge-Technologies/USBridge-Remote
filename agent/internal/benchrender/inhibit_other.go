//go:build !linux

package benchrender

// inhibitIdle is Linux-only so far: KDE's idle dimming, which scales the HDMI
// signal itself, is where a dimmed picture was seen to break the barcode.
func inhibitIdle() {}

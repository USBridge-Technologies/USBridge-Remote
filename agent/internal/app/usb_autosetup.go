package app

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// usbAutoSetupMarker records, in StateDir, that autoSetupUSBPassthrough
// already tried once -- so a dismissed password prompt isn't shown again on
// every start (the "Install USB Driver" row and the USB "Grant" chip stay
// for a manual retry).
const usbAutoSetupMarker = "usb-autosetup-tried"

// autoSetupUSBPassthrough is the Linux first-start counterpart of the
// package postinst (agent/scripts/linux-usbip-hooks.sh): with broker
// consent on record and the usbip tool or vhci-hcd still missing -- the
// Arch package only optdepends on usbip, and `dpkg -i` skips Recommends --
// it installs them right away (one pkexec prompt) instead of waiting for
// the "Install USB Driver" click, then the passwordless attach grant if the
// package didn't install it either. Once per StateDir; runs in the
// background because pkexec blocks on the user.
func (a *App) autoSetupUSBPassthrough() {
	if runtime.GOOS != "linux" || a.usbBroker == nil || !a.cfg.USBBrokerConsentGiven() {
		return
	}
	st := a.usbBroker.Status()
	if st.VhciDriver && st.AttachGranted {
		return
	}
	if _, err := exec.LookPath("pkexec"); err != nil {
		return
	}
	marker := filepath.Join(a.cfg.StateDir, usbAutoSetupMarker)
	if _, err := os.Stat(marker); err == nil {
		return
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		log.Printf("[usbpass] auto-setup skipped: %v", err)
		return
	}
	go func() {
		if !st.VhciDriver {
			if err := a.usbBroker.InstallDrivers(); err != nil {
				log.Printf("[usbpass] auto-setup: usbip/vhci-hcd install failed: %v", err)
				return
			}
			log.Printf("[usbpass] auto-setup: usbip + vhci-hcd installed")
		}
		if err := a.usbBroker.GrantAttachAccess(); err != nil {
			log.Printf("[usbpass] auto-setup: attach grant failed: %v", err)
			return
		}
		log.Printf("[usbpass] auto-setup: USB passthrough ready")
	}()
}

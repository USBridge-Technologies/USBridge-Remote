//go:build darwin

package remotelock

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Cocoa -framework ApplicationServices

extern void usbridgeRemoteLockSetArmed(int on);
extern int usbridgeRemoteLockInstall(void);
extern void usbridgeRemoteLockRemove(void);
*/
import "C"

import (
	"log"
	"sync"
)

var (
	hookMu    sync.Mutex
	installed bool
)

func setHookEnabled(on bool) {
	if on {
		C.usbridgeRemoteLockSetArmed(1)
		startHook()
		return
	}
	C.usbridgeRemoteLockSetArmed(0)
	stopHook()
}

func setX11Window(uintptr) {}

func startHook() {
	hookMu.Lock()
	defer hookMu.Unlock()
	if installed {
		return
	}
	if C.usbridgeRemoteLockInstall() == 0 {
		log.Printf("[remotelock] CGEventTapCreate failed (Accessibility permission required)")
		return
	}
	installed = true
	log.Printf("[remotelock] blocking injected CGEvent input on this process's windows")
}

func stopHook() {
	hookMu.Lock()
	defer hookMu.Unlock()
	if !installed {
		return
	}
	C.usbridgeRemoteLockRemove()
	installed = false
}

//go:build windows

package usbpass

import (
	"os"
	"testing"
)

func TestUSBIPDriverInstalled_NeedsEveryService(t *testing.T) {
	orig := serviceKeyExists
	t.Cleanup(func() { serviceKeyExists = orig })

	present := map[string]bool{}
	serviceKeyExists = func(name string) bool { return present[name] }

	if USBIPDriverInstalled() {
		t.Fatal("nothing installed, reported installed")
	}
	present["usbip2_ude"] = true
	if USBIPDriverInstalled() {
		t.Fatal("filter missing, reported installed")
	}
	present["usbip2_filter"] = true
	if !USBIPDriverInstalled() {
		t.Fatal("both services present, reported missing")
	}
	// The legacy usbip-win (v0.3) service name alone doesn't count.
	present = map[string]bool{"usbip_vhci_ude": true}
	if USBIPDriverInstalled() {
		t.Fatal("legacy usbip-win service counted as usbip-win2")
	}
}

// TestUSBIPDriverInstalled_Live: USBRIDGE_EXPECT_USBIP=1 or 0.
func TestUSBIPDriverInstalled_Live(t *testing.T) {
	want := os.Getenv("USBRIDGE_EXPECT_USBIP")
	if want == "" {
		t.Skip("set USBRIDGE_EXPECT_USBIP=1 or 0 to check detection on this machine")
	}
	if got := USBIPDriverInstalled(); got != (want == "1") {
		t.Fatalf("USBIPDriverInstalled() = %v, want %v", got, want == "1")
	}
}

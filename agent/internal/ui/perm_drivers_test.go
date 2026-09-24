package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"

	"usbridge_agent/internal/ui/i18n"
	"usbridge_agent/internal/usbpass"
)

func TestUSBPermGranted(t *testing.T) {
	installed := func() bool { return true }
	missing := func() bool { return false }
	granted := usbpass.Status{AttachGranted: true}

	if !usbPermGranted("windows", usbpass.Status{}, installed) {
		t.Error("windows: usbip-win2 installed should tick the chip even with no broker status yet")
	}
	if usbPermGranted("windows", granted, missing) {
		t.Error("windows: a missing usbip-win2 must not tick the chip, whatever the broker says")
	}
	if !usbPermGranted("linux", granted, missing) {
		t.Error("linux: follows the polkit attach grant")
	}
	if usbPermGranted("linux", usbpass.Status{}, installed) {
		t.Error("linux: the Windows driver probe must not leak into the Linux answer")
	}
}

func TestShowUSBDriverRow(t *testing.T) {
	needsDriver := usbpass.Status{ConsentGiven: true, Available: true, VhciDriver: false}
	if showUSBDriverRow("windows", needsDriver) {
		t.Error("windows: the chip's Download button replaces the separate driver row")
	}
	if !showUSBDriverRow("linux", needsDriver) {
		t.Error("linux: driver row shown when consented, available and vhci missing")
	}
	for _, st := range []usbpass.Status{
		{ConsentGiven: false, Available: true},
		{ConsentGiven: true, Available: false},
		{ConsentGiven: true, Available: true, VhciDriver: true},
	} {
		if showUSBDriverRow("linux", st) {
			t.Errorf("linux: driver row shown for %+v", st)
		}
	}
}

func TestPermRequestLabel_EveryLanguage(t *testing.T) {
	t.Cleanup(func() { i18n.Init("en") })
	for _, lang := range []string{"en", "es", "uk"} {
		i18n.Init(lang)
		dl := loc().PermDownload
		if dl == "" || dl == loc().PermGrant {
			t.Errorf("%s: PermDownload = %q, must be set and differ from Grant", lang, dl)
		}
		if got := permRequestLabel("windows", dl); got != dl {
			t.Errorf("%s: windows label = %q, want %q", lang, got, dl)
		}
		if got := permRequestLabel("linux", dl); got != "" {
			t.Errorf("%s: linux keeps the default Grant label, got %q", lang, got)
		}
	}
}

// TestPermStatusChip_RequestLabel drives the real widget: the custom label
// shows only while not granted, and clearing it restores "Grant".
func TestPermStatusChip_RequestLabel(t *testing.T) {
	test.NewApp()
	t.Cleanup(func() { i18n.Init("en") })
	i18n.Init("en")

	requests := 0
	c := newPermStatusChip("Virtual Display", func() { requests++ })
	c.SetRequestLabel(loc().PermDownload)

	c.SetChecked(false)
	if c.btn.Text != "Download" || c.btn.Disabled() {
		t.Fatalf("not granted: button %q disabled=%v, want enabled Download", c.btn.Text, c.btn.Disabled())
	}
	c.SetChecked(true)
	if c.btn.Text != loc().PermGranted || !c.btn.Disabled() {
		t.Fatalf("granted: button %q disabled=%v, want disabled Granted", c.btn.Text, c.btn.Disabled())
	}
	c.SetChecked(false)
	c.SetRequestLabel("")
	if c.btn.Text != loc().PermGrant {
		t.Fatalf("cleared request label: button %q, want Grant", c.btn.Text)
	}

	// Language switch re-applies the label (refreshPermRequestLabels path).
	i18n.Init("uk")
	c.SetRequestLabel(permRequestLabel("windows", loc().PermDownload))
	if c.btn.Text != "Завантажити" {
		t.Fatalf("uk: button %q, want Завантажити", c.btn.Text)
	}

	// A busy chip ignores repeat clicks until requestDone -- so a slow
	// download/UAC install can't be launched twice.
	c.btn.Tapped(&fyne.PointEvent{})
	c.btn.Tapped(&fyne.PointEvent{})
	if requests != 1 {
		t.Fatalf("second tap while busy started another request (%d)", requests)
	}
	c.requestDone()
	c.btn.Tapped(&fyne.PointEvent{})
	if requests != 2 {
		t.Fatalf("tap after requestDone didn't start a request (%d)", requests)
	}
}

// Nil chips (platforms that don't build them) must be safe to relabel.
func TestRefreshPermRequestLabels_NilChips(t *testing.T) {
	(&Window{}).refreshPermRequestLabels()
}

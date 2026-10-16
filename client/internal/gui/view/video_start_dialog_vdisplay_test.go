package view

import (
	"testing"

	"usbridge-client/internal/service"

	"fyne.io/fyne/v2/test"
)

// The host "Virtual display" row: hidden unless the host has one, a draft
// until Start, reverted by Cancel.
func TestHostVirtualDisplayRow(t *testing.T) {
	defer service.SetHostVirtualDisplayEnabled(true)
	win := test.NewWindow(nil)
	defer win.Close()
	vsd := NewVideoStartDialog(win)
	vsd.Configure(nil, 1920, 1080, 60, "20M")

	if vsd.hostVirtualDisplayRow.Visible() {
		t.Fatal("row shown without a capable host")
	}
	vsd.SetHostVirtualDisplayOption(true)
	if !vsd.hostVirtualDisplayRow.Visible() || !vsd.hostVirtualDisplayCheck.Checked {
		t.Fatal("capable host: want the row shown and on by default")
	}

	vsd.hostVirtualDisplayCheck.SetChecked(false)
	vsd.revertLocalOverlayDrafts()
	if !vsd.hostVirtualDisplayCheck.Checked || !service.HostVirtualDisplayEnabled() {
		t.Fatal("cancel must drop the draft")
	}

	vsd.hostVirtualDisplayCheck.SetChecked(false)
	vsd.applyLocalOverlaySettings()
	if service.HostVirtualDisplayEnabled() {
		t.Fatal("start must apply the choice")
	}

	vsd.SetHostVirtualDisplayOption(false)
	if vsd.hostVirtualDisplayRow.Visible() {
		t.Fatal("row still shown for a host without one")
	}
}

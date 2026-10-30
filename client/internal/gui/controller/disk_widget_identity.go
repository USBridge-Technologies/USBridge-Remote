package controller

import (
	"fmt"

	"usbridge-client/internal/gui/view"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// The monitor (EDID) and USB device the KVM shows the PC: pickers in the
// headers of the Video and HID cards (dashboardVideoHeaderPickers,
// dashboardHIDHeaderPickers), only where the KVM has the API -- the EDID one
// only where its HDMI chip can be written (NanoKVM, not the UE revision).

const usbIdentityOwn = "USBridge"

// loadIdentityOptions fetches both; a KVM without them leaves the pickers out.
func (dw *DiskWidget) loadIdentityOptions() {
	client := dw.usbClient
	if client == nil || !knownUSBridgeHardware(dw.agentOS) {
		return
	}
	edid, err := client.GetEDID()
	if err != nil {
		logrus.Debugf("EDID: %v", err)
		edid = nil
	}
	usbID, err := client.GetUSBIdentity()
	if err != nil {
		logrus.Debugf("USB identity: %v", err)
		usbID = nil
	}
	fyne.Do(func() {
		if dw.usbClient != client {
			return
		}
		dw.edidStatus = edid
		dw.usbIdentity = usbID
		dw.refreshDashboard()
	})
}

// refreshIdentityPickers puts the pickers into the card headers.
func (dw *DiskWidget) refreshIdentityPickers(hasKeyboardOrMouse bool) {
	if dw.dashboardVideoHeaderPickers != nil {
		dw.dashboardVideoHeaderPickers.Objects = nil
		if p := dw.newDashboardEDIDPicker(); p != nil {
			dw.dashboardVideoHeaderPickers.Objects = []fyne.CanvasObject{p}
		}
		dw.dashboardVideoHeaderPickers.Refresh()
	}
	if dw.dashboardHIDHeaderPickers != nil {
		dw.dashboardHIDHeaderPickers.Objects = nil
		if hasKeyboardOrMouse {
			dw.dashboardHIDHeaderPickers.Objects = dw.newDashboardUSBIdentityPickers()
		}
		dw.dashboardHIDHeaderPickers.Refresh()
	}
}

// newDashboardEDIDPicker is the monitor picker, nil where the KVM can't change it.
func (dw *DiskWidget) newDashboardEDIDPicker() fyne.CanvasObject {
	s := dw.edidStatus
	if s == nil || !s.Supported || len(s.Presets) == 0 {
		return nil
	}
	labels := make([]string, len(s.Presets))
	selected := s.Presets[0].Label
	for i, p := range s.Presets {
		labels[i] = p.Label
		if p.ID == s.Selected {
			selected = p.Label
		}
	}
	var picker *view.HeaderDropdown
	picker = view.NewDeviceDashboardModePicker(labels, selected, func(label string) {
		if label == selected {
			return
		}
		var id string
		for _, p := range s.Presets {
			if p.Label == label {
				id = p.ID
			}
		}
		view.ShowConfirmYesLeft("Monitor (EDID)",
			fmt.Sprintf("Write the %s EDID into the KVM's HDMI chip?\n\nThe PC sees the new monitor after the KVM is unplugged from power and plugged back in.", label),
			func(ok bool) {
				if !ok {
					picker.SetSelected(selected)
					return
				}
				dw.applyEDID(id)
			}, dw.window)
	})
	picker.OnHover = dw.dashboardVideoHover
	picker.SetDisabled(dw.controlsLocked())
	return picker
}

func (dw *DiskWidget) applyEDID(id string) {
	client := dw.usbClient
	if client == nil {
		return
	}
	go func() {
		s, err := client.SetEDID(id)
		fyne.Do(func() {
			if err != nil {
				view.ShowErrorDialog(err, dw.window)
				dw.refreshDashboard()
				return
			}
			dw.edidStatus = s
			dw.refreshDashboard()
			view.ShowInfoDialog("Monitor (EDID)", "Written. Unplug the KVM from power and plug it back in: the PC then sees the new monitor.", dw.window)
		})
	}()
}

// newDashboardUSBIdentityPickers are the USB device pickers: one for the
// keyboard and mouse, one for when USB audio is in the gadget too (the KVM
// is one USB device, one name covers everything in it).
func (dw *DiskWidget) newDashboardUSBIdentityPickers() []fyne.CanvasObject {
	s := dw.usbIdentity
	if s == nil || len(s.Identities) == 0 {
		return nil
	}
	picker := func(audio bool, current string) fyne.CanvasObject {
		prefix := ""
		if audio {
			prefix = "Audio: "
		}
		labels := []string{prefix + usbIdentityOwn}
		ids := []string{""}
		selected := labels[0]
		for _, id := range s.Identities {
			if id.Audio != audio {
				continue
			}
			labels = append(labels, prefix+id.Label)
			ids = append(ids, id.ID)
			if id.ID == current {
				selected = prefix + id.Label
			}
		}
		if len(labels) == 1 {
			return nil
		}
		p := view.NewDeviceDashboardModePicker(labels, selected, func(label string) {
			for i, l := range labels {
				if l == label && ids[i] != current {
					kbm, aud := s.KBM, s.Audio
					if audio {
						aud = ids[i]
					} else {
						kbm = ids[i]
					}
					dw.applyUSBIdentity(kbm, aud)
				}
			}
		})
		p.OnHover = dw.dashboardHIDHover
		p.SetDisabled(dw.controlsLocked())
		return p
	}
	var out []fyne.CanvasObject
	if p := picker(false, s.KBM); p != nil {
		out = append(out, p)
	}
	if p := picker(true, s.Audio); p != nil {
		out = append(out, p)
	}
	return out
}

func (dw *DiskWidget) applyUSBIdentity(kbm, audio string) {
	client := dw.usbClient
	if client == nil {
		return
	}
	go func() {
		s, err := client.SetUSBIdentity(kbm, audio)
		fyne.Do(func() {
			if err != nil {
				view.ShowErrorDialog(err, dw.window)
			} else {
				dw.usbIdentity = s
				view.ShowInfoDialog("USB device", "Saved. The PC sees the new device the next time the KVM starts its USB devices (switch the keyboard or mouse off and on).", dw.window)
			}
			dw.refreshDashboard()
		})
	}()
}

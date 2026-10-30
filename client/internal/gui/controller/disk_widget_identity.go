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
// usbAudio: the USB audio codec is in the gadget, which then takes the
// audio identity (the board's selectedUSBIdentity): only that picker shows.
func (dw *DiskWidget) refreshIdentityPickers(hasKeyboardOrMouse, usbAudio bool) {
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
			dw.dashboardHIDHeaderPickers.Objects = dw.newDashboardUSBIdentityPickers(usbAudio)
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

// newDashboardUSBIdentityPickers is the USB device picker in use: the
// keyboard-and-mouse one, or with USB audio in the gadget the audio one (the
// KVM is one USB device, one name covers everything in it).
func (dw *DiskWidget) newDashboardUSBIdentityPickers(usbAudio bool) []fyne.CanvasObject {
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
	current := s.KBM
	if usbAudio {
		current = s.Audio
	}
	if p := picker(usbAudio, current); p != nil {
		return []fyne.CanvasObject{p}
	}
	return nil
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

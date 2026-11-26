package api

import "strings"

// GuestRights is the checkbox matrix of what an invited guest may do. It
// mirrors the agent's guestauth.Policy; Policy() builds the wire form.
type GuestRights struct {
	View, Audio, Input, Pen, Gamepad  bool
	ClipToHost, ClipFromHost, Files   bool
	USBHID, USBPen, USBAudio          bool
	USBStorage, USBOther              bool
	Display, DisplayMain, DisplayExcl bool
}

// Capability bits of the agent's guestauth.Cap.
const (
	capView uint32 = 1 << iota
	capAudio
	capInput
	capPenTablet
	capGamepad
	capClipToHost
	capClipFromHost
	capFileTransfer
	capUSB
	capDisks
	capDisplay
	capVideoSettings
	capDisplayMain
	capDisplayExclusive
)

// GuestUSBPolicy and GuestPolicy are the agent's USBPolicy and Policy.
type GuestUSBPolicy struct {
	AllowHID         bool `json:"allow_hid"`
	AllowPenTablet   bool `json:"allow_pen_tablet"`
	AllowAudio       bool `json:"allow_audio"`
	AllowMassStorage bool `json:"allow_mass_storage"`
	AllowOther       bool `json:"allow_other"`
}

type GuestPolicy struct {
	Caps uint32         `json:"caps"`
	USB  GuestUSBPolicy `json:"usb"`
}

// Policy is the wire form of the matrix, built the way the agent's own
// editor does: video settings come with the screen, any USB class needs the
// USB capability and storage also the disks one.
func (r GuestRights) Policy() GuestPolicy {
	var caps uint32
	set := func(on bool, bits uint32) {
		if on {
			caps |= bits
		}
	}
	set(r.View, capView|capVideoSettings)
	set(r.Audio, capAudio)
	set(r.Input, capInput)
	set(r.Pen, capPenTablet)
	set(r.Gamepad, capGamepad)
	set(r.ClipToHost, capClipToHost)
	set(r.ClipFromHost, capClipFromHost)
	set(r.Files, capFileTransfer)
	set(r.USBHID || r.USBPen || r.USBAudio || r.USBOther, capUSB)
	set(r.USBStorage, capDisks|capUSB)
	set(r.Display, capDisplay)
	set(r.DisplayMain, capDisplayMain)
	set(r.DisplayExcl, capDisplayExclusive)
	return GuestPolicy{Caps: caps, USB: GuestUSBPolicy{
		AllowHID: r.USBHID, AllowPenTablet: r.USBPen, AllowAudio: r.USBAudio,
		AllowMassStorage: r.USBStorage, AllowOther: r.USBOther,
	}}
}

// Presets, the same sets the agent offers.
func GuestRightsViewer() GuestRights { return GuestRights{View: true, Audio: true} }

func GuestRightsOperator() GuestRights {
	return GuestRights{View: true, Audio: true, Input: true, ClipToHost: true, ClipFromHost: true, USBHID: true}
}

func GuestRightsTechnician() GuestRights {
	return GuestRights{View: true, Audio: true, Input: true, Pen: true, Gamepad: true,
		ClipToHost: true, ClipFromHost: true, Files: true,
		USBHID: true, USBPen: true, USBAudio: true, USBStorage: true, USBOther: true,
		Display: true, DisplayMain: true, DisplayExcl: true}
}

func GuestRightsVFXTPN() GuestRights {
	return GuestRights{View: true, Audio: true, Input: true, Pen: true, Display: true, USBHID: true, USBPen: true}
}

// Summary names what is allowed, for a guest row.
func (r GuestRights) Summary() string {
	var t []string
	for _, f := range []struct {
		on   bool
		name string
	}{{r.View, "Screen"}, {r.Audio, "Audio"}, {r.Input, "Input"}, {r.Pen, "Pen"}, {r.Gamepad, "Gamepad"},
		{r.ClipToHost || r.ClipFromHost, "Clipboard"}, {r.Files, "Files"},
		{r.USBHID || r.USBPen || r.USBAudio || r.USBOther, "USB"}, {r.USBStorage, "Storage"}, {r.Display, "Displays"}} {
		if f.on {
			t = append(t, f.name)
		}
	}
	return strings.Join(t, " · ")
}

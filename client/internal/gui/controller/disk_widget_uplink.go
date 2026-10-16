package controller

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/platform"
	"usbridge-client/internal/service"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// This client's microphone, MIDI inputs and cameras, played on the streaming host
// (USBridge extension, service.MoonlightUplinkSender): on a NanoKVM the
// target PC gets them as its own USB microphone and USB MIDI port; a PC
// running the agent gets them, and the camera, as USB devices of its own
// (the camera is H.264 over the stream, a USB webcam on the host). They sit
// in the Audio card, one row each, and switch on locally like a captured
// pen tablet (disk_widget_pen.go): there is no agent-side mount step, the
// capture just forwards into whatever stream is up.

const uplinkMicKey = "mic"

func uplinkMIDIKey(id string) string { return "midi:" + id }

const uplinkCameraPrefix = "cam:"

func uplinkCameraKey(id string) string { return uplinkCameraPrefix + id }

// loadUplinkDevices refreshes the MIDI input and camera lists (polled with
// the pen tablets, see startPenTabletPolling).
func (dw *DiskWidget) loadUplinkDevices() {
	inputs := platform.ListMIDIInputs()
	cameras := platform.ListCameras()
	dw.updateUIAsync(func() {
		if midiInputsEqual(dw.midiInputs, inputs) && camerasEqual(dw.cameras, cameras) {
			return
		}
		dw.midiInputs = inputs
		dw.cameras = cameras
		dw.scheduleCombine()
	})
}

func camerasEqual(a, b []platform.CameraInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func midiInputsEqual(a, b []platform.MIDIInputInfo) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// uplinkItems are the rows combineDrives adds to the Audio card; oldMounted
// carries each row's toggle over the rebuild.
func (dw *DiskWidget) uplinkItems(oldMounted map[string]bool) []DriveItem {
	var items []DriveItem
	if service.MicUplinkSupported() {
		items = append(items, DriveItem{
			Name:      i18n.Current.UplinkMicrophone,
			Size:      "N/A",
			Source:    "uplink",
			IsMounted: oldMounted[uplinkMicKey],
			IsUplink:  true,
			UplinkKey: uplinkMicKey,
		})
	}
	for _, in := range dw.midiInputs {
		key := uplinkMIDIKey(in.ID)
		items = append(items, DriveItem{
			Name:      i18n.Current.UplinkMIDIPrefix + in.Name,
			Size:      "N/A",
			Source:    "uplink",
			IsMounted: oldMounted[key],
			IsUplink:  true,
			UplinkKey: key,
		})
	}
	for _, cam := range dw.cameras {
		key := uplinkCameraKey(cam.ID)
		items = append(items, DriveItem{
			Name:      i18n.Current.UplinkCameraPrefix + cam.Name,
			Size:      "N/A",
			Source:    "uplink",
			IsMounted: oldMounted[key],
			IsUplink:  true,
			UplinkKey: key,
		})
	}
	return items
}

// uplinkSender is the stream as service.MoonlightUplinkSender, or nil.
func (dw *DiskWidget) uplinkSender() service.MoonlightUplinkSender {
	if dw.moonlightProvider == nil {
		return nil
	}
	s, _ := dw.moonlightProvider().(service.MoonlightUplinkSender)
	return s
}

var uplinkDropLog atomic.Uint64

// syncUplinkCaptures starts and stops captures to match the switched-on
// rows. UI goroutine only (dw.allDrives/dw.activeUplink); opening a device
// doesn't block on the network, so it runs right here.
func (dw *DiskWidget) syncUplinkCaptures() {
	if dw.activeUplink == nil {
		dw.activeUplink = make(map[string]platform.UplinkCapture)
	}
	wanted := make(map[string]bool)
	for _, d := range dw.allDrives {
		if d.IsUplink && d.IsMounted {
			wanted[d.UplinkKey] = true
		}
	}
	for key, c := range dw.activeUplink {
		if !wanted[key] {
			logrus.Infof("🎤 [UPLINK] stopping %s", key)
			c.Stop()
			delete(dw.activeUplink, key)
		}
	}
	for key := range wanted {
		if _, ok := dw.activeUplink[key]; ok {
			continue
		}
		c, err := dw.startUplinkCapture(key)
		if err != nil {
			logrus.Warnf("🎤 [UPLINK] %s: %v", key, err)
			// Switch the row back off rather than retry on every refresh,
			// and say why once.
			for i := range dw.allDrives {
				if dw.allDrives[i].IsUplink && dw.allDrives[i].UplinkKey == key {
					dw.allDrives[i].IsMounted = false
				}
			}
			dw.showUplinkError(key, err)
			dw.requestDevicesRefresh()
			continue
		}
		logrus.Infof("🎤 [UPLINK] started %s", key)
		dw.activeUplink[key] = c
	}
}

func (dw *DiskWidget) startUplinkCapture(key string) (platform.UplinkCapture, error) {
	if key == uplinkMicKey {
		return service.StartMicUplink(func(seq uint16, opus []byte) {
			s := dw.uplinkSender()
			if s == nil {
				return
			}
			if _, mic := s.UplinkSupport(); !mic || !s.SendMoonlightMic(seq, opus) {
				if uplinkDropLog.Add(1)%500 == 1 {
					logrus.Warnf("🎤 [UPLINK] microphone frame not sent: no stream, or the host has no microphone")
				}
			}
		})
	}
	if camID, ok := strings.CutPrefix(key, uplinkCameraPrefix); ok {
		return dw.startCameraUplink(camID)
	}
	id, ok := strings.CutPrefix(key, "midi:")
	if !ok {
		return nil, fmt.Errorf("unknown uplink %q", key)
	}
	return platform.StartMIDICapture(id, func(data []byte) {
		s := dw.uplinkSender()
		if s == nil {
			return
		}
		if midi, _ := s.UplinkSupport(); !midi || !s.SendMoonlightMIDI(data) {
			logrus.Warnf("🎹 [UPLINK] MIDI not sent: no stream, or the host has no MIDI port")
		}
	})
}

var cameraDropLog atomic.Uint64

// startCameraUplink sends the camera's H.264 over the stream; frames that
// can't go out (no stream, host without a camera, network behind) are
// skipped and the encoder restarts with a keyframe.
func (dw *DiskWidget) startCameraUplink(id string) (platform.UplinkCapture, error) {
	var frame uint16
	return platform.StartCameraCapture(id, func(au []byte, keyframe bool) bool {
		s := dw.uplinkSender()
		if s == nil || !s.CameraUplinkSupported() {
			if cameraDropLog.Add(1)%300 == 1 {
				logrus.Warnf("📷 [UPLINK] camera picture not sent: no stream, or the host shows no camera")
			}
			return false
		}
		frame++
		if !s.SendMoonlightCamera(frame, keyframe, au) {
			if cameraDropLog.Add(1)%300 == 1 {
				logrus.Warnf("📷 [UPLINK] camera picture skipped: the network is behind")
			}
			return false
		}
		return true
	})
}

// showUplinkError tells why a microphone/MIDI/camera row didn't switch on;
// for the microphone with access off in Windows, with a button to the
// privacy page that turns it on.
func (dw *DiskWidget) showUplinkError(key string, err error) {
	if dw.window == nil {
		return
	}
	if key == uplinkMicKey && errors.Is(err, platform.ErrMicAccessDenied) {
		view.ShowErrorDialogWithAction(errors.New(i18n.Current.UplinkMicAccessOff), i18n.Current.UplinkMicOpenSettings, func() {
			if u, perr := url.Parse("ms-settings:privacy-microphone"); perr == nil {
				_ = fyne.CurrentApp().OpenURL(u)
			}
		}, dw.window)
		return
	}
	if key == uplinkMicKey {
		err = fmt.Errorf(i18n.Current.UplinkMicFailed, err)
	}
	view.ShowErrorDialog(err, dw.window)
}

// newUplinkToggle switches a microphone/MIDI row; looked up by key at click
// time, since combineDrives rebuilds dw.allDrives on its own cadence (see
// newPenTabletToggle).
func (dw *DiskWidget) newUplinkToggle(drive DriveItem) *view.DeviceToggle {
	key := drive.UplinkKey
	t := view.NewDeviceToggle(drive.IsMounted, func(on bool) {
		for i := range dw.allDrives {
			if dw.allDrives[i].IsUplink && dw.allDrives[i].UplinkKey == key {
				dw.allDrives[i].IsMounted = on
				break
			}
		}
		dw.syncUplinkCaptures()
		dw.requestDevicesRefresh()
	})
	t.SetEnabled(!dw.controlsLocked())
	return t
}

// stopAllUplinkCaptures stops every capture and switches the rows off;
// called on disconnect (a microphone left recording for no stream).
func (dw *DiskWidget) stopAllUplinkCaptures() {
	for i := range dw.allDrives {
		if dw.allDrives[i].IsUplink {
			dw.allDrives[i].IsMounted = false
		}
	}
	for key, c := range dw.activeUplink {
		c.Stop()
		delete(dw.activeUplink, key)
	}
}

//go:build windows

package usbpass

// Windows HID access for the HID bridge (hidbridge_windows.go).
//
// libusb on Windows can only claim an interface that was first re-bound to
// WinUSB (Zadig), which replaces the device's real driver and breaks it locally.
// A HID-class device does not need that: hid.dll opens each HID "collection"
// (Windows splits one USB HID interface into a COLnn device per top-level
// collection) with a shared handle, reads its input reports and round-trips
// feature/output reports, while the OS driver keeps owning the device.

import (
	"fmt"
	"strings"
	"sync"
	"unsafe"

	"github.com/sirupsen/logrus"
	"golang.org/x/sys/windows"
)

var (
	modHid = windows.NewLazySystemDLL("hid.dll")

	procHidDGetPreparsedData  = modHid.NewProc("HidD_GetPreparsedData")
	procHidDFreePreparsedData = modHid.NewProc("HidD_FreePreparsedData")
	procHidDGetAttributes     = modHid.NewProc("HidD_GetAttributes")
	procHidDGetFeature        = modHid.NewProc("HidD_GetFeature")
	procHidDSetFeature        = modHid.NewProc("HidD_SetFeature")
	procHidDSetOutputReport   = modHid.NewProc("HidD_SetOutputReport")
	procHidDGetInputReport    = modHid.NewProc("HidD_GetInputReport")
	procHidDGetProductString  = modHid.NewProc("HidD_GetProductString")
	procHidDGetMfrString      = modHid.NewProc("HidD_GetManufacturerString")
	procHidDGetSerialString   = modHid.NewProc("HidD_GetSerialNumberString")
	procHidPGetCaps           = modHid.NewProc("HidP_GetCaps")

	modCfgMgr = windows.NewLazySystemDLL("cfgmgr32.dll")

	procCMLocateDevNode = modCfgMgr.NewProc("CM_Locate_DevNodeW")
	procCMGetParent     = modCfgMgr.NewProc("CM_Get_Parent")
	procCMGetDeviceID   = modCfgMgr.NewProc("CM_Get_Device_IDW")
)

// GUID_DEVINTERFACE_HID
const hidInterfaceGUID = "{4d1e55b2-f16f-11cf-88cb-001111000030}"

const (
	hidpStatusSuccess = 0x110000
)

// hidNode is one HID collection device (a "HID\...&COLnn\..." node) that belongs
// to a USB device.
type hidNode struct {
	InstanceID string
	// Interface identifies the USB interface the node belongs to: the instance
	// ID of the "USB\...&MI_nn\..." interface node, or "" for a non-composite
	// device whose HID nodes hang directly under the USB device.
	Interface string
}

func cmDeviceID(devInst uint32) string {
	var buf [512]uint16
	r, _, _ := procCMGetDeviceID.Call(uintptr(devInst), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), 0)
	if r != 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:])
}

func cmLocate(instanceID string) (uint32, bool) {
	p, err := windows.UTF16PtrFromString(instanceID)
	if err != nil {
		return 0, false
	}
	var dn uint32
	r, _, _ := procCMLocateDevNode.Call(uintptr(unsafe.Pointer(&dn)), uintptr(unsafe.Pointer(p)), 0)
	return dn, r == 0
}

// hidNodesOfUSBDevice lists the present HID collection nodes below the USB
// device with the given instance ID, in the order Windows enumerates them
// (which follows the top-level collection order of the report descriptor).
func hidNodesOfUSBDevice(usbInstanceID string) ([]hidNode, error) {
	set, err := windows.SetupDiGetClassDevsEx(nil, "HID", 0, windows.DIGCF_PRESENT|windows.DIGCF_ALLCLASSES, 0, "")
	if err != nil {
		return nil, fmt.Errorf("enumerate HID devices: %w", err)
	}
	defer set.Close()

	var out []hidNode
	for i := 0; ; i++ {
		data, err := windows.SetupDiEnumDeviceInfo(set, i)
		if err != nil {
			break
		}
		id, err := set.DeviceInstanceID(data)
		if err != nil || !strings.HasPrefix(strings.ToUpper(id), "HID\\") {
			continue
		}
		dn, ok := cmLocate(id)
		if !ok {
			continue
		}
		// Walk up: HID node -> [USB interface node ->] USB device.
		iface := ""
		cur := dn
		for depth := 0; depth < 3; depth++ {
			var parent uint32
			r, _, _ := procCMGetParent.Call(uintptr(unsafe.Pointer(&parent)), uintptr(cur), 0)
			if r != 0 {
				break
			}
			pid := cmDeviceID(parent)
			if strings.EqualFold(pid, usbInstanceID) {
				out = append(out, hidNode{InstanceID: id, Interface: iface})
				break
			}
			iface = pid
			cur = parent
		}
	}
	return out, nil
}

func hidPathFromInstanceID(instanceID string) string {
	return `\\?\` + strings.ReplaceAll(instanceID, `\`, "#") + "#" + hidInterfaceGUID
}

// winHIDCollection is one opened COLnn device.
type winHIDCollection struct {
	path string
	h    windows.Handle
	pp   *ppData
	ids  [ppReportTypes]map[uint8]bool

	inLen, outLen, featLen int // report byte lengths including the report ID byte

	// queryOnly: Windows refused read access (pen, digitizer, mouse and keyboard
	// collections are held exclusively by the OS input stack), so the handle can
	// only query caps/preparsed data and feature reports. Its input reports come
	// from Raw Input instead (hidrawinput_windows.go).
	queryOnly bool
	instance  string // HID instance ID this collection was opened from

	// Serialises the HidD_* calls on this handle.
	callMu sync.Mutex
}

func openHIDCollection(instanceID string) (*winHIDCollection, error) {
	path := hidPathFromInstanceID(instanceID)
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var h windows.Handle
	// Prefer read+write (input reports and output/feature writes); fall back to
	// read-only. System keyboards/mice are opened exclusively by Windows and
	// refuse both, which surfaces as an error the caller treats as "not
	// bridgeable through HID".
	queryOnly := false
	for i, access := range []uint32{windows.GENERIC_READ | windows.GENERIC_WRITE, windows.GENERIC_READ, 0} {
		h, err = windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			queryOnly = i == 2
			break
		}
	}
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", instanceID, err)
	}
	c := &winHIDCollection{path: path, h: h, queryOnly: queryOnly, instance: instanceID}
	if err := c.loadPreparsed(); err != nil {
		windows.CloseHandle(h)
		return nil, fmt.Errorf("%s: %w", instanceID, err)
	}
	return c, nil
}

func (c *winHIDCollection) loadPreparsed() error {
	var ppPtr unsafe.Pointer
	r, _, e := procHidDGetPreparsedData.Call(uintptr(c.h), uintptr(unsafe.Pointer(&ppPtr)))
	if r == 0 {
		return fmt.Errorf("HidD_GetPreparsedData: %v", e)
	}
	defer procHidDFreePreparsedData.Call(uintptr(ppPtr))

	// The blob's size is not returned by the API; read the header first, derive
	// the size from it, then copy exactly that much.
	hdr := unsafe.Slice((*byte)(ppPtr), ppHeaderSize)
	size, err := preparsedBlobSize(hdr)
	if err != nil {
		return err
	}
	blob := append([]byte(nil), unsafe.Slice((*byte)(ppPtr), size)...)
	pp, err := parsePreparsed(blob)
	if err != nil {
		return err
	}
	c.pp = pp
	c.ids = pp.reportIDs()

	// HIDP_CAPS: InputReportByteLength at 4, OutputReportByteLength at 6,
	// FeatureReportByteLength at 8.
	var caps [64]byte
	st, _, _ := procHidPGetCaps.Call(uintptr(ppPtr), uintptr(unsafe.Pointer(&caps[0])))
	if st != hidpStatusSuccess {
		return fmt.Errorf("HidP_GetCaps: status %#x", st)
	}
	c.inLen = int(caps[4]) | int(caps[5])<<8
	c.outLen = int(caps[6]) | int(caps[7])<<8
	c.featLen = int(caps[8]) | int(caps[9])<<8
	return nil
}

func (c *winHIDCollection) close() {
	if c.h != 0 {
		windows.CloseHandle(c.h)
		c.h = 0
	}
}

// hidStringProc reads a HidD_Get*String value.
func (c *winHIDCollection) hidString(proc *windows.LazyProc) string {
	var buf [256]uint16
	r, _, _ := proc.Call(uintptr(c.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:])
}

// attributes returns VID, PID and the device version (bcdDevice).
func (c *winHIDCollection) attributes() (vid, pid, ver uint16, ok bool) {
	var a struct {
		Size      uint32
		VendorID  uint16
		ProductID uint16
		Version   uint16
	}
	a.Size = uint32(unsafe.Sizeof(a))
	r, _, _ := procHidDGetAttributes.Call(uintptr(c.h), uintptr(unsafe.Pointer(&a)))
	return a.VendorID, a.ProductID, a.Version, r != 0
}

// readLoop delivers input reports until stop is closed or the handle fails. The
// report ID byte is always present in buf[0] on Windows (0 when the device
// uses no report IDs); onReport receives the report exactly as it goes on the
// wire (ID byte only when the device uses report IDs).
func (c *winHIDCollection) readLoop(stop <-chan struct{}, hasReportIDs bool, onReport func([]byte)) {
	if c.inLen <= 0 {
		return
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		logrus.Warnf("usbpass: hid read %s: CreateEvent: %v", c.path, err)
		return
	}
	defer windows.CloseHandle(ev)
	buf := make([]byte, c.inLen)
	consecutive := 0
	for {
		select {
		case <-stop:
			return
		default:
		}
		ov := windows.Overlapped{HEvent: ev}
		windows.ResetEvent(ev)
		var n uint32
		err := windows.ReadFile(c.h, buf, &n, &ov)
		if err == windows.ERROR_IO_PENDING {
			for {
				w, _ := windows.WaitForSingleObject(ev, 100)
				if w == windows.WAIT_OBJECT_0 {
					break
				}
				select {
				case <-stop:
					windows.CancelIoEx(c.h, &ov)
					windows.GetOverlappedResult(c.h, &ov, &n, true)
					return
				default:
				}
			}
			err = windows.GetOverlappedResult(c.h, &ov, &n, false)
		}
		if err != nil {
			consecutive++
			if consecutive > 20 {
				logrus.Warnf("usbpass: hid read %s giving up: %v", c.path, err)
				return
			}
			continue
		}
		consecutive = 0
		if n == 0 {
			continue
		}
		rep := append([]byte(nil), buf[:n]...)
		if !hasReportIDs && len(rep) > 0 {
			rep = rep[1:]
		}
		onReport(rep)
	}
}

// setReport sends an output or feature report. data is in wire form (report ID
// first when the device uses report IDs).
func (c *winHIDCollection) setReport(typ uint8, id uint8, data []byte, hasReportIDs bool) error {
	c.callMu.Lock()
	defer c.callMu.Unlock()
	want := c.featLen
	proc := procHidDSetFeature
	if typ == 2 { // HID report type: 1 input, 2 output, 3 feature
		want = c.outLen
		proc = procHidDSetOutputReport
	}
	buf := make([]byte, max(want, len(data)+1))
	if hasReportIDs {
		copy(buf, data)
	} else {
		copy(buf[1:], data)
	}
	buf[0] = id
	r, _, e := proc.Call(uintptr(c.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(want))
	if r == 0 {
		return fmt.Errorf("set report type %d id %d: %v", typ, id, e)
	}
	return nil
}

// getReport reads an input or feature report, returned in wire form.
func (c *winHIDCollection) getReport(typ uint8, id uint8, length int, hasReportIDs bool) ([]byte, error) {
	c.callMu.Lock()
	defer c.callMu.Unlock()
	want := c.featLen
	proc := procHidDGetFeature
	if typ == 1 {
		want = c.inLen
		proc = procHidDGetInputReport
	}
	if want <= 0 {
		return nil, fmt.Errorf("device has no report of type %d", typ)
	}
	buf := make([]byte, want)
	buf[0] = id
	r, _, e := proc.Call(uintptr(c.h), uintptr(unsafe.Pointer(&buf[0])), uintptr(want))
	if r == 0 {
		return nil, fmt.Errorf("get report type %d id %d: %v", typ, id, e)
	}
	if !hasReportIDs {
		buf = buf[1:]
	}
	if length >= 0 && len(buf) > length {
		buf = buf[:length]
	}
	return buf, nil
}

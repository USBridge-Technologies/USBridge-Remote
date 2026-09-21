//go:build windows

package platform

import (
	"fmt"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// A thin HID layer for talking *to* a DirectInput gamepad: WinMM and
// DirectInput only read a pad, so vibrating a PlayStation-layout pad means
// writing its HID output report directly. The pad must be bound to the inbox
// HID driver (a WinUSB binding hides it from HID altogether).

var (
	modHidOut                = windows.NewLazySystemDLL("hid.dll")
	procHidOutGetHidGuid     = modHidOut.NewProc("HidD_GetHidGuid")
	procHidOutGetAttributes  = modHidOut.NewProc("HidD_GetAttributes")
	procHidOutGetPreparsed   = modHidOut.NewProc("HidD_GetPreparsedData")
	procHidOutFreePreparsed  = modHidOut.NewProc("HidD_FreePreparsedData")
	procHidOutGetCaps        = modHidOut.NewProc("HidP_GetCaps")
	procHidOutSetOutputRep   = modHidOut.NewProc("HidD_SetOutputReport")
	modCfgMgr                = windows.NewLazySystemDLL("cfgmgr32.dll")
	procCMInterfaceListSize  = modCfgMgr.NewProc("CM_Get_Device_Interface_List_SizeW")
	procCMInterfaceListW     = modCfgMgr.NewProc("CM_Get_Device_Interface_ListW")
	hidpStatusSuccessOutPart = uintptr(0x00110000)
)

// hidCollection is one top-level HID collection of a pad (a composite HID
// interface shows up as several).
type hidCollection struct {
	Path      string
	VendorID  uint16
	ProductID uint16
	Usage     uint16
	UsagePage uint16
	// Report lengths in bytes, including the leading report id byte.
	InputLen, OutputLen, FeatureLen uint16
}

func hidInterfacePaths() ([]string, error) {
	var guid windows.GUID
	procHidOutGetHidGuid.Call(uintptr(unsafe.Pointer(&guid)))
	for attempt := 0; attempt < 3; attempt++ {
		var size uint32
		if r, _, _ := procCMInterfaceListSize.Call(uintptr(unsafe.Pointer(&size)), uintptr(unsafe.Pointer(&guid)), 0, 0); r != 0 {
			return nil, fmt.Errorf("CM_Get_Device_Interface_List_Size: cr=%#x", r)
		}
		buf := make([]uint16, size)
		r, _, _ := procCMInterfaceListW.Call(uintptr(unsafe.Pointer(&guid)), 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(size), 0)
		if r == 0x1a { // CR_BUFFER_SMALL: the list grew between the two calls
			continue
		}
		if r != 0 {
			return nil, fmt.Errorf("CM_Get_Device_Interface_List: cr=%#x", r)
		}
		var paths []string
		for start := 0; start < len(buf); {
			end := start
			for end < len(buf) && buf[end] != 0 {
				end++
			}
			if end == start {
				break
			}
			paths = append(paths, windows.UTF16ToString(buf[start:end]))
			start = end + 1
		}
		return paths, nil
	}
	return nil, fmt.Errorf("HID interface list kept changing")
}

func openHIDPath(path string, write bool) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return windows.InvalidHandle, err
	}
	access := uint32(0)
	if write {
		access = windows.GENERIC_READ | windows.GENERIC_WRITE
	}
	return windows.CreateFile(p, access, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

// hidCollections lists the HID collections of the pad with this USB id.
func hidCollections(vid, pid uint16) ([]hidCollection, error) {
	paths, err := hidInterfacePaths()
	if err != nil {
		return nil, err
	}
	want := fmt.Sprintf("vid_%04x&pid_%04x", vid, pid)
	var out []hidCollection
	for _, path := range paths {
		if !strings.Contains(strings.ToLower(path), want) {
			continue
		}
		// Querying needs no access rights, so this works while a game holds the pad.
		h, err := openHIDPath(path, false)
		if err != nil {
			continue
		}
		c := hidCollection{Path: path}
		var attr struct {
			Size      uint32
			VendorID  uint16
			ProductID uint16
			Version   uint16
		}
		attr.Size = uint32(unsafe.Sizeof(attr))
		if r, _, _ := procHidOutGetAttributes.Call(uintptr(h), uintptr(unsafe.Pointer(&attr))); r != 0 {
			c.VendorID, c.ProductID = attr.VendorID, attr.ProductID
		}
		var pp unsafe.Pointer
		if r, _, _ := procHidOutGetPreparsed.Call(uintptr(h), uintptr(unsafe.Pointer(&pp))); r != 0 {
			// HIDP_CAPS: Usage@0, UsagePage@2, InputReportByteLength@4,
			// OutputReportByteLength@6, FeatureReportByteLength@8.
			var caps [64]byte
			if st, _, _ := procHidOutGetCaps.Call(uintptr(pp), uintptr(unsafe.Pointer(&caps[0]))); st == hidpStatusSuccessOutPart {
				u16 := func(o int) uint16 { return uint16(caps[o]) | uint16(caps[o+1])<<8 }
				c.Usage, c.UsagePage = u16(0), u16(2)
				c.InputLen, c.OutputLen, c.FeatureLen = u16(4), u16(6), u16(8)
			}
			procHidOutFreePreparsed.Call(uintptr(pp))
		}
		windows.CloseHandle(h)
		out = append(out, c)
	}
	return out, nil
}

// hidWriter writes output reports to one HID collection.
type hidWriter struct {
	h windows.Handle
}

func openHIDWriter(path string) (*hidWriter, error) {
	h, err := openHIDPath(path, true)
	if err != nil {
		return nil, err
	}
	return &hidWriter{h: h}, nil
}

// write sends one output report (report id first). It uses WriteFile, which is
// the interrupt-OUT path, and falls back to HidD_SetOutputReport (a control
// transfer) for a pad without an OUT endpoint.
func (w *hidWriter) write(report []byte) error {
	var n uint32
	if err := windows.WriteFile(w.h, report, &n, nil); err == nil {
		return nil
	}
	if r, _, e := procHidOutSetOutputRep.Call(uintptr(w.h), uintptr(unsafe.Pointer(&report[0])), uintptr(len(report))); r == 0 {
		return fmt.Errorf("output report write failed: %v", e)
	}
	return nil
}

func (w *hidWriter) close() {
	if w.h != 0 && w.h != windows.InvalidHandle {
		windows.CloseHandle(w.h)
		w.h = 0
	}
}

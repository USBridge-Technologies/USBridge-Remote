//go:build windows

package usbpass

import (
	"encoding/binary"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Direct USB control transfers to a device another driver owns, through the
// hub that carries it (the IOCTLs USBView uses). No driver swap: the device stays
// with its HID driver. This is how the bridge reads what hid.dll hides -- the
// device's true HID report descriptor and its feature reports.

var guidDevInterfaceUSBHub = windows.GUID{Data1: 0xf18a0e88, Data2: 0xc30c, Data3: 0x11d0, Data4: [8]byte{0x88, 0x15, 0x00, 0xa0, 0xc9, 0x06, 0xbe, 0xd8}}

const (
	ioctlUSBGetNodeInformation       = 0x00220408
	ioctlUSBGetNodeConnectionInfoEx  = 0x00220448
	ioctlUSBGetDescriptorFromNodeCon = 0x00220410
)

// usbHubPort is one downstream port of one hub carrying a device.
type usbHubPort struct {
	hubPath string
	port    uint32
}

// findUSBHubPort locates the hub port a USB device (vid/pid) is plugged into.
func findUSBHubPort(vid, pid uint16) (usbHubPort, error) {
	hubs, err := windows.CM_Get_Device_Interface_List("", &guidDevInterfaceUSBHub, windows.CM_GET_DEVICE_INTERFACE_LIST_PRESENT)
	if err != nil {
		return usbHubPort{}, fmt.Errorf("list USB hubs: %w", err)
	}
	for _, path := range hubs {
		h, err := openUSBHub(path)
		if err != nil {
			continue
		}
		ports := usbHubPortCount(h)
		for p := uint32(1); p <= ports; p++ {
			info := make([]byte, 35+30*16)
			binary.LittleEndian.PutUint32(info, p)
			var n uint32
			if err := windows.DeviceIoControl(h, ioctlUSBGetNodeConnectionInfoEx, &info[0], uint32(len(info)), &info[0], uint32(len(info)), &n, nil); err != nil {
				continue
			}
			v := binary.LittleEndian.Uint16(info[4+8:])
			d := binary.LittleEndian.Uint16(info[4+10:])
			if v == vid && d == pid {
				windows.CloseHandle(h)
				return usbHubPort{hubPath: path, port: p}, nil
			}
		}
		windows.CloseHandle(h)
	}
	return usbHubPort{}, fmt.Errorf("USB device %04x:%04x not found on any hub", vid, pid)
}

func openUSBHub(path string) (windows.Handle, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	return windows.CreateFile(p, windows.GENERIC_WRITE, windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING, 0, 0)
}

func usbHubPortCount(h windows.Handle) uint32 {
	buf := make([]byte, 76)
	var n uint32
	if err := windows.DeviceIoControl(h, ioctlUSBGetNodeInformation, &buf[0], uint32(len(buf)), &buf[0], uint32(len(buf)), &n, nil); err != nil {
		return 0
	}
	return uint32(buf[4+2]) // USB_HUB_DESCRIPTOR.bNumberOfPorts
}

// control sends a device-to-host control request and returns the data stage.
func (p usbHubPort) control(bmRequest, bRequest uint8, wValue, wIndex uint16, wLength int) ([]byte, error) {
	h, err := openUSBHub(p.hubPath)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(h)
	buf := make([]byte, 12+wLength)
	binary.LittleEndian.PutUint32(buf[0:], p.port)
	buf[4], buf[5] = bmRequest, bRequest
	binary.LittleEndian.PutUint16(buf[6:], wValue)
	binary.LittleEndian.PutUint16(buf[8:], wIndex)
	binary.LittleEndian.PutUint16(buf[10:], uint16(wLength))
	var n uint32
	if err := windows.DeviceIoControl(h, ioctlUSBGetDescriptorFromNodeCon, &buf[0], uint32(len(buf)), &buf[0], uint32(len(buf)), &n, nil); err != nil {
		return nil, err
	}
	if n < 12 {
		return nil, fmt.Errorf("short reply (%d bytes)", n)
	}
	return append([]byte(nil), buf[12:n]...), nil
}

var _ = unsafe.Sizeof(0)

// wacomModelForDevice returns the model to export a Wacom tablet from: a captured
// one, else one built from the descriptor database around the device's own
// device/configuration descriptors (read through its hub), or nil when the tablet
// is unknown (it is then bridged the generic way).
func wacomModelForDevice(vid, pid, bcd uint16, mfr, prod, serial string) *wacomModel {
	if m := wacomModelFor(vid, pid); m != nil {
		return m
	}
	if vid != wacomVendorID {
		return nil
	}
	var dd, cd []byte
	if hp, err := findUSBHubPort(vid, pid); err == nil {
		dd, _ = hp.control(0x80, 0x06, 0x0100, 0, 18)
		if h, _ := hp.control(0x80, 0x06, 0x0200, 0, 9); len(h) == 9 {
			cd, _ = hp.control(0x80, 0x06, 0x0200, 0, int(h[2])|int(h[3])<<8)
		}
	}
	return wacomModelFromDB(vid, pid, bcd, mfr, prod, serial, dd, cd)
}

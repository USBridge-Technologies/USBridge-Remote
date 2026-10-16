//go:build windows

package audio

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"

	"usbridge_agent/internal/api"
)

// On Windows the sinks are the active playback endpoints (MMDevice API), named by
// their endpoint ID ("{0.0.0.00000000}.{guid}") with the friendly name as the
// description. Every stream host captures the default one (RustShine's WASAPI
// loopback always does), so picking a sink makes it the default playback device
// (SetDefaultSink) -- the Windows counterpart of the PulseAudio default sink.

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoUninitialize   = ole32.NewProc("CoUninitialize")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procCoTaskMemFree    = ole32.NewProc("CoTaskMemFree")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

var (
	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	// IPolicyConfig: undocumented but stable since Windows 7; what the Sound
	// control panel itself uses to set the default device.
	clsidPolicyConfigClient = windows.GUID{Data1: 0x870AF99C, Data2: 0x171D, Data3: 0x4F9E, Data4: [8]byte{0xAF, 0x0D, 0xE6, 0x3D, 0xF4, 0x0C, 0x2B, 0xC9}}
	iidIPolicyConfig        = windows.GUID{Data1: 0xF8679F50, Data2: 0x850A, Data3: 0x41CF, Data4: [8]byte{0x9C, 0x72, 0x43, 0x0F, 0x29, 0x02, 0x90, 0xC8}}
	// PKEY_Device_FriendlyName.
	pkeyFriendlyName = propertyKey{FmtID: windows.GUID{Data1: 0xA45C254E, Data2: 0xDF1C, Data3: 0x4EFD, Data4: [8]byte{0x80, 0x20, 0x67, 0xD1, 0x46, 0xA8, 0x50, 0xE0}}, PID: 14}
)

const (
	clsctxAll            = 0x17
	coinitMultithreaded  = 0x0
	eRender              = 0
	eConsole             = 0
	eMultimedia          = 1
	eCommunications      = 2
	deviceStateActive    = 0x1
	stgmRead             = 0
	vtLPWSTR             = 31
	rpcEChangedMode      = 0x80010106
	sFalse               = 1
	vtblEnumEndpoints    = 3 // IMMDeviceEnumerator
	vtblGetDefault       = 4
	vtblCollectionCount  = 3 // IMMDeviceCollection
	vtblCollectionItem   = 4
	vtblDeviceOpenProps  = 4 // IMMDevice
	vtblDeviceGetID      = 5
	vtblPropsGetValue    = 5 // IPropertyStore
	vtblSetDefaultTarget = 13
	vtblRelease          = 2
)

type propertyKey struct {
	FmtID windows.GUID
	PID   uint32
}

// propVariant is PROPVARIANT, 24 bytes on 64-bit: vt, three reserved words, then
// the value union (a string pointer for VT_LPWSTR).
type propVariant struct {
	VT  uint16
	_   [3]uint16
	Val *uint16
	_   uintptr
}

// comInterface is a COM object as seen through its interface pointer: a
// pointer to its vtable.
type comInterface struct {
	vtbl *[32]uintptr
}

// comObject calls COM methods by vtable index.
type comObject = *comInterface

func call(o comObject, index int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(o.vtbl[index], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return r
}

func release(o comObject) {
	if o != nil {
		call(o, vtblRelease)
	}
}

func hresult(what string, r uintptr) error {
	if int32(r) < 0 {
		return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(r))
	}
	return nil
}

// withCOM runs f on a locked thread with COM initialized.
func withCOM(f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded)
	if r == 0 || r == sFalse {
		defer procCoUninitialize.Call()
	} else if r != rpcEChangedMode {
		return hresult("CoInitializeEx", r)
	}
	return f()
}

func createInstance(clsid, iid *windows.GUID) (comObject, error) {
	var obj comObject
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxAll, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))
	if err := hresult("CoCreateInstance", r); err != nil {
		return nil, err
	}
	return obj, nil
}

func deviceID(dev comObject) (string, error) {
	var p *uint16
	if err := hresult("IMMDevice.GetId", call(dev, vtblDeviceGetID, uintptr(unsafe.Pointer(&p)))); err != nil {
		return "", err
	}
	defer procCoTaskMemFree.Call(uintptr(unsafe.Pointer(p)))
	return windows.UTF16PtrToString(p), nil
}

func friendlyName(dev comObject) string {
	var store comObject
	if hresult("OpenPropertyStore", call(dev, vtblDeviceOpenProps, stgmRead, uintptr(unsafe.Pointer(&store)))) != nil {
		return ""
	}
	defer release(store)
	var pv propVariant
	if hresult("GetValue", call(store, vtblPropsGetValue, uintptr(unsafe.Pointer(&pkeyFriendlyName)), uintptr(unsafe.Pointer(&pv)))) != nil {
		return ""
	}
	defer procPropVariantClear.Call(uintptr(unsafe.Pointer(&pv)))
	if pv.VT != vtLPWSTR || pv.Val == nil {
		return ""
	}
	return windows.UTF16PtrToString(pv.Val)
}

func defaultID(enum comObject) string {
	var dev comObject
	if hresult("GetDefaultAudioEndpoint", call(enum, vtblGetDefault, eRender, eConsole, uintptr(unsafe.Pointer(&dev)))) != nil {
		return ""
	}
	defer release(dev)
	id, _ := deviceID(dev)
	return id
}

// ListSinks lists the active playback endpoints.
func ListSinks() ([]api.AudioSink, error) {
	var out []api.AudioSink
	err := withCOM(func() error {
		enum, err := createInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
		if err != nil {
			return err
		}
		defer release(enum)
		def := defaultID(enum)
		var coll comObject
		if err := hresult("EnumAudioEndpoints", call(enum, vtblEnumEndpoints, eRender, deviceStateActive, uintptr(unsafe.Pointer(&coll)))); err != nil {
			return err
		}
		defer release(coll)
		var n uint32
		if err := hresult("GetCount", call(coll, vtblCollectionCount, uintptr(unsafe.Pointer(&n)))); err != nil {
			return err
		}
		for i := uint32(0); i < n; i++ {
			var dev comObject
			if hresult("Item", call(coll, vtblCollectionItem, uintptr(i), uintptr(unsafe.Pointer(&dev)))) != nil {
				continue
			}
			id, err := deviceID(dev)
			name := friendlyName(dev)
			release(dev)
			if err != nil || id == "" {
				continue
			}
			if name == "" {
				name = id
			}
			out = append(out, api.AudioSink{Name: id, Description: name, Default: id == def})
		}
		return nil
	})
	return out, err
}

// DefaultSink is the default playback endpoint's ID.
func DefaultSink() (string, error) {
	var id string
	err := withCOM(func() error {
		enum, err := createInstance(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
		if err != nil {
			return err
		}
		defer release(enum)
		id = defaultID(enum)
		if id == "" {
			return fmt.Errorf("no default playback device")
		}
		return nil
	})
	return id, err
}

// SetDefaultSink makes the playback endpoint with this ID the default one, for
// every role (console, multimedia, communications), as the Sound control panel does.
func SetDefaultSink(id string) error {
	wid, err := windows.UTF16PtrFromString(id)
	if err != nil {
		return err
	}
	return withCOM(func() error {
		pc, err := createInstance(&clsidPolicyConfigClient, &iidIPolicyConfig)
		if err != nil {
			return err
		}
		defer release(pc)
		for _, role := range []uintptr{eConsole, eMultimedia, eCommunications} {
			if err := hresult("IPolicyConfig.SetDefaultEndpoint", call(pc, vtblSetDefaultTarget, uintptr(unsafe.Pointer(wid)), role)); err != nil {
				return err
			}
		}
		return nil
	})
}

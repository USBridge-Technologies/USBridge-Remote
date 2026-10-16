//go:build windows

package netutil

import (
	"fmt"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows keeps scanning other Wi-Fi channels in the background while
// connected (location, the network flyout, any app asking for the network
// list); each scan takes the radio off the AP's channel for a few seconds
// in bursts -- with 6 GHz enabled there are a lot of channels to visit.
// Confirmed live on an AX210: pinging the router every 30 ms showed
// multi-second windows of 40-180 ms round trips and lost packets every
// 20-30 s, which on a stream are exactly the periodic stutters. With the
// interface in media streaming mode and background scans off the p99 went
// 51 -> 20 ms and the bursts were gone.
//
// On a client moonlight-common-c already sets media streaming mode
// (enterLowLatencyMode) but leaves background scans on. Both settings
// belong to the WLAN client handle that set them: Windows undoes them when
// the handle closes (or the process exits), so the handle is held for the
// whole stream, on the host (agent) and the client alike.

var (
	modWlanapi             = windows.NewLazySystemDLL("wlanapi.dll")
	procWlanOpenHandle     = modWlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle    = modWlanapi.NewProc("WlanCloseHandle")
	procWlanEnumInterfaces = modWlanapi.NewProc("WlanEnumInterfaces")
	procWlanSetInterface   = modWlanapi.NewProc("WlanSetInterface")
	procWlanFreeMemory     = modWlanapi.NewProc("WlanFreeMemory")
)

const (
	wlanIntfOpcodeBackgroundScanEnabled = 2
	wlanIntfOpcodeMediaStreamingMode    = 3
	wlanInterfaceStateConnected         = 1
	// WLAN_INTERFACE_INFO: GUID, WCHAR[256] description, state.
	wlanInterfaceInfoSize = 16 + 256*2 + 4
)

var wlanStreaming struct {
	mu     sync.Mutex
	handle uintptr
	tuned  map[windows.GUID]bool
}

func wlanOpen() (uintptr, error) {
	if err := modWlanapi.Load(); err != nil {
		return 0, nil // Server SKU without the wireless feature
	}
	var negotiated uint32
	var h uintptr
	r, _, _ := procWlanOpenHandle.Call(2, 0, uintptr(unsafe.Pointer(&negotiated)), uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		if windows.Errno(r) == windows.ERROR_SERVICE_NOT_ACTIVE {
			return 0, nil // WLAN AutoConfig off: no Wi-Fi in use
		}
		return 0, fmt.Errorf("WlanOpenHandle: %w", windows.Errno(r))
	}
	return h, nil
}

// wlanInterfaces lists every interface on handle with whether it is
// connected.
func wlanInterfaces(handle uintptr) (map[windows.GUID]bool, error) {
	var list *byte
	r, _, _ := procWlanEnumInterfaces.Call(handle, 0, uintptr(unsafe.Pointer(&list)))
	if r != 0 {
		return nil, fmt.Errorf("WlanEnumInterfaces: %w", windows.Errno(r))
	}
	defer procWlanFreeMemory.Call(uintptr(unsafe.Pointer(list)))

	count := *(*uint32)(unsafe.Pointer(list))
	out := make(map[windows.GUID]bool, count)
	for i := uintptr(0); i < uintptr(count); i++ {
		info := unsafe.Add(unsafe.Pointer(list), 8+i*wlanInterfaceInfoSize)
		state := *(*uint32)(unsafe.Add(info, 16+256*2))
		out[*(*windows.GUID)(info)] = state == wlanInterfaceStateConnected
	}
	return out, nil
}

// WLANConnected reports whether any Wi-Fi interface is connected.
func WLANConnected() bool {
	h, err := wlanOpen()
	if err != nil || h == 0 {
		return false
	}
	defer procWlanCloseHandle.Call(h, 0)
	ifaces, err := wlanInterfaces(h)
	if err != nil {
		return false
	}
	for _, connected := range ifaces {
		if connected {
			return true
		}
	}
	return false
}

// SetWLANStreamingMode(true) puts every connected Wi-Fi interface into
// media streaming mode with background scans off and keeps them so until
// SetWLANStreamingMode(false). Safe to call repeatedly: an interface that
// connects later is picked up on the next call. No Wi-Fi (or no WLAN
// service) is not an error.
func SetWLANStreamingMode(on bool) error {
	wlanStreaming.mu.Lock()
	defer wlanStreaming.mu.Unlock()
	if !on {
		if wlanStreaming.handle != 0 {
			procWlanCloseHandle.Call(wlanStreaming.handle, 0)
			wlanStreaming.handle = 0
			wlanStreaming.tuned = nil
		}
		return nil
	}
	if wlanStreaming.handle == 0 {
		h, err := wlanOpen()
		if err != nil || h == 0 {
			return err
		}
		wlanStreaming.handle = h
		wlanStreaming.tuned = map[windows.GUID]bool{}
	}
	ifaces, err := wlanInterfaces(wlanStreaming.handle)
	if err != nil {
		return err
	}
	var firstErr error
	for guid, connected := range ifaces {
		if !connected {
			delete(wlanStreaming.tuned, guid)
			continue
		}
		if wlanStreaming.tuned[guid] {
			continue
		}
		if err := wlanSetBool(&guid, wlanIntfOpcodeMediaStreamingMode, true); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("media streaming mode: %w", err)
		}
		if err := wlanSetBool(&guid, wlanIntfOpcodeBackgroundScanEnabled, false); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("background scan off: %w", err)
		}
		wlanStreaming.tuned[guid] = true
	}
	return firstErr
}

func wlanSetBool(guid *windows.GUID, opcode uintptr, value bool) error {
	var v uint32
	if value {
		v = 1
	}
	r, _, _ := procWlanSetInterface.Call(wlanStreaming.handle, uintptr(unsafe.Pointer(guid)), opcode, 4, uintptr(unsafe.Pointer(&v)), 0)
	if r != 0 {
		return windows.Errno(r)
	}
	return nil
}

//go:build android

package usbpass

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>
#include <stdint.h>

int jni_usbHasPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *deviceName);
void jni_usbRequestPermission(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *deviceName);
char* jni_usbClaim(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, const char *deviceName);
int jni_usbControlTransfer(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, long long connHandle,
                            int requestType, int request, int value, int index,
                            void *buf, int len, int timeoutMs);
int jni_usbBulkTransfer(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, long long connHandle, long long ifaceHandle,
                         int epAddr, void *buf, int len, int timeoutMs);
void jni_usbClose(uintptr_t jni_env_ptr, uintptr_t ctx_ptr, long long connHandle, long long ifaceHandle);
*/
import "C"

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"
)

// TryClaimGousb is Android's stand-in for backend_gousb.go's libusb claim
// (see that file's `!android` build tag) -- same call site in session.go,
// no caller changes needed, same naming-to-match convention as
// hidbridge_darwin.go's TryClaimGousb.
//
// Unlike macOS, Android's UsbDeviceConnection.claimInterface(iface, force)
// can evict a kernel driver already bound to the interface (see
// usb_claim_jni_android.c's doc comment) -- so this is a real raw-endpoint
// claim like the Linux/Windows gousb path, not the HID-only synthetic
// descriptor mac has to fall back to. It is not yet hardened the way
// backend_gousb.go's HandleBulk is (BOT CBW/CSW cycle serialization,
// STALL+babble recovery, xHCI halt-bookkeeping workarounds -- all called
// out in that file's comments as lessons learned against a real flash
// stick under Windows). Mass-storage-class devices are therefore
// best-effort here; HID/gamepad/serial-class devices, which don't depend
// on any of that, are the well-supported case.
func TryClaimGousb(dev *ExportedDevice) error {
	deviceName := dev.InstanceID // list_usb_android.go sets this to UsbDevice.getDeviceName(), the exact key UsbManager.getDeviceList() uses
	if deviceName == "" {
		return fmt.Errorf("usbpass: android claim requires InstanceID (UsbDevice.getDeviceName())")
	}

	if err := ensureAndroidUSBPermission(deviceName); err != nil {
		return err
	}

	var connHandle, ifaceHandle int64
	var ifaceNum int
	var descHex string
	err := driver.RunNative(func(nctx any) error {
		ac, ok := nctx.(*driver.AndroidContext)
		if !ok || ac == nil {
			return fmt.Errorf("usbpass: android native context unavailable")
		}
		cDeviceName := C.CString(deviceName)
		defer C.free(unsafe.Pointer(cDeviceName))

		cResult := C.jni_usbClaim(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), cDeviceName)
		if cResult == nil {
			return fmt.Errorf("usbpass: android claim failed for %s (see logcat tag USB_CLAIM_JNI)", deviceName)
		}
		defer C.free(unsafe.Pointer(cResult))

		parts := strings.SplitN(C.GoString(cResult), "\t", 4)
		if len(parts) != 4 {
			return fmt.Errorf("usbpass: android claim: malformed result %q", C.GoString(cResult))
		}
		var perr error
		if connHandle, perr = strconv.ParseInt(parts[0], 10, 64); perr != nil {
			return fmt.Errorf("usbpass: android claim: bad connHandle: %w", perr)
		}
		if ifaceHandle, perr = strconv.ParseInt(parts[1], 10, 64); perr != nil {
			return fmt.Errorf("usbpass: android claim: bad ifaceHandle: %w", perr)
		}
		if ifaceNum, perr = strconv.Atoi(parts[2]); perr != nil {
			return fmt.Errorf("usbpass: android claim: bad ifaceNum: %w", perr)
		}
		descHex = parts[3]
		return nil
	})
	if err != nil {
		return err
	}

	rawDesc, err := hex.DecodeString(descHex)
	if err != nil {
		return fmt.Errorf("usbpass: android claim: bad descriptor hex: %w", err)
	}
	deviceDesc, configDesc := splitAndroidRawDescriptors(rawDesc)
	if len(deviceDesc) >= 18 {
		dev.DeviceDesc = deviceDesc
		dev.Class = deviceDesc[4]
		dev.SubClass = deviceDesc[5]
		dev.Protocol = deviceDesc[6]
		dev.BCDDevice = binary.LittleEndian.Uint16(deviceDesc[12:14])
		dev.NumConfigs = deviceDesc[17]
	}
	if len(configDesc) >= 9 {
		dev.ConfigDesc = configDesc
		dev.ConfigVal = configDesc[5]
		dev.Interfaces = androidInterfacesFromConfigDesc(configDesc)
	}
	// dev.BusID is a StableUSBIPBusID hash, not a real Linux busid (Android
	// app sandboxes can't read /sys/bus/usb/devices), so resolveUSBSpeed
	// always falls through to its HIGH default here -- kept anyway in case
	// a future Android version exposes UsbDevice.getSpeed() through this
	// same code path.
	if sp := resolveUSBSpeed(dev.BusID); sp != 0 {
		dev.Speed = sp
	}

	dev.Backend = &androidUSBBackend{
		connHandle:  C.longlong(connHandle),
		ifaceHandle: C.longlong(ifaceHandle),
		deviceName:  deviceName,
	}
	logrus.Infof("usbpass: android claimed %04x:%04x iface=%d (%d raw descriptor bytes)", dev.VID, dev.PID, ifaceNum, len(rawDesc))
	return nil
}

// ensureAndroidUSBPermission blocks until UsbManager.hasPermission() is
// true for deviceName, prompting the system permission dialog if needed.
// Polling hasPermission (instead of a BroadcastReceiver + PendingIntent
// callback wired through Kotlin) is deliberate: the dialog result would
// otherwise need a new Kotlin/manifest-registered receiver and a
// Go-callable bridge method (the androidbridge.Androidbridge pattern
// NbdBridge.kt/callbacks_android.go use for SAF/QR results) just to flip a
// boolean this goroutine could ask for directly.
func ensureAndroidUSBPermission(deviceName string) error {
	hasPermission := func() (bool, error) {
		var granted bool
		err := driver.RunNative(func(nctx any) error {
			ac, ok := nctx.(*driver.AndroidContext)
			if !ok || ac == nil {
				return fmt.Errorf("usbpass: android native context unavailable")
			}
			cDeviceName := C.CString(deviceName)
			defer C.free(unsafe.Pointer(cDeviceName))
			granted = C.jni_usbHasPermission(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), cDeviceName) != 0
			return nil
		})
		return granted, err
	}

	granted, err := hasPermission()
	if err != nil {
		return err
	}
	if granted {
		return nil
	}

	err = driver.RunNative(func(nctx any) error {
		ac, ok := nctx.(*driver.AndroidContext)
		if !ok || ac == nil {
			return fmt.Errorf("usbpass: android native context unavailable")
		}
		cDeviceName := C.CString(deviceName)
		defer C.free(unsafe.Pointer(cDeviceName))
		C.jni_usbRequestPermission(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), cDeviceName)
		return nil
	})
	if err != nil {
		return err
	}

	logrus.Infof("usbpass: waiting for USB permission dialog for %s", deviceName)
	const pollInterval = 300 * time.Millisecond
	const timeout = 30 * time.Second
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		time.Sleep(pollInterval)
		granted, err := hasPermission()
		if err != nil {
			return err
		}
		if granted {
			return nil
		}
	}
	return fmt.Errorf("usbpass: USB permission for %s not granted within %s (dialog dismissed or denied?)", deviceName, timeout)
}

// splitAndroidRawDescriptors splits UsbDeviceConnection.getRawDescriptors()
// (device descriptor immediately followed by the active configuration's
// descriptor set, exactly as the kernel enumerated it -- no synthesis,
// unlike mac's HID path) into the two pieces ExportedDevice wants
// separately. configDesc is clamped to the first config's own
// wTotalLength (offset 2-3 of its 9-byte header) the same way
// backend_gousb.go's readConfigDescriptor does, in case rawDesc happens to
// carry more than one configuration's descriptors back to back.
func splitAndroidRawDescriptors(raw []byte) (deviceDesc, configDesc []byte) {
	if len(raw) < 18 {
		return raw, nil
	}
	deviceDesc = raw[:18]
	rest := raw[18:]
	if len(rest) < 4 {
		return deviceDesc, rest
	}
	total := int(binary.LittleEndian.Uint16(rest[2:4]))
	if total <= 0 || total > len(rest) {
		total = len(rest)
	}
	return deviceDesc, rest[:total]
}

// androidInterfacesFromConfigDesc is a copy of backend_gousb.go's
// interfacesFromConfigDesc -- duplicated rather than shared because that
// file's build tag excludes android (and because mac's hidbridge_darwin.go
// already sets the precedent of each platform backend owning its own copy
// of this kind of raw-descriptor-walking helper rather than reaching into
// another platform's file).
func androidInterfacesFromConfigDesc(cfg []byte) [][3]uint8 {
	var out [][3]uint8
	i := 0
	for i+2 <= len(cfg) {
		length := int(cfg[i])
		if length < 2 || i+length > len(cfg) {
			break
		}
		if cfg[i+1] == 0x04 && length >= 9 { // INTERFACE
			out = append(out, [3]uint8{cfg[i+5], cfg[i+6], cfg[i+7]})
		}
		i += length
	}
	return out
}

// --- DeviceBackend ---

type androidUSBBackend struct {
	connHandle  C.longlong
	ifaceHandle C.longlong
	deviceName  string

	// UsbDeviceConnection's thread-safety for concurrent transfers isn't
	// documented; serializing every control/bulk call through it is the
	// conservative choice backend_gousb.go's bulkSem makes for bulk-only,
	// widened here to control too since both go through the same
	// connection object.
	mu     sync.Mutex
	closed bool
}

func (b *androidUSBBackend) HandleControl(_ context.Context, setup [8]byte, wLength int, _ []byte) (int32, []byte) {
	bm := setup[0]
	req := setup[1]
	wValue := binary.LittleEndian.Uint16(setup[2:4])
	wIndex := binary.LittleEndian.Uint16(setup[4:6])

	// Same rationale as backend_gousb.go's HandleControl: forwarding
	// SET_CONFIGURATION/SET_INTERFACE after claim would make the connection
	// tear down and rebuild the endpoints Android already set up when we
	// claimed the interface.
	switch {
	case bm == 0x00 && req == 0x09: // SET_CONFIGURATION
		return 0, nil
	case bm == 0x01 && req == 0x0b: // SET_INTERFACE
		return 0, nil
	case bm == 0x80 && req == 0x08: // GET_CONFIGURATION
		return 0, []byte{1}
	case bm == 0x81 && req == 0x0a: // GET_INTERFACE
		return 0, []byte{0}
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errnoEPIPE, nil
	}

	buf := make([]byte, wLength)
	var n C.int
	err := driver.RunNative(func(nctx any) error {
		ac, ok := nctx.(*driver.AndroidContext)
		if !ok || ac == nil {
			return fmt.Errorf("android native context unavailable")
		}
		var bufPtr unsafe.Pointer
		if wLength > 0 {
			bufPtr = unsafe.Pointer(&buf[0])
		}
		n = C.jni_usbControlTransfer(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), b.connHandle,
			C.int(bm), C.int(req), C.int(wValue), C.int(wIndex), bufPtr, C.int(wLength), 3000)
		return nil
	})
	if err != nil || n < 0 {
		logrus.Debugf("usbpass: android control bm=%#02x req=%#02x wValue=%#04x wIndex=%#04x wLength=%d -> n=%d err=%v",
			bm, req, wValue, wIndex, wLength, n, err)
		return errnoEPIPE, nil
	}
	return 0, buf[:n]
}

func (b *androidUSBBackend) HandleBulk(ctx context.Context, ep uint8, dirIn bool, length int, outData []byte) (int32, []byte) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return errnoEPIPE, nil
	}
	if ctx.Err() != nil {
		return errnoEPIPE, nil
	}

	fullAddr := int(ep & 0x7f)
	if dirIn {
		fullAddr |= 0x80
	}

	timeoutMs := 15000
	if dl, ok := ctx.Deadline(); ok {
		if remaining := time.Until(dl); remaining > 0 && remaining < 15*time.Second {
			timeoutMs = int(remaining / time.Millisecond)
		}
	}

	buf := make([]byte, length)
	if !dirIn {
		copy(buf, outData)
	}

	var n C.int
	err := driver.RunNative(func(nctx any) error {
		ac, ok := nctx.(*driver.AndroidContext)
		if !ok || ac == nil {
			return fmt.Errorf("android native context unavailable")
		}
		var bufPtr unsafe.Pointer
		if length > 0 {
			bufPtr = unsafe.Pointer(&buf[0])
		}
		n = C.jni_usbBulkTransfer(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), b.connHandle, b.ifaceHandle,
			C.int(fullAddr), bufPtr, C.int(length), C.int(timeoutMs))
		return nil
	})
	if err != nil || n < 0 {
		logrus.Debugf("usbpass: android bulk ep=%#02x dirIn=%v length=%d -> n=%d err=%v", fullAddr, dirIn, length, n, err)
		return errnoEPIPE, nil
	}
	if dirIn {
		return 0, buf[:n]
	}
	return 0, nil
}

func (b *androidUSBBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil
	}
	b.closed = true
	return driver.RunNative(func(nctx any) error {
		ac, ok := nctx.(*driver.AndroidContext)
		if !ok || ac == nil {
			return fmt.Errorf("android native context unavailable")
		}
		C.jni_usbClose(C.uintptr_t(ac.Env), C.uintptr_t(ac.Ctx), b.connHandle, b.ifaceHandle)
		return nil
	})
}

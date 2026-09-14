//go:build android

package usbpass

/*
#cgo LDFLAGS: -landroid -llog

#include <stdlib.h>

int jni_usbHostSupported(uintptr_t jni_env_ptr, uintptr_t ctx_ptr);
char* jni_listUsbDevices(uintptr_t jni_env_ptr, uintptr_t ctx_ptr);
*/
import "C"

import (
	"fmt"
	"strconv"
	"strings"
	"unsafe"

	"fyne.io/fyne/v2/driver"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
)

// listUSBAndroid enumerates UsbManager.getDeviceList() (see
// usb_jni_android.c) as passthrough candidates -- the Android counterpart
// to listHIDDarwin/listSysfs. Unlike macOS, Android's UsbDeviceConnection
// can force-claim an interface away from a kernel driver
// (claimInterface(intf, true)), so this is not limited to HID devices the
// way the mac HID-tap path is; a future TryClaim-style backend here can
// forward raw bulk/control transfers for any class (mass storage included).
// This file only covers enumeration.
//
// Devices without android.hardware.usb.host (checked first, per live
// verification that a plain USB-accessory-only phone should show nothing
// rather than a permanently-empty-looking "USB devices" list) never reach
// UsbManager at all.
func listUSBAndroid() ([]models.USBPassthroughDevice, error) {
	var out []models.USBPassthroughDevice
	var callErr error

	err := driver.RunNative(func(ctx any) error {
		androidCtx, ok := ctx.(*driver.AndroidContext)
		if !ok || androidCtx == nil {
			callErr = fmt.Errorf("usbpass: android native context unavailable")
			return callErr
		}

		env := C.uintptr_t(androidCtx.Env)
		jctx := C.uintptr_t(androidCtx.Ctx)

		if C.jni_usbHostSupported(env, jctx) == 0 {
			logrus.Debug("usbpass: android.hardware.usb.host not supported on this device — no USB devices to list")
			return nil
		}

		cList := C.jni_listUsbDevices(env, jctx)
		if cList == nil {
			return nil
		}
		defer C.free(unsafe.Pointer(cList))

		raw := C.GoString(cList)
		for _, line := range strings.Split(raw, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			parts := strings.Split(line, "\t")
			if len(parts) < 6 {
				continue
			}
			deviceName, vidHex, pidHex, classStr := parts[0], parts[1], parts[2], parts[3]
			manufacturer, product := parts[4], parts[5]

			desc := strings.TrimSpace(manufacturer + " " + product)
			if desc == "" {
				desc = fmt.Sprintf("USB device %s:%s", vidHex, pidHex)
			}
			if label := usbClassLabel(classStr); label != "" {
				desc = fmt.Sprintf("%s (%s)", desc, label)
			}

			out = append(out, models.USBPassthroughDevice{
				BusID:       StableUSBIPBusID(deviceName),
				InstanceID:  deviceName,
				VID:         vidHex,
				PID:         pidHex,
				Description: desc,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if callErr != nil {
		return nil, callErr
	}
	return out, nil
}

// usbClassLabel gives a human name to the USB device class codes actually
// worth calling out on a passthrough candidate; see
// https://www.usb.org/defined-class-codes. Interface-associated classes
// (0x00, 0xEF) report per-interface and aren't resolved here — falls back
// to no label, same as hidUsageLabel's philosophy on mac.
func usbClassLabel(classStr string) string {
	class, err := strconv.Atoi(classStr)
	if err != nil {
		return ""
	}
	switch class {
	case 0x03:
		return "HID"
	case 0x08:
		return "Mass Storage"
	case 0x09:
		return "Hub"
	case 0x02, 0x0A:
		return "Communications"
	case 0xE0:
		return "Wireless"
	}
	return ""
}

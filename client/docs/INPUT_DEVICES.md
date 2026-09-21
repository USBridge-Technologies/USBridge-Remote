# Supported input devices

Gamepads and pen tablets the client can send to the host, and what has been tried on
real hardware. Details: [Gamepads](./GAMEPADS.md), [Pen tablets](./TABLETS.md).

**Status legend.**
✅ tried on real hardware · 🧪 implemented and unit-tested, not tried on hardware · ➖ not supported

## Gamepads (client Windows; host sees an Xbox 360 controller)

| Device | Capture | Buttons, sticks, triggers | Rumble to the pad | Extras | Status |
| --- | --- | --- | --- | --- | --- |
| Razer Raiju Tournament Edition (`1532:1007`, PlayStation layout) | WinMM through its SDL mapping | ✅ | ✅ (DS4 output report) | touchpad as a relative mouse ✅, PS button 🧪 | ✅ |
| Razer Wolverine V2 (Xbox class) | XInput | 🧪 | ✅ on Linux (evdev force feedback) · 🧪 on Windows (XInput) | — | 🧪 / ✅ Linux rumble |
| Xbox 360 / One / Series and clones | XInput | 🧪 | 🧪 (`XInputSetState`) | — | 🧪 |
| Sony DualShock 4 and clones (`054C:05C4`, `09CC`, `0BA0`) and the ~860 layouts in the SDL database | WinMM through the SDL mapping | 🧪 | 🧪 (DS4 report; only the ids listed in `hidRumbleProtocols`) | touchpad as a mouse 🧪, PS button 🧪 | 🧪 |
| Other DirectInput pads | WinMM, assumed Xbox layout | 🧪 the trigger axes may be wrong | ➖ | — | 🧪 |
| Any pad on a Linux client | evdev | 🧪 | ✅ (Wolverine V2) | — | 🧪 |
| macOS client, HID pads | IOKit | 🧪 | ➖ | — | 🧪 |
| DualSense, Switch Pro | not specifically supported | ➖ | ➖ | gyro, adaptive triggers, lightbar ➖ | ➖ |
| Android, iOS, Web | not captured | ➖ | ➖ | — | ➖ |

Up to four pads at once (each on its own Moonlight controller number): 🧪, not yet run with
two pads on hardware. Host side: Windows (loopback USB/IP into usbip-win2, `xusb22.sys`) ✅;
Linux (uinput, Xbox 360 ids) 🧪; hardware KVM: one pad.

## Pen tablets (client Windows; host sees the original USB tablet)

| Tablet | Exported from | Windows host (Wacom driver) | Linux host (kernel `wacom`) | Status |
| --- | --- | --- | --- | --- |
| Wacom Intuos S (CTL-4100, `056A:0374`) | captured model | ✅ | ✅ pen, pressure, hover, touch, 2 pen buttons, 4 ExpressKeys | ✅ |
| 84 other Wacom tablets (Intuos, Intuos Pro S/M/L, Wacom One, Cintiq Pro, Graphire, laptop digitizers, ...; full list in [Pen tablets](./TABLETS.md)) | real descriptor from the database | 🧪 | 🧪 | 🧪 |
| A Wacom tablet not in the list | generic bridge (reconstructed descriptor) | 🧪 the vendor driver may not accept it | 🧪 | 🧪 |
| Tablets of other vendors | generic bridge | 🧪 | 🧪 | 🧪 |
| macOS client | the tablet's real descriptor, no model needed | 🧪 | 🧪 | 🧪 |

While a tablet is exported from Windows its local pointer is switched off (a UAC prompt
when the export starts and ends) ✅.

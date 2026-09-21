# Gamepads

How the client reads a physical gamepad, sends it to the host as a Moonlight
controller, and what comes back (rumble). This is about *gamepad input over the
stream*; passing a whole pad through as a USB device is
[USB Passthrough](./USB_PASSTHROUGH.md).

**Status legend.**
✅ tried on real hardware · 🧪 implemented and unit-tested, not tried on hardware · ➖ not supported

## The path

```
physical pad ──► client capture ──► Moonlight controller N ──► host virtual Xbox 360 pad ──► game
   (XInput / WinMM+SDL map /          (buttons, sticks,        (Windows: loopback USB/IP,
    evdev / IOKit)                     triggers, mask)          Linux: uinput 045E:028E)
                     ▲                                                     │
                     └────────────── rumble (motor levels) ◄───────────────┘
```

Whatever the pad is, the host always sees an **Xbox 360 controller** ("Map Xbox 360",
the default on software agents), so games and Steam show Xbox glyphs and every
layout is translated on the *client*: the wire format is XInput's (16 button bits,
two 0..255 triggers, four 16-bit sticks with up positive).

## Client capture

| Client OS | Pad | Capture | Triggers | Status |
| --- | --- | --- | --- | --- |
| Windows | Xbox 360 / One / Series and clones (Razer Wolverine V2) | XInput, listed as `xinput:N` | separate, full range | 🧪 capture (rumble ✅) |
| Windows | PlayStation-layout DirectInput pad in the [SDL database](../third_party/SDL_GameControllerDB/) (Razer Raiju TE, DualShock 4 and clones, ~860 pads) | WinMM read through the pad's SDL mapping, listed as `winmm:N` with the model name | analog axes or buttons, whatever the mapping says | ✅ Razer Raiju TE · 🧪 others |
| Windows | DirectInput pad *not* in the database | WinMM with an assumed Xbox layout | one guess (Z / V axes) | 🧪 may be wrong |
| Linux | any pad the kernel exposes through evdev | `/dev/input/event*`, state read from the kernel at start and after `SYN_DROPPED` | as the driver reports them | 🧪 (rumble ✅ on a Razer Wolverine V2) |
| macOS | HID gamepad | IOKit | as the device reports them | 🧪 |
| Android, iOS, Web | physical pads | not captured by this path | ➖ | ➖ |

Notes for Windows:

- An Xbox-class pad is shown once (as `xinput:N`); its WinMM twin is hidden, because
  WinMM merges the two triggers into one axis.
- A DirectInput pad must be bound to the **inbox HID driver**. The WinUSB binding that
  [USB Passthrough](./USB_PASSTHROUGH.md) installs (a libwdi package) removes the pad
  from HID, WinMM and XInput entirely: the client will not list it until that package
  is removed (`pnputil /delete-driver oemNN.inf /uninstall`) and the pad replugged.
- The SDL entry decides which WinMM axis is which control. WinMM numbers axes by the
  order of the pad's HID report, so the table that ties SDL's axes to WinMM's
  (`sdlAxisToJoy`) was measured on one pad (the Raiju TE). Most database entries put
  the triggers on buttons and do not depend on it; a pad whose triggers are axes may
  need it checked.
- The list of pads is read when the client starts and on *Refresh*; it is not
  hot-plug aware.

The touchpad reads the pad's own HID input report next to the normal capture
(`gamepad_touchpad*.go`): finger 1 sits in bytes 35..38 (contact flag + tracking id, then
X and Y as 12-bit values, 1920 x 942 units) and the click is byte 7 bit 1, measured on
the Raiju TE. A new touch only sets a reference point, so lifting and re-placing a finger
never makes the pointer jump; about 0.6 mouse counts per touchpad unit.

### Buttons

| Pad control | Sent as | Status |
| --- | --- | --- |
| D-pad, A/B/X/Y (cross/circle/square/triangle), L1/R1, L3/R3, Start (Options), Back (Share) | the same-named XInput bit | ✅ Raiju TE · 🧪 others |
| Guide: Xbox button, **PS button** | Guide | 🧪 Xbox / Linux `BTN_MODE`; the PS button of DS4-layout pads is button 13 (report byte 7 bit 0), not yet pressed on the Raiju |
| **Touchpad** (DualShock 4 layout: Raiju family, Sony DS4) | a **relative mouse**: finger motion moves the host pointer, the touchpad click is the **left mouse button** (an Xbox pad has no touchpad, so it is not a pad button); Windows only | ✅ Razer Raiju TE |
| L2 / R2 | trigger axis | ✅ Raiju TE (analog) |
| Second finger / multi-touch, gyro / accelerometer, lightbar, adaptive triggers, paddles, extra face buttons | not sent | ➖ |

## Several pads at once

Up to **4** pads run at the same time. Each captured pad gets the lowest free
Moonlight controller number and keeps it until it stops, and every packet carries the
mask of all active controllers, so the host builds one virtual pad per controller and
removes it when the pad is switched off (the client sends the controller's departure).

| Agent | Several pads | Notes |
| --- | --- | --- |
| Software agent (Windows / Linux / macOS host) | 🧪 up to 4 (unit-tested; not yet run with two pads on hardware) | switching one pad on keeps the others; the agent replaces the whole device list on every start, so the client always sends the full set |
| USBridge hardware KVM | ➖ one | a single gamepad gadget; a new pad replaces the old one |

## Rumble (host → client)

The host game's vibration arrives as Moonlight rumble for controller N and goes to the
pad that holds that number.

| Client | Pad | Path | Status |
| --- | --- | --- | --- |
| Windows | Xbox-class | `XInputSetState` | ✅ |
| Windows | Razer Raiju TE (`1532:1007`) | DualShock 4 HID output report `0x05`, 32 bytes, written to the pad's game collection; refreshed every 250 ms while non-zero | ✅ |
| Windows | Sony DualShock 4 (`054C:05C4`, `09CC`, `0BA0`) | same report | 🧪 |
| Windows | other DirectInput pads, DualSense, Switch Pro | none | ➖ |
| Linux | any pad with evdev force feedback (`FF_RUMBLE`) | the pad's own event node | ✅ Razer Wolverine V2 |
| macOS, Android, iOS, Web | any | none | ➖ |

A DirectInput pad is matched to a rumble path by its **USB id**, never by number:
a PlayStation-layout pad never borrows an Xbox pad's XInput slot. Only pads listed in
`hidRumbleProtocols` are written to, because an output report the pad does not expect
could reconfigure it. Trigger rumble and LED requests from the host are ignored.

## Host side

| Host | Virtual pad | Rumble back to the client | Status |
| --- | --- | --- | --- |
| Windows (rust-shine streamer) | Xbox 360 (`045E:028E`) over loopback USB/IP into the usbip-win2 VHCI, bound to the inbox `xusb22.sys`; no ViGEmBus | game's `XInputSetState` → control-stream rumble | ✅ |
| Linux (rust-shine streamer) | one uinput device per controller, Xbox 360 ids, created when the client adds the controller and removed when it leaves | `FF_RUMBLE` effects answered by a per-pad thread (length, delay, repeat, gain) | 🧪 compiled for Linux, not run there |
| macOS | none | ➖ | ➖ |

The Linux pad used to be created when the streamer started, so Steam showed an Xbox
controller even with no pad on the client; from streamer 0.3.66 it appears on demand.

## Not supported

Touchpad on Linux and macOS (a DS4's touchpad is its own evdev device there), multi-touch, gyro, lightbar / LED, adaptive-trigger and trigger rumble,
DualSense- and Switch Pro-specific features, paddle and misc buttons (the upper 16
Moonlight button bits are not sent: the client's send path carries 16), more than four
pads, and pad hot-plug without a *Refresh*.

## Checking it on a real pad (Windows)

```
# what the client lists, and one pad's decoded state while you press things
USBRIDGE_PAD_LIVE=winmm:0 USBRIDGE_PAD_LIVE_SECS=60 go test -v -run TestLiveGamepadCapture ./internal/platform/

# a pad's HID collections and report sizes (is it on the HID driver? what does it accept?)
USBRIDGE_HID_DUMP=1532:1007 go test -v -run TestLiveHIDCollections ./internal/platform/

# the touchpad-as-mouse reader: swipe and click after the beep, prints what would be sent
USBRIDGE_TOUCHPAD_LIVE=winmm:0 go test -v -run TestLiveTouchpad ./internal/platform/

# vibrate a pad, announced by beeps: both motors, large only, small only
USBRIDGE_RUMBLE_LIVE="winmm:0,xinput:0" go test -v -run TestLiveRumble ./internal/platform/
```

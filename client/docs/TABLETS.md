# Pen tablets

How a Wacom tablet plugged into the client reaches the host, which models are covered,
and which of them have been tried on hardware. Gamepads are in [Gamepads](./GAMEPADS.md);
the whole matrix is in [Supported input devices](./INPUT_DEVICES.md).

**Status legend.**
✅ tried on real hardware · 🧪 implemented and unit-tested, not tried on hardware · ➖ not supported

## Two paths

| Path | What the host sees | Needs on the host | Models |
| --- | --- | --- | --- |
| **USB export** (this page) | the original USB tablet, with its own VID/PID, descriptors and serial | the vendor's own driver (Wacom's on Windows, the kernel `wacom` driver on Linux); **no Zadig / WinUSB** on the client | the table below |
| Pen over the stream ([macOS only](./PEN_TABLET_SUPPORT.md)) | a Moonlight pen event | nothing | the IntuosV2 family |

Switching a tablet on in **Devices → HID & Input Hub** starts the USB export. It
carries everything the tablet has: position, pressure, hover distance, tilt where the
pen has it, both side buttons and the ExpressKeys.

## How the export works

```
tablet ──► client (live input reports) ──► synthetic USB device ──► USB/IP ──► host driver
                                            built from a model:
                                            descriptors + feature reports
```

The host's driver initialises the tablet from its **complete HID report descriptor**. An
operating system's HID API does not hand a bridge that faithfully (on Windows `hid.dll`
returns a reconstructed, shorter descriptor and refuses reports it dropped), which is
why a plain bridge never got Wacom's driver to poll the tablet. So for the tablets in
the table the exported device is built from a **model**: the descriptors of the real
device, and only the live input reports come from the tablet you hold.

| Source of the model | What it holds |
| --- | --- |
| `internal/usbpass/wacom_models/*.json` | captured from real hardware, including the real feature-report values (CTL-4100) |
| `internal/usbpass/wacom_db/descriptors.json` | real report descriptors dumped from real tablets by the [wacom-hid-descriptors](https://github.com/linuxwacom/wacom-hid-descriptors) project (regenerate with `tools/wacom_db/generate.py`). The drivers do not need the real feature-report values (all-zero ones work, checked on the CTL-4100), so those are answered zero-filled at their declared size |

The tablet's own device and configuration descriptors and its strings are read from the
tablet itself (Windows: through its USB hub, no driver change), so serial number and
firmware version are the real ones. A tablet that is not in the table is exported the
generic way (from the OS's reconstructed descriptor), which a vendor driver may not accept.

### Local input while exported (Windows)

Exporting a tablet must not also move the local pointer, so the client switches off the
tablet's mouse, pen and digitizer nodes for the duration and brings them back when the
export ends. Disabling a device needs administrator rights: Windows shows a UAC prompt
when the export starts and once more when it ends. If you decline, the export still
works and the tablet also drives the local pointer. Details in `hidlocal_windows.go`;
if the client is killed in between, the nodes are restored at the next start.

## Client and host support

| | Windows client | macOS client | Linux client |
| --- | --- | --- | --- |
| Export from a model | ✅ CTL-4100 · 🧪 other models | 🧪 exports the tablet's real descriptor (no model needed) | ➖ (whole-device passthrough through libusb is used) |
| Local input switched off while exported | ✅ (UAC) | ➖ | ➖ |

| Host | Native driver | Status |
| --- | --- | --- |
| Linux (kernel `wacom` over vhci) | binds; pen, pressure, hover, touch, both pen buttons and all four ExpressKeys arrive | ✅ CTL-4100 |
| Windows (Wacom driver over usbip-win2, loopback) | binds as Wacom Pointer / pen / digitizer; the cursor follows the pen | ✅ CTL-4100 |
| macOS | not tried | 🧪 |

## Tried on hardware

| Tablet | Client | Host | Result |
| --- | --- | --- | --- |
| Wacom Intuos S (CTL-4100, `056A:0374`) | Windows 11, Wacom driver 4.0 | Linux, kernel 6.17 | ✅ pen X/Y, pressure (0..~3200 seen), hover, touch, 2 pen buttons, 4 ExpressKeys |
| Wacom Intuos S (CTL-4100) | Windows 11 (model built from the database instead of the captured one) | Windows 11, native Wacom driver | ✅ cursor follows the pen |
| Wacom Intuos S (CTL-4100) | Windows 11 | Windows 11 loopback, all-zero feature reports | ✅ still binds and moves the cursor |

Everything else in the table below has **not** been tried on hardware; its descriptor is
a real one from a real tablet, which is what the drivers need, but a model that does not
bind on your setup should be reported (a capture of the real tablet fixes it: dump its HID
report descriptor and feature reports the way `wacom_models/056a_0374.json` was made).

## Tablets in the database

Only tablets whose USB HID interfaces are all described are listed (Wacom's tablets on I²C
or Bluetooth are not USB devices). Many entries are the digitizers built into laptops and
all-in-ones; they are exported the same way.

| USB id | Model | HID interfaces | Status |
| --- | --- | --- | --- |
| `056A:0043` | Wacom Intuos2 9x12 | 1 | 🧪 |
| `056A:00B1` | Wacom Intuos3 6x8 | 1 | 🧪 |
| `056A:00DE` | Wacom Bamboo Capture | 2 | 🧪 |
| `056A:00E3` | Motion Computing J3500 | 2 | 🧪 |
| `056A:00E6` | Lenovo ThinkPad X220 | 2 | 🧪 |
| `056A:00EC` | Lenovo ThinkPad S1 Yoga 12 | 1 | 🧪 |
| `056A:0148` | Panasonic CF-20-2 | 1 | 🧪 |
| `056A:014E` | Fujitsu LIFEBOOK T726 | 1 | 🧪 |
| `056A:0157` | Fujitsu LIFEBOOK T936 | 1 | 🧪 |
| `056A:0317` | Wacom Intuos Pro L | 2 | 🧪 |
| `056A:0318` | Wacom Bamboo Pad | 2 | 🧪 |
| `056A:0319` | Wacom Bamboo Pad Wireless | 2 | 🧪 |
| `056A:0325` | Wacom Cintiq Companion 2 | 1 | 🧪 |
| `056A:0326` | Wacom Cintiq Companion 2 | 1 | 🧪 |
| `056A:033C` | Wacom Intuos S (2nd-gen) | 1 | 🧪 |
| `056A:0350` | Wacom Cintiq Pro 16 | 1 | 🧪 |
| `056A:0354` | Wacom Cintiq Pro 16 | 1 | 🧪 |
| `056A:0374` | Wacom Intuos S (3rd-gen) | 1 | ✅ real tablet, native driver on a Windows and a Linux host |
| `056A:037A` | GPD Pocket 3 | 1 | 🧪 |
| `056A:03A6` | Wacom One Creative Pen Display | 1 | 🧪 |
| `056A:03C0` | Wacom Cintiq Pro 27 | 2 | 🧪 |
| `056A:03C4` | Wacom Cintiq Pro 17 | 2 | 🧪 |
| `056A:03CB` | Wacom One 13 | 2 | 🧪 |
| `056A:03CE` | Wacom One 12 | 1 | 🧪 |
| `056A:03D0` | Wacom Cintiq Pro 22 | 2 | 🧪 |
| `056A:03EC` | Wacom DTH134 | 2 | 🧪 |
| `056A:03ED` | Wacom DTC121 | 1 | 🧪 |
| `056A:03F0` | Wacom Movink DTH135K0C | 2 | 🧪 |
| `056A:03F5` | Wacom Intuos Pro S (3rd-gen) | 1 | 🧪 |
| `056A:03F7` | Wacom Intuos Pro M (3rd-gen) | 1 | 🧪 |
| `056A:03F9` | Wacom Intuos Pro L (3rd-gen) | 1 | 🧪 |
| `056A:0529` | Wimaxit M1560CT3 | 1 | 🧪 |
| `056A:4004` | Motion Computing R12 | 1 | 🧪 |
| `056A:5019` | Fujitsu LIFEBOOK T935 | 2 | 🧪 |
| `056A:5040` | Lenovo ThinkPad X1 Yoga (Version C) | 2 | 🧪 |
| `056A:5044` | Lenovo ThinkPad Yoga 260 | 2 | 🧪 |
| `056A:504A` | Lenovo ThinkPad P40 Yoga | 2 | 🧪 |
| `056A:504C` | Lenovo ThinkPad Yoga 460 | 2 | 🧪 |
| `056A:5087` | Lenovo ThinkPad | 2 | 🧪 |
| `056A:5093` | Lenovo ThinkPad X1 Yoga 1st | 2 | 🧪 |
| `056A:5094` | Lenovo ThinkPad X1 Yoga 1st | 2 | 🧪 |
| `056A:509C` | Lenovo ThinkPad Yoga 370 | 2 | 🧪 |
| `056A:509D` | Lenovo ThinkPad Yoga 370 | 2 | 🧪 |
| `056A:509F` | Lenovo ThinkPad Yoga 370 | 2 | 🧪 |
| `056A:50A0` | Lenovo Thinkpad YOGA370 | 2 | 🧪 |
| `056A:50A9` | Fujitsu LIFEBOOK U729X | 2 | 🧪 |
| `056A:50B4` | Lenovo ThinkPad X1 Yoga 2nd | 2 | 🧪 |
| `056A:50B6` | Lenovo ThinkPad X1 Yoga 2nd | 2 | 🧪 |
| `056A:50B7` | Lenovo ThinkPad X1 Yoga 2nd | 2 | 🧪 |
| `056A:50B8` | Lenovo ThinkPad X1 Yoga 2nd | 2 | 🧪 |
| `056A:5144` | Lenovo ThinkPad X1 Yoga 3rd | 2 | 🧪 |
| `056A:5146` | Lenovo ThinkPad X1 Yoga 3rd | 2 | 🧪 |
| `056A:5147` | Lenovo ThinkPad X1 Yoga 3rd | 2 | 🧪 |
| `056A:5148` | Lenovo ThinkPad X1 Yoga 3rd | 2 | 🧪 |
| `056A:5150` | Lenovo ThinkPad X380 Yoga | 2 | 🧪 |
| `056A:5155` | Lenovo ThinkPad X380 Yoga | 2 | 🧪 |
| `056A:5157` | Lenovo ThinkPad L380 Yoga | 2 | 🧪 |
| `056A:5158` | Lenovo ThinkPad L390 Yoga | 2 | 🧪 |
| `056A:5159` | Lenovo ThinkPad L390 Yoga | 2 | 🧪 |
| `056A:51A0` | Lenovo ThinkPad X1 Extreme 2nd | 1 | 🧪 |
| `056A:51AF` | Lenovo ThinkPad X13 Yoga | 2 | 🧪 |
| `056A:51B0` | Lenovo ThinkPad X13 Yoga | 2 | 🧪 |
| `056A:51B1` | Lenovo ThinkPad X13 Yoga | 2 | 🧪 |
| `056A:51B2` | Lenovo ThinkPad X13 Yoga | 2 | 🧪 |
| `056A:51B3` | Lenovo ThinkPad X13 Yoga | 2 | 🧪 |
| `056A:51B6` | Lenovo ThinkPad X1 Carbon 7th | 2 | 🧪 |
| `056A:51B7` | Lenovo ThinkPad X1 Yoga 4th | 2 | 🧪 |
| `056A:51B8` | Lenovo ThinkPad X1 Yoga 4th | 2 | 🧪 |
| `056A:51B9` | Lenovo ThinkPad | 2 | 🧪 |
| `056A:51BB` | Lenovo ThinkPad X1 Yoga 4th | 2 | 🧪 |
| `056A:51BC` | Lenovo ThinkPad X1 Yoga 4th | 2 | 🧪 |
| `056A:51BD` | Lenovo ThinkPad X1 Yoga Gen 5 | 2 | 🧪 |
| `056A:51BE` | Lenovo ThinkPad X1 Yoga 4th | 2 | 🧪 |
| `056A:51BF` | Lenovo ThinkPad X1 Yoga 4th | 2 | 🧪 |
| `056A:51D0` | Lenovo ThinkPad X1 Titanium Gen 1 | 2 | 🧪 |
| `056A:51F5` | Lenovo ThinkPad L13 Yoga | 1 | 🧪 |
| `056A:51F6` | Lenovo ThinkPad L13 Yoga | 1 | 🧪 |
| `056A:51F9` | Lenovo ThinkPad L13 Yoga | 1 | 🧪 |
| `056A:521F` | Lenovo ThinkPad X13 Yoga | 1 | 🧪 |
| `056A:5220` | Lenovo ThinkPad X13 Yoga Gen 1 | 1 | 🧪 |
| `056A:5221` | Lenovo ThinkPad X13 Yoga | 1 | 🧪 |
| `056A:5222` | Lenovo ThinkPad X13 Yoga | 1 | 🧪 |
| `056A:5229` | Lenovo ThinkPad X1 Yoga Gen 5 | 1 | 🧪 |
| `056A:52E1` | Lenovo Yoga 7 16IAP7 | 1 | 🧪 |
| `056A:8191` | espresso Touch 15 | 1 | 🧪 |

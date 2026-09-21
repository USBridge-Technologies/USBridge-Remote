# USB passthrough (client side)

This is the **client** (device-holder) half — the machine with the physical USB
device plugged into it. It owns the USB/IP **export server** (`server.go`,
`StartExport`) and the AES **attach client** (`usbaes_attach.go`) that tells a
remote agent (Windows or Linux — see rust-shine's own
[`docs/USB_PASSTHROUGH.md`](https://github.com/itsme228/rust-shine/blob/main/docs/USB_PASSTHROUGH.md))
where to pull it from. Referenced from code comments in `access_linux.go` and
`session.go` as `docs/USB_PASSTHROUGH.md` — this file.

## Two ways to get a device's data onto the wire

| Path | Where | Needs |
| --- | --- | --- |
| Raw libusb claim (`backend_gousb.go`) | Windows, Linux (build tag `-tags usbpass_gousb`) | libusb-1.0 at build time; **Windows: WinUSB bound to the target interface first — see below** |
| Non-exclusive HID tap (`hidbridge_darwin.go`) | macOS only | Nothing extra — IOHIDManager reads input reports without claiming the interface |
| HID bridge (`hidbridge_windows.go`, `hidgen_backend.go`) | Windows, HID-class devices | Nothing extra — no Zadig/WinUSB; see "Windows HID bridge" below |
| Synthetic Xbox 360 (`x360_claim_windows.go`, `x360_backend.go`) | Windows, Xbox pads readable through XInput | Nothing extra — see "Xbox pads" below |

Android has its own raw-claim path (`backend_android.go`, JNI). Everything
below is about the raw libusb path, which is what a gamepad, tablet, or
storage device on Windows/Linux actually goes through.

## Windows HID bridge (no Zadig)

For a HID-class device the Windows client does **not** need WinUSB. `TryClaimGousb`
first tries `tryClaimHID` (`hidbridge_windows.go`): it finds the device's HID
collection nodes (`HID\...&COLnn`), opens each one with a shared handle through
`hid.dll`, reads its input reports and round-trips feature/output reports, and
presents the result to the importer as a synthetic USB HID device (real VID/PID,
strings and report descriptor — same idea as the macOS bridge). The device keeps
its normal Windows driver locally, and the importer binds its own class/vendor
driver to it.

Windows never gives user mode the raw report descriptor, so it is reconstructed
from the preparsed data (`hidpp_reconstruct.go`, a port of hidapi's
`hidapi_descriptor_reconstruct.c`). The result is functionally equivalent, not
byte-identical to what the device sends.

**Limits (verified on real hardware):**

- Windows holds the collections of **mice, keyboards and pens/digitizers
  exclusively**: `hid.dll` refuses read access (`Access is denied` / sharing
  violation). Those collections are still opened with query-only access (enough
  for the descriptor and feature reports), and their input reports come from
  **Raw Input** (`WM_INPUT`, `hidrawinput_windows.go`), which delivers raw HID
  reports for pen/digitizer/consumer/vendor/gamepad collections without taking
  the device from the OS. A Wacom Intuos S is bridged this way (verified: ~740
  pen reports in 12 s through the URB path, WinUSB not needed).
- **Mice and keyboards give no raw reports** (Raw Input returns parsed
  `RAWMOUSE`/`RAWKEYBOARD`), so such collections are exported but stay silent
  (a warning lists them, e.g. the tablet's mouse-emulation collection). An
  interface consisting only of them is left out.
- If some interfaces of a composite device cannot be bridged, the bridge exports
  only the interfaces it could (a warning names the rest). If none can be
  bridged, the libusb path is used.
- Not HID, not bridged by this path: mass storage. Xbox pads have their own path,
  see "Xbox pads" below.
- Aliased usages/collections (report descriptor delimiters) are not supported;
  such a device also falls back to libusb.
- **The bridge is opt-in** (`USBRIDGE_HID_BRIDGE=1`); by default every device keeps
  the full-fidelity libusb path. Reason: a vendor driver such as Wacom's router
  sends feature reports the HID descriptor does not declare (`0x02`, `0x83`,
  `0x0D`, ...) at USB level, and `hid.dll` refuses undeclared report IDs
  ("The parameter is incorrect"), so they cannot be forwarded. The importer's
  native driver still binds (`WacHidRouterPro`) and pen reports flow, but its
  initialisation/config traffic is acknowledged, not delivered — whether the
  tablet is then fully usable was not verified with a separate importer machine.
- Live check against a physically attached device:
  `USBRIDGE_HID_LIVE_USB='USB\VID_xxxx&PID_yyyy\<instance>' go test -v -run TestLiveWindowsHIDBridge ./internal/usbpass/`

## Xbox pads: synthetic Xbox 360 controller (Windows)

An Xbox One/Series (GIP) pad cannot be passed through raw: the importer's driver
waits for an Announce, runs GIP authentication and is timing sensitive. So when
the device's setup class is `XboxComposite` (GIP) or `XnaComposite` (wired 360)
and XInput can read a pad, `TryClaimGousb` calls `tryClaimX360`
(`x360_claim_windows.go`) **before** the HID and libusb paths:

- the export becomes a genuine Xbox 360 wired controller (`045E:028E`, four
  interfaces, descriptors in `x360_backend.go`);
- its state is the local pad read through XInput (`x360_xinput_windows.go`,
  first connected slot, 4 ms poll, follows a replug), sent as the 20-byte
  interrupt-IN report; rumble/LED written by the importer go back to the pad
  through `XInputSetState`;
- the importer's Windows binds its inbox `xusb22.sys` (no ViGEmBus, no Zadig,
  nothing to change on the agent) and games there see a normal XInput pad.

Verified live: a Razer Wolverine V2 (`1532:0A29`, GIP) exported through
`usbip attach` to the local VHCI enumerates as "Xbox 360 Controller for Windows"
(`XnaComposite`), appears in a free XInput slot and the driver's LED command
reaches the backend. `USBRIDGE_X360=0` disables it and restores the raw libusb
path. Not covered: the pad's headset/audio and plugin interfaces (acknowledged,
never active), and more than one Xbox pad exported at once (all would mirror the
first XInput slot).

Live checks: `USBRIDGE_X360_LIVE_USB='USB\VID_xxxx&PID_yyyy\<serial>' go test -v -run TestLiveX360Claim ./internal/usbpass/`
and, for the loopback export, `USBRIDGE_X360_LIVE_SECS=60 USBRIDGE_X360_SOURCE=xinput go test -v -run TestLiveX360Loopback ./internal/usbpass/`
then `usbip attach -r 127.0.0.1 -b 9-8`.

## Windows: WinUSB must already be bound (no automatic step)

libusb on Windows can only claim an interface that's already associated with
**WinUSB**, **libusb-win32**, or **libusbK** — not whatever driver Windows
picked by default (`HidUsb` for a HID device, the class driver for anything
else). Unlike Linux (`EnsureUSBAccess` in `access_linux.go`, which installs a
udev rule + unbinds the kernel driver automatically via `pkexec`), **there is
no automated equivalent on Windows today.** The interface has to be bound to
WinUSB by hand first, with [Zadig](https://zadig.akeo.ie/), before this
client can claim it at all.

### Why not automate it (already investigated)

- A plain, unsigned `Include=winusb.inf` association INF is flatly rejected —
  confirmed live via `pnputil /add-driver`: *"The third-party INF does not
  contain digital signature information."* Modern Windows will not stage an
  unsigned third-party driver package, full stop, even one that only
  references the already-trusted in-box `winusb.sys`.
- The library Zadig itself is built on ([libwdi](https://github.com/pbatard/libwdi))
  solves this by self-signing a generated catalog on the fly, but building it
  requires a real Windows Driver Kit install (Visual Studio + WDK, several GB)
  for the `WdfCoInstallerXX.dll`/`winusbcoinstaller2.dll` redistributables —
  not something this project bundles or auto-installs.
- Reimplementing libwdi's self-sign/catalog logic from scratch (`pki.c`) was
  considered and explicitly rejected: shipping an app that silently generates
  and installs its own trusted signing certificate to auto-install drivers is
  exactly the profile that gets a legitimate build flagged by AV/EDR
  heuristics. Not worth it for a one-time, per-device-model setup step.

So: **Zadig, once per device (or device model), is the intended workflow on
Windows**, not a gap to be papered over. It's a real driver rebind — the
device stops being usable by its normal Windows driver (a gamepad stops being
a normal game controller, a tablet stops working as a normal pointer) for as
long as it stays WinUSB-bound.

### Doing it

1. Download [Zadig](https://zadig.akeo.ie/) (portable, no install).
2. Run it (accept the UAC prompt — driver installation always needs admin).
3. **Options → List All Devices** (required — without this, only devices
   *without* an existing driver show up, which excludes every gamepad/tablet).
4. Pick the exact target from the dropdown. For a **composite** device (a
   gamepad is typically one — see [Multi-interface devices](#multi-interface-devices-composite-usb-below)),
   there will be one entry per interface (`MI_00`, `MI_03`, ...); pick the one
   that's actually the HID function you want (game controller / digitizer),
   not e.g. the same device's separate audio interface.
5. Driver: **WinUSB**. Click **Install Driver**.
6. Confirm the bind: `Get-PnpDevice` on that instance ID should now show
   `Class = USBDevice` instead of `HIDClass`/`MEDIA`/whatever it was.

### Reverting

WinUSB is not a system replacement, just a more specific driver association
layered over the default one — removing it restores the original binding:

1. Device Manager → find the device (usually under **libusb-win32 devices** /
   **Universal Serial Bus devices**).
2. Right-click → **Uninstall device** → check **"Attempt to remove the driver
   for this device"**.
3. Unplug/replug (or Action → Scan for hardware changes). Windows rebinds its
   normal default driver automatically.

## Multi-interface devices (composite USB)

`TryClaimGousb` (`backend_gousb.go`) claims **every interface of the active
config it can**, not just interface 0 — a composite device's actual
HID/game-controller function is frequently *not* the first interface.
Confirmed live: a Razer Raiju 2's game controller is interface 3, behind a
separate (and, in this project, unclaimed) media/audio interface 0. Only the
interface(s) actually WinUSB-bound get claimed; everything else fails to
claim and is silently left out — see the next point for why that's important,
not just a log warning to ignore.

The advertised USB/IP configuration descriptor is then **rebuilt to include
only the interfaces this process actually claimed** (`filterConfigDesc`).
Advertising an interface nothing can answer is not harmless: confirmed live
that the far side's own class driver for it (Linux's `snd-usb-audio`, for
that same Raiju's unclaimed audio interface) retried against it indefinitely,
which was observed to also delay — non-deterministically, sometimes for the
whole session — the *claimed* HID interface's own enumeration on the same
shared USB/IP connection. Filtering the unclaimed interface out of the wire
descriptor entirely made HID enumeration succeed reliably on every attempt.

## Interrupt vs. bulk endpoints

`backend_gousb.go` was originally written and tuned exclusively for USB Mass
Storage (CBW/CSW Bulk-Only Transport, `HandleBulk`'s `bulkSem`/`cycleHeld`
machinery). Every non-control endpoint used to be forced through that same
BOT cycle tracking regardless of its real transfer type — which works for a
bulk mass-storage endpoint, but an **interrupt** endpoint (a gamepad's
button/stick reports, a tablet's pen position/pressure) has no CBW/CSW
framing at all. The first interrupt read left the BOT semaphore permanently
"held" waiting for a CSW that would never come, and every read after it
queued behind a 120-second failsafe timeout — confirmed live, gamepad input
arrived roughly once every two minutes instead of in real time.

Fixed: `HandleBulk` now branches on the endpoint's actual transfer type (read
from gousb's own descriptor at claim time, not guessed from the URB). Bulk
keeps the original BOT logic, untouched. Interrupt/iso goes through
`handleNonBulk` instead — a plain per-URB read/write, no cross-URB cycle
state, polled in short bounded windows so one idle endpoint's indefinite wait
can never starve another request on the same device.

That fix alone surfaced two more real bugs, also fixed, both confirmed live
against a real Razer Raiju 2 and a real Wacom Intuos S passed through
end-to-end (Windows client → WinUSB/libusb claim → USB/IP export → rust-shine
AES attach → Linux agent → vhci-hcd → native `usbhid`/`wacom.ko` driver →
evdev, live button presses / pen strokes with pressure all the way through):

- **`devMu`**: gousb's Windows/WinUSB backend returned spurious
  `LIBUSB_ERROR_NOT_FOUND`/`PIPE` when an interrupt read and an unrelated
  control request hit the same device handle at the same instant — `devMu`
  serializes individual libusb calls against each other. Deliberately *not*
  held across `HandleControl`'s device-forward call: an unclaimed interface's
  own class-specific probe can legitimately eat gousb's full 5s
  `ControlTimeout` on every attempt (confirmed live: USB Audio Class
  `GET_CUR`/`SET_CUR` probes from an unclaimed audio interface), and holding
  a shared lock for that starved everything else on the device behind it.
- **`server.go`'s `Stop()` ordering**: it used to close each device's
  `Backend` (`libusb_close` for the gousb backend) *before* waiting for that
  connection's in-flight URB-handling goroutines to actually finish, not
  after. Closing a `net.Conn` only unblocks its blocked `Read()` — the
  goroutine still has to run `serveURBs`' own deferred cancel-and-drain
  before it returns. A bulk transfer rarely hit this window (it completes in
  well under a second); an interrupt read blocking for its own poll window
  made the race land reliably — confirmed live as a SIGSEGV inside
  `libusb_close`, racing a still-in-flight control call. Fixed by waiting for
  every connection to actually finish before touching any backend.

## Known remaining gap

A Wacom tablet's vendor `SET_REPORT` (feature report, mode/sensitivity
configuration) STALLs when forwarded through the raw claim — confirmed live,
`wacom.ko` logs `wacom_set_report: ran out of retries`. This does **not**
block the main data path: pen X/Y/pressure/distance and stylus buttons all
arrive live and correctly over the interrupt endpoint regardless. Only that
one optional vendor configuration write is affected. Not yet root-caused.

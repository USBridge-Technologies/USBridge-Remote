# Client TODO

## Rumble on PlayStation-compatible (DirectInput) pads, Windows

A host game's vibration reaches the client as Moonlight `ConnListenerRumble`
(`internal/service/moonlight_rumble.go`) and is applied to the captured pad by
`platform.SetGamepadRumble` (`internal/platform/gamepad_rumble_windows.go`). That
only works for XInput pads (`xinput:N`, `XInputSetState`). A pad captured as
`winmm:N` -- e.g. the Razer Raiju Tournament Edition, USB `1532:1007`, whose
input is mapped through its SDL entry in `gamepad_sdlmap.go` -- does not vibrate:
WinMM/DirectInput input has no output path, and the pad has no XInput slot.

What is missing:

- **Write the pad's HID output report.** Open the pad's HID interface (its
  `HID\VID_1532&PID_1007&MI_03` device path, via SetupAPI/`CreateFile`, not WinMM)
  and `WriteFile` an output report. A DualShock 4-class pad takes report `0x05` over
  USB (flags byte, then the small/high-frequency and large/low-frequency motor
  bytes, then lightbar RGB). **Not verified for the Raiju TE**: capture what the
  vendor software or a game sends first (USBPcap / the `hiddescprobe` tool), do not
  assume the DS4 layout.
- **Map levels.** Moonlight gives `low` (large motor) and `high` (small motor) as
  0..65535; the report wants 0..255 per motor.
- **Stop cleanly.** Send zeros when the capture stops or the host sends `(0, 0)`,
  and keep re-sending while non-zero if the pad has a rumble watchdog (DS4 pads
  stop after a few seconds without a refresh).
- **Wrong-pad bug to fix at the same time.** `xinputSlotFor` treats a `winmm:N` id
  as a hint and falls back to the *first connected XInput slot*. With an Xbox pad
  plugged in next to the Raiju, the Raiju's rumble would vibrate the Xbox pad. A
  `winmm:N` pad with an SDL mapping should never fall back to an XInput slot.

Prerequisite for testing: the pad must be bound to the inbox HID driver. The
libwdi WinUSB package that USB passthrough installs (`razer_raiju_..._(interface_3)`,
`oem*.inf`) replaces it and hides the pad from WinMM/HID entirely; it has to be
removed or rolled back (`pnputil /delete-driver oemNN.inf /uninstall /force`, then
replug) before any of the above can be exercised. Whether passthrough should
restore the original binding itself when it releases a device is a separate open
question.

Linux hosts/clients are not affected: `hid-sony` / `xpad` expose force feedback
through evdev, which `gamepad_rumble_linux.go` already drives (verified on a
Razer Wolverine V2). macOS has no gamepad rumble path at all.

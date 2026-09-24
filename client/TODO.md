# Client TODO

See [docs/GAMEPADS.md](docs/GAMEPADS.md) for what is supported today.

## Gamepads

- **Verify on hardware what is only unit-tested:** two pads at once through the real
  client (controller numbers, the departure event, rumble routed by controller
  number), the PS button as Guide on the Raiju TE (button 13 by the DS4 descriptor,
  not pressed yet), XInput capture input on a Razer Wolverine V2.
- **Touchpad-as-mouse on Linux and macOS.** Windows reads the DS4 touchpad from the HID
  input report. On Linux the touchpad is a separate evdev device
  (`Wireless Controller Touchpad`, `ABS_MT_*`), on macOS an IOKit element; the mouse
  gain (about 0.6 counts per unit) and a tap-to-click option are not configurable.
- **Rumble for more pads.** Only the Raiju TE and Sony DualShock 4 ids have an HID
  output-report protocol (`hidRumbleProtocols`). Other Raiju models
  (`1532:1000/1004/1009/100A`) map like the TE but are not enabled for rumble until
  someone has felt them vibrate. DualSense (`054C:0CE6`) uses a different report and
  Switch Pro another; neither is written.
- **Hot-plug.** The list of pads is read at start and on *Refresh*; watch for
  device arrival (Windows `RegisterDeviceNotification` / evdev inotify).
- **Send the upper 16 button bits.** The cgo senders take `unsigned short` buttons,
  so paddles and Misc cannot go over the wire (the touchpad is sent as a mouse
  instead).
- **libwdi WinUSB leftovers.** The WinUSB package that USB passthrough installs
  (`razer_raiju_..._(interface_3)`, `oem*.inf`) hides the pad from HID/WinMM/XInput
  and is not rolled back when the device is released. Passthrough should restore the
  original binding itself.

## Keyboard input modes

The Control footer has a keyboard input mode menu: **Keys** (raw keys by physical
position, the host layout decides) and **Characters** (the client layout decides,
the character is typed on the host). Done and tested for a Windows host; the rest:

- **Characters mode for Linux and macOS hosts through the clipboard.** Today ASCII
  goes as VK+Shift (right only while the host layout is Latin) and anything else
  through Sunshine `unicode()` (on Linux the IBus Ctrl+Shift+U trick, which most
  non-GTK apps don't understand). Type through the host clipboard instead: send
  the text over the clipboard channel, press Ctrl+V (Cmd+V on macOS), put the
  previous host clipboard back. Batch fast typing into one paste.
- **Check on a real macOS client:** the double-typed `0`/`8`/`A`/`S`/`E`/`R` fix
  (`input.NormalizeScanCode`, kVK table) and Keys mode. Only unit-tested.
- **Check on a real Linux client:** Keys mode and the xkb navigation-cluster table.
- **Web (wasm) client and mobile hardware keyboards** have no Keys mode yet.
- **Clipboard menu on mobile.** Send/Get are in the desktop footer only; mobile
  keeps just the auto-sync toggle in the mouse menu.

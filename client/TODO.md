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

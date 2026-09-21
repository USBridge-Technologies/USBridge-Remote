# SDL_GameControllerDB

Source: https://github.com/mdqinc/SDL_GameControllerDB (zlib license, see `LICENSE`).

`internal/platform/gamecontrollerdb_windows.txt` is a filtered excerpt of the
database's `gamecontrollerdb.txt` (only the `platform:Windows` lines whose GUID
is a USB HID pad), embedded into the Windows client. It tells the client which
button and axis of a DirectInput gamepad is which Xbox control, so a
PlayStation-layout pad (Razer Raiju, DualShock clones, ...) maps correctly. The
excerpt is marked as altered in its own header, as the license requires.

To refresh it, re-run the filter over a newer `gamecontrollerdb.txt` and keep the header.

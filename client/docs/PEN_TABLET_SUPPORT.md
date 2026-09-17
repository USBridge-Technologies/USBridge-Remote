# Wacom pen tablet support (macOS client)

## Two independent paths

The macOS client can get a Wacom tablet's pen input to the remote host two different ways:

1. **Semantic pen protocol** (`internal/platform/pen_capture_darwin.go` +
   `internal/gui/controller/disk_widget_pen.go`) — IOHIDManager taps the tablet's raw HID
   input reports, decodes them client-side into X/Y/pressure/tilt/rotation, and calls
   Moonlight's native `LiSendPenEvent` directly. No USB emulation involved; auto-starts the
   instant a supported tablet is plugged in and a session is active, the same way built-in
   keyboard/mouse capture works. **Only understands one raw byte layout** — see
   [Supported models](#supported-models) below. A tablet whose model isn't in that list still
   shows up as "connected" but produces zero pen events.
2. **Real USB/IP HID passthrough** (`internal/usbpass/hidbridge_darwin.go`, via the USB
   Passthrough device list) — taps the same raw HID input reports non-exclusively, but instead
   of decoding them, wraps them in a byte-faithful synthetic USB Device/Configuration/HID
   descriptor (the tablet's *real* VID/PID/report descriptor) and serves it over the project's
   USB/IP export server. The agent's own native Wacom driver enumerates and parses it — **this
   path has no model restriction at all**, since it never decodes anything itself.

If you just need a tablet to work and don't care which path, USB/IP passthrough already
supports any Wacom (or other HID) device. The rest of this document is about path 1, which
gets you lower latency and no driver install on the agent side, but only for cataloged models.

## Supported models

`wacomIntuosV2Ranges` in `pen_capture_darwin.go` lists every model this project has confirmed
shares `decodePenReport`'s exact byte layout (report ID `0x10`, 24-bit little-endian X/Y,
`uint16` LE pressure at offset 8, ...). That list was built by asking GitHub which of
[OpenTabletDriver](https://github.com/OpenTabletDriver/OpenTabletDriver)'s own per-model
configs tag the device with its `IntuosV2.IntuosV2ReportParser` class — not by guessing from
model-name similarity, which turned out to be actively misleading (see
[What doesn't work](#what-doesnt-work)):

```
gh api search/code -q '"IntuosV2ReportParser" repo:OpenTabletDriver/OpenTabletDriver \
  path:OpenTabletDriver.Configurations/Configurations/Wacom'
```

| Model | PID | MaxX | MaxY | MaxPressure | Verified |
|---|---|---|---|---|---|
| CTL-4100 (Intuos S / Bamboo) | `0x0374` | 15200 | 9500 | 4095 | **Live, real hardware** |
| CTL-6100 (Intuos M / Bamboo) | `0x0375` | 21600 | 13500 | 4095 | OpenTabletDriver spec |
| CTL-4100WL | `0x0376`, `0x0377`, `0x03C5` | 15200 | 9500 | 4095 | OpenTabletDriver spec |
| CTL-6100WL | `0x0378`, `0x03C7` | 21600 | 13500 | 4095 | OpenTabletDriver spec |
| PTH-460 (Intuos Pro Small) | `0x0392`, `0x03DC` | 31920 | 19950 | 8191 | OpenTabletDriver spec |
| PTH-660 (Intuos Pro Medium) | `0x0357` | 44800 | 29600 | 8191 | OpenTabletDriver spec |
| PTH-860 (Intuos Pro Large) | `0x0358` | 62200 | 43200 | 8191 | OpenTabletDriver spec |
| DTC-121 | `0x03CE` | 25632 | 14418 | 4095 | OpenTabletDriver spec |
| DTC-133 | `0x03A6` | 29434 | 16556 | 4095 | OpenTabletDriver spec |
| DTH-227 (Cintiq Pro 22) | `0x03D0` | 96012 | 54356 | 8191 | OpenTabletDriver spec |
| DTH-271 (Cintiq Pro 27) | `0x03C0` | 120032 | 67868 | 8191 | OpenTabletDriver spec |
| DTH-1320 (Cintiq Pro 13) | `0x034F` | 59552 | 33848 | 8191 | OpenTabletDriver spec |
| DTH-3220 (Cintiq Pro 32) | `0x0352` | 140384 | 79316 | 8191 | OpenTabletDriver spec |
| DTK-1660 (Cintiq 16) | `0x0390`, `0x03AE` | 69632 | 39518 | 8191 | OpenTabletDriver spec |

All entries use Wacom's vendor ID `0x056A`. A PID not in this table falls back to the
CTL-4100 range (`PenRangeFor` in `pen_capture_darwin.go`) rather than guessing a large-format
one, on the assumption an uncatalogued device is more likely a close relative of the small
consumer tablet than a Cintiq Pro 32.

Only **CTL-4100** has been confirmed against real hardware (a physical Wacom Intuos S — see
`client/cmd/pentest`). Every other row is copied from OpenTabletDriver's own published
`Configurations/Wacom/<model>.json` specs and trusted on the assumption OpenTabletDriver's
parser-class tag for that model is accurate. If it's ever wrong for one model, only that
model's normalization is affected — CTL-4100 stays correct.

## What doesn't work

OpenTabletDriver's Wacom support spans **14 genuinely different binary report formats**
(`OpenTabletDriver.Configurations/Parsers/Wacom/{Bamboo,BambooPad,BambooV2,CintiqV1,Graphire,
Intuos,Intuos3,Intuos4,IntuosPro,IntuosV1,IntuosV2,IntuosV3,PL,PTU}`), covering ~85 total
model configs. This project only ports `IntuosV2`. Notably, older/other-generation models
that *sound* like they should share CTL-4100's layout do not — **CTH-680** and **CTL-470**,
for example, are tagged `Intuos.IntuosReportParser`, a completely different 10-byte report
format, not `IntuosV2`. Do not extend `wacomIntuosV2Ranges` by guessing from a model name;
confirm the exact `ReportParser` class in that model's OpenTabletDriver JSON first.

Porting the other 13 parser classes would require translating their C# byte-offset logic by
hand, and none of them can be verified against real hardware in this project today (only one
physical tablet, a CTL-4100, is available for live testing) — silently-wrong pressure/position
decoding is worse than a device that visibly does nothing, so that work hasn't been done
speculatively. If you need one of those older models supported, the USB/IP passthrough path
(above) already works for it today with no changes.

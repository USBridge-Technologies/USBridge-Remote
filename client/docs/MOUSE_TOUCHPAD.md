# Pointer control: touchpad / touchscreen / absolute

## Description

The client can control the pointer on the remote machine over the video using three main modes:

- **`mouse` (touchpad / relative)** — movement is sent as `dx/dy` (HID mouse).
- **`touchscreen` (touchscreen / absolute + touch)** — movement and clicks are sent as `x/y` + `tip` (HID touchscreen).
- **`absolute` (absolute / absolute without touch)** — position is sent as `x/y` without touch; clicks are sent as separate mouse clicks.

## Available modes

Currently available:
1. **Touchpad** — standard relative-movement mode.
2. **Absolute** — absolute positioning mode (Single Display).

> **Note:** **Abs L/2** and **Abs R/2** modes (for multi-monitor systems) are temporarily disabled pending further work on the coordinate calibration algorithm.

## API and security

All mouse control commands (`POST /api/mouse`) now require:
1. A valid HMAC-SHA256 signature in the headers.
2. An active sync session (Master QR Sync).

### Request format
```json
{
  "action": "move|click|scroll|touch|touch_position",
  "dx": 0,
  "dy": 0,
  "x": 0,
  "y": 0,
  "button": 1,
  "tip": false
}
```
Coordinate range for absolute modes: **0..4095**.

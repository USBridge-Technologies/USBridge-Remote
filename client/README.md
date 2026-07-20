# USBridge Client

Native cross-platform Go client for controlling USBridge 2.

The project uses the new secure **Master QR Sync** protocol for authorization and control. All commands are signed with HMAC-SHA256, and sensitive data (PIN codes, keys) is encrypted with AES-GCM.

## Features

- **Master QR Sync**: Fast one-scan connection. A single API Secret is passed via QR code and used to sign all requests.
- **Security**: Full API protection against interception and spoofing on the local network.
- **Device control**: keyboard, mouse/touchscreen (Touchpad/Absolute), RNDIS, CD-ROM images.
- **NBD export**: streaming local images (`.iso`, `.img`, etc.) to the remote machine.
- **Hardware-Accelerated Video**: Ultra-low latency streaming using Sunshine/Moonlight stack directly over Vulkan and Metal. 
- **Android & iOS support**: Full support for high-performance streaming and input on mobile devices.

## How the client connects (Protocol v2)

1. On the server (Agent/Bridge), the **Master Sync QR** screen is opened.
2. In the client, the QR scanner icon is tapped.
3. After scanning, the client:
   - Extracts the **API Secret** and stores it for signing requests.
   - Performs a secure sync (Sunshine PIN exchange, Tailscale registration).
   - Brings up a direct connection or a Tailscale tunnel.
4. All subsequent actions (mouse movement, key presses) are automatically signed with the key.

## Video & Rendering

- Protocol: **Sunshine / Moonlight Stack**.
- Rendering: **Vulkan** (Linux/Windows/Android) and **Metal** (macOS/iOS).
- Performance: Pure native hardware decoding and rendering, no subprocess or IPC overhead.

## Mouse modes

Two main modes are supported:
- **Touchpad** (relative) — emulates a standard touchpad.
- **Absolute** — direct cursor positioning on screen.

> Multi-monitor modes (Abs L/2, Abs R/2) are temporarily disabled.

## Build & dependencies

Detailed build instructions for all platforms (Linux, macOS, Windows, Android, iOS) are in the `scripts/` folder.

## Documentation

- `docs/api_endpoints.md` — description of the secure API and sync protocol.
- `docs/MOUSE_TOUCHPAD.md` — details of pointer control modes.
- `docs/NATIVE_VIDEO_AUDIO.md` — details of the Vulkan/Metal and Moonlight integration.

## License

GPLv3 (see `LICENSE`). The project uses moonlight-common-c (GPLv3).

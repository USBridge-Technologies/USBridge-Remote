<div align="center">

# USBridge Agent

**Turn any PC into a USBridge — no hardware required.**

![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)
![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)
![License: GPLv3](https://img.shields.io/badge/license-GPLv3-blue)

![USBridge Agent](docs/assets/screenshot_agent.png)

</div>

One binary. Windows, macOS, Linux — same codebase, same protocol as the
physical [USBridge](https://github.com/itsme228/usbridge_client) KVM. Install
it on any machine and [usbridge_client](https://github.com/itsme228/usbridge_client)
controls it exactly like it would a real USBridge device.

## Why

- Tailscale is built in — system or userspace, agent registers itself on the
  tailnet on boot. Connect direct on LAN or over Tailscale, nothing else in
  between.
- Sunshine does the capture/encode, bundled and auto-launched. The agent never
  touches a video frame — it pairs with Sunshine's admin API and relays
  Moonlight PINs.
- Every request is HMAC-SHA256 signed over the master key, ±60s replay window.
  Pairing itself is AES-256-GCM. Same scheme as the client and the hardware unit.
- Native input injection: `SendInput` on Windows, `CGEvent`/Quartz on macOS,
  direct injection on Linux.
- On Linux, flipping Sunshine to KMS capture only needs one `pkexec` grant —
  it sets `CAP_SYS_ADMIN` on the binary, persists across reboots, and the
  portal permission dialog doesn't come back.
- Control window shows LAN/Tailscale addresses, Sunshine health, permission
  status, and Tailscale sign-in in one place.

## Quick start

```
1. Run the agent on the machine you want to control.
2. It shows a pairing token + LAN/Tailscale address.
3. Install usbridge_client anywhere else, scan/enter the token, connect.
```

That's the whole setup.

## Build

```bash
# macOS
./scripts/build_macos.sh

# Windows — MSYS2 UCRT64
./scripts/build_windows.sh

# Linux
./scripts/build_linux.sh

# or just run it
go run ./cmd/usbridge_agent
```

Each script fetches and stages the matching Sunshine release automatically.

```
USBRIDGE_SKIP_SUNSHINE=1        skip bundling Sunshine (dev builds)
USBRIDGE_SUNSHINE_FORCE=1       re-download even if already staged
USBRIDGE_SUNSHINE_VERSION=<tag> pin a specific Sunshine release
```

> **macOS:** grant Screen Recording + Accessibility when prompted, then always
> launch from the same installed path (`~/Applications/USBridgeAgent.app`).
> Dev builds aren't ad-hoc signed, so re-signing on every rebuild looks like a
> new app to macOS and both prompts come back.

## License

GPLv3 — see [`LICENSE`](LICENSE). Bundles [Sunshine](https://github.com/LizardByte/Sunshine) (GPLv3).

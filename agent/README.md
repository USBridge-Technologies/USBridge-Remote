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

- **Built-in Tailscale**: system or userspace, agent registers itself on the tailnet on boot. Connect direct on LAN or over Tailscale, nothing else in between.
- **Hardware-Accelerated Sunshine Streaming**: Sunshine does the capture and encoding, bundled and auto-launched. The agent leverages Vulkan/Metal for optimal performance. It pairs with Sunshine's admin API and relays Moonlight PINs flawlessly.
- **Zero Extra Processes**: Native decode/render pipeline via Vulkan/Metal, no subprocess or IPC overhead, for maximum frame rates and zero latency.
- **Bank-grade Security**: API requests are HMAC-SHA256 signed over the master key, ±60s replay window. The pairing handshake is AES-256-GCM encrypted. Same scheme as the client and the hardware unit.
- **Native input injection**: `SendInput` on Windows, `CGEvent`/Quartz on macOS, direct injection on Linux.
- **Wayland Native**: On Linux, flipping Sunshine to KMS capture only needs one `pkexec` grant — it sets `CAP_SYS_ADMIN` on the binary, persists across reboots, and the portal permission dialog doesn't come back.
- **Unified Dashboard**: Control window shows LAN/Tailscale addresses, Sunshine health, permission status, and Tailscale sign-in in one place.

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

## Launch at Login (autostart)

The "Launch at Login" checkbox has no stored on/off state of its own — it
always reflects whatever the OS's real autostart mechanism currently says
(`systemctl is-enabled usbridge-agent.service` on Linux), so it only shows
checked if that mechanism is actually registered on this machine. It starts
unchecked on a fresh machine and only gets enabled when you check it.

Checking it registers **the exact file you're currently running** as the
command to launch at boot:
- Running as an AppImage: the `$APPIMAGE` path (the outer `.AppImage` file
  itself, not its ephemeral mount point) — e.g.
  `/path/to/USBridgeAgent-Linux-x86_64-2.0.23.AppImage`.
- Running as a plain binary: `os.Executable()`'s path.

Always with `--headless` appended, so autostart brings up the HTTP/Sunshine/
Tailscale engine on every boot without popping a window; opening the app
normally afterwards just attaches a GUI to that already-running instance.

On Linux this installs a **system-wide** (not `--user`) systemd unit at
`/etc/systemd/system/usbridge-agent.service` via `pkexec`, deliberately, so
it can start before any graphical session exists — needed for KMS capture,
which reads DRM/KMS directly with no compositor/portal required. Re-checking
the box from a different path overwrites the unit with the new path;
un-checking it removes the unit entirely. If you move/rename the binary
without re-toggling the checkbox, the installed unit still points at the old,
now-missing path — toggle it off/on again from the new location, or fix it
manually:
```bash
sudo systemctl disable --now usbridge-agent.service
sudo rm -f /etc/systemd/system/usbridge-agent.service
sudo systemctl daemon-reload
```

## Lock GPU Clocks (Windows + NVIDIA)

Windows-only, shown in the **Permissions** panel only on machines where it's
supported. When checked, the agent launches an elevated helper
(`gamestream-server.exe --gpu-clock-lock-daemon --watch-pid <PID>`) that holds
an NVML max-clock lock for the life of the streaming session, so the GPU
doesn't idle into a lower power state between frames and stall the encoder on
the next one.

There's no persistent, one-time grant for this on Windows (unlike Linux's
`CAP_SYS_ADMIN` setcap for KMS capture) — NVML's clock-lock call requires the
*calling process itself* to be elevated, so a fresh UAC prompt is required
every time a streaming session actually (re)starts. Checking the box both
arms the lock immediately (if a session is already running) and re-arms it
automatically on every future session start; there's no separate "Request"
button, since the checkbox itself is what triggers the request. Unchecking it
only stops *future* sessions from spawning the helper — it doesn't kill one
that's already running, since that would drop clocks mid-session; the helper
exits on its own once the streaming process it's watching does.

## License

GPLv3 — see [`LICENSE`](LICENSE). Bundles [Sunshine](https://github.com/LizardByte/Sunshine) (GPLv3).

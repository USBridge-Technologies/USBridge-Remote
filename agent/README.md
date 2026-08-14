<div align="center">

# 💻 USBridge Agent

**Turn any PC or Mac into a remote-controllable USBridge — no hardware required.**

![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)
![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)
![License: GPLv3](https://img.shields.io/badge/license-GPLv3-blue)

![USBridge Agent](docs/assets/screenshot_agent.png)

</div>

One lightweight binary for Windows, macOS, and Linux. It shares the exact same codebase and high-performance protocol as the physical [USBridge KVM](https://usbridge.io). Install it on any machine, and the USBridge Client controls it just like it would a real, hardware USBridge device.

---

> 💖 **Patreon Exclusive:** Early access to cutting-edge features—such as **WebRTC support for the Web Client**, **Sunshine performance optimizations for macOS**, **working Linux login screens on NVIDIA**, and more—is available to our supporters on [Patreon](https://www.patreon.com/USBridge_Technologies). *(Note: The standard upstream Sunshine build does not support WebRTC, so the web client requires this custom build).*

---

## ✨ Why it's Awesome

- **Hardware-Accelerated Sunshine Streaming**: Sunshine handles the capture and encoding, seamlessly bundled and auto-launched. The agent leverages Vulkan/Metal for optimal performance and pairs with Sunshine's admin API to relay Moonlight PINs flawlessly.
- **Built-in Tailscale**: Whether system-wide or userspace, the agent registers itself on your Tailnet on boot. Connect directly on your LAN or securely over Tailscale—nothing else in between.
- **Bank-Grade Security**: API requests are HMAC-SHA256 signed over the master key with a ±60s replay window. The pairing handshake is AES-256-GCM encrypted, using the same robust scheme as the client and the hardware unit.
- **Native Input Injection**: Uses `SendInput` on Windows, `CGEvent`/Quartz on macOS, and direct injection on Linux for 1:1 precise, latency-free mouse and keyboard emulation.
- **Wayland Native**: On Linux, switching Sunshine to KMS capture only needs a single `pkexec` grant. It sets `CAP_SYS_ADMIN` on the binary and persists across reboots—meaning the annoying portal permission dialog never comes back.
- **Unified Dashboard**: The control window neatly displays your LAN/Tailscale addresses, Sunshine health, permission statuses, and Tailscale sign-in all in one place.

## 🚀 Quick Start

1. **Run the agent** on the machine you want to control.
2. It will display a Master QR pairing token alongside its LAN and Tailscale IP addresses.
3. **Open the USBridge Client** anywhere else, scan or enter the token, and connect instantly.

That's the entire setup process.

## 🛠️ Build Instructions

Each script fetches and stages the matching native Sunshine release automatically.

```bash
# macOS
./scripts/build_macos.sh

# Windows — MSYS2 UCRT64
./scripts/build_windows.sh

# Linux
./scripts/build_linux.sh

# or just run it directly (dev mode)
go run ./cmd/usbridge_agent
```

**Environment Variables for Build Customization:**
```bash
USBRIDGE_SKIP_SUNSHINE=1        # Skip bundling Sunshine (useful for fast dev builds)
USBRIDGE_SUNSHINE_FORCE=1       # Force re-download even if already staged
USBRIDGE_SUNSHINE_VERSION=<tag> # Pin to a specific Sunshine release version
```

> **macOS Note:** Grant Screen Recording + Accessibility when prompted, then always launch from the same installed path (`~/Applications/USBridgeAgent.app`). Development builds aren't ad-hoc signed, so re-signing on every rebuild looks like a completely new app to macOS, causing both prompts to return.

## ⚙️ Launch at Login (Autostart)

The "Launch at Login" checkbox has no stored on/off state of its own. It directly reflects whatever your OS's actual autostart mechanism currently reports (`systemctl is-enabled usbridge-agent.service` on Linux). It starts unchecked on a fresh machine and only becomes enabled when you check it.

Checking it registers **the exact executable you're currently running** as the command to launch at boot:
- **AppImage**: The outer `$APPIMAGE` path (the `.AppImage` file itself, not its ephemeral mount point).
- **Plain binary**: Evaluates via `os.Executable()`.

It always appends the `--headless` flag, ensuring autostart brings up the HTTP, Sunshine, and Tailscale engines silently in the background on every boot. Opening the app normally afterwards simply attaches the GUI to the already-running background instance.

**Linux specifics:**
On Linux, this installs a **system-wide** (not `--user`) systemd unit at `/etc/systemd/system/usbridge-agent.service` via `pkexec`. This is done deliberately so it can start before any graphical session exists—which is crucial for KMS capture (reading DRM/KMS directly with no compositor or portal required). Re-checking the box from a different path overwrites the unit with the new path; un-checking it removes the unit entirely.

If you move or rename the binary without toggling the checkbox, the installed unit will point at a missing path. Simply toggle it off and on again from the new location, or fix it manually:
```bash
sudo systemctl disable --now usbridge-agent.service
sudo rm -f /etc/systemd/system/usbridge-agent.service
sudo systemctl daemon-reload
```

## 🎮 Lock GPU Clocks (Windows + NVIDIA)

This is a Windows-only feature, shown in the **Permissions** panel exclusively on supported machines. When checked, the agent launches an elevated helper (`gamestream-server.exe --gpu-clock-lock-daemon --watch-pid <PID>`) that holds an NVML max-clock lock for the entire duration of the streaming session. This prevents the GPU from idling into a lower power state between frames, ensuring the encoder doesn't stall on the next frame.

Unlike Linux's permanent `CAP_SYS_ADMIN` setcap, there is no persistent one-time grant for this on Windows. NVML's clock-lock call requires the *calling process itself* to be elevated, which means a fresh UAC prompt is required every time a streaming session actually (re)starts.

Checking the box arms the lock immediately (if a session is already running) and re-arms it automatically for all future sessions. There's no separate "Request" button because the checkbox itself triggers the UAC request. Unchecking it only stops *future* sessions from spawning the helper; it won't kill an already-running helper to avoid dropping clocks mid-session (the helper exits on its own once the streaming process it's watching exits).

## 📜 License

**GPLv3** — see [`LICENSE`](LICENSE). The agent automatically bundles and interfaces with [Sunshine](https://github.com/LizardByte/Sunshine) (GPLv3).

# USBridge Agent

![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)
![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)
![License: GPLv3](https://img.shields.io/badge/license-GPLv3-blue)

**USBridge Agent** is the software counterpart to the [USBridge](https://github.com/itsme228/usbridge_client) KVM hardware: a small, fully cross-platform background service — one codebase, native builds for **Windows, macOS, and Linux** — that you install on the machine you want to control remotely, no dongle required. It bundles [Sunshine](https://github.com/LizardByte/Sunshine) for GameStream-grade capture/encode and speaks the exact same protocol as `usbridge_client` and the physical USBridge device, so one client app controls all three.

![USBridge Agent](docs/assets/screenshot_agent.png)

## What it does

- **Runs Sunshine for you.** On startup the agent launches a bundled Sunshine binary (skipped if one's already reachable), bootstraps its admin account, and relays Moonlight pairing PINs — you never touch Sunshine's own UI.
- **Tailscale is built in.** No FRP, no relay server, no port forwarding to set up. The agent can run its own userspace Tailscale node or ride on a system Tailscale install, and registers itself over the tailnet automatically.
- **Two ways in: direct or Tailscale.** LAN socket to `host:8080`, or the same socket over your tailnet — you pick per connection, nothing routes through a third-party relay.
- **Every request is signed.** All API calls (except the initial pairing handshake) carry an HMAC-SHA256 signature derived from your master key, with a ±60s replay window. Pairing itself is AES-256-GCM encrypted.
- **Native input injection.** `SendInput` on Windows, Quartz/`CGEvent` on macOS, matching backends on Linux.
- **A real control window**, not just a tray icon — shows your LAN/Tailscale addresses, permission status (Accessibility/Screen Recording), Sunshine health, and a Tailscale sign-in/sign-out button. On Linux it also lets you switch Sunshine's capture backend (desktop portal vs. root/KMS) with one click.
- **No repeated Wayland prompts.** Switch to the root/KMS capture backend once — a single `pkexec` grant sets a persistent `CAP_SYS_ADMIN` capability on the bundled Sunshine binary — and capture keeps working with no portal permission dialog popping up on every new connection or after every reboot, unlike the default XDG desktop-portal capture path.

## Quick start

1. **Install the agent** on the machine you want to control. Launch it — the window shows a pairing token and your network addresses (LAN + Tailscale, once signed in).
2. **Install [usbridge_client](https://github.com/itsme228/usbridge_client)** on the device you'll control *from*.
3. **Pair once** — scan the QR / enter the token in the client, pick direct or Tailscale, and connect.

## Building from source

```bash
# macOS (run on macOS)
./scripts/build_macos.sh
./scripts/install_macos.sh
open "$HOME/Applications/USBridgeAgent.app"

# Windows (MSYS2 UCRT64 shell)
./scripts/build_windows.sh

# Linux
./scripts/build_linux.sh
```

All three scripts download and stage the matching official Sunshine release automatically (`scripts/fetch_sunshine.sh`). Useful overrides:

| Variable | Effect |
| --- | --- |
| `USBRIDGE_SKIP_SUNSHINE=1` | Skip bundling Sunshine (offline/dev builds) |
| `USBRIDGE_SUNSHINE_FORCE=1` | Re-download Sunshine even if already staged |
| `USBRIDGE_SUNSHINE_VERSION=<tag>` | Pin a specific Sunshine release |

> **macOS note:** always launch the same installed app path (`~/Applications/USBridgeAgent.app` recommended). Dev builds aren't ad-hoc signed by default — re-signing on every rebuild makes macOS treat each build as a new app and re-prompt for Accessibility/Screen Recording.

Run without building a bundle:

```bash
go run ./cmd/usbridge_agent
```

## macOS permissions

| Permission | Needed for |
| --- | --- |
| Screen Recording | video/screen capture |
| Accessibility | mouse and keyboard injection |

## Configuration & logs

- Config: `config.yaml` next to the binary, or `~/.config/usbridge-agent/`
- Log: `~/Library/Logs/USBridgeAgent/app.log` (override with `USBRIDGE_LOG_DIR`)

Key `config.yaml` fields:

```yaml
http_port: 8080          # agent's own API port
tailscale_enabled: true
tailscale_mode: system   # or "userspace" — no system Tailscale install needed
sunshine_port: 47990     # Sunshine's local admin API
master_key: ""           # set during pairing; signs every request
```

## Known limitations

- Disk mount via `nbd-iSCSI` is wired up at the transport/API level, but the concrete Windows mount command still needs environment-specific tuning.
- A legacy FFmpeg-based `/api/video/*` capture path still exists as a fallback but isn't exercised by the normal Moonlight/Sunshine flow.

## License

GPLv3 — see [`LICENSE`](LICENSE). Bundles [Sunshine](https://github.com/LizardByte/Sunshine) (GPLv3).

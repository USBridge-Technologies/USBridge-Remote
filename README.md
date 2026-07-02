# usbridge_agent

Software KVM backend for `usbridge_client`, compatible with the same
Moonlight/Sunshine protocol as the `usbridge` hardware implementation.

Implemented now:

- compatible core HTTP API (`/api/device/*`, `/api/keyboard`, `/api/mouse`, `/api/video/*`, `/api/screen`,
  `/api/drives/local`, `/api/auth/tailscale/*`)
- every request except `/api/healthz`, `/api/auth/qr*`, and `/api/auth/sync` is HMAC-SHA256 signed
  (`X-Auth-Signature` / `X-Auth-Timestamp`, ±60s window) using the master key as the raw secret
  (`SHA256(masterKey)`, never hex-decoded) — matches `usbridge_client` and the canonical `usbridge` server
- `/api/auth/sync` is AES-256-GCM encrypted with the same derived key (pairing/Tailscale/Sunshine-PIN bootstrap)
- Sunshine (Moonlight GameStream host) does all video/audio/input capture and streaming; the agent is not in
  the video path — it only pairs with Sunshine's local admin API (port 47990) and relays Moonlight PINs
- the agent launches the bundled Sunshine binary itself on startup (skipped if an instance is already
  reachable on the admin port) and bootstraps its `sunshine`/`sunshine` web-UI admin account on first run
- two connection modes only, matching `usbridge_client`: **direct** (plain LAN socket to
  `internal_host:http_port`) and **tailscale** (plain socket over the Tailscale interface) — no tunnel/proxy
  layer (FRP was removed; neither the client nor the canonical `usbridge` server route traffic through it
  anymore)
- Windows HID input via `SendInput`
- macOS HID input via Quartz / `CGEvent`
- Tailscale integration: interactive login, or unattended auth-key registration via `/api/auth/sync` /
  `/api/auth/tailscale/register`
- Fyne desktop control window, including a Linux-only Sunshine capture-mode selector (Portal vs. KMS/root,
  with a "Request" button that grants `CAP_SYS_ADMIN` to the bundled Sunshine binary via `pkexec`)
- build scripts (`scripts/build_*.sh`) download and bundle the matching official Sunshine release next to
  the agent binary automatically (see `scripts/fetch_sunshine.sh`)

Current limitation:

- disk mount through `nbd-iSCSI` is prepared at transport/API level, but concrete Windows mount command still needs environment-specific tuning
- the legacy FFmpeg-based `/api/video/*` capture pipeline is still present as a fallback but is not exercised
  by the Moonlight/Sunshine flow

Start:

```powershell
go run ./cmd/usbridge_agent
```

Build for macOS from macOS:

```bash
./scripts/build_macos.sh
./scripts/install_macos.sh
open "$HOME/Applications/USBridgeAgent.app"
```

For stable macOS TCC permissions, always launch the same installed app path.
Recommended path: `~/Applications/USBridgeAgent.app`.
Development builds are intentionally not ad-hoc signed by default, because re-signing on each rebuild can cause macOS to treat the app as a different client for Accessibility/Screen Recording.

Build for Windows from Windows/MSYS2 UCRT64:

```bash
./scripts/build_windows.sh
```

The script expects the `UCRT64` shell and builds `dist/windows/USBridgeAgent.exe`.

Build for Linux:

```bash
./scripts/build_linux.sh
```

All three build scripts fetch and stage the matching official Sunshine release automatically
(`scripts/fetch_sunshine.sh`). Env overrides:

- `USBRIDGE_SKIP_SUNSHINE=1` — skip bundling Sunshine (offline/dev builds)
- `USBRIDGE_SUNSHINE_FORCE=1` — re-download even if already staged
- `USBRIDGE_SUNSHINE_VERSION=<tag>` — pin a specific Sunshine release instead of latest

macOS permissions:

- Screen Recording is required for video/screen capture
- Accessibility is required for mouse and keyboard injection

Configuration:

`config.yaml` next to the app, or `~/.config/usbridge-agent/`

Application log:

`~/Library/Logs/USBridgeAgent/app.log`
If `USBRIDGE_LOG_DIR` is set, logs are written there instead.

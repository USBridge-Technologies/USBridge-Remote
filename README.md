# usbridge_agent

Software KVM backend for `usbridge_client` on Windows and macOS.

Implemented now:

- compatible core HTTP API (`/api/device/*`, `/api/keyboard`, `/api/mouse`, `/api/video/*`, `/api/screen`)
- embedded `frps` + local `frpc` with QUIC
- `http_srv` STCP proxy for client API access
- `video_sudp` SUDP proxy for client RTP video path
- dynamic NBD visitors for client-hosted `nbd_srv1..N`
- Windows HID input via `SendInput`
- macOS HID input via Quartz / `CGEvent`
- screen snapshots via desktop capture (`screencapture` on macOS)
- Fyne desktop control window
- video streaming via `ffmpeg` (`dxgi`/`gdigrab` on Windows, `avfoundation` on macOS)

Current limitation:

- disk mount through `nbd-iSCSI` is prepared at transport/API level, but concrete Windows mount command still needs environment-specific tuning
- video pipeline depends on local `ffmpeg` availability

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

macOS permissions:

- Screen Recording is required for video/screen capture
- Accessibility is required for mouse and keyboard injection

Configuration:

`config.yaml` next to the app, or `~/.config/usbridge-agent/`

Application log:

`~/Library/Logs/USBridgeAgent/app.log`
If `USBRIDGE_LOG_DIR` is set, logs are written there instead.

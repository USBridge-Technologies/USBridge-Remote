# usbridge_agent

Windows-first software KVM backend for `usbridge_client`.

Implemented now:

- compatible core HTTP API (`/api/device/*`, `/api/keyboard`, `/api/mouse`, `/api/video/*`, `/api/screen`)
- embedded `frps` + local `frpc` with QUIC
- `http_srv` STCP proxy for client API access
- `video_sudp` SUDP proxy for client RTP video path
- dynamic NBD visitors for client-hosted `nbd_srv1..N`
- Windows HID input via `SendInput`
- screen snapshots via desktop capture
- Fyne desktop control window
- video streaming via `ffmpeg`

Current limitation:

- disk mount through `nbd-iSCSI` is prepared at transport/API level, but concrete Windows mount command still needs environment-specific tuning
- video pipeline depends on local `ffmpeg` availability

Start:

```powershell
go run ./cmd/usbridge_agent
```

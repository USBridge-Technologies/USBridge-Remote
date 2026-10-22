# USBridge 2 API Documentation (Master QR Sync Protocol)

## Authentication & Security (v2)

All API requests (except `/api/healthz`) require a cryptographic signature. The root of trust is the **API Secret**, which is shared via a physical QR code on the device's screen.

### 1. Mandatory Headers
Every request must include:
- `X-Auth-Timestamp`: Current Unix timestamp (seconds). Requests older than 60s are rejected.
- `X-Auth-Signature`: HMAC-SHA256 hash of the request.

### 2. Signature Calculation
`HMAC_SHA256(API_SECRET, METHOD + PATH + TIMESTAMP + BODY)`
- `METHOD`: HTTP method in uppercase (e.g., "POST").
- `PATH`: Full URI path starting with `/` (e.g., `/api/mouse`).
- `TIMESTAMP`: The same string used in the `X-Auth-Timestamp` header.
- `BODY`: Raw request body string (empty string if no body).

---

## Initialization & Sync

### Master Sync
```http
POST /api/auth/sync
```
**Description**: Unified endpoint for pairing and initial synchronization. Securely transmits sensitive data (PINs, keys) using AES-256-GCM encryption.

**Request Body**:
```json
{
  "payload": "AES_GCM_ENCRYPTED_BASE64",
  "iv": "IV_BASE64",
  "timestamp": 1717760000
}
```

**Decrypted Payload Content**:
```json
{
  "moonlight_pin": "1234",
  "tailscale_key": "tskey-auth-...",
  "hostname": "my-device",
  "client_id": "uuid-..."
}
```

**Response**:
```json
{
  "success": true,
  "data": {
    "tailscale_status": { ... },
    "sunshine_status": "paired"
  }
}
```

---

## Device Control

### Device Start (Mounting)
```http
POST /api/device/start
```
**Description**: Starts USB gadgets (Drive, Keyboard, Mouse, RNDIS).
**Request Body**: Array of device objects.

### Device Stop
```http
POST /api/device/stop
```
**Description**: Stops all active USB gadgets.

---

## Input Control

### Keyboard
```http
POST /api/keyboard
```
**Actions**: `key`, `combo`, `text`.

### Mouse
```http
POST /api/mouse
```
**Actions**: `move`, `click`, `scroll`, `touch`, `touch_position`.

---

## Video & Audio

### Video Info
```http
GET /api/video/info
```

### Video Start
```http
POST /api/video/start
```

### Video Devices
```http
GET /api/video/devices
```
**Description**: Lists capturable monitors/displays. On Linux these are the
connected DRM/KMS outputs (works even headless, before any login), each
reported as `{"path": "drm:<index>", "name": "...", "bus": "drm", ...}`.
**Response `data`**: `{"devices": [...], "count": N}`.

### Video Set Device
```http
POST /api/video/set_device
```
**Description**: Pins Sunshine's capture to one monitor from the list above.
**Request Body**: `{"device": "drm:<index>", "pixel_format": "..."}` — `device`
is a `path` value from `/api/video/devices`; empty string clears the pin
(Sunshine auto-picks). Persists into `sunshine.conf`'s `output_name` and
restarts Sunshine.

### Video FEC
```http
POST /api/video/fec
```
**Description**: Sets the streamer's FEC from the video dialog's FEC slider
(hardware agent `usbridge` and this software agent alike).
**Request Body**: `{"percent": 5..50, "auto": false}` -- `auto` (rust-shine
only) lets FEC follow measured loss from 20% to 40%, with the encoder bitrate
budgeted for 40% so it never adds traffic past the session bitrate. Written to
the streamer's config (`fec_percentage`, `fec_auto`); the streamer restarts only
when the value changed. `GET /api/video/info` reports it as `"fec": {"percent",
"auto", "auto_supported", "min_percent", "max_percent"}` when the agent takes it;
the client shows the slider only then.

### Set Video Source
```http
POST /api/video/source
```
**Description**: Which part of a capture card's frame is the target's screen,
from the video dialog's "Screen" picker (hardware agent `usbridge`). The
software agent with the rust-shine backend takes it as the stream's shape:
`auto` (default) streams the desktop's own shape fitted into the requested
size, no bars; `capture` the requested size exactly, the desktop
letterboxed into it; `manual` the `width` x `height` shape (`rect` is not
taken there).
**Request Body**: `{"mode": "capture"}` (the whole frame),
`{"mode": "manual", "width": 1440, "height": 900}` (the monitor's own
resolution: the streamer cuts out where the MS2130 puts a picture of that shape
-- it fits every source as if the UVC frame were 16:9, bars at the sides or
above/below), optionally with `"rect": [x, y, w, h]` (fractions of the frame)
for a source the rule doesn't fit, or `{"mode": "auto"}` (the default: the
streamer finds the black bars the source drew and crops them). The absolute mouse maps the picture onto exactly that screen; the
client shows the picture in the source's aspect where its native renderer can
(Vulkan/GL). The streamer restarts only when the value changed. `GET
/api/video/info` reports it as `"source": {"mode", "width", "height", "rect",
"auto_supported"}` when the agent takes it; the client shows the picker only
then.

### Audio Info
```http
GET /api/audio/info
```

---

## Storage & ISO

### Storage Status
```http
GET /api/storage/status
```

### ISO Upload
```http
POST /api/iso/upload
```
**Note**: Large files (up to 50GB) supported.

---

## Public Endpoints

### Health Check
```http
GET /api/healthz
```
**Description**: Only endpoint that does **not** require a signature. Returns 200 OK if service is alive.

---

## Transport & scope notes

- This API is served over **plain HTTP/WS** (not HTTPS/WSS) — `agent/internal/app/app.go` calls
  plain `http.Server.ListenAndServe`, no TLS. The HMAC signature above gives every request
  authenticity/integrity (only someone holding `API_SECRET` can produce a valid signature, and a
  tampered body invalidates it) but **not confidentiality** — request/response bodies are
  readable in plaintext by anything else on the same LAN. Confidentiality for off-LAN access
  comes from the optional Tailscale (WireGuard) tunnel, not from this API layer itself.
- `X-Auth-Timestamp` skew tolerance is ±60s (`agent/internal/api/security.go`'s `verifyHMAC`) —
  the replay window for a captured, still-valid signature.
- This document covers only the agent's own `/api/*` surface. Two other, completely separate
  protocol surfaces exist and are **not** covered by this HMAC scheme at all:
  - **Classic GameStream/Moonlight** (rustshine's own ports: HTTPS 47984, HTTP 47989, RTSP 48010,
    ENet control 47999, RTP audio/video) — its own PIN-pairing + self-signed-certificate trust
    model (mirrors NVIDIA GameStream/Sunshine's `nvhttp.cpp` handshake exactly). Control channel
    is AES-128-GCM encrypted; audio RTP is AES-128-CBC encrypted; **video is not encrypted** (see
    rust-shine's `README.md` "Known gaps").
  - **Native WebRTC signaling** (rustshine's `POST /webrtc/offer`, its own port, used by the
    browser/WASM client) — authenticated by the same `X-Auth-Timestamp`/`X-Auth-Signature` HMAC
    scheme described above, reusing this same master key: the agent hands rustshine a copy of its
    master key at launch (`--webrtc-shared-secret`), and `client/internal/webrtcweb/client_wasm.go`
    signs every offer request with it (`signHMAC`), so it authenticates *this* agent's master key,
    not the agent's `/api/*` HTTP path itself — this is a separate server surface on rustshine's
    own port, not a route the agent's HTTP server serves. The actual media/input payloads are also
    DTLS-SRTP-encrypted (mandatory, non-optional part of the WebRTC spec). Only unset for
    standalone/dev use of `gamestream-server` outside the agent, where the endpoint stays
    unauthenticated (logs a startup warning) — see rust-shine's `docs/WEBRTC.md` "Authentication"
    section, which also has the live-verification notes (real agent install, real device).

    **WebTransport (the header's WebRTC / WebTransport switch)**: `GET /webtransport/info`,
    signed the same way, returns rustshine's WebTransport UDP port, the SHA-256 of its
    short-lived certificate (`serverCertificateHashes`) and the session path; the session URL
    carries a signed timestamp. Video arrives one access unit per QUIC stream and is decoded with
    WebCodecs in a worker (`client/internal/webrtcweb/wt_player.js`); input and the
    `usbpass-*`/`clipboard-sync`/`api-tunnel` channels ride the same session. On a KVM the
    agent proxies `/webtransport/info` and `/webrtc/offer` on its own origin (`:9443`). See
    rust-shine's `docs/WEBRTC.md` "WebTransport + WebCodecs".

    **Signaling relay fallback (pro/enterprise only)**: `client_wasm.go`'s `postOffer` tries the
    direct fetch above first; only on a genuine network-level failure (connection refused, DNS
    failure, mixed content or Local Network Access blocking the request — i.e. the agent was never
    actually reached, as opposed to a real HTTP rejection like a 401) does it retry the identical
    signed request against `usbridge-entitlement`'s
    `POST /v1/webrtc/signal-relay/offer {hw_id, sdp, bitrate_kbps?, codec?}`, which forwards it to
    the agent over a persistent outbound WebSocket (`agent/internal/app/webrtc_signal_relay.go`)
    instead of requiring the browser to reach it directly — for a browser with zero LAN/Tailscale
    route to an agent sitting behind NAT/CGNAT. `hw_id` comes from the pairing QR/deep link
    (`SavedConnection.HwID`) — absent for a manual host:key entry or an unsaved QR/deep-link
    "Connect now", both of which just mean no relay fallback is available, same as before this
    existed. Gated server-side to RustShine Pro/Enterprise (a free-tier `hw_id` gets a clean
    `403 not_pro`, surfaced as a distinct "remote connect requires RustShine Pro" error rather than
    the generic network failure that triggered the fallback) — see
    `usbridge-entitlement-backend/README.md`'s "WebRTC signaling relay" section for the full
    Worker/Durable-Object design. The relay carries only this one-shot offer/answer exchange; once
    negotiated, media/data ride the same DTLS-SRTP `RTCPeerConnection` either way.

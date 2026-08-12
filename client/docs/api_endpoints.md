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

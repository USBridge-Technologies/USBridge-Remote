#!/usr/bin/env python3
"""
Debug script: probe /api/auth/sync with tailscale_register=true multiple times
to observe whether the server returns a stable or new auth URL on each call.

Usage:
    python3 debug_tailscale_sync.py <bridge_ip> <api_secret>
    python3 debug_tailscale_sync.py 192.168.1.108 mysecret

It performs:
1. Status-only sync (no tailscale_register) → baseline
2. 4 register syncs, 5s apart → check if auth URLs change
3. Second round after 15s → confirm stability
"""

import sys
import os
import json
import time
import base64
import hashlib
import struct
import urllib.request
import urllib.error

# ---------- crypto (mirrors Go: SHA-256 key → AES-256-GCM) ----------

def _derive_key(secret: bytes) -> bytes:
    return hashlib.sha256(secret).digest()

def _encrypt_payload(payload, secret):
    from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    key = _derive_key(secret)
    iv = os.urandom(12)
    data = json.dumps(payload, separators=(',', ':')).encode()
    ct = AESGCM(key).encrypt(iv, data, None)
    return base64.b64encode(ct).decode(), base64.b64encode(iv).decode()

def sync(host: str, port: int, secret: bytes, tailscale_register: bool = False, timeout: int = 30) -> dict:
    payload = {
        "tailscale_register": tailscale_register,
        "hostname": "usbridge",
        "client_id": "debug-script",
    }
    encrypted, iv = _encrypt_payload(payload, secret)
    body = json.dumps({
        "payload": encrypted,
        "iv": iv,
        "timestamp": int(time.time()),
    }).encode()

    url = f"http://{host}:{port}/api/auth/sync"
    req = urllib.request.Request(url, data=body, method="POST",
                                  headers={"Content-Type": "application/json"})
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        return {"error": e.code, "body": e.read().decode()}
    except Exception as e:
        return {"error": str(e)}

def fmt_ts(ts_status) -> str:
    if not ts_status:
        return "no tailscale_status"
    auth_url = ts_status.get("auth_url", "")
    return (
        f"backend={ts_status.get('backend','?')} "
        f"logged_in={ts_status.get('logged_in','?')} "
        f"ip={ts_status.get('ip4','(none)')} "
        f"auth_url={'YES → ' + auth_url if auth_url else 'none'}"
    )

def main():
    if len(sys.argv) < 3:
        print(__doc__)
        sys.exit(1)

    host = sys.argv[1]
    secret = sys.argv[2].encode()
    port = int(sys.argv[3]) if len(sys.argv) > 3 else 80

    print(f"Bridge: {host}:{port}\n{'='*70}")

    # ── 1. Status-only baseline ──────────────────────────────────────
    print("\n[1] Status-only sync (tailscale_register=false):")
    r = sync(host, port, secret, tailscale_register=False)
    print(f"    RAW: {json.dumps(r)[:800]}")
    ts = r.get("data", {}).get("tailscale_status") if "data" in r else None
    print(f"    {fmt_ts(ts)}")

    # ── 2. Four register-syncs, 5 s apart ────────────────────────────
    print("\n[2] Repeated register syncs (tailscale_register=true), 5s apart:")
    seen_urls = []
    for i in range(4):
        t0 = time.time()
        r = sync(host, port, secret, tailscale_register=True, timeout=35)
        elapsed = time.time() - t0
        ts = r.get("data", {}).get("tailscale_status") if "data" in r else None
        auth_url = (ts or {}).get("auth_url", "")
        status_str = fmt_ts(ts)

        tag = ""
        if auth_url:
            if auth_url in seen_urls:
                tag = " ✅ SAME as before"
            else:
                tag = f" ❌ NEW URL (#{len(seen_urls)+1})" if seen_urls else " 🆕 first URL"
            seen_urls.append(auth_url)

        print(f"    [{i+1}] {elapsed:.1f}s  {status_str}{tag}")
        if i < 3:
            print(f"        waiting 5s…")
            time.sleep(5)

    # ── 3. 15 s pause then one more ──────────────────────────────────
    print("\n[3] Waiting 15s then one more register sync:")
    time.sleep(15)
    r = sync(host, port, secret, tailscale_register=True, timeout=35)
    ts = r.get("data", {}).get("tailscale_status") if "data" in r else None
    auth_url = (ts or {}).get("auth_url", "")
    tag = ""
    if auth_url:
        tag = " ✅ SAME" if auth_url in seen_urls else " ❌ NEW"
    print(f"    {fmt_ts(ts)}{tag}")

    # ── Summary ──────────────────────────────────────────────────────
    print(f"\n{'='*70}")
    unique = list(dict.fromkeys(seen_urls))  # preserve order, dedup
    print(f"Unique auth URLs seen: {len(unique)}")
    for i, u in enumerate(unique, 1):
        print(f"  {i}. {u}")
    if len(unique) == 1:
        print("\n✅ Server returns STABLE auth URL — client dedup fix is sufficient.")
    elif len(unique) > 1:
        print("\n❌ Server regenerates URL on each call — server-side cache fix needed.")
    else:
        print("\n⚠️  No auth URL returned (bridge already logged in or error).")

if __name__ == "__main__":
    try:
        from cryptography.hazmat.primitives.ciphers.aead import AESGCM
    except ImportError:
        print("Install dependency: pip install cryptography")
        sys.exit(1)
    main()

#!/usr/bin/env python3
"""
Test: Moonlight video streaming over internet (DERP/Tailscale tsnet proxy).

Usage:
    python3 scripts/test_moonlight_internet.py
    python3 scripts/test_moonlight_internet.py --ts-host 100.71.26.121 --key YOUR_QUIC_TOKEN

The test:
  1. Clears logcat
  2. Sends deep link with immediate=true, ts_mode=userspace
  3. Waits for up to TIMEOUT seconds
  4. Reads logcat and checks for proxy startup + video success markers
  5. Exits 0 on success, 1 on failure with diagnostics
"""

import subprocess
import time
import sys
import argparse
import re

TAILSCALE_HOST  = "100.71.26.121"
MASTER_KEY      = "YOUR_QUIC_TOKEN"
TIMEOUT         = 30  # seconds to wait for first frame

# Log markers we look for
PROXY_STARTED   = "🌕 [Moonlight/Proxy] ✅ Internet proxy active"
RTSP_TUNNEL     = "🌕 [Moonlight/Proxy] RTSP tunnel via tsnet"
RTSP_OK         = "🌕 [Moonlight] ✅ rtsp-handshake"
FIRST_FRAME     = "first frame"          # substring in "Waiting for first frame"
STAGE_FAILED    = "cl_stage_failed"
LI_START_ERROR  = "LI_START_ERROR_CODE"
PROXY_FAILED    = "no reachable LAN IP and proxy failed"
NO_PROXY_WARN   = "no reachable LAN IP — C sockets will use Tailscale IP"


def run(cmd, **kwargs):
    return subprocess.run(cmd, shell=True, capture_output=True, text=True, **kwargs)


def adb(cmd):
    return run(f"adb {cmd}")


def check_device():
    r = adb("devices")
    lines = [l for l in r.stdout.strip().splitlines() if "\tdevice" in l]
    if not lines:
        print("❌ No Android device connected (adb devices)")
        sys.exit(1)
    print(f"✅ Device: {lines[0].split()[0]}")


def clear_logcat():
    adb("logcat -c")
    time.sleep(0.5)


def send_deeplink(ts_host, key, ts_mode="userspace"):
    # Force-stop first so getIntent() returns the new deep link (not the stale launch intent).
    adb("shell am force-stop com.usbridge.client")
    time.sleep(1.5)

    deeplink = (
        f"usbridge://connect"
        f"?tailscale_host={ts_host}"
        f"&master_key={key}"
        f"&immediate=true"
        f"&ts_mode={ts_mode}"
    )
    # Pass the entire am start command as one string to adb shell so Android's sh
    # sees the single quotes and treats & literally (not as a background operator).
    am_cmd = f"am start -a android.intent.action.VIEW -d '{deeplink}'"
    r = subprocess.run(["adb", "shell", am_cmd], capture_output=True, text=True)
    if r.returncode != 0 and "Error" in r.stderr:
        print(f"⚠️  am start warning: {r.stderr.strip()}")
    else:
        print(f"✅ Deep link sent: {deeplink}")


def collect_logcat(timeout_sec):
    """Stream logcat lines for timeout_sec seconds, return list of relevant lines."""
    lines = []
    deadline = time.time() + timeout_sec
    proc = subprocess.Popen(
        ["adb", "logcat", "-v", "time"],
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, text=True
    )
    try:
        while time.time() < deadline:
            proc.stdout.flush()
            line = proc.stdout.readline()
            if not line:
                break
            if any(tag in line for tag in ("USBridge", "Moonlight/CGO", "Moonlight/Proxy",
                                           "Moonlight/tsnet", "cl_stage_")):
                lines.append(line.rstrip())
                # Print live so we can see progress
                print(f"  {line.rstrip()}")
            # Early exit: success or failure detected
            if RTSP_OK in line or (STAGE_FAILED in line and "stage=4" in line):
                time.sleep(2)  # collect a bit more
                break
    finally:
        proc.terminate()
        proc.wait()
    return lines


def analyse(lines):
    text = "\n".join(lines)

    proxy_active    = PROXY_STARTED in text
    rtsp_tunnel     = RTSP_TUNNEL in text
    rtsp_ok         = RTSP_OK in text
    stage4_failed   = re.search(r"cl_stage_failed.*stage=4", text) is not None
    li_error        = LI_START_ERROR in text
    proxy_fail      = PROXY_FAILED in text
    old_warn        = NO_PROXY_WARN in text

    print("\n" + "="*60)
    print("RESULTS")
    print("="*60)
    print(f"  Proxy started      : {'✅' if proxy_active  else '❌'}")
    print(f"  RTSP tunnel via tsnet: {'✅' if rtsp_tunnel  else '❌'}")
    print(f"  RTSP handshake OK  : {'✅' if rtsp_ok       else '❌'}")
    print(f"  Stage-4 failed     : {'❌ YES' if stage4_failed else '✅ no'}")
    print(f"  LiStartConnection err: {'❌ YES' if li_error else '✅ no'}")

    if old_warn:
        print("\n⚠️  OLD code path executed: proxy was NOT called.")
        print("   APK may not have been rebuilt/reinstalled with proxy changes.")
        return False

    if proxy_fail:
        print("\n❌ Proxy startup failed — check tsnet/tailscale state on device.")
        return False

    if not proxy_active:
        print("\n❌ Proxy never started. Check that:")
        print("   • GetPeerDirectIP returned '' (DERP-only path)")
        print("   • startMoonlightProxy was reached in ConnectToRTP()")
        return False

    if stage4_failed or li_error:
        print("\n❌ RTSP stage 4 still failing even with proxy active.")
        print("   Check RTSP TCP proxy connection logs above for errors.")
        return False

    if rtsp_ok:
        print("\n✅ SUCCESS — Moonlight connected over internet via tsnet proxy!")
        return True

    print("\n⚠️  RTSP stage passed but no '✅ rtsp-handshake' found yet.")
    print("   Connection may still be in progress. Check full logcat.")
    return False


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--ts-host", default=TAILSCALE_HOST, help="Tailscale IP of the server")
    parser.add_argument("--key",     default=MASTER_KEY,     help="Master key / API token")
    parser.add_argument("--timeout", type=int, default=TIMEOUT, help="Seconds to wait for result")
    parser.add_argument("--ts-mode", default="userspace", choices=["userspace","kernel",""], help="ts_mode deep link param")
    args = parser.parse_args()

    print(f"🧪 Moonlight internet streaming test")
    print(f"   Server : {args.ts_host}  mode={args.ts_mode}  timeout={args.timeout}s\n")

    check_device()
    clear_logcat()
    print(f"🔗 Sending deep link...")
    send_deeplink(args.ts_host, args.key, args.ts_mode)

    print(f"\n📋 Collecting logs for up to {args.timeout}s...\n")
    lines = collect_logcat(args.timeout)

    success = analyse(lines)
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()

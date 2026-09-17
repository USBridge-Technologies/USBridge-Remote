#!/usr/bin/env python3
"""Cross-reference every client-reported stutter event against what the
Windows-side streamer (and the client's own log) was doing at that exact
moment, and print a best-guess cause for each one -- not just a count.

Why this exists: test_streamer_benchmark_macos_win.sh (and its predecessors)
already count stalls/reconnects per pass, which tells you *how much* is
wrong but not *what* to fix. The client's own Metal "Stutter Profiler"
(metal_video_impl_darwin.m) already splits stalls into two kinds --
"AppKit/DisplayLink stalled" (the render/main-thread side: something on
*this* Mac blocked the run loop) and "Moonlight Decoder/Network stalled"
(the delivery side: no frame arrived from the network in time, which can
be the network itself OR the server failing to produce/send a frame in
time) -- this script pushes that classification further by pulling in the
Windows streamer's own log for the delivery-side half.

Usage:
    correlate_freeze_causes.py <client_app.log> [--win-log windows.log] [--backend rustshine|sunshine]

<client_app.log> is a logrus-formatted USBridgeClient log (second-resolution
timestamps only -- `time="2026-09-15 18:56:08"`). --win-log is a slice of
the Windows agent's shared streamer/sunshine stdout log
(<StateDir>/logs/sunshine-stdout.log, tracing_subscriber's default format,
UTC microsecond timestamps: `2026-09-15T16:56:11.669237Z  WARN pipeline: ...`)
covering the same wall-clock window. Without --win-log, only client-side
(render-stall) causes can be attempted.

Clock handling: the client log has no UTC marker and only second
resolution, so timestamps are treated as *this machine's current* local
time zone (safe for a log just collected in this same session; re-running
against an old log from a different time zone would need --tz-offset).
"""
import argparse
import re
import sys
from datetime import datetime, timedelta, timezone

CLIENT_TS_RE = re.compile(r'time="(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2})"')
PROFILER_RE = re.compile(r'\[Metal\] .*Profiler.* (AppKit/DisplayLink|Moonlight Decoder/Network) stalled for (\d+) ms')
WIN_TS_RE = re.compile(r'^(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+)Z\s+(\w+)\s+([\w:]+):\s*(.*)$')
KEYFRAME_RE = re.compile(r'encode_us[=:](\d+).*packetize_us[=:](\d+).*send_us[=:](\d+)')
GPU_CLOCK_WARN = "NVIDIA GPU clock lock skipped"
DXGI_BLACK_WARN = "DXGI capture has produced zero frames"

# Client-side activity that has *already* been confirmed live to block the
# Fyne/AppKit main thread long enough to trip the DisplayLink profiler --
# see disk_widget_refresh.go's scheduleCombine fix (the exact bug this
# repo's benchmark run first caught: a periodic 10s poll rebuilding the
# whole Devices dashboard on the main thread). Extend this list as new
# root causes get confirmed the same way -- it's a lookup table of known
# culprits, not a heuristic guess.
CLIENT_SUSPECTS = [
    ("combineDrives", "Devices dashboard rebuild on main thread (disk_widget_refresh.go) -- if this still fires while streaming, scheduleCombine's NavVideoHidden() guard may have regressed or a new caller bypasses it"),
    ("Loaded 0 snapshots", "Snapshots list reload on main thread"),
    ("resize", "window/canvas resize handling"),
]


def parse_client_log(path):
    events = []
    with open(path, errors="replace") as f:
        lines = f.readlines()
    last_ts = None
    for i, line in enumerate(lines):
        m = CLIENT_TS_RE.search(line)
        if m:
            last_ts = datetime.strptime(m.group(1), "%Y-%m-%d %H:%M:%S")
        pm = PROFILER_RE.search(line)
        if pm and last_ts:
            kind = "render" if pm.group(1) == "AppKit/DisplayLink" else "delivery"
            events.append({"ts": last_ts, "kind": kind, "ms": int(pm.group(2)), "line_idx": i})
    return events, lines


def local_utc_offset():
    return datetime.now().astimezone().utcoffset()


def parse_win_log(path):
    rows = []
    with open(path, errors="replace") as f:
        for line in f:
            m = WIN_TS_RE.match(line)
            if not m:
                continue
            ts = datetime.strptime(m.group(1), "%Y-%m-%dT%H:%M:%S.%f").replace(tzinfo=timezone.utc)
            rows.append({"ts": ts, "level": m.group(2), "target": m.group(3), "msg": m.group(4)})
    return rows


def nearby_client_evidence(lines, idx, window=15):
    lo = max(0, idx - window)
    for j in range(idx - 1, lo - 1, -1):
        line = lines[j]
        for needle, explanation in CLIENT_SUSPECTS:
            if needle in line:
                return f"{explanation} (log: {line.strip()[:110]})"
    return None


def nearby_win_evidence(win_rows, ts_utc, gpu_clock_flagged, before=timedelta(seconds=2), after=timedelta(seconds=1)):
    window = [r for r in win_rows if ts_utc - before <= r["ts"] <= ts_utc + after]
    warns = [r for r in window if r["level"] == "WARN" and GPU_CLOCK_WARN not in r["msg"]]
    if warns:
        r = warns[-1]
        return f"server WARN {r['ts'].strftime('%H:%M:%S.%f')[:-3]}Z: {r['msg'][:140]}"
    # Nearest preceding per-window/keyframe timing sample, for an elevated-latency hint.
    samples = [r for r in window if "stage timing" in r["msg"]]
    if samples:
        r = samples[-1]
        km = KEYFRAME_RE.search(r["msg"])
        if km:
            encode_us, packetize_us, send_us = (int(x) for x in km.groups())
            parts = []
            if encode_us > 8000:
                parts.append(f"encode_us={encode_us}")
            if packetize_us > 3000:
                parts.append(f"packetize_us={packetize_us}")
            if send_us > 3000:
                parts.append(f"send_us={send_us}")
            if parts:
                return f"elevated {' '.join(parts)} in nearest sample ({r['ts'].strftime('%H:%M:%S.%f')[:-3]}Z)"
    if gpu_clock_flagged:
        return "no server WARN/spike nearby -- session has GPU clock lock disabled (non-admin), consistent with the documented occasional 30-60ms encode stall on GPU idle-to-busy transitions"
    return None


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("client_log")
    ap.add_argument("--win-log", default=None)
    ap.add_argument("--backend", choices=["rustshine", "sunshine"], default=None)
    args = ap.parse_args()

    events, lines = parse_client_log(args.client_log)
    if not events:
        print("No Profiler stall events found in the client log.")
        return

    win_rows = parse_win_log(args.win_log) if args.win_log else []
    gpu_clock_flagged = args.backend == "rustshine" and any(GPU_CLOCK_WARN in r["msg"] for r in win_rows)
    dxgi_black_flagged = any(DXGI_BLACK_WARN in r["msg"] for r in win_rows)
    offset = local_utc_offset()

    print(f"{len(events)} stall events found. win_log={'yes' if win_rows else 'no'} "
          f"gpu_clock_lock_skipped_this_session={gpu_clock_flagged} dxgi_black_frames_seen={dxgi_black_flagged}")
    print("-" * 100)

    causes_tally = {}
    for ev in events:
        ts_utc = (ev["ts"] - offset).replace(tzinfo=timezone.utc)
        if ev["kind"] == "render":
            cause = nearby_client_evidence(lines, ev["line_idx"]) or "unexplained render stall -- no correlated client-log activity found (check OS/GPU compositor scheduling, thermal throttling)"
        else:
            cause = nearby_win_evidence(win_rows, ts_utc, gpu_clock_flagged) if win_rows else "no --win-log provided -- cannot attribute delivery-side stalls"
            if cause is None:
                cause = "no explanatory signal in server log -- likely real network jitter (cross-check ping RTT for this timestamp) or below current log granularity"
        causes_tally[cause] = causes_tally.get(cause, 0) + 1
        print(f"{ev['ts'].strftime('%H:%M:%S')}  {ev['kind']:9s}  {ev['ms']:4d}ms  {cause}")

    print("-" * 100)
    print("Summary (by distinct cause, most common first):")
    for cause, n in sorted(causes_tally.items(), key=lambda kv: -kv[1]):
        print(f"  {n:4d}x  {cause}")


if __name__ == "__main__":
    main()

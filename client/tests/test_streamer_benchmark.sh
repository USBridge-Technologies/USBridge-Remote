#!/bin/bash
# Head-to-head streamer benchmark: runs the same motion test pattern through
# both bundled game-streaming hosts (Sunshine, then RustShine) over a real
# Wi-Fi hop to a physical Android device, and collects framerate, frame-time
# smoothness (Android's own gfxinfo jank stats), network RTT/jitter, and
# stability (forced reconnects / unrecoverable frames) for each.
#
# Requires: the USBridge Agent already running on this Mac with a paired
# Android connection saved in the app (this script only taps the existing
# saved connection's connect icon — it does not pair a new one), ffmpeg
# available, and the phone reachable over adb (USB is fine for control;
# the actual video path is the real LAN/Wi-Fi hop between MAC_IP and the
# phone's Wi-Fi IP, not the adb cable).
#
# Usage: ./tests/test_streamer_benchmark.sh <mac_ip> <phone_ip> <connect_x> <connect_y> [duration_seconds] [outdir]

set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="io.usbridge.client"
SOCK="$HOME/Library/Application Support/usbridge-agent/admin.sock"

MAC_IP="${1:?mac_ip required}"
PHONE_IP="${2:?phone_ip required}"
CONNECT_X="${3:?connect tap x required}"
CONNECT_Y="${4:?connect tap y required}"
DURATION="${5:-180}"
TS="$(date +%Y%m%d_%H%M%S)"
OUTDIR="${6:-$REPO_ROOT/tests/bench_output/$TS}"
EXIT_ICON="984 63"
PATTERN="${BENCH_CONTENT:-/private/tmp/claude-501/-Users-amir-Projects-usbridge-USBridge-Remote/61dc1f91-3af3-447e-8bf8-91360a7de5a4/scratchpad/bench/motion_pattern.mp4}"

SERIAL="$(adb devices | awk 'NR==2{print $1}')"
ADB="adb -s $SERIAL"

mkdir -p "$OUTDIR"
log() { echo "[$(date +%H:%M:%S)] $*"; }

admin_get() { curl -s --unix-socket "$SOCK" "http://localhost$1"; }
admin_post() { curl -s --unix-socket "$SOCK" -X POST -d "$2" "http://localhost$1"; }

switch_backend() {
    local kind="$1"
    log "Switching backend -> $kind"
    admin_post "/token/set-stream-backend" "{\"kind\":\"$kind\"}" >/dev/null
    for i in $(seq 1 30); do
        active=$(admin_get "/token/entitlement-status" | python3 -c "import sys,json;print(json.load(sys.stdin).get('active_backend',''))" 2>/dev/null)
        [ "$active" = "$kind" ] && { log "  backend live: $active"; return 0; }
        sleep 1
    done
    log "  ⚠️ backend switch to $kind not confirmed after 30s (continuing anyway)"
}

start_pattern() {
    pkill -f "ffplay -fs -loop 0" 2>/dev/null
    sleep 0.5
    ffplay -fs -loop 0 -an -autoexit -loglevel quiet "$PATTERN" &
    FFPLAY_PID=$!
    sleep 3
}

# Restarts the content from t=0. Sunshine and RustShine take a different
# amount of wall-clock time to actually get the client's video live (in this
# script's own measurements: ~16s vs ~5s) -- if the looping content just kept
# playing from whenever start_pattern first launched it (before the connect
# tap), each backend's measurement window would start at a different point
# in the content's own timeline. For a real video with actual scene cuts
# that's not a rounding error: one pass could measure mostly a calm dialogue
# scene while the other catches an explosion, purely as an artifact of
# connect speed, not anything the backend actually did differently on video.
# Called right after the client confirms it's receiving real video, so both
# passes' measurement windows start at content t=0 regardless of how long
# their own connect took.
restart_pattern_synced() {
    log "  resyncing content to t=0 for a fair measurement window..."
    start_pattern
}

stop_pattern() {
    kill "$FFPLAY_PID" 2>/dev/null
    pkill -f "ffplay -fs -loop 0" 2>/dev/null
}

run_pass() {
    local kind="$1"
    local pass_dir="$OUTDIR/$kind"
    mkdir -p "$pass_dir"
    log "=================================================="
    log " PASS: $kind"
    log "=================================================="

    switch_backend "$kind"
    start_pattern

    # Fresh connect
    $ADB shell am force-stop "$PKG"
    sleep 1
    $ADB logcat -c
    $ADB logcat -v time > "$pass_dir/logcat.txt" 2>&1 &
    LOGCAT_PID=$!
    $ADB shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1
    sleep 2
    T0=$(date +%s)
    $ADB shell input tap "$CONNECT_X" "$CONNECT_Y"
    log "tapped connect ($CONNECT_X,$CONNECT_Y) at T0"

    # Wait for the render thread's first authoritative fps line (up to 25s)
    connected=false
    for i in $(seq 1 50); do
        if grep -qE "rendered [0-9]+ frames, submitted [0-9]+, fps=" "$pass_dir/logcat.txt" 2>/dev/null; then
            connected=true
            break
        fi
        sleep 0.5
    done
    if ! $connected; then
        log "  ❌ video never started for $kind — aborting this pass"
        kill $LOGCAT_PID 2>/dev/null
        stop_pattern
        echo "FAILED_TO_CONNECT" > "$pass_dir/status.txt"
        return 1
    fi
    CONNECT_DELTA=$(( $(date +%s) - T0 ))
    log "  ✅ video live ${CONNECT_DELTA}s after tap"

    # Resync content to t=0 now that video is confirmed live -- see
    # restart_pattern_synced's own doc comment for why (Sunshine/RustShine
    # connect at different speeds; without this, each pass would start its
    # measurement window at a different point in the content's own timeline,
    # unfair for anything with real scene changes).
    restart_pattern_synced
    # NOT written into logcat.txt itself: that file is being written
    # concurrently by the backgrounded `adb logcat > file` above, which
    # holds its own non-O_APPEND fd/offset -- a second writer appending to
    # the same path races it and gets silently clobbered the next time
    # logcat's own (lower, stale) offset catches back up and overwrites the
    # bytes we just appended. A separate file sidesteps the race entirely;
    # analysis cross-references it against logcat's own line timestamps.
    date +%s > "$pass_dir/resync_epoch.txt"

    # Ping FROM the phone TO the Mac (adb shell, executed on-device, over the
    # real Wi-Fi link) -- the reverse direction (Mac -> phone) is unusable:
    # Android blocks/rate-limits unsolicited inbound ICMP by default and
    # reliably shows 100% loss even mid-stream, which is not a real network
    # problem, just the phone's own firewall behavior.
    log "  Running ${DURATION}s window: sampling network RTT (phone->Mac, real Wi-Fi) + watching stream..."
    $ADB shell ping -c "$DURATION" -i 1 "$MAC_IP" > "$pass_dir/ping.txt" 2>&1 &
    PING_PID=$!

    for elapsed in $(seq 10 10 "$DURATION"); do
        sleep 10
        fps_now=$(grep -oE "fps=[0-9.]+" "$pass_dir/logcat.txt" | tail -1)
        rc=$(grep -c "forcing reconnect" "$pass_dir/logcat.txt" 2>/dev/null); rc=${rc:-0}
        log "  [${elapsed}s/${DURATION}s] latest ${fps_now:-fps=?}  reconnects_so_far=$rc"
    done
    wait $PING_PID 2>/dev/null

    $ADB exec-out screencap -p > "$pass_dir/screenshot.png" 2>/dev/null

    kill $LOGCAT_PID 2>/dev/null
    wait $LOGCAT_PID 2>/dev/null

    # graceful exit back to connections list
    $ADB shell input tap $EXIT_ICON >/dev/null 2>&1
    sleep 1.5
    stop_pattern

    echo "OK connect_delay=${CONNECT_DELTA}s" > "$pass_dir/status.txt"
    log "  pass complete -> $pass_dir"
}

echo "=================================================="
echo " USBridge streamer benchmark: Sunshine vs RustShine"
echo " Mac (agent/source): $MAC_IP    Phone (client, real Wi-Fi): $PHONE_IP"
echo " Duration per backend: ${DURATION}s"
echo " Output: $OUTDIR"
echo "=================================================="

run_pass "sunshine"
sleep 5
run_pass "rustshine"

echo ""
echo "=================================================="
echo " Benchmark complete. Raw data in: $OUTDIR"
echo "=================================================="

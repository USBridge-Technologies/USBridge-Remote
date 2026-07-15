#!/bin/bash
# Measures how long it takes for real video to start rendering on the
# Control tab after tapping the saved connection's connect icon, and reports
# a stage-by-stage timing breakdown from logcat markers plus a pixel-level
# motion check (two screenshots of the video area diffed) as independent
# proof that frames are actually moving, not just that the logs say so.
#
# Stages timed (each is the first matching logcat line after T0 = the tap):
#   bridge_connected   "Connected to USBridge"            — bridge API session up
#   video_http_started "Video streaming started"          — device capture (HTTP) started
#   rtp_connecting     "ConnectToRTP called"               — first Moonlight/RTSP attempt
#   rtp_connected      "[Moonlight] connected"             — RTSP/RTP stream established
#   codec_started      "AMediaCodec started"               — video decoder pipeline live
#   vulkan_overlay     "Vulkan overlay created"             — SurfaceView overlay attached
#
# A failed connect (e.g. remote Sunshine host down) shows up as: bridge_connected
# reached but rtp_connected never appears, with repeated
# "ConnectToRTP failed (attempt N/20)" lines up to the give-up point — this
# script surfaces that distinctly from a genuine timing regression.
#
# Usage: ./tests/test_android_video_launch.sh [serial] [cycles]
#   cycles defaults to 5 (run several times to see variance, not just one sample).

set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="com.usbridge.client"

SERIAL="${1:-$(adb devices | awk 'NR==2{print $1}')}"
if [ -z "$SERIAL" ]; then
    echo "❌ No adb device found."
    exit 1
fi
ADB="adb -s $SERIAL"
CYCLES="${2:-5}"

TS="$(date +%Y%m%d_%H%M%S)"
OUTDIR="$REPO_ROOT/tests/android_video_launch_output/$TS"
mkdir -p "$OUTDIR"

CONNECT_ICON="930 455"
VIDEO_REGION="0 250 1080 1800" # x y w h, in 1080x2400 reference coords — the Control tab's video canvas area

log() { echo "[$(date +%H:%M:%S)] $*"; }

EXIT_ICON="984 63"

# Exit gracefully through the app's own exit icon (if it's running and
# connected) before force-stopping. force-stop alone kills the process
# outright and skips MoonlightService.Disconnect() entirely — which now
# fires an async /quit to Sunshine so the *next* connect gets the fast
# /launch path instead of /resume. Skipping this graceful exit would make
# every cycle look artificially slow regardless of the fix.
graceful_exit_if_running() {
    local pid
    pid=$($ADB shell pidof "$PKG" 2>/dev/null | tr -d '\r\n')
    if [ -n "$pid" ]; then
        $ADB shell input tap $EXIT_ICON >/dev/null 2>&1
        sleep 1.5
    fi
}

launch_fresh() {
    graceful_exit_if_running
    $ADB shell am force-stop "$PKG"
    sleep 1
    $ADB shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1
    sleep 2
}

# Returns "<epoch.millis> <rest of line>" for the first logcat line (since the
# last logcat -c) matching the given regex, or empty if not found within the
# polling window. Uses logcat's own timestamps, not adb round-trip time, so
# stage deltas are not polluted by adb/screenshot overhead.
find_marker() {
    local pattern="$1" logfile="$2"
    grep -m1 -E "$pattern" "$logfile" 2>/dev/null
}

logcat_to_epoch_millis() {
    # logcat -v time format: "MM-DD HH:MM:SS.mmm ..." — assume current year, local tz.
    local line="$1"
    local ts
    ts=$(echo "$line" | awk '{print $1" "$2}')
    python3 - "$ts" <<'EOF'
import sys, datetime
ts = sys.argv[1]
now = datetime.datetime.now()
dt = datetime.datetime.strptime(f"{now.year}-{ts}", "%Y-%m-%d %H:%M:%S.%f")
print(dt.timestamp())
EOF
}

# Crude motion check: crop the video region from two screenshots and count
# pixels that differ by more than a small threshold. Real video content
# (even mostly-static desktop capture) has cursor/encoder noise; a frozen or
# black canvas produces ~0 differing pixels.
motion_check_py="$OUTDIR/_motion_check.py"
cat > "$motion_check_py" <<'PYEOF'
import sys
from PIL import Image
a = Image.open(sys.argv[1]).convert("RGB")
b = Image.open(sys.argv[2]).convert("RGB")
x, y, w, h = (int(v) for v in sys.argv[3:7])
a = a.crop((x, y, x + w, y + h)).load()
b = b.crop((x, y, x + w, y + h)).load()
diff = 0
total = w * h
step = 4  # sample every 4th pixel for speed
for j in range(0, h, step):
    for i in range(0, w, step):
        ar, ag, ab = a[i, j]
        br, bg, bb = b[i, j]
        if abs(ar - br) + abs(ag - bg) + abs(ab - bb) > 30:
            diff += 1
sampled = (w // step) * (h // step)
pct = 100.0 * diff / max(sampled, 1)
print(f"{pct:.2f}")
PYEOF

results=()
fail_count=0

for i in $(seq 1 "$CYCLES"); do
    echo ""
    echo "──────────────────────────────────────────────────"
    echo " CYCLE $i/$CYCLES"
    echo "──────────────────────────────────────────────────"

    launch_fresh
    LOGFILE="$OUTDIR/logcat_cycle${i}.txt"
    $ADB logcat -c
    $ADB logcat -v time > "$LOGFILE" 2>&1 &
    LOGCAT_PID=$!

    T0=$(date +%s.%N)
    $ADB shell input tap $CONNECT_ICON
    log "tapped connect icon at T0"

    # Poll for each marker in order, up to a generous per-marker timeout.
    declare -A marker_time
    markers=(
        "bridge_connected:Connected to USBridge"
        "video_http_started:Video streaming started"
        "rtp_connecting:ConnectToRTP called"
        "rtp_connected:\[Moonlight\] connected"
        "codec_started:AMediaCodec started"
        "vulkan_overlay:Vulkan overlay created"
    )

    deadline=$(python3 -c "print($T0 + 20)")
    for spec in "${markers[@]}"; do
        name="${spec%%:*}"
        pattern="${spec#*:}"
        found=""
        while [ "$(python3 -c "print(1 if $(date +%s.%N) < $deadline else 0)")" = "1" ]; do
            line=$(find_marker "$pattern" "$LOGFILE")
            if [ -n "$line" ]; then
                found="$line"
                break
            fi
            sleep 0.2
        done
        if [ -n "$found" ]; then
            t=$(logcat_to_epoch_millis "$found")
            delta=$(python3 -c "print(f'{$t - $T0:.3f}')")
            marker_time[$name]=$delta
            log "  ✅ $name at T+${delta}s"
        else
            marker_time[$name]="TIMEOUT"
            log "  ❌ $name not seen within timeout"
        fi
    done

    # Two screenshots ~1s apart, purely informational: the Vulkan overlay
    # renders as a Z-order-on-top hardware-composited SurfaceView, which
    # `screencap` frequently cannot capture at all (reads back black there
    # even while real frames are being presented to the physical screen) —
    # confirmed by comparing against the render thread's own stats log
    # below, which kept reporting healthy fps while this showed ~0% motion.
    # So it's logged for visual sanity-checking only and never fails a cycle.
    sleep 1
    $ADB exec-out screencap -p > "$OUTDIR/cycle${i}_frame_a.png" 2>/dev/null
    sleep 1
    $ADB exec-out screencap -p > "$OUTDIR/cycle${i}_frame_b.png" 2>/dev/null
    motion_pct=$(python3 "$motion_check_py" "$OUTDIR/cycle${i}_frame_a.png" "$OUTDIR/cycle${i}_frame_b.png" $VIDEO_REGION 2>/dev/null || echo "ERR")
    log "  motion check (informational only, see note above): ${motion_pct}% of sampled video-area pixels changed"

    # Authoritative "is video actually flowing" signal: the Vulkan render
    # thread's own periodic stats line, straight from the native renderer.
    fps_deadline=$(python3 -c "print($(date +%s.%N) + 8)")
    fps=""
    while [ "$(python3 -c "print(1 if $(date +%s.%N) < $fps_deadline else 0)")" = "1" ]; do
        line=$(grep -m1 -E "rendered [0-9]+ frames, submitted [0-9]+, fps=" "$LOGFILE" 2>/dev/null)
        if [ -n "$line" ]; then
            fps=$(echo "$line" | grep -oE "fps=[0-9.]+" | cut -d= -f2)
            break
        fi
        sleep 0.3
    done
    if [ -n "$fps" ]; then
        log "  ✅ render thread reports fps=$fps (authoritative: frames are actually decoding+rendering)"
    else
        log "  ❌ render thread never reported an fps stats line"
    fi

    kill $LOGCAT_PID 2>/dev/null
    wait $LOGCAT_PID 2>/dev/null

    retry_fail_count=$(grep -cE "ConnectToRTP failed \(attempt 20/20\)" "$LOGFILE" 2>/dev/null)

    if [ "${marker_time[rtp_connected]:-TIMEOUT}" = "TIMEOUT" ]; then
        fail_count=$((fail_count + 1))
        if [ "$retry_fail_count" -gt 0 ]; then
            results+=("cycle$i: ❌ FAILED — remote Sunshine host unreachable (gave up after 20 attempts, ~10s), not a client bug")
        else
            results+=("cycle$i: ❌ FAILED — rtp_connected marker never seen")
        fi
    elif [ -z "$fps" ]; then
        fail_count=$((fail_count + 1))
        results+=("cycle$i: ⚠️  connected (T+${marker_time[rtp_connected]}s) but render thread never reported fps — video pipeline likely stalled")
    else
        results+=("cycle$i: ✅ video live at T+${marker_time[codec_started]:-?}s (codec) / T+${marker_time[vulkan_overlay]:-?}s (overlay), fps=$fps")
    fi

    unset marker_time
done

echo ""
echo "=================================================="
echo " SUMMARY ($CYCLES cycles, $fail_count failed)"
echo "=================================================="
for r in "${results[@]}"; do
    echo "  $r"
done
echo ""
echo " Logs + screenshots saved under: $OUTDIR"
echo "=================================================="

[ "$fail_count" -eq 0 ]

#!/bin/bash
# Measures how long it takes for live video to reappear on the Control tab
# after switching away to another tab and back, across a range of away-tab
# dwell times. Useful to quantify the cost of the current full
# stop-and-reconnect behavior (RequestStreaming(false) on tab-away tears
# down the whole Moonlight session; returning to Control pays the full
# reconnect cost again) versus a future pause/resume approach.
#
# Usage: ./tests/test_android_tab_switch_video.sh [serial] [away_seconds...]
#   away_seconds: one or more dwell times to test, e.g. "0.5 2 5 15"
#   Defaults to a spread: 0.3 1 3 8 20

set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PKG="io.usbridge.client"

SERIAL="${1:-$(adb devices | awk 'NR==2{print $1}')}"
if [ -z "$SERIAL" ]; then
    echo "❌ No adb device found."
    exit 1
fi
ADB="adb -s $SERIAL"
shift || true
AWAY_TIMES=("$@")
if [ ${#AWAY_TIMES[@]} -eq 0 ]; then
    AWAY_TIMES=(0.3 1 3 8 20)
fi

TS="$(date +%Y%m%d_%H%M%S)"
OUTDIR="$REPO_ROOT/tests/android_tab_switch_output/$TS"
mkdir -p "$OUTDIR"
LOGFILE="$OUTDIR/logcat_full.txt"

TAB_CONTROL="102 232"
TAB_DEVICES="313 232"
CONNECT_ICON="930 455"

log() { echo "[$(date +%H:%M:%S)] $*"; }

logcat_to_epoch_millis() {
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

# Waits up to `timeout` seconds for the first NEW line matching pattern that
# appears after `since_line_count` lines into $LOGFILE. Prints the line and
# its logcat timestamp-derived epoch, or nothing on timeout.
wait_for_marker() {
    local pattern="$1" since_line_count="$2" timeout="$3"
    local deadline
    deadline=$(python3 -c "print($(date +%s.%N) + $timeout)")
    while [ "$(python3 -c "print(1 if $(date +%s.%N) < $deadline else 0)")" = "1" ]; do
        local line
        line=$(tail -n +"$((since_line_count + 1))" "$LOGFILE" 2>/dev/null | grep -m1 -E "$pattern")
        if [ -n "$line" ]; then
            echo "$line"
            return 0
        fi
        sleep 0.15
    done
    return 1
}

line_count() { wc -l < "$LOGFILE" 2>/dev/null || echo 0; }

echo "=================================================="
echo " Tab-switch video-recovery timing"
echo " Device: $SERIAL"
echo " Away dwell times to test: ${AWAY_TIMES[*]}"
echo " Output: $OUTDIR"
echo "=================================================="

SIZE=$($ADB shell wm size | grep -o '[0-9]*x[0-9]*' | tail -1)
if [ "$SIZE" != "1080x2400" ]; then
    echo "❌ Device resolution is $SIZE, coordinates calibrated for 1080x2400."
    exit 1
fi

$ADB shell am force-stop "$PKG"
sleep 1
$ADB logcat -c
$ADB logcat -v time > "$LOGFILE" 2>&1 &
LOGCAT_PID=$!
trap 'kill $LOGCAT_PID 2>/dev/null' EXIT

$ADB shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1
sleep 2
$ADB shell input tap $CONNECT_ICON
log "connecting..."

marker=$(wait_for_marker "rendered [0-9]+ frames, submitted" 0 40)
if [ -z "$marker" ]; then
    echo "❌ Initial connect never produced a live frame — aborting (check bridge/Sunshine reachability)."
    exit 1
fi
log "✅ initial connect confirmed live"

results=()
for away in "${AWAY_TIMES[@]}"; do
    echo ""
    echo "──────────────────────────────────────────────────"
    echo " Away dwell: ${away}s"
    echo "──────────────────────────────────────────────────"

    $ADB shell input tap $TAB_DEVICES
    log "switched away to Devices tab"
    sleep "$away"

    lc=$(line_count)
    t0=$(date +%s.%N)
    $ADB shell input tap $TAB_CONTROL
    log "switched back to Control at T0"

    marker=$(wait_for_marker "rendered [0-9]+ frames, submitted" "$lc" 20)
    if [ -z "$marker" ]; then
        log "  ❌ video never resumed within 20s"
        results+=("away=${away}s: ❌ FAILED to resume")
        continue
    fi
    t_marker=$(logcat_to_epoch_millis "$marker")
    delta=$(python3 -c "print(f'{$t_marker - $t0:.3f}')")
    fps=$(echo "$marker" | grep -oE "fps=[0-9.]+" | cut -d= -f2)

    # Also record whether this was a full reconnect (CALLING_C_LI_START seen
    # after T0) or something cheaper (no fresh LiStartConnection call).
    reconnect_line=$(tail -n +"$((lc + 1))" "$LOGFILE" | grep -m1 -E "CALLING_C_LI_START")
    if [ -n "$reconnect_line" ]; then
        method="full reconnect (do_li_start called again)"
    else
        method="no reconnect (resumed existing session)"
    fi

    log "  ✅ video live again in ${delta}s (fps=$fps) — $method"
    results+=("away=${away}s: video resumed in ${delta}s, fps=$fps — $method")
done

kill $LOGCAT_PID 2>/dev/null
wait $LOGCAT_PID 2>/dev/null
trap - EXIT

echo ""
echo "=================================================="
echo " SUMMARY"
echo "=================================================="
for r in "${results[@]}"; do
    echo "  $r"
done
echo ""
echo " Full logcat saved under: $OUTDIR"
echo "=================================================="

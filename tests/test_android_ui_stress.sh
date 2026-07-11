#!/bin/bash
# Android UI stress test — drives the real app on a connected device via adb,
# using the already-saved connection in the connection manager ("Connection 1").
#
# Goal: reproduce/detect the leftover-video-window bug (Vulkan SurfaceView
# overlay from VulkanOverlayBridge.kt surviving past disconnect, sometimes
# still visible on the connection-manager screen) by hammering
# connect/disconnect and tab switching in several patterns:
#   - slow, deliberate taps through every tab
#   - fast repeated tab switching while connected
#   - aggressive connect -> disconnect hammering (button-based disconnect)
#   - back-button disconnect (the path most likely to skip cleanup)
#   - randomized chaos mixing all of the above
#
# After every disconnect it captures a screenshot (pixel-classified via
# android_ui_check.py to confirm the actual tab/screen — Fyne renders
# everything into one opaque GL surface with no accessibility text, so pixel
# sampling is the only way to identify *which* tab is showing) AND pulls the
# device's `uiautomator dump` XML view hierarchy, which — while textless —
# does structurally expose the leftover overlay: Fyne's content is always
# exactly one `android.view.View` node; the VulkanOverlayBridge SurfaceView
# (android/.../VulkanOverlayBridge.kt) is added as a second sibling View
# directly on the Activity's decorView. A second View node still present
# after we're back on the connections screen means destroy() never ran /
# raced — the exact leak this test hunts for. dumpsys SurfaceFlinger/activity
# are also sampled as a secondary cross-check.
#
# Usage: ./tests/test_android_ui_stress.sh [serial]
#   serial defaults to the first device from `adb devices`.
#
# Env overrides: SLOW_CYCLES, FAST_SWITCH_TAPS, HAMMER_CYCLES, BACK_CYCLES,
#                CHAOS_CYCLES (iteration counts per phase)

set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
CHECK_PY="$REPO_ROOT/tests/android_ui_check.py"
PKG="com.usbridge.client"

SERIAL="${1:-$(adb devices | awk 'NR==2{print $1}')}"
if [ -z "$SERIAL" ]; then
    echo "❌ No adb device found. Connect a device and retry."
    exit 1
fi
ADB="adb -s $SERIAL"

TS="$(date +%Y%m%d_%H%M%S)"
OUTDIR="$REPO_ROOT/tests/android_ui_stress_output/$TS"
mkdir -p "$OUTDIR"
LOGCAT_FILE="$OUTDIR/logcat_full.txt"

SLOW_CYCLES="${SLOW_CYCLES:-3}"
FAST_SWITCH_TAPS="${FAST_SWITCH_TAPS:-40}"
HAMMER_CYCLES="${HAMMER_CYCLES:-10}"
BACK_CYCLES="${BACK_CYCLES:-10}"
CHAOS_CYCLES="${CHAOS_CYCLES:-20}"

# ── screen coordinates (calibrated at 1080x2400; aborts below if device differs) ──
TAB_CONTROL="102 232"
TAB_DEVICES="313 232"
TAB_SNAPSHOTS="554 232"
TAB_SCRIPTS="788 232"
CONNECT_ICON="930 455"      # link icon on the saved "Connection 1" row
# Top-right red-bordered icon (present on every in-session tab) that actually
# tears down the bridge session and returns to the connections screen. NOT the
# "Disconnect All" button on the Devices tab — that only toggles individual USB
# peripherals (video/keyboard/mouse) and does not exit the session, confirmed
# by manual probing when this test kept reporting the wrong screen after
# "disconnecting" via that button.
EXIT_ICON="984 63"

seq_n=0
pass=0
fail=0
leak_events=()
crash_events=()

log() { echo "[$(date +%H:%M:%S)] $*"; }

# Reads the focused-window package straight from dumpsys — the ground truth
# for "which app is actually receiving these taps right now".
foreground_pkg() {
    $ADB shell dumpsys window 2>/dev/null \
        | grep -m1 mCurrentFocus \
        | grep -oE '[a-zA-Z0-9_.]+/[a-zA-Z0-9_.]+' \
        | cut -d/ -f1
}

# Hard safety gate: a prior run crashed the app mid-loop and the script kept
# firing coordinate taps into whatever ended up in the foreground afterwards
# (the home screen, with WhatsApp/Telegram/Phone icons at those coordinates).
# Every tap/back-press now checks focus first and hard-aborts the whole run
# rather than ever tapping blind into an app we don't control.
require_app_foreground_or_abort() {
    local fg
    fg=$(foreground_pkg)
    if [ "$fg" != "$PKG" ]; then
        log "🛑 SAFETY ABORT: foreground app is '$fg', expected $PKG — refusing further taps."
        crash_events+=("SAFETY ABORT: foreground=$fg (expected $PKG), likely crash/backgrounding mid-test")
        print_summary
        exit 1
    fi
}

tap() {
    require_app_foreground_or_abort
    $ADB shell input tap "$1" "$2"
}

back_key() {
    require_app_foreground_or_abort
    $ADB shell input keyevent KEYCODE_BACK
}

# Stops the background logcat capture, mines it for crash signatures, and
# prints the final report. Called both at normal end-of-script and from the
# safety-abort path, so it must not assume any phase actually ran.
print_summary() {
    if [ -n "${LOGCAT_PID:-}" ]; then
        kill "$LOGCAT_PID" 2>/dev/null
        wait "$LOGCAT_PID" 2>/dev/null
    fi

    # Matches both Java-level crashes (FATAL EXCEPTION / ANR) and native
    # crashes (F/libc "Fatal signal N (SIGxxx)" from cgo/C code, which produce
    # no "FATAL EXCEPTION" line at all).
    local crash_log_hits native_crash_count vk_create vk_destroy
    crash_log_hits=$(grep -cE "FATAL EXCEPTION|ANR in $PKG|AndroidRuntime: FATAL|Fatal signal [0-9]+ \(SIG" "$LOGCAT_FILE" 2>/dev/null)
    native_crash_count=$(grep -cE "Fatal signal [0-9]+ \(SIG" "$LOGCAT_FILE" 2>/dev/null)
    vk_create=$(grep -c "createOverlay: SurfaceView added" "$LOGCAT_FILE" 2>/dev/null)
    vk_destroy=$(grep -c "destroy: SurfaceView removed" "$LOGCAT_FILE" 2>/dev/null)

    echo ""
    echo "=================================================="
    echo " RESULTS: $pass passed, $fail failed"
    echo "=================================================="
    echo " Vulkan overlay: created=$vk_create destroyed=$vk_destroy (mismatch = leak candidate)"
    echo " Logcat crash/ANR hits: $crash_log_hits (native SIGSEGV/SIGABRT: $native_crash_count)"
    echo " Screenshots + XML dumps + full logcat saved under: $OUTDIR"
    echo ""

    if [ ${#leak_events[@]} -gt 0 ]; then
        echo "🔴 Leak events (${#leak_events[@]}):"
        for e in "${leak_events[@]}"; do echo "  - $e"; done
        echo ""
    fi
    if [ ${#crash_events[@]} -gt 0 ]; then
        echo "💥 Crash/restart events (${#crash_events[@]}):"
        for e in "${crash_events[@]}"; do echo "  - $e"; done
        echo ""
    fi
    if [ "$native_crash_count" -gt 0 ]; then
        echo "⚠️  Native crash signals (SIGSEGV/SIGABRT) with top native frame:"
        grep -E "Fatal signal [0-9]+ \(SIG" "$LOGCAT_FILE" | sed 's/^/  - /'
        echo ""
        echo "   Top native frames (#00 pc ...) following each crash:"
        grep -A1 "backtrace:$" "$LOGCAT_FILE" | grep "#00 pc" | sed 's/^/     /'
        echo ""
    fi
    local java_hits
    java_hits=$(grep -cE "FATAL EXCEPTION|ANR in $PKG|AndroidRuntime: FATAL" "$LOGCAT_FILE" 2>/dev/null)
    if [ "$java_hits" -gt 0 ]; then
        echo "⚠️  Java-level FATAL/ANR lines:"
        grep -E "FATAL EXCEPTION|ANR in $PKG|AndroidRuntime: FATAL" "$LOGCAT_FILE" | head -20
        echo ""
    fi

    echo "=================================================="

    [ "$fail" -eq 0 ] && [ "$vk_create" -eq "$vk_destroy" ] && [ "$crash_log_hits" -eq 0 ]
}

screenshot() {
    local tag="$1"
    seq_n=$((seq_n + 1))
    local fname
    fname=$(printf "%s/%03d_%s.png" "$OUTDIR" "$seq_n" "$tag")
    $ADB exec-out screencap -p > "$fname" 2>/dev/null
    echo "$fname"
}

# Pulls the live uiautomator view-hierarchy XML from the device to $OUTDIR.
dump_xml() {
    local tag="$1"
    seq_n=$((seq_n + 1))
    local fname
    fname=$(printf "%s/%03d_%s.xml" "$OUTDIR" "$seq_n" "$tag")
    $ADB shell uiautomator dump /sdcard/usbridge_ui_stress_dump.xml >/dev/null 2>&1
    $ADB pull /sdcard/usbridge_ui_stress_dump.xml "$fname" >/dev/null 2>&1
    echo "$fname"
}

# Validates a screenshot against an expected screen; records pass/fail.
# expected: Control | Devices | Snapshots | Scripts | connections
validate() {
    local fname="$1" expected="$2" ctx="$3"
    local out
    out=$(python3 "$CHECK_PY" tab "$fname" "$expected")
    local rc=$?
    if [ $rc -eq 0 ]; then
        pass=$((pass + 1))
        log "  ✅ [$ctx] expected=$expected -> $out"
    else
        fail=$((fail + 1))
        log "  ❌ [$ctx] expected=$expected -> $out  ($(basename "$fname"))"
    fi
    return $rc
}

# Retries a couple of times before reporting empty — a single `adb shell`
# round-trip occasionally drops output under heavy back-to-back invocation,
# which must not be confused with an actual process death.
get_pid() {
    local p
    for _ in 1 2 3; do
        p=$($ADB shell pidof "$PKG" 2>/dev/null | tr -d '\r\n')
        [ -n "$p" ] && break
        sleep 0.3
    done
    echo "$p"
}

surface_leak_count() {
    $ADB shell dumpsys SurfaceFlinger --list 2>/dev/null | grep -c "SurfaceView\[$PKG"
}

# Returns "V" / "I" / "G" (visible/invisible/gone) for the first SurfaceView
# state found in the activity dump, or empty if none exists at all.
surface_visibility() {
    $ADB shell dumpsys activity "$PKG" 2>/dev/null \
        | grep -o 'SurfaceView{[^}]*} [A-Z]\.' \
        | head -1 \
        | grep -o '} [A-Z]\.' \
        | tr -dc 'A-Z'
}

# Primary leak check: pull the live uiautomator XML and count View nodes
# (see file header). Also cross-checks dumpsys as a secondary signal.
check_no_leak() {
    local ctx="$1"
    local xml_path out rc
    xml_path=$(dump_xml "leakcheck_$ctx")
    out=$(python3 "$CHECK_PY" leak "$xml_path" 1)
    rc=$?
    if [ $rc -ne 0 ]; then
        fail=$((fail + 1))
        leak_events+=("$ctx: uiautomator dump -> $out (extra View node = leftover Vulkan overlay)")
        log "  🔴 LEAK [$ctx] $out — SurfaceView still attached to decorView"
    else
        pass=$((pass + 1))
        log "  ✅ [$ctx] $out — no leftover SurfaceView in view hierarchy"
    fi

    # Secondary cross-check via dumpsys (belt & suspenders).
    local n leak_vis
    n=$(surface_leak_count)
    leak_vis=$(surface_visibility)
    if [ "$n" -gt 0 ]; then
        leak_events+=("$ctx: [dumpsys] SurfaceFlinger still lists $n SurfaceView(s) for $PKG")
        log "  🔴 LEAK(dumpsys) [$ctx] SurfaceFlinger --list still shows $n SurfaceView(s)"
    fi
    if [ "$leak_vis" = "V" ]; then
        leak_events+=("$ctx: [dumpsys] SurfaceView visibility=VISIBLE while on connections screen")
        log "  🔴 LEAK(dumpsys) [$ctx] SurfaceView visibility flag = VISIBLE (should be gone)"
    fi
}

# Pulls the most recent native-crash signature (signal + top native frame)
# out of the live logcat file, for attaching to a crash/restart event.
last_crash_signature() {
    local sig
    sig=$(grep -E "Fatal signal [0-9]+ \(SIG" "$LOGCAT_FILE" 2>/dev/null | tail -1)
    [ -z "$sig" ] && { echo "(no native 'Fatal signal' line found in logcat)"; return; }
    local frame
    frame=$(grep -A2 "^.*backtrace:$" "$LOGCAT_FILE" 2>/dev/null | grep "#00 pc" | tail -1 | sed -E 's/^[0-9: .A-Za-z\/]*F\/DEBUG *\( *[0-9]+\): *//')
    echo "$sig | ${frame:-no backtrace captured}"
}

check_alive() {
    local ctx="$1" pid_before="$2"
    local pid_after
    pid_after=$(get_pid)
    if [ -z "$pid_after" ]; then
        fail=$((fail + 1))
        local sig
        sig=$(last_crash_signature)
        crash_events+=("$ctx: process not running (was pid=$pid_before) -- $sig")
        log "  💥 CRASH [$ctx] process is gone (was pid=$pid_before)"
        log "      $sig"
        return 1
    fi
    if [ -n "$pid_before" ] && [ "$pid_after" != "$pid_before" ]; then
        fail=$((fail + 1))
        local sig
        sig=$(last_crash_signature)
        crash_events+=("$ctx: pid changed $pid_before -> $pid_after (crash+restart) -- $sig")
        log "  💥 CRASH [$ctx] pid changed $pid_before -> $pid_after"
        log "      $sig"
        return 1
    fi
    return 0
}

launch_fresh() {
    $ADB shell am force-stop "$PKG"
    sleep 1
    $ADB shell monkey -p "$PKG" -c android.intent.category.LAUNCHER 1 >/dev/null 2>&1
    sleep 2
}

connect_saved() {
    tap $CONNECT_ICON
}

goto_tab() {
    case "$1" in
        Control)   tap $TAB_CONTROL ;;
        Devices)   tap $TAB_DEVICES ;;
        Snapshots) tap $TAB_SNAPSHOTS ;;
        Scripts)   tap $TAB_SCRIPTS ;;
    esac
}

disconnect_via_button() {
    sleep "${1:-1}"
    tap $EXIT_ICON
}

# ── preflight ────────────────────────────────────────────────────────────────

echo "=================================================="
echo " Android UI Stress Test"
echo " Device: $SERIAL"
echo " Output: $OUTDIR"
echo "=================================================="

SIZE=$($ADB shell wm size | grep -o '[0-9]*x[0-9]*' | tail -1)
if [ "$SIZE" != "1080x2400" ]; then
    echo "❌ Device resolution is $SIZE, tap coordinates are calibrated for 1080x2400."
    echo "   Re-calibrate TAB_*/CONNECT_ICON/EXIT_ICON constants before running on this device."
    exit 1
fi
echo "✅ Resolution: $SIZE"

if ! $ADB shell pm list packages | grep -q "$PKG"; then
    echo "❌ $PKG is not installed on $SERIAL"
    exit 1
fi
echo "✅ $PKG installed"

$ADB logcat -c
$ADB logcat -v time > "$LOGCAT_FILE" 2>&1 &
LOGCAT_PID=$!
trap 'kill $LOGCAT_PID 2>/dev/null' EXIT

launch_fresh
f=$(screenshot "boot")
validate "$f" connections "boot"
check_no_leak "boot"

# ── Phase 1: slow, deliberate pass through every tab ────────────────────────

echo ""
echo "=== PHASE 1: slow deliberate cycles ($SLOW_CYCLES x) ==="
for i in $(seq 1 "$SLOW_CYCLES"); do
    log "-- slow cycle $i/$SLOW_CYCLES --"
    connect_saved
    sleep 2
    f=$(screenshot "slow${i}_connect")
    validate "$f" Control "slow$i:connect"

    for tab in Devices Snapshots Scripts Control; do
        goto_tab "$tab"
        sleep 1.5
        f=$(screenshot "slow${i}_$tab")
        validate "$f" "$tab" "slow$i:$tab"
    done

    disconnect_via_button 1
    sleep 2
    f=$(screenshot "slow${i}_disconnect")
    validate "$f" connections "slow$i:disconnect"
    check_no_leak "slow$i:after-disconnect"
done

# ── Phase 2: fast repeated tab switching while connected ────────────────────

echo ""
echo "=== PHASE 2: fast tab switching ($FAST_SWITCH_TAPS taps) ==="
connect_saved
sleep 2
pid_before=$(get_pid)
tabs=(Control Devices Snapshots Scripts)
for i in $(seq 1 "$FAST_SWITCH_TAPS"); do
    t=${tabs[$((RANDOM % 4))]}
    goto_tab "$t"
    sleep 0.15
done
sleep 1
f=$(screenshot "fastswitch_end")
python3 "$CHECK_PY" tab "$f"
check_alive "fastswitch:after-burst" "$pid_before"

disconnect_via_button 1
sleep 2
f=$(screenshot "fastswitch_disconnect")
validate "$f" connections "fastswitch:disconnect"
check_no_leak "fastswitch:after-disconnect"

# ── Phase 3: aggressive connect/disconnect hammering (button-based) ─────────

echo ""
echo "=== PHASE 3: connect/disconnect hammering ($HAMMER_CYCLES x) ==="
for i in $(seq 1 "$HAMMER_CYCLES"); do
    pid_before=$(get_pid)
    connect_saved
    sleep 0.4
    tap $EXIT_ICON
    sleep 0.4
    check_alive "hammer$i" "$pid_before"
done
sleep 1.5
f=$(screenshot "hammer_end")
validate "$f" connections "hammer:end"
check_no_leak "hammer:end"

# ── Phase 4: back-button disconnect (suspected leak path) ───────────────────

echo ""
echo "=== PHASE 4: back-button disconnect cycles ($BACK_CYCLES x) ==="
for i in $(seq 1 "$BACK_CYCLES"); do
    pid_before=$(get_pid)
    connect_saved
    sleep "$(python3 -c 'import random;print(round(random.uniform(0.3,1.5),2))')"
    back_key
    sleep 1
    f=$(screenshot "back${i}")
    validate "$f" connections "back$i"
    check_no_leak "back$i"
    check_alive "back$i" "$pid_before"
done

# ── Phase 5: randomized chaos ────────────────────────────────────────────────

echo ""
echo "=== PHASE 5: chaos ($CHAOS_CYCLES x) ==="
for i in $(seq 1 "$CHAOS_CYCLES"); do
    pid_before=$(get_pid)
    connect_saved
    hops=$((RANDOM % 5))
    for h in $(seq 1 "$hops"); do
        goto_tab "${tabs[$((RANDOM % 4))]}"
        sleep "0.$((RANDOM % 6))"
    done
    if [ $((RANDOM % 2)) -eq 0 ]; then
        disconnect_via_button "0.$((RANDOM % 8 + 2))"
        method="button"
    else
        sleep "0.$((RANDOM % 8 + 1))"
        back_key
        method="back"
    fi
    sleep 1
    f=$(screenshot "chaos${i}_${method}")
    validate "$f" connections "chaos$i:$method"
    check_no_leak "chaos$i:$method"
    check_alive "chaos$i:$method" "$pid_before"
done

# ── summary ──────────────────────────────────────────────────────────────────

print_summary
FINAL_RC=$?
trap - EXIT
exit $FINAL_RC

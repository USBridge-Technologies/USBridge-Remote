#!/bin/bash
# Head-to-head streamer benchmark: macOS client (this machine) vs a real
# Windows USBridge Agent host, over the real LAN/cable. Unlike
# test_streamer_benchmark_win_linux.sh (which needs a Linux client binary
# and drives ffplay via a scheduled task), this one targets a live macOS
# client + a real Windows box reachable over SSH with password auth, and
# additionally samples the Windows-side streamer/sunshine process's own
# CPU + memory for the whole window, not just client-side smoothness.
#
# Same content, both passes: this project's own capture backends
# (DXGI/Desktop Duplication, ScreenCaptureKit) only ever see whatever's on
# the real interactive desktop, and sshd on Windows runs commands in
# Session 0 -- a separate, non-interactive session -- so a plain
# `ssh host ffplay ...` never actually appears on the captured desktop
# (see sunshine_backend.go's useSunshineSessionBroker doc comment for the
# same isolation issue elsewhere in this codebase). `schtasks /IT` hops
# ffplay.exe into the real console session instead, playing an ffmpeg
# lavfi testsrc2 motion pattern -- self-looping by construction, so both
# passes always encode the exact same dynamic content, not whatever happens
# to be on the desktop that day.
#
# Usage: ./tests/test_streamer_benchmark_macos_win.sh <win_host> <master_key> <win_pass> [duration_seconds] [win_user] [outdir]

set -u

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

WIN_HOST="${1:?win_host required (agent LAN IP)}"
MASTER_KEY="${2:?master_key required}"
WIN_PASS="${3:?win_pass required}"
DURATION="${4:-120}"
WIN_USER="${5:-amir}"
TS="$(date +%Y%m%d_%H%M%S)"
OUTDIR="${6:-$REPO_ROOT/tests/bench_output/resource_$TS}"

APP_BINARY="$REPO_ROOT/dist/macos/USBridgeClient.app/Contents/MacOS/USBridgeClient"
LOG_FILE="$HOME/Library/Logs/USBridgeClient/app.log"
SAMPLER_PS1="$REPO_ROOT/client/tests/win_resource_sampler.ps1"
WIN_SOCK="C:\\Users\\${WIN_USER}\\AppData\\Roaming\\usbridge-agent\\admin.sock"
WIN_HOME_FS="C:/Users/${WIN_USER}"  # forward-slash form: scp's "host:path" splits on ':', which a
                                     # backslash Windows path's own drive-letter colon (C:\...)
                                     # collides with -- confirmed live, scp silently mis-parsed the
                                     # destination and never uploaded anything. Only used for
                                     # scp source/destination arguments; ssh/curl/-File arguments
                                     # keep the backslash form, which cmd.exe and PowerShell both
                                     # accept natively.
FFPLAY='C:\msys64\ucrt64\bin\ffplay.exe'
PATTERN_TASK="usbridge_bench_pattern"

mkdir -p "$OUTDIR"
log() { echo "[$(date +%H:%M:%S)] $*"; }

ssh_win() { sshpass -p "$WIN_PASS" ssh -o StrictHostKeyChecking=accept-new "${WIN_USER}@${WIN_HOST}" "$1" 2>/dev/null; }
scp_to_win() { sshpass -p "$WIN_PASS" scp -o StrictHostKeyChecking=accept-new "$1" "${WIN_USER}@${WIN_HOST}:$2" >/dev/null 2>&1; }
scp_from_win() { sshpass -p "$WIN_PASS" scp -o StrictHostKeyChecking=accept-new "${WIN_USER}@${WIN_HOST}:$1" "$2" >/dev/null 2>&1; }

win_admin_get() {
    ssh_win 'curl.exe -s --unix-socket "'"$WIN_SOCK"'" http://localhost'"$1"
}
win_admin_post_kind() {
    # $1 = path, $2 = kind value for {"kind":"<value>"}
    ssh_win 'curl.exe -s --unix-socket "'"$WIN_SOCK"'" -X POST -d "{\"kind\":\"'"$2"'\"}" http://localhost'"$1"
}

active_backend() {
    win_admin_get "/token/entitlement-status" | python3 -c "import sys,json;print(json.load(sys.stdin).get('active_backend',''))" 2>/dev/null
}

switch_backend() {
    local kind="$1"
    log "Switching backend -> $kind"
    win_admin_post_kind "/token/set-stream-backend" "$kind" >/dev/null
    for i in $(seq 1 30); do
        [ "$(active_backend)" = "$kind" ] && { log "  backend live: $kind"; return 0; }
        sleep 1
    done
    log "  WARNING: backend switch to $kind not confirmed after 30s (continuing anyway)"
}

proc_name_for() {
    case "$1" in
        sunshine) echo "sunshine" ;;
        rustshine) echo "usbridge-streamer" ;;
    esac
}

wait_for_process() {
    # $1 = process image name (no .exe). Returns 0 and prints elapsed seconds on stdout via caller's own timer.
    local name="$1"
    for i in $(seq 1 60); do
        if ssh_win "tasklist /fi \"imagename eq ${name}.exe\"" | grep -qi "${name}.exe"; then
            return 0
        fi
        sleep 0.5
    done
    return 1
}

start_pattern() {
    log "  starting motion pattern on Windows desktop (interactive session)..."
    ssh_win "schtasks /delete /tn ${PATTERN_TASK} /f >nul 2>&1 & schtasks /create /tn ${PATTERN_TASK} /tr \"'${FFPLAY}' -fs -an -loglevel quiet -f lavfi -i testsrc2=size=1920x1080:rate=60\" /sc onstart /ru ${WIN_USER} /it /f >nul & schtasks /run /tn ${PATTERN_TASK}" >/dev/null
    sleep 3
}

stop_pattern() {
    ssh_win "taskkill /F /IM ffplay.exe >nul 2>&1 & schtasks /delete /tn ${PATTERN_TASK} /f >nul 2>&1" >/dev/null
}

run_pass() {
    local kind="$1"
    local pass_dir="$OUTDIR/$kind"
    mkdir -p "$pass_dir"
    local proc; proc="$(proc_name_for "$kind")"
    log "=================================================="
    log " PASS: $kind (process: ${proc}.exe)"
    log "=================================================="

    switch_backend "$kind"
    start_pattern

    # Startup timing #1: backend-switch-confirmed -> process actually alive.
    T0=$(date +%s.%N)
    if wait_for_process "$proc"; then
        SPAWN_DELTA=$(python3 -c "print(f'{$(date +%s.%N) - $T0:.1f}')")
        log "  process ${proc}.exe alive after ${SPAWN_DELTA}s"
    else
        SPAWN_DELTA="timeout"
        log "  WARNING: ${proc}.exe never appeared within 30s"
    fi

    # Kick off the Windows-side CPU/mem sampler for the full window, in the
    # background on this Mac (the ssh connection itself blocks for
    # DurationSec on the remote end -- no scheduled-task indirection needed
    # here since Get-Process doesn't require the interactive session the
    # way DXGI/ffplay capture does).
    REMOTE_CSV="C:\\Users\\${WIN_USER}\\bench_${kind}.csv"
    REMOTE_CSV_FS="${WIN_HOME_FS}/bench_${kind}.csv"
    LOCAL_CSV="$pass_dir/resource_usage.csv"
    scp_to_win "$SAMPLER_PS1" "${WIN_HOME_FS}/win_resource_sampler.ps1"
    # Bypasses ssh_win() here (not just its call-site redirect) so real
    # PowerShell errors reach sampler.log -- ssh_win's own 2>/dev/null
    # swallows remote stderr internally, which is exactly what hid the
    # scp-upload failure this function shipped with the first time.
    sshpass -p "$WIN_PASS" ssh -o StrictHostKeyChecking=accept-new "${WIN_USER}@${WIN_HOST}" \
        "powershell -NoProfile -ExecutionPolicy Bypass -File C:\\Users\\${WIN_USER}\\win_resource_sampler.ps1 -ProcName ${proc} -DurationSec $((DURATION + 10)) -IntervalSec 5 -Out ${REMOTE_CSV}" \
        > "$pass_dir/sampler.log" 2>&1 &
    SAMPLER_PID=$!

    pkill -f "USBridgeClient.app/Contents/MacOS/USBridgeClient" 2>/dev/null
    sleep 1
    : > "$LOG_FILE"

    # Startup timing #2: client launch -> client-visible connected video
    # (the end-to-end number a real user actually feels).
    T1=$(date +%s.%N)
    "$APP_BINARY" "usbridge://connect?internal_host=${WIN_HOST}&master_key=${MASTER_KEY}&protocol=direct&immediate=true" >/dev/null 2>&1 &
    CLIENT_PID=$!

    CONNECTED=false
    for i in $(seq 1 30); do
        sleep 1
        if grep -q "✅ Connected to USBridge via direct" "$LOG_FILE" 2>/dev/null; then
            CONNECTED=true
            break
        fi
        if ! kill -0 $CLIENT_PID 2>/dev/null; then
            log "  WARNING: client exited before connecting"
            break
        fi
    done
    if $CONNECTED; then
        CONNECT_DELTA=$(python3 -c "print(f'{$(date +%s.%N) - $T1:.1f}')")
        log "  client connected after ${CONNECT_DELTA}s"
    else
        CONNECT_DELTA="timeout"
        log "  WARNING: client never confirmed connect within 30s"
    fi

    log "  Watching stream for ${DURATION}s..."
    START_TS=$(date +%s)
    END_TS=$((START_TS + DURATION))
    while [ "$(date +%s)" -lt "$END_TS" ]; do
        sleep 10
        ELAPSED=$(( $(date +%s) - START_TS ))
        STALLS=$(grep -c "Profiler" "$LOG_FILE" 2>/dev/null); STALLS=${STALLS:-0}
        RECONNECTS=$(grep -c "forcing reconnect" "$LOG_FILE" 2>/dev/null); RECONNECTS=${RECONNECTS:-0}
        log "  [${ELAPSED}s/${DURATION}s] stalls_so_far=${STALLS} reconnects_so_far=${RECONNECTS}"
        if ! kill -0 $CLIENT_PID 2>/dev/null; then
            log "  WARNING: client exited mid-window"
            break
        fi
    done

    kill $CLIENT_PID 2>/dev/null
    wait $CLIENT_PID 2>/dev/null
    cp "$LOG_FILE" "$pass_dir/app.log" 2>/dev/null

    wait $SAMPLER_PID 2>/dev/null
    scp_from_win "$REMOTE_CSV_FS" "$LOCAL_CSV"
    ssh_win "del ${REMOTE_CSV} >nul 2>&1" >/dev/null

    stop_pattern

    STALL_COUNT=$(grep -c "Profiler" "$pass_dir/app.log" 2>/dev/null); STALL_COUNT=${STALL_COUNT:-0}
    RECONNECT_COUNT=$(grep -c "forcing reconnect" "$pass_dir/app.log" 2>/dev/null); RECONNECT_COUNT=${RECONNECT_COUNT:-0}

    { echo "kind=$kind"
      echo "spawn_delay_s=$SPAWN_DELTA"
      echo "connect_delay_s=$CONNECT_DELTA"
      echo "stalls=$STALL_COUNT"
      echo "reconnects=$RECONNECT_COUNT"
      if [ -f "$LOCAL_CSV" ]; then
          python3 - "$LOCAL_CSV" <<'PYEOF'
import csv, sys
path = sys.argv[1]
cpu, ws, pm = [], [], []
with open(path) as f:
    for row in csv.DictReader(f):
        try:
            if row["cpu_pct"]: cpu.append(float(row["cpu_pct"]))
            if row["working_set_mb"]: ws.append(float(row["working_set_mb"]))
            if row["private_mb"]: pm.append(float(row["private_mb"]))
        except (ValueError, KeyError):
            pass
def stats(name, vals):
    if not vals:
        print(f"{name}_avg=n/a {name}_peak=n/a")
        return
    print(f"{name}_avg={sum(vals)/len(vals):.1f} {name}_peak={max(vals):.1f}")
stats("cpu_pct", cpu[1:] if len(cpu) > 1 else cpu)  # drop first sample (no baseline yet)
stats("working_set_mb", ws)
stats("private_mb", pm)
PYEOF
      else
          echo "resource_csv_missing=true"
      fi
    } | tee "$pass_dir/summary.txt"

    log "  pass complete -> $pass_dir"
}

if [ ! -x "$APP_BINARY" ]; then
    echo "❌ macOS client not built at $APP_BINARY — run client/scripts/build_macos.sh first" >&2
    exit 1
fi

echo "=================================================="
echo " USBridge streamer benchmark: Sunshine vs RustShine"
echo " macOS client (this machine)  <->  Windows agent: $WIN_HOST"
echo " Same dynamic content both passes (ffplay testsrc2 motion, 1080p60)"
echo " Duration per backend: ${DURATION}s"
echo " Output: $OUTDIR"
echo "=================================================="

run_pass "sunshine"
sleep 5
run_pass "rustshine"

echo ""
echo "=================================================="
echo " Benchmark complete. Per-pass summaries + raw CSVs in: $OUTDIR"
echo "=================================================="
for kind in sunshine rustshine; do
    echo "--- $kind ---"
    cat "$OUTDIR/$kind/summary.txt" 2>/dev/null
    echo ""
done

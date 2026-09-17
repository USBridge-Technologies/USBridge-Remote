#!/bin/bash
# ONE continuous client connection, stepping a MikroTik random-drop firewall
# rule through increasing loss levels while the stream stays up throughout --
# shows real transition/recovery behavior between levels instead of just
# isolated fresh-connect snapshots per level. Also samples the Windows
# host process's own CPU/memory for the whole run via win_resource_sampler.ps1,
# so a single run gives video stalls, audio health, AND resource cost under
# degrading network conditions all at once.
#
# Router-side prerequisite (not automated here -- topology, not test logic):
# a MikroTik forward-chain rule matching UDP host->client traffic, tagged
# `comment=usbridge_loss_test`, initially disabled. See
# rust-shine/docs/benchmarks/BENCHMARKS.md for exactly why this needs the
# client's port on a routed subnet rather than the default bridge (hardware
# switch offload otherwise bypasses the firewall entirely -- confirmed live,
# `random=99` matched zero packets on a bridged port).
#
# Usage: ./loss_sweep_benchmark.sh <backend> <win_host> <master_key> <win_pass> \
#            [mik_host] [mik_pass] [levels] [hold_seconds] [win_user] [outdir]
set -u
BACKEND="${1:?backend required (sunshine|rustshine)}"
WIN_HOST="${2:?win_host required}"
MASTER_KEY="${3:?master_key required}"
WIN_PASS="${4:?win_pass required}"
MIK_HOST="${5:-192.168.200.1}"
MIK_PASS="${6:-$WIN_PASS}"
LEVELS="${7:-0 3 8 15 25 40 60 0}"   # trailing 0 = recovery check
HOLD="${8:-40}"
WIN_USER="${9:-amir}"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OUTDIR="${10:-$REPO_ROOT/tests/bench_output/loss_sweep_${BACKEND}_$(date +%Y%m%d_%H%M%S)}"
mkdir -p "$OUTDIR"

LOG="$HOME/Library/Logs/USBridgeClient/app.log"
APP="$REPO_ROOT/dist/macos/USBridgeClient.app/Contents/MacOS/USBridgeClient"
WIN_SOCK="C:\\Users\\${WIN_USER}\\AppData\\Roaming\\usbridge-agent\\admin.sock"
WIN_HOME_FS="C:/Users/${WIN_USER}"
SAMPLER_PS1="$REPO_ROOT/client/tests/win_resource_sampler.ps1"

mik() { sshpass -p "$MIK_PASS" ssh -o StrictHostKeyChecking=no admin@"$MIK_HOST" "$1" 2>&1; }
win() { sshpass -p "$WIN_PASS" ssh -o StrictHostKeyChecking=no "${WIN_USER}@${WIN_HOST}" "$1" 2>&1; }
proc_name_for() { case "$1" in sunshine) echo "sunshine" ;; rustshine) echo "usbridge-streamer" ;; esac; }

if [ ! -x "$APP" ]; then
    echo "macOS client not built at $APP -- run client/scripts/build_macos.sh first" >&2
    exit 1
fi

CUR=$(win "curl.exe -s --unix-socket $WIN_SOCK http://localhost/token/entitlement-status" | python3 -c "import sys,json;print(json.load(sys.stdin).get('active_backend',''))" 2>/dev/null)
if [ "$CUR" != "$BACKEND" ]; then
    win 'curl.exe -s --unix-socket "'"$WIN_SOCK"'" -X POST -d "{\"kind\":\"'"$BACKEND"'\"}" http://localhost/token/set-stream-backend' >/dev/null
    sleep 4
fi

# Resource sampling runs for the whole sweep -- generous duration estimate
# (number of levels * hold, plus slack) since it can't know the real
# per-level timing in advance.
LEVEL_COUNT=$(echo "$LEVELS" | wc -w | tr -d ' ')
SAMPLE_DURATION=$(( (LEVEL_COUNT + 2) * HOLD ))
PROC=$(proc_name_for "$BACKEND")
REMOTE_CSV="C:\\Users\\${WIN_USER}\\loss_sweep_${BACKEND}.csv"
REMOTE_CSV_FS="${WIN_HOME_FS}/loss_sweep_${BACKEND}.csv"
scp -o StrictHostKeyChecking=no -o BatchMode=no "$SAMPLER_PS1" "${WIN_USER}@${WIN_HOST}:${WIN_HOME_FS}/win_resource_sampler.ps1" >/dev/null 2>&1 \
    || sshpass -p "$WIN_PASS" scp -o StrictHostKeyChecking=no "$SAMPLER_PS1" "${WIN_USER}@${WIN_HOST}:${WIN_HOME_FS}/win_resource_sampler.ps1" >/dev/null 2>&1
sshpass -p "$WIN_PASS" ssh -o StrictHostKeyChecking=no "${WIN_USER}@${WIN_HOST}" \
    "powershell -NoProfile -ExecutionPolicy Bypass -File C:\\Users\\${WIN_USER}\\win_resource_sampler.ps1 -ProcName ${PROC} -DurationSec ${SAMPLE_DURATION} -IntervalSec 5 -Out ${REMOTE_CSV}" \
    > "$OUTDIR/sampler.log" 2>&1 &
SAMPLER_PID=$!

pkill -f "USBridgeClient.app/Contents/MacOS/USBridgeClient" 2>/dev/null
sleep 1
: > "$LOG"
mik "/ip firewall filter set [find comment=usbridge_loss_test] random=1 disabled=yes" >/dev/null

"$APP" "usbridge://connect?internal_host=${WIN_HOST}&master_key=${MASTER_KEY}&protocol=direct&immediate=true" >/dev/null 2>&1 &
CLIENT_PID=$!
for i in $(seq 1 20); do
    grep -qa "Connected to USBridge via" "$LOG" 2>/dev/null && break
    sleep 1
done
sleep 5
echo "SWEEP_START backend=$BACKEND utc=$(date -u +%Y-%m-%dT%H:%M:%S)"

audio_snapshot() {
    grep -a "CoreAudio: frames=" "$LOG" | tail -1 | grep -oE "drops=[0-9]+ restarts=[0-9]+.*plc=[0-9]+ opus_err=[0-9]+"
}
audio_field() { echo "$1" | grep -oE "$2=[0-9]+" | head -1 | cut -d= -f2; }

RESULTS_CSV="$OUTDIR/sweep_results.csv"
echo "loss_dial_pct,video_stalls_in_window,reconnects_total,audio_drops_after,audio_restarts_after,audio_plc_after" > "$RESULTS_CSV"

for pct in $LEVELS; do
    T0=$(grep -aoc "Profiler" "$LOG")
    if [ "$pct" -eq 0 ]; then
        mik "/ip firewall filter set [find comment=usbridge_loss_test] disabled=yes" >/dev/null
        echo "LEVEL loss=0% (baseline/recovery) utc=$(date -u +%Y-%m-%dT%H:%M:%S) stalls_before=$T0"
    else
        mik "/ip firewall filter set [find comment=usbridge_loss_test] random=$pct disabled=no" >/dev/null
        echo "LEVEL loss=${pct}% utc=$(date -u +%Y-%m-%dT%H:%M:%S) stalls_before=$T0"
    fi
    sleep "$HOLD"
    T1=$(grep -aoc "Profiler" "$LOG")
    A1=$(audio_snapshot)
    ALIVE=$(kill -0 $CLIENT_PID 2>/dev/null && echo yes || echo no)
    RECON=$(grep -aoc "forcing reconnect" "$LOG")
    echo "  -> stalls_in_window=$((T1-T0)) total_reconnects_so_far=$RECON client_alive=$ALIVE audio_after=[$A1]"
    echo "${pct},$((T1-T0)),${RECON},$(audio_field "$A1" drops),$(audio_field "$A1" restarts),$(audio_field "$A1" plc)" >> "$RESULTS_CSV"
    if [ "$ALIVE" = "no" ]; then
        echo "CLIENT DIED at loss=${pct}% -- stopping sweep early"
        break
    fi
done

mik "/ip firewall filter set [find comment=usbridge_loss_test] disabled=yes" >/dev/null
echo "SWEEP_END utc=$(date -u +%Y-%m-%dT%H:%M:%S)"
cp "$LOG" "$OUTDIR/app.log" 2>/dev/null
kill $CLIENT_PID 2>/dev/null
wait $CLIENT_PID 2>/dev/null

wait $SAMPLER_PID 2>/dev/null
sshpass -p "$WIN_PASS" scp -o StrictHostKeyChecking=no "${WIN_USER}@${WIN_HOST}:${REMOTE_CSV_FS}" "$OUTDIR/resource_usage.csv" >/dev/null 2>&1
sshpass -p "$WIN_PASS" ssh -o StrictHostKeyChecking=no "${WIN_USER}@${WIN_HOST}" "del ${REMOTE_CSV} >nul 2>&1" >/dev/null 2>&1

echo "Results: $RESULTS_CSV"
echo "Resource usage during sweep: $OUTDIR/resource_usage.csv"
echo "Full client log: $OUTDIR/app.log"

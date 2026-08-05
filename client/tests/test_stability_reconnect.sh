#!/bin/bash
# Stability test: connect to a real (separate-machine) USBridge host and watch
# the video stream for the reconnect-storm symptom ("forcing reconnect" /
# "Unrecoverable frame" cascades) over a sustained window, not just the
# initial handshake that test_macos_direct_connect.sh already covers.
#
# Usage: ./tests/test_stability_reconnect.sh [host] [master_key] [protocol] [duration_seconds]

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HOST="${1:?host required}"
MASTER_KEY="${2:?master_key required (see USBridge Master QR Sync)}"
PROTOCOL="${3:-direct}"
DURATION="${4:-90}"

DEEPLINK="usbridge://connect?internal_host=${HOST}&master_key=${MASTER_KEY}&protocol=${PROTOCOL}&immediate=true"

echo "=================================================="
echo " macOS Stability Test (sustained-stream watch)"
echo "=================================================="
echo " Host:     $HOST"
echo " Protocol: $PROTOCOL"
echo " Duration: ${DURATION}s"
echo "=================================================="

echo ""
echo "=> [1/5] Verifying server is reachable..."
if curl -sf --connect-timeout 3 "http://${HOST}:8080/api/healthz" > /dev/null; then
    echo "   ✅ Server at ${HOST}:8080 is reachable"
else
    echo "   ❌ Server at ${HOST}:8080 is NOT reachable — aborting"
    exit 1
fi

echo ""
echo "=> [2/5] Building macOS app..."
"$SCRIPTS_DIR/build_macos.sh"

APP_BINARY="$REPO_ROOT/dist/macos/USBridgeClient.app/Contents/MacOS/USBridgeClient"
LOG_FILE="$HOME/Library/Logs/USBridgeClient/app.log"

if [ ! -f "$APP_BINARY" ]; then
    echo "   ❌ Binary not found at $APP_BINARY"
    exit 1
fi
echo "   ✅ Built: $APP_BINARY"

pkill -f "USBridgeClient" 2>/dev/null || true
sleep 0.5

mkdir -p "$(dirname "$LOG_FILE")"
: > "$LOG_FILE"

echo ""
echo "=> [3/5] Launching app with deeplink..."
"$APP_BINARY" "$DEEPLINK" &
APP_PID=$!
echo "   PID: $APP_PID"

echo ""
echo "=> [4/5] Waiting for initial connect..."
CONNECTED=false
for i in $(seq 1 20); do
    sleep 1
    if grep -q "✅ Connected to USBridge via ${PROTOCOL}" "$LOG_FILE" 2>/dev/null; then
        CONNECTED=true
        break
    fi
    if grep -q "Connection failed:" "$LOG_FILE" 2>/dev/null; then
        echo "   ❌ Connection failed before streaming even started"
        grep "Connection failed:" "$LOG_FILE" | tail -5
        kill $APP_PID 2>/dev/null || true
        exit 1
    fi
    if ! kill -0 $APP_PID 2>/dev/null; then
        echo "   ⚠️  App exited unexpectedly during connect"
        exit 1
    fi
done

if ! $CONNECTED; then
    echo "   ⚠️  No connect confirmation within 20s — continuing to watch anyway"
fi

echo ""
echo "=> [5/5] Watching stream for ${DURATION}s for reconnect-storm symptoms..."
START_TS=$(date +%s)
END_TS=$((START_TS + DURATION))
while [ "$(date +%s)" -lt "$END_TS" ]; do
    sleep 5
    if ! kill -0 $APP_PID 2>/dev/null; then
        echo "   ⚠️  App exited unexpectedly during the watch window"
        break
    fi
    ELAPSED=$(( $(date +%s) - START_TS ))
    # grep -c exits 1 (while still printing "0") when there are no matches,
    # so `|| echo 0` used to append a second "0" line on top of grep's own
    # output instead of replacing it -- breaking the -eq check below.
    RECONNECTS_SO_FAR=$(grep -c "forcing reconnect" "$LOG_FILE" 2>/dev/null); RECONNECTS_SO_FAR=${RECONNECTS_SO_FAR:-0}
    echo "   [${ELAPSED}s/${DURATION}s] forced reconnects so far: ${RECONNECTS_SO_FAR}"
done

kill $APP_PID 2>/dev/null || true
wait $APP_PID 2>/dev/null || true

echo ""
echo "=================================================="
RECONNECT_COUNT=$(grep -c "forcing reconnect" "$LOG_FILE" 2>/dev/null); RECONNECT_COUNT=${RECONNECT_COUNT:-0}
UNRECOVERABLE_COUNT=$(grep -c "Unrecoverable frame" "$LOG_FILE" 2>/dev/null); UNRECOVERABLE_COUNT=${UNRECOVERABLE_COUNT:-0}
NEGOTIATED=$(grep -o "negotiated fmt=0x[0-9a-fA-F]*, [0-9]*x[0-9]*@[0-9]*" "$LOG_FILE" | tail -1)
ACTUAL_SIZES=$(grep -oE 'size=[0-9]+x[0-9]+' "$LOG_FILE" | sort | uniq -c | sort -rn | head -5)

echo " Negotiated: ${NEGOTIATED:-unknown}"
echo " Actual decoded frame sizes seen (top 5):"
echo "${ACTUAL_SIZES}"
echo ""
echo " Forced reconnects during ${DURATION}s watch: ${RECONNECT_COUNT}"
echo " 'Unrecoverable frame' events:                ${UNRECOVERABLE_COUNT}"
echo ""
if [ "$RECONNECT_COUNT" -eq 0 ]; then
    echo " ✅ TEST PASSED — stream stayed up for ${DURATION}s with zero forced reconnects"
    exit 0
else
    echo " ❌ TEST FAILED — ${RECONNECT_COUNT} forced reconnect(s) during the watch window"
    echo ""
    echo " Reconnect timeline:"
    grep "forcing reconnect" "$LOG_FILE" | tail -20
    exit 1
fi

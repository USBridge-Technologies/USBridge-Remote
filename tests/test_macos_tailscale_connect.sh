#!/bin/bash
# Test Tailscale connection from macOS using a deep link.
# Usage: ./tests/test_macos_tailscale_connect.sh [internal_host] [tailscale_host] [master_key]
#
# Requires system Tailscale (Homebrew) to be logged in on this machine.

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

INTERNAL_HOST="${1:-192.168.1.110}"
TAILSCALE_HOST="${2:-100.71.26.121}"
MASTER_KEY="${3:-YOUR_QUIC_TOKEN}"
TIMEOUT="${4:-30}"

DEEPLINK="usbridge://connect?internal_host=${INTERNAL_HOST}&tailscale_host=${TAILSCALE_HOST}&master_key=${MASTER_KEY}&protocol=tailscale&immediate=true"

echo "=================================================="
echo " macOS Tailscale Connection Test"
echo "=================================================="
echo " Internal: $INTERNAL_HOST"
echo " Tailscale: $TAILSCALE_HOST"
echo " Deeplink: usbridge://connect?internal_host=${INTERNAL_HOST}&tailscale_host=${TAILSCALE_HOST}&master_key=****&protocol=tailscale&immediate=true"
echo "=================================================="

# 1. Verify system Tailscale is running
echo ""
echo "=> [1/4] Checking system Tailscale..."
TS_STATE=$(/opt/homebrew/bin/tailscale status --json 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('BackendState',''))" 2>/dev/null || echo "")
if [ "$TS_STATE" != "Running" ]; then
    echo "   ❌ System Tailscale is not Running (state: ${TS_STATE:-not found})"
    echo "   Run: tailscale up"
    exit 1
fi
echo "   ✅ System Tailscale: Running"

# 2. Verify Tailscale IP is reachable
echo ""
echo "=> [2/4] Verifying Tailscale host is reachable..."
if curl -sf --connect-timeout 5 "http://${TAILSCALE_HOST}:8080/api/healthz" > /dev/null; then
    echo "   ✅ Bridge at ${TAILSCALE_HOST}:8080 is reachable via Tailscale"
else
    echo "   ❌ Bridge at ${TAILSCALE_HOST}:8080 is NOT reachable — aborting"
    exit 1
fi

# 3. Build
echo ""
echo "=> [3/4] Building macOS app..."
"$SCRIPTS_DIR/build_macos.sh"

APP_BINARY="$REPO_ROOT/dist/macos/USBridgeClient.app/Contents/MacOS/USBridgeClient"
LOG_FILE="$HOME/Library/Logs/USBridgeClient/app.log"

if [ ! -f "$APP_BINARY" ]; then
    echo "   ❌ Binary not found at $APP_BINARY"
    exit 1
fi
echo "   ✅ Built: $APP_BINARY"

# Kill any existing instance and truncate log
pkill -f "USBridgeClient" 2>/dev/null || true
sleep 0.5
mkdir -p "$(dirname "$LOG_FILE")"
: > "$LOG_FILE"

echo ""
echo "=> [4/4] Launching app with Tailscale deeplink (timeout: ${TIMEOUT}s)..."
"$APP_BINARY" "$DEEPLINK" &
APP_PID=$!
echo "   PID: $APP_PID"

echo ""
echo " Monitoring logs..."
CONNECTED=false
FAILED=false
ELAPSED=0

while [ $ELAPSED -lt $TIMEOUT ]; do
    sleep 1
    ELAPSED=$((ELAPSED + 1))

    if ! kill -0 $APP_PID 2>/dev/null; then
        echo "   ⚠️  App exited unexpectedly"
        break
    fi

    if grep -q "✅ Connected to USBridge via tailscale" "$LOG_FILE" 2>/dev/null; then
        CONNECTED=true
        break
    fi

    if grep -q "Connection failed:" "$LOG_FILE" 2>/dev/null; then
        FAILED=true
        break
    fi
done

echo ""
echo "=================================================="
if $CONNECTED; then
    echo " ✅ TEST PASSED — Connected successfully via Tailscale"
    echo ""
    echo " Relevant log lines:"
    grep -E "CONNECT|SYNC|Connected|Tailscale|tailscale" "$LOG_FILE" 2>/dev/null | head -20
elif $FAILED; then
    echo " ❌ TEST FAILED — Connection error detected"
    echo ""
    echo " Error:"
    grep "Connection failed:" "$LOG_FILE" 2>/dev/null | tail -5
    echo ""
    echo " Full connection log:"
    grep -E "CONNECT|SYNC|Connected|tailscale|Tailscale|no route|refused" "$LOG_FILE" 2>/dev/null | head -30
else
    echo " ⚠️  TEST INCONCLUSIVE — No result within ${TIMEOUT}s"
    echo ""
    echo " Last log lines:"
    tail -20 "$LOG_FILE" 2>/dev/null
fi
echo "=================================================="

kill $APP_PID 2>/dev/null || true
wait $APP_PID 2>/dev/null || true

$CONNECTED

#!/bin/bash
# Test direct (LAN) connection from macOS using a deep link.
# Usage: ./tests/test_macos_direct_connect.sh [host] [master_key]
#
# The app handles usbridge:// links via CLI arguments (deeplink_desktop.go).
# immediate=true skips the confirmation dialog and connects automatically.

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

HOST="${1:-192.168.1.110}"
MASTER_KEY="${2:?master_key required (see USBridge Master QR Sync)}"
PROTOCOL="${3:-direct}"
TIMEOUT="${4:-20}"

DEEPLINK="usbridge://connect?internal_host=${HOST}&master_key=${MASTER_KEY}&protocol=${PROTOCOL}&immediate=true"

echo "=================================================="
echo " macOS Direct Connection Test"
echo "=================================================="
echo " Host:     $HOST"
echo " Protocol: $PROTOCOL"
echo " Deeplink: usbridge://connect?internal_host=${HOST}&master_key=****&protocol=${PROTOCOL}&immediate=true"
echo "=================================================="

# 1. Verify server is reachable before building
echo ""
echo "=> [1/4] Verifying server is reachable..."
if curl -sf --connect-timeout 3 "http://${HOST}:8080/api/healthz" > /dev/null; then
    echo "   ✅ Server at ${HOST}:8080 is reachable"
else
    echo "   ❌ Server at ${HOST}:8080 is NOT reachable — aborting"
    exit 1
fi

# 2. Build
echo ""
echo "=> [2/4] Building macOS app..."
"$SCRIPTS_DIR/build_macos.sh"

APP_BINARY="$REPO_ROOT/dist/macos/USBridgeClient.app/Contents/MacOS/USBridgeClient"
LOG_FILE="$HOME/Library/Logs/USBridgeClient/app.log"

if [ ! -f "$APP_BINARY" ]; then
    echo "   ❌ Binary not found at $APP_BINARY"
    exit 1
fi
echo "   ✅ Built: $APP_BINARY"

# 3. Kill any existing instance
pkill -f "USBridgeClient" 2>/dev/null || true
sleep 0.5

# 4. Truncate log so we only see this run's output
mkdir -p "$(dirname "$LOG_FILE")"
: > "$LOG_FILE"

echo ""
echo "=> [3/4] Launching app with deeplink (timeout: ${TIMEOUT}s)..."
"$APP_BINARY" "$DEEPLINK" &
APP_PID=$!
echo "   PID: $APP_PID"

# 5. Wait for connection result
echo ""
echo "=> [4/4] Monitoring logs for connection result..."
echo "   (waiting up to ${TIMEOUT}s)"

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

    if grep -q "✅ Connected to USBridge via direct" "$LOG_FILE" 2>/dev/null; then
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
    echo " ✅ TEST PASSED — Connected successfully via direct"
    echo ""
    echo " Relevant log lines:"
    grep -E "CONNECT|SYNC|Connected|healthz|no route" "$LOG_FILE" 2>/dev/null | head -20
elif $FAILED; then
    echo " ❌ TEST FAILED — Connection error detected"
    echo ""
    echo " Error:"
    grep "Connection failed:" "$LOG_FILE" 2>/dev/null | tail -5
    echo ""
    echo " Full connection log:"
    grep -E "CONNECT|SYNC|Connected|healthz|no route|route to host" "$LOG_FILE" 2>/dev/null | head -30
else
    echo " ⚠️  TEST INCONCLUSIVE — No result within ${TIMEOUT}s"
    echo ""
    echo " Last log lines:"
    tail -20 "$LOG_FILE" 2>/dev/null
fi
echo "=================================================="

# Kill the app
kill $APP_PID 2>/dev/null || true
wait $APP_PID 2>/dev/null || true

$CONNECTED

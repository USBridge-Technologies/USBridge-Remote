#!/bin/bash
# Tests all 4 connection mode combinations on macOS:
#   1. system Tailscale + direct protocol
#   2. system Tailscale + tailscale protocol
#   3. userspace tsnet   + direct protocol
#   4. userspace tsnet   + tailscale protocol
#
# Usage: ./tests/test_macos_all_modes.sh [internal_host] [tailscale_host] [master_key]
#
# For tests 3 & 4 the script temporarily runs `tailscale down` so tsnet can start
# without the dual-WireGuard routing conflict, then restores `tailscale up`.

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/scripts"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

INTERNAL_HOST="${1:-192.168.1.110}"
TAILSCALE_HOST="${2:-100.71.26.121}"
MASTER_KEY="${3:-YOUR_QUIC_TOKEN}"
TIMEOUT="${4:-30}"
APP_BINARY="$REPO_ROOT/dist/macos/USBridgeClient.app/Contents/MacOS/USBridgeClient"
LOG_FILE="$HOME/Library/Logs/USBridgeClient/app.log"
TS_BIN="/opt/homebrew/bin/tailscale"

pass=0
fail=0
results=()

# ── helpers ──────────────────────────────────────────────────────────────────

ts_state() { "$TS_BIN" status --json 2>/dev/null | python3 -c "import sys,json;print(json.load(sys.stdin).get('BackendState',''))" 2>/dev/null || echo ""; }

run_test() {
    local label="$1"
    local deeplink="$2"
    local success_pattern="$3"

    echo ""
    echo "──────────────────────────────────────────────────"
    echo " TEST: $label"
    echo " deeplink: $(echo "$deeplink" | sed 's/master_key=[^&]*/master_key=****/g')"
    echo "──────────────────────────────────────────────────"

    pkill -f "USBridgeClient" 2>/dev/null || true
    sleep 0.5
    mkdir -p "$(dirname "$LOG_FILE")"
    : > "$LOG_FILE"

    "$APP_BINARY" "$deeplink" &
    local pid=$!

    local connected=false
    local elapsed=0
    while [ $elapsed -lt $TIMEOUT ]; do
        sleep 1
        elapsed=$((elapsed + 1))
        if ! kill -0 $pid 2>/dev/null; then
            echo "   ⚠️  app exited unexpectedly"
            break
        fi
        if grep -q "$success_pattern" "$LOG_FILE" 2>/dev/null; then
            connected=true
            break
        fi
        if grep -q "Connection failed:" "$LOG_FILE" 2>/dev/null; then
            break
        fi
    done

    kill $pid 2>/dev/null || true
    wait $pid 2>/dev/null || true

    if $connected; then
        echo " ✅ PASS"
        results+=("✅ $label")
        pass=$((pass + 1))
        # Show key lines
        grep -E "Mode changed|CONNECT.*start|Connected to USBridge via|Starting tsnet" "$LOG_FILE" 2>/dev/null | head -8
    else
        echo " ❌ FAIL"
        results+=("❌ $label")
        fail=$((fail + 1))
        grep -E "Connection failed|no route|refused|Mode changed|Starting tsnet" "$LOG_FILE" 2>/dev/null | head -10
    fi
}

# ── preflight ────────────────────────────────────────────────────────────────

echo "=================================================="
echo " macOS All-Modes Connection Test"
echo "=================================================="
echo " Internal: $INTERNAL_HOST"
echo " Tailscale: $TAILSCALE_HOST"
echo "=================================================="

# Verify bridge is reachable
if ! curl -sf --connect-timeout 3 "http://${INTERNAL_HOST}:8080/api/healthz" > /dev/null; then
    echo "❌ Bridge ${INTERNAL_HOST}:8080 not reachable — aborting"
    exit 1
fi
echo "✅ Bridge reachable at ${INTERNAL_HOST}:8080"

# Verify system Tailscale
if [ "$(ts_state)" != "Running" ]; then
    echo "❌ System Tailscale not Running — aborting (run: tailscale up)"
    exit 1
fi
echo "✅ System Tailscale: Running"

if ! curl -sf --connect-timeout 5 "http://${TAILSCALE_HOST}:8080/api/healthz" > /dev/null; then
    echo "❌ Bridge ${TAILSCALE_HOST}:8080 not reachable via Tailscale — aborting"
    exit 1
fi
echo "✅ Bridge reachable at ${TAILSCALE_HOST}:8080 via Tailscale"

# Build once
echo ""
echo "=> Building macOS app..."
"$SCRIPTS_DIR/build_macos.sh"
if [ ! -f "$APP_BINARY" ]; then echo "❌ Binary not found at $APP_BINARY"; exit 1; fi
echo "✅ Built: $APP_BINARY"

# ── tests 1 & 2: system Tailscale (kernel) mode ──────────────────────────────

echo ""
echo "=== GROUP 1: system Tailscale (kernel) mode ==="

run_test "system-TS + direct" \
    "usbridge://connect?internal_host=${INTERNAL_HOST}&master_key=${MASTER_KEY}&protocol=direct&ts_mode=kernel&immediate=true" \
    "Connected to USBridge via direct"

run_test "system-TS + tailscale" \
    "usbridge://connect?internal_host=${INTERNAL_HOST}&tailscale_host=${TAILSCALE_HOST}&master_key=${MASTER_KEY}&protocol=tailscale&ts_mode=kernel&immediate=true" \
    "Connected to USBridge via tailscale"

# ── tests 3 & 4: userspace tsnet mode ────────────────────────────────────────
# tsnet is purely userspace — it does NOT modify the kernel routing table.
# Direct LAN connections (192.168.1.x) go via en0 regardless of whether
# tsnet or system Tailscale is running. No need to stop system Tailscale.

echo ""
echo "=== GROUP 2: userspace tsnet mode (system Tailscale stays running) ==="

run_test "tsnet + direct" \
    "usbridge://connect?internal_host=${INTERNAL_HOST}&master_key=${MASTER_KEY}&protocol=direct&ts_mode=userspace&immediate=true" \
    "Connected to USBridge via direct"

# tsnet + tailscale requires tsnet to already be authenticated (state saved in
# ~/Library/Application Support/usbridge-client/tailscale/). If tsnet shows
# NeedsLogin (AuthURL logged), the test is SKIPPED — authorize via browser first.
echo ""
echo "──────────────────────────────────────────────────"
echo " TEST: tsnet + tailscale"
DEEPLINK="usbridge://connect?internal_host=${INTERNAL_HOST}&tailscale_host=${TAILSCALE_HOST}&master_key=${MASTER_KEY}&protocol=tailscale&ts_mode=userspace&immediate=true"
echo " deeplink: $(echo "$DEEPLINK" | sed 's/master_key=[^&]*/master_key=****/g')"
echo "──────────────────────────────────────────────────"
pkill -f "USBridgeClient" 2>/dev/null || true; sleep 0.5
mkdir -p "$(dirname "$LOG_FILE")"; : > "$LOG_FILE"
"$APP_BINARY" "$DEEPLINK" &
_pid=$!
sleep 5
_elapsed=0; _done=false
while [ $_elapsed -lt 25 ]; do
    sleep 1; _elapsed=$((_elapsed + 1))
    if grep -q "New AuthURL\|NeedsLogin" "$LOG_FILE" 2>/dev/null; then _done=true; break; fi
    if grep -q "Connected to USBridge via tailscale" "$LOG_FILE" 2>/dev/null; then _done=true; break; fi
    if grep -q "Connection failed:" "$LOG_FILE" 2>/dev/null; then _done=true; break; fi
done
kill $_pid 2>/dev/null; wait $_pid 2>/dev/null

if grep -q "Connected to USBridge via tailscale" "$LOG_FILE" 2>/dev/null; then
    echo " ✅ PASS"
    results+=("✅ tsnet + tailscale")
    pass=$((pass + 1))
elif grep -q "New AuthURL\|NeedsLogin" "$LOG_FILE" 2>/dev/null; then
    AUTH_URL=$(grep "New AuthURL" "$LOG_FILE" 2>/dev/null | tail -1 | grep -o 'https://[^ "]*' || echo "see log")
    echo " ⚠️  SKIPPED — tsnet needs first-time login"
    echo "     Run the app once manually to authorize tsnet, then re-run"
    echo "     Auth URL: $AUTH_URL"
    results+=("⚠️  tsnet + tailscale (skipped: tsnet needs login)")
else
    echo " ❌ FAIL"
    results+=("❌ tsnet + tailscale")
    fail=$((fail + 1))
    grep -E "Connection failed|no route|refused" "$LOG_FILE" 2>/dev/null | head -5
fi

# After tsnet tests, reset system Tailscale to clear any coordination state
# left by the rapid tsnet start/kill cycles.
echo ""
echo "=> Resetting system Tailscale state after tsnet tests..."
"$TS_BIN" down 2>/dev/null && sleep 1 && "$TS_BIN" up --accept-dns=false 2>/dev/null
sleep 2
echo "   System Tailscale state: $(ts_state)"

# ── summary ──────────────────────────────────────────────────────────────────

echo ""
echo "=================================================="
echo " RESULTS ($pass passed, $fail failed)"
echo "=================================================="
for r in "${results[@]}"; do
    echo "  $r"
done
echo "=================================================="

[ $fail -eq 0 ]

#!/usr/bin/env bash
# Full local end-to-end test of the iSCSI disk-streaming pipeline: the
# REAL production client (client/internal/service/iscsi_target.go, an iSCSI
# target) talking to the REAL production agent initiator
# (agent/internal/iscsi, exactly what App.ReplaceDevices/ClearDevices call).
# No physical USBridge/yocto-rz3w hardware needed — see the plan this
# implements at /home/amir/.claude/plans/temporal-foraging-mountain.md.
#
# The agent side runs inside a `--privileged --net=host` Docker container
# (needs open-iscsi's iscsiadm + a working iscsid, i.e. root/CAP_NET_ADMIN)
# rather than touching the host's real iSCSI initiator state.
#
# Usage: ./client/tests/test_iscsi_e2e_docker.sh
#
# Requires: docker (with a privileged-container-capable daemon), go.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CLIENT_DIR="$REPO_ROOT/client"
AGENT_DIR="$REPO_ROOT/agent"
WORKDIR="$(mktemp -d /tmp/iscsi-e2e.XXXXXX)"
IMG="$WORKDIR/testdisk.img"
IQN="iqn.2026-01.com.usbridge.e2e:$$"
PORT=3267
TARGET_BIN="$WORKDIR/iscsi_target_harness"
INITIATOR_BIN="$WORKDIR/iscsi_initiator_harness"
TARGET_LOG="$WORKDIR/target.log"
TARGET_PID=""

cleanup() {
  set +e
  [[ -n "$TARGET_PID" ]] && kill "$TARGET_PID" >/dev/null 2>&1
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

if ! command -v docker &>/dev/null; then
  echo "Error: docker not found."
  exit 1
fi

echo "--- Building client iSCSI target harness (production IscsiTargetRunner) ---"
( cd "$CLIENT_DIR" && go build -o "$TARGET_BIN" ./tools/iscsi_target_harness )

echo "--- Building agent iSCSI initiator harness (production iscsi.Initiator) ---"
( cd "$AGENT_DIR" && go build -o "$INITIATOR_BIN" ./tools/iscsi_initiator_harness )

echo "--- Generating test image (8 MiB random data) ---"
dd if=/dev/urandom of="$IMG" bs=1M count=8 status=none
EXPECTED_SHA=$(sha256sum "$IMG" | cut -d' ' -f1)
echo "Expected sha256: $EXPECTED_SHA"

echo "--- Starting client iSCSI target ($IQN on 127.0.0.1:$PORT) ---"
"$TARGET_BIN" "$IMG" "$IQN" "127.0.0.1:$PORT" >"$TARGET_LOG" 2>&1 &
TARGET_PID=$!
for i in $(seq 1 20); do
  ss -tln 2>/dev/null | grep -q ":$PORT " && break
  sleep 0.2
done
if ! ss -tln 2>/dev/null | grep -q ":$PORT "; then
  echo "Target failed to start, log:"; cat "$TARGET_LOG"; exit 1
fi

echo "--- Logging in from the agent's real iSCSI initiator (in a privileged container) ---"
docker run --rm --privileged --net=host -v "$WORKDIR:/e2e" -v /dev:/dev -w /e2e debian:stable-slim sh -c "
set -e
apt-get update -qq >/dev/null 2>&1 && apt-get install -y -qq open-iscsi procps coreutils >/dev/null 2>&1
./$(basename "$INITIATOR_BIN") 127.0.0.1:$PORT '$IQN' > initiator.log 2>&1 &
HPID=\$!
for i in \$(seq 1 30); do
  grep -q LOGGED_IN initiator.log 2>/dev/null && break
  sleep 0.3
done
cat initiator.log
if ! grep -q LOGGED_IN initiator.log; then
  echo 'FAIL: agent never logged in'
  kill -INT \$HPID 2>/dev/null || true
  exit 1
fi
DEV=\$(grep -oP 'device=\K\S+' initiator.log)
echo \"Device: \$DEV\"
ACTUAL=\$(dd if=\"\$DEV\" bs=1M count=8 status=none | sha256sum | cut -d' ' -f1)
echo \"Expected: $EXPECTED_SHA\"
echo \"Actual:   \$ACTUAL\"
kill -INT \$HPID
sleep 1
cat initiator.log
if [ \"\$ACTUAL\" != \"$EXPECTED_SHA\" ]; then
  echo 'FAIL: checksum mismatch'
  exit 1
fi
"

echo ""
echo "PASS: full client(iSCSI target) <-> agent(iSCSI initiator) round trip, data verified byte-for-byte."

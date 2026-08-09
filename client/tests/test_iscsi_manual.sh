#!/usr/bin/env bash
# Phase-0 spike validation: prove the client can act as a real iSCSI target
# (github.com/gostor/gotgt) that a standards-compliant Linux initiator
# (open-iscsi's iscsiadm) can discover, log into, read from, and log out of.
#
# Fully automated, self-cleaning. Only the iscsiadm steps need root (the
# kernel iSCSI initiator requires it) — everything else runs as your user.
#
# Usage: ./client/tests/test_iscsi_manual.sh
#
# Optional: grant passwordless sudo for iscsiadm only, so this script (and
# future test runs) don't prompt for a password each time:
#   echo "$USER ALL=(root) NOPASSWD: /usr/sbin/iscsiadm" | sudo tee /etc/sudoers.d/iscsiadm-test
#   sudo chmod 0440 /etc/sudoers.d/iscsiadm-test
# (Scoped to the iscsiadm binary only — not full passwordless sudo. Remove
# the file with `sudo rm /etc/sudoers.d/iscsiadm-test` when done testing.)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CLIENT_DIR="$REPO_ROOT/client"
WORKDIR="$(mktemp -d /tmp/iscsi-spike.XXXXXX)"
IMG="$WORKDIR/testdisk.img"
IQN="iqn.2026-01.com.usbridge.spike:$$"
PORTAL="127.0.0.1:3260"
SPIKE_BIN="$WORKDIR/iscsi_spike"
SPIKE_LOG="$WORKDIR/target.log"
TARGET_PID=""

cleanup() {
  set +e
  if [[ -n "$TARGET_PID" ]]; then
    sudo iscsiadm -m node -T "$IQN" -p "$PORTAL" --logout >/dev/null 2>&1
    sudo iscsiadm -m node -T "$IQN" -p "$PORTAL" -o delete >/dev/null 2>&1
    kill "$TARGET_PID" >/dev/null 2>&1
    wait "$TARGET_PID" 2>/dev/null
  fi
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

if ! command -v iscsiadm &>/dev/null; then
  echo "Error: iscsiadm not found. Install open-iscsi (apt install open-iscsi)."
  exit 1
fi

echo "--- Building throwaway gotgt spike target ---"
( cd "$CLIENT_DIR" && go build -o "$SPIKE_BIN" ./tools/iscsi_spike )

echo "--- Generating test image (8 MiB random data) ---"
dd if=/dev/urandom of="$IMG" bs=1M count=8 status=none
EXPECTED_SHA=$(sha256sum "$IMG" | cut -d' ' -f1)
echo "Expected sha256: $EXPECTED_SHA"

echo "--- Starting gotgt target ($IQN on $PORTAL) ---"
"$SPIKE_BIN" "$IMG" "$IQN" "$PORTAL" >"$SPIKE_LOG" 2>&1 &
TARGET_PID=$!
for i in $(seq 1 20); do
  ss -tln 2>/dev/null | grep -q ':3260 ' && break
  sleep 0.2
done
if ! ss -tln 2>/dev/null | grep -q ':3260 '; then
  echo "Target failed to start, log:"; cat "$SPIKE_LOG"; exit 1
fi

echo "--- Discovery (sudo) ---"
sudo iscsiadm -m discovery -t sendtargets -p "$PORTAL"

echo "--- Login (sudo) ---"
sudo iscsiadm -m node -T "$IQN" -p "$PORTAL" --login

echo "--- Waiting for block device to appear ---"
DEV_LINK="/dev/disk/by-path/ip-$PORTAL-iscsi-$IQN-lun-0"
for i in $(seq 1 30); do
  [[ -e "$DEV_LINK" ]] && break
  sleep 0.2
done
if [[ ! -e "$DEV_LINK" ]]; then
  echo "Error: $DEV_LINK never appeared after login"
  exit 1
fi
DEV=$(readlink -f "$DEV_LINK")
echo "Block device: $DEV -> $DEV_LINK"

echo "--- Reading back data and checksumming (read-only) ---"
if [[ ! -r "$DEV" ]]; then
  echo "$DEV not readable by $USER directly; add yourself to the 'disk' group or extend sudo scope."
  exit 1
fi
ACTUAL_SHA=$(dd if="$DEV" bs=1M count=8 status=none | sha256sum | cut -d' ' -f1)
echo "Actual sha256:   $ACTUAL_SHA"

if [[ "$EXPECTED_SHA" == "$ACTUAL_SHA" ]]; then
  echo ""
  echo "PASS: data read back through the iSCSI target matches the source file."
else
  echo ""
  echo "FAIL: checksum mismatch."
  exit 1
fi

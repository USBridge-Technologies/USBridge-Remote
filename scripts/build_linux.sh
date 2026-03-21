#!/bin/bash
# Build USBridgeClient for Linux
# Output: dist/linux/USBridgeClient.bin (+ config.yaml if present)

set -euo pipefail

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPTS_DIR/.." && pwd)"
cd "$REPO_ROOT"

if [ -z "${USBRIDGE_LOGGING_ACTIVE:-}" ]; then
  export USBRIDGE_LOGGING_ACTIVE=1
  LOG_DIR="$REPO_ROOT/logs"
  mkdir -p "$LOG_DIR"
  LOG_FILE="$LOG_DIR/$(basename "$0" .sh).log"
  exec > >(tee -a "$LOG_FILE") 2>&1
  echo "=== $(date '+%Y-%m-%d %H:%M:%S') [$0] ==="
fi

OUT_DIR="$REPO_ROOT/dist/linux"
mkdir -p "$OUT_DIR"

# Suppress format-security warnings from the go-gst dependency (gst_debug.go)
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"

go build -o "$OUT_DIR/USBridgeClient.bin" ./cmd

[ -f "$REPO_ROOT/config.yaml" ] && cp -f "$REPO_ROOT/config.yaml" "$OUT_DIR/"

echo "✅ Done: $OUT_DIR/USBridgeClient.bin"

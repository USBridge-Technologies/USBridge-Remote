#!/bin/bash
# Single entry point for the Android build → dist/android
#
# Usage:
#   ./scripts/build_android.sh

set -euo pipefail

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# build_moonlight.sh's Android cross-compile block (opus, moonlight-common-c)
# needs ANDROID_NDK_HOME set to run; build_android_gradle.sh normally
# resolves it via export_android_env, but that happens too late for
# build_moonlight.sh below. Resolve it here first so opus actually gets
# built instead of silently skipping and failing later at the Go cgo step.
source "$SCRIPTS_DIR/android_env.sh"
export_android_env

echo "=> Building Moonlight Core (Android only, host build skipped)..."
MOONLIGHT_ANDROID_TARGET=1 MOONLIGHT_SKIP_HOST=1 "$SCRIPTS_DIR/build_moonlight.sh" || {
    echo "❌ Failed to build Moonlight Core"; exit 1
}

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

exec "$SCRIPTS_DIR/build_android_gradle.sh"

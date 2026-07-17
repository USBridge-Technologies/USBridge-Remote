#!/bin/bash
# Single entry point for the Android build → dist/android
#
# Usage:
#   ./scripts/build_android.sh              # Moonlight-only (GStreamer not needed)
#   ./scripts/build_android.sh --gstreamer  # + GStreamer (needs gstreamer-android-dynamic)
#   WITH_GSTREAMER=1 ./scripts/build_android.sh  # same, via env variable

set -euo pipefail

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Parse flags
WITH_GSTREAMER="${WITH_GSTREAMER:-0}"
for arg in "$@"; do
    case "$arg" in
        --gstreamer)    WITH_GSTREAMER=1 ;;
        --no-gstreamer) WITH_GSTREAMER=0 ;;
    esac
done
export WITH_GSTREAMER

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

if [ "$WITH_GSTREAMER" = "1" ]; then
    echo "🎬 GStreamer: enabled (requires gstreamer-android-dynamic/)"
else
    echo "🎬 GStreamer: disabled — Moonlight-only build. To enable: --gstreamer"
fi

exec "$SCRIPTS_DIR/build_android_gradle.sh"

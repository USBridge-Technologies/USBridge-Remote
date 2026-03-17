#!/bin/bash
# Сборка артефактов iOS в dist/ios
#
# Важно: iOS сборка требует Apple SDK (Xcode), обычно выполняется на macOS.
# На Linux этот скрипт покажет понятную ошибку.

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

LEGACY_DIR="$REPO_ROOT/old/linux_crosscompile"

if [ -x "$LEGACY_DIR/build_ios.sh" ]; then
  exec "$LEGACY_DIR/build_ios.sh"
fi

echo "❌ Не найден iOS build-скрипт: $LEGACY_DIR/build_ios.sh"
echo "   Ожидается, что iOS сборка лежит в old/linux_crosscompile/."
exit 1

#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DIST_DIR="$REPO_ROOT/dist/macos"
APP_NAME="USBridgeAgent.app"
SOURCE_APP="$DIST_DIR/$APP_NAME"
TARGET_DIR="${USBRIDGE_APP_INSTALL_DIR:-$HOME/Applications}"
TARGET_APP="$TARGET_DIR/$APP_NAME"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo -e "${RED}Этот скрипт должен запускаться на macOS${NC}"
    exit 1
fi

if [ ! -d "$SOURCE_APP" ]; then
    echo -e "${RED}Не найден app bundle: $SOURCE_APP${NC}"
    echo "Сначала выполните: ./scripts/build_macos.sh"
    exit 1
fi

mkdir -p "$TARGET_DIR"
rm -rf "$TARGET_APP"
ditto "$SOURCE_APP" "$TARGET_APP"

echo -e "${GREEN}Installed:${NC} $TARGET_APP"
echo -e "${YELLOW}Open with:${NC} open \"$TARGET_APP\""
echo -e "${YELLOW}Important:${NC} выдавайте macOS permissions именно этой копии приложения."

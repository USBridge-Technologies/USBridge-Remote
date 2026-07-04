#!/bin/bash
# Build USBridgeClient for iOS as a signed .ipa for App Store Connect / TestFlight.
# Requires: Xcode, Go, fyne CLI.

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

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

APP_NAME="USBridge Client"
APP_ID="io.usbridge.client"
TEAM_ID="AJVY97F5QT"
PROVISIONING_PROFILE="USBridge Client iOS Distribution"
SIGN_CERT="Apple Distribution: Amir Fatkulin ($TEAM_ID)"

DIST_DIR="$REPO_ROOT/dist/ios"
IPA_NAME="USBridgeClient.ipa"

echo -e "${GREEN}📱 Building USBridgeClient for iOS${NC}"

# 1. Check dependencies
echo -e "\n${YELLOW}🔍 Проверка зависимостей...${NC}"
if ! command -v xcodebuild >/dev/null 2>&1; then
    echo -e "${RED}❌ xcodebuild не найден — установите Xcode${NC}"; exit 1
fi
echo -e "   ${GREEN}✓${NC} xcodebuild $(xcodebuild -version 2>/dev/null | head -1)"

FYNE_BIN="$(go env GOPATH)/bin/fyne"
if [ ! -x "$FYNE_BIN" ]; then
    echo -e "${YELLOW}⚠${NC}  fyne не найден — устанавливаю..."
    go install fyne.io/fyne/v2/cmd/fyne@latest
fi
echo -e "   ${GREEN}✓${NC} fyne: $FYNE_BIN"

# 2. Build Moonlight Core (iOS + skip host)
echo -e "\n${YELLOW}📦 Сборка Moonlight Core (iOS)...${NC}"
MOONLIGHT_IOS_TARGET=1 MOONLIGHT_SKIP_HOST=1 \
    "$SCRIPTS_DIR/build_moonlight.sh" || { echo -e "${RED}❌ Moonlight Core build failed${NC}"; exit 1; }

# 3. fyne package → produces signed .app bundle
echo -e "\n${YELLOW}🔨 fyne package --target ios...${NC}"
# Remove previously generated .app
find "$REPO_ROOT" -maxdepth 1 -name "*.app" -exec rm -rf {} + 2>/dev/null || true

# Run fyne from cmd/ so it builds the correct main package (not repo root)
cd "$REPO_ROOT/cmd"
"$FYNE_BIN" package \
    --target ios \
    --app-id "$APP_ID" \
    --name "$APP_NAME" \
    --icon "$REPO_ROOT/Icon.png" \
    --certificate "$SIGN_CERT" \
    --profile "$PROVISIONING_PROFILE" \
    --release
cd "$REPO_ROOT"

# Find the generated .app
APP_BUNDLE=$(find "$REPO_ROOT/cmd" -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
if [ -z "$APP_BUNDLE" ]; then
    APP_BUNDLE=$(find "$REPO_ROOT" -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
fi
if [ -z "$APP_BUNDLE" ]; then
    echo -e "${RED}❌ .app не найден после fyne package${NC}"; exit 1
fi
echo -e "${GREEN}✓${NC} App bundle: $APP_BUNDLE"

# 4. Package .app → .ipa  (Payload/<App>.app zipped)
echo -e "\n${YELLOW}📦 Упаковка в .ipa...${NC}"
mkdir -p "$DIST_DIR"
PAYLOAD_DIR="$(mktemp -d)/Payload"
mkdir -p "$PAYLOAD_DIR"
cp -R "$APP_BUNDLE" "$PAYLOAD_DIR/"
IPA_PATH="$DIST_DIR/$IPA_NAME"
(cd "$(dirname "$PAYLOAD_DIR")" && zip -qr "$IPA_PATH" Payload)
rm -rf "$(dirname "$PAYLOAD_DIR")"
echo -e "${GREEN}✓${NC} IPA: $IPA_PATH"

echo -e "\n${GREEN}✅ iOS сборка завершена!${NC}"
echo -e "   IPA:  $IPA_PATH"
echo -e "   Лог:  ${LOG_FILE:-stdout}"
echo -e ""
echo -e "   Загрузка в App Store Connect (через Transporter или altool):"
echo -e "   xcrun altool --upload-app -f \"$IPA_PATH\" -t ios -u fatkulinamir80@gmail.com"

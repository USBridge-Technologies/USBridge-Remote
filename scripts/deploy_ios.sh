#!/bin/bash
# Build USBridgeClient for iOS and install directly on a connected iPhone.
# Requires: Xcode 15+, Go, fyne CLI.
# Requires in Apple Developer portal:
#   - Apple Development certificate
#   - Development provisioning profile with device UDID registered

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
DEV_CERT="Apple Development: Amir Fatkulin (84D7LQHALZ)"
DEV_PROFILE="USBridge Client iOS Development"

DIST_DIR="$REPO_ROOT/dist/ios-dev"

echo -e "${GREEN}📱 Deploy USBridgeClient → iPhone${NC}"

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

if ! command -v ios-deploy >/dev/null 2>&1; then
    echo -e "${YELLOW}⚠${NC}  ios-deploy не найден — устанавливаю..."
    brew install ios-deploy
fi
echo -e "   ${GREEN}✓${NC} ios-deploy: $(ios-deploy --version 2>/dev/null)"

# Check Development certificate
if ! security find-identity -v -p codesigning 2>/dev/null | grep -q "Apple Development:"; then
    echo -e "${RED}❌ Apple Development сертификат не найден в Keychain.${NC}"
    echo "   Создайте его в Apple Developer → Certificates → + → Apple Development"
    echo "   И создайте Development Provisioning Profile с UDID вашего iPhone."
    exit 1
fi
echo -e "   ${GREEN}✓${NC} Development certificate найден"

# 2. Check connected device
echo -e "\n${YELLOW}📱 Поиск подключённого iPhone...${NC}"
DEVICE_ID=""
DEVICE_NAME=""

# xcrun xctrace list devices shows real devices with UDID (works for wired connections)
XCTRACE_OUT=$(xcrun xctrace list devices 2>/dev/null || true)
while IFS= read -r line; do
    # skip Mac, simulators, offline devices
    [[ "$line" == *"MacBook"* ]] && continue
    [[ "$line" == *"Simulator"* ]] && continue
    [[ "$line" == *"Offline"* ]] && continue
    [[ "$line" =~ \(([0-9a-f]{40})\) ]] && {
        DEVICE_ID="${BASH_REMATCH[1]}"
        DEVICE_NAME=$(echo "$line" | sed 's/ ([^)]*) ([^)]*)$//')
        break
    }
done <<< "$XCTRACE_OUT"

if [ -z "$DEVICE_ID" ]; then
    echo -e "${RED}❌ iPhone не найден — подключите по проводу и разблокируйте${NC}"
    echo ""
    echo "   Доступные устройства:"
    xcrun xctrace list devices 2>/dev/null | grep -v Simulator || true
    exit 1
fi
echo -e "   ${GREEN}✓${NC} Устройство: ${DEVICE_NAME} ($DEVICE_ID)"

# 3. Build Moonlight Core
echo -e "\n${YELLOW}📦 Сборка Moonlight Core...${NC}"
"$SCRIPTS_DIR/build_moonlight.sh" || { echo -e "${RED}❌ Moonlight Core build failed${NC}"; exit 1; }

# 4. fyne package (development build — no --release)
echo -e "\n${YELLOW}🔨 fyne package --target ios (development)...${NC}"
find "$REPO_ROOT" -maxdepth 1 -name "*.app" -exec rm -rf {} + 2>/dev/null || true

# Force iOS 16 deployment target so app runs on older devices
export CGO_CFLAGS="${CGO_CFLAGS:-} -target arm64-apple-ios16.0"
export CGO_LDFLAGS="${CGO_LDFLAGS:-} -target arm64-apple-ios16.0"
export IPHONEOS_DEPLOYMENT_TARGET=16.0

"$FYNE_BIN" package \
    --target ios \
    --app-id "$APP_ID" \
    --name "$APP_NAME" \
    --icon "$REPO_ROOT/Icon.png" \
    --certificate "$DEV_CERT" \
    --profile "$DEV_PROFILE"

APP_BUNDLE=$(find "$REPO_ROOT" -maxdepth 1 -name "*.app" 2>/dev/null | head -1)
if [ -z "$APP_BUNDLE" ]; then
    echo -e "${RED}❌ .app не найден после fyne package${NC}"; exit 1
fi
echo -e "${GREEN}✓${NC} App bundle: $APP_BUNDLE"

# 5. Inject LaunchScreen.storyboardc (fyne doesn't generate it, iOS needs it)
echo -e "\n${YELLOW}📋 Компиляция LaunchScreen.storyboard...${NC}"
STORYBOARD_SRC="$(mktemp /tmp/LaunchScreen.XXXXXX.storyboard)"
cat > "$STORYBOARD_SRC" << 'STORYBOARD'
<?xml version="1.0" encoding="UTF-8"?>
<document type="com.apple.InterfaceBuilder3.CocoaTouch.Storyboard.XIB" version="3.0" toolsVersion="20037" targetRuntime="iOS.CocoaTouch" propertyAccessControl="none" useAutolayout="YES" launchScreen="YES" useTraitCollections="YES" useSafeAreas="YES" colorMatched="YES" initialViewController="01J-lp-oVM">
    <dependencies>
        <deployment identifier="iOS"/>
        <plugIn identifier="com.apple.InterfaceBuilder.IBCocoaTouchPlugin" version="20037"/>
    </dependencies>
    <scenes>
        <scene sceneID="EHf-IW-A2E">
            <objects>
                <viewController id="01J-lp-oVM" sceneMemberID="viewController">
                    <view key="view" contentMode="scaleToFill" id="Ze5-6b-2t3">
                        <rect key="frame" x="0.0" y="0.0" width="375" height="667"/>
                        <autoresizingMask key="autoresizingMask" widthSizable="YES" heightSizable="YES"/>
                        <color key="backgroundColor" systemColor="systemBackgroundColor"/>
                    </view>
                </viewController>
                <placeholder placeholderIdentifier="IBFirstResponder" id="iYj-Kq-Ea1" userLabel="First Responder" sceneMemberID="firstResponder"/>
            </objects>
        </scene>
    </scenes>
</document>
STORYBOARD

STORYBOARDC="$APP_BUNDLE/LaunchScreen.storyboardc"
xcrun --sdk iphoneos ibtool --compile "$STORYBOARDC" "$STORYBOARD_SRC" 2>/dev/null \
    && echo -e "   ${GREEN}✓${NC} LaunchScreen.storyboardc добавлен" \
    || echo -e "   ${YELLOW}⚠${NC} ibtool не смог скомпилировать storyboard — продолжаем без него"
rm -f "$STORYBOARD_SRC"

# 6. Re-sign with entitlements from embedded provisioning profile
echo -e "\n${YELLOW}🔏 Переподпись с entitlements...${NC}"
ENTITLEMENTS_PLIST="$(mktemp /tmp/entitlements.XXXXXX.plist)"
security cms -D -i "$APP_BUNDLE/embedded.mobileprovision" > /tmp/provision_decoded.plist 2>/dev/null
/usr/libexec/PlistBuddy -x -c "Print Entitlements" /tmp/provision_decoded.plist > "$ENTITLEMENTS_PLIST" 2>/dev/null
rm -f /tmp/provision_decoded.plist

codesign --force --deep \
    --sign "$DEV_CERT" \
    --entitlements "$ENTITLEMENTS_PLIST" \
    "$APP_BUNDLE" 2>&1 | grep -v "replacing existing" || true
rm -f "$ENTITLEMENTS_PLIST"
echo -e "   ${GREEN}✓${NC} Переподписано"

# 7. Install directly on device via ios-deploy (no IPA needed)
echo -e "\n${YELLOW}📲 Установка на устройство...${NC}"
ios-deploy --bundle "$APP_BUNDLE" --no-wifi --id "$DEVICE_ID" 2>&1

echo -e "\n${GREEN}✅ Готово! Откройте USBridge Client на iPhone.${NC}"

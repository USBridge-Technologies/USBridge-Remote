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
IPA_NAME="USBridgeClient-iOS-$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0").ipa"

echo -e "${GREEN}📱 Building USBridgeClient for iOS${NC}"

# 1. Check dependencies
if ! command -v xcodebuild >/dev/null 2>&1; then
    :
fi
echo -e "   ${GREEN}✓${NC} xcodebuild $(xcodebuild -version 2>/dev/null | head -1)"

FYNE_BIN="$(go env GOPATH)/bin/fyne"
if [ ! -x "$FYNE_BIN" ]; then
    go install fyne.io/fyne/v2/cmd/fyne@latest
fi
echo -e "   ${GREEN}✓${NC} fyne: $FYNE_BIN"

# 2. Build Moonlight Core (iOS + skip host)
MOONLIGHT_IOS_TARGET=1 MOONLIGHT_SKIP_HOST=1 \
    "$SCRIPTS_DIR/build_moonlight.sh" || { echo -e "${RED}❌ Moonlight Core build failed${NC}"; exit 1; }

# 3. fyne package → produces signed .app bundle
echo -e "\n${YELLOW}🔨 fyne package --target ios...${NC}"
# Remove previously generated .app
find "$REPO_ROOT" -maxdepth 1 -name "*.app" -exec rm -rf {} + 2>/dev/null || true

# Run fyne from cmd/ so it builds the correct main package (not repo root)
VERSION=$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0")
# CFBundleVersion (build number) must strictly increase across App Store Connect
# uploads of the same CFBundleShortVersionString. Without --app-build, fyne falls
# back to the hardcoded "Build" value in cmd/FyneApp.toml, which never changes and
# gets every re-upload rejected. Git commit count is a simple monotonic counter.
BUILD_NUMBER=$(git -C "$REPO_ROOT" rev-list --count HEAD 2>/dev/null || echo 1)
cd "$REPO_ROOT/cmd"
"$FYNE_BIN" package \
    --target ios \
    --app-id "$APP_ID" \
    --name "$APP_NAME" \
    --app-version "$VERSION" \
    --app-build "$BUILD_NUMBER" \
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
    :
fi
echo -e "${GREEN}✓${NC} App bundle: $APP_BUNDLE"

# 4a. Inject NSCameraUsageDescription into Info.plist
INFO_PLIST="$APP_BUNDLE/Info.plist"
if [ -f "$INFO_PLIST" ]; then
    /usr/libexec/PlistBuddy -c "Add :NSCameraUsageDescription string 'Scan QR code to connect to your USBridge device'" "$INFO_PLIST" 2>/dev/null || \
    /usr/libexec/PlistBuddy -c "Set :NSCameraUsageDescription 'Scan QR code to connect to your USBridge device'" "$INFO_PLIST"
fi

# 4b. Rebuild Assets.car via actool with all required icon sizes including 167x167 (iPad Pro)
_XCASSETS="$(mktemp -d)/AppIcons.xcassets"
_ICONSET="$_XCASSETS/AppIcon.appiconset"
mkdir -p "$_ICONSET"
sips -z 76   76   "$REPO_ROOT/Icon.png" --out "$_ICONSET/Icon_76.png"   2>/dev/null
sips -z 120  120  "$REPO_ROOT/Icon.png" --out "$_ICONSET/Icon_120.png"  2>/dev/null
sips -z 152  152  "$REPO_ROOT/Icon.png" --out "$_ICONSET/Icon_152.png"  2>/dev/null
sips -z 167  167  "$REPO_ROOT/Icon.png" --out "$_ICONSET/Icon_167.png"  2>/dev/null
sips -z 180  180  "$REPO_ROOT/Icon.png" --out "$_ICONSET/Icon_180.png"  2>/dev/null
sips -z 1024 1024 "$REPO_ROOT/Icon.png" --out "$_ICONSET/Icon_1024.png" 2>/dev/null
cat > "$_ICONSET/Contents.json" << 'ICONJSON'
{
  "images": [
    {"idiom":"iphone","scale":"2x","size":"60x60","filename":"Icon_120.png"},
    {"idiom":"iphone","scale":"3x","size":"60x60","filename":"Icon_180.png"},
    {"idiom":"ipad","scale":"1x","size":"76x76","filename":"Icon_76.png"},
    {"idiom":"ipad","scale":"2x","size":"76x76","filename":"Icon_152.png"},
    {"idiom":"ipad","scale":"2x","size":"83.5x83.5","filename":"Icon_167.png"},
    {"idiom":"ios-marketing","scale":"1x","size":"1024x1024","filename":"Icon_1024.png"}
  ],
  "info":{"author":"xcode","version":1}
}
ICONJSON
_ACTOOL_OUT="$(mktemp -d)"
xcrun actool "$_XCASSETS" \
    --compile "$_ACTOOL_OUT" \
    --platform iphoneos \
    --minimum-deployment-target 16.0 \
    --app-icon AppIcon \
    --output-partial-info-plist "$_ACTOOL_OUT/partial_info.plist" 2>/dev/null
if [ -f "$_ACTOOL_OUT/Assets.car" ]; then
    cp "$_ACTOOL_OUT/Assets.car" "$APP_BUNDLE/Assets.car"
else
    :
fi
# Also add loose PNG + Info.plist entry for older iOS compatibility
cp "$_ICONSET/Icon_167.png" "$APP_BUNDLE/AppIcon83.5x83.5@2x~ipad.png" 2>/dev/null || true
/usr/libexec/PlistBuddy \
    -c "Add :CFBundleIcons~ipad:CFBundlePrimaryIcon:CFBundleIconFiles: string AppIcon83.5x83.5" \
    "$APP_BUNDLE/Info.plist" 2>/dev/null || true
rm -rf "$(dirname "$_XCASSETS")" "$_ACTOOL_OUT"

# 4c. Inject LaunchScreen.storyboardc (required for App Store)
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
rm -f "$STORYBOARD_SRC"

# 4c. Re-sign with entitlements from embedded provisioning profile
# Adding new files (LaunchScreen) invalidates the fyne signature.
ENTITLEMENTS_PLIST="$(mktemp /tmp/entitlements.XXXXXX.plist)"
PROVISION_TMP="$(mktemp /tmp/provision.XXXXXX.plist)"
security cms -D -i "$APP_BUNDLE/embedded.mobileprovision" > "$PROVISION_TMP" 2>/dev/null
/usr/libexec/PlistBuddy -x -c "Print Entitlements" "$PROVISION_TMP" > "$ENTITLEMENTS_PLIST" 2>/dev/null
rm -f "$PROVISION_TMP"
# hotspot-provider is not allowed in iOS App Store builds (90046)
/usr/libexec/PlistBuddy -c "Delete :com.apple.developer.networking.networkextension" "$ENTITLEMENTS_PLIST" 2>/dev/null || true

codesign --force \
    --sign "$SIGN_CERT" \
    "$APP_BUNDLE/main" 2>&1 | grep -v "replacing existing" || true

codesign --force \
    --sign "$SIGN_CERT" \
    --entitlements "$ENTITLEMENTS_PLIST" \
    "$APP_BUNDLE" 2>&1 | grep -v "replacing existing" || true
rm -f "$ENTITLEMENTS_PLIST"

# 5. Package .app → .ipa  (Payload/<App>.app zipped)
mkdir -p "$DIST_DIR"
PAYLOAD_DIR="$(mktemp -d)/Payload"
mkdir -p "$PAYLOAD_DIR"
cp -R "$APP_BUNDLE" "$PAYLOAD_DIR/"
IPA_PATH="$DIST_DIR/$IPA_NAME"
(cd "$(dirname "$PAYLOAD_DIR")" && zip -qr "$IPA_PATH" Payload)
rm -rf "$(dirname "$PAYLOAD_DIR")"
echo -e "${GREEN}✓${NC} IPA: $IPA_PATH"

echo -e "   IPA:  $IPA_PATH"
echo -e ""
echo -e "   xcrun altool --upload-app -f \"$IPA_PATH\" -t ios -u fatkulinamir80@gmail.com"

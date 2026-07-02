#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

OUTPUT_NAME="USBridgeAgent"
DIST_DIR="$REPO_ROOT/dist/macos"
APP_BUNDLE="$DIST_DIR/${OUTPUT_NAME}.app"
APP_CONTENTS="$APP_BUNDLE/Contents"
APP_MACOS="$APP_CONTENTS/MacOS"
APP_RESOURCES="$APP_CONTENTS/Resources"
BIN_PATH="$APP_MACOS/$OUTPUT_NAME"
CODESIGN_IDENTITY="${USBRIDGE_CODESIGN_IDENTITY:-}"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

if [[ "$(uname -s)" != "Darwin" ]]; then
    echo -e "${RED}Этот скрипт должен запускаться на macOS${NC}"
    exit 1
fi

echo -e "${GREEN}Building usbridge_agent for macOS${NC}"

mkdir -p "$APP_MACOS" "$APP_RESOURCES"
rm -rf "$APP_BUNDLE"
mkdir -p "$APP_MACOS" "$APP_RESOURCES"

export CGO_ENABLED=1

echo -e "${YELLOW}Compiling app bundle...${NC}"
go build -o "$BIN_PATH" ./cmd/usbridge_agent
chmod +x "$BIN_PATH"

# Icon generation
if [ -f "$REPO_ROOT/assets/icons/appicon-256.png" ]; then
    echo -e "${YELLOW}Generating app icon...${NC}"
    ICONSET_DIR="$DIST_DIR/icon.iconset"
    mkdir -p "$ICONSET_DIR"
    
    # Generate standard sizes
    # sips -z height width source --out destination
    sips -z 16 16   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_16x16.png" > /dev/null 2>&1
    sips -z 32 32   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_16x16@2x.png" > /dev/null 2>&1
    sips -z 32 32   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_32x32.png" > /dev/null 2>&1
    sips -z 64 64   "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_32x32@2x.png" > /dev/null 2>&1
    sips -z 128 128 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_128x128.png" > /dev/null 2>&1
    sips -z 256 256 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_128x128@2x.png" > /dev/null 2>&1
    sips -z 256 256 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_256x256.png" > /dev/null 2>&1
    sips -z 512 512 "$REPO_ROOT/assets/icons/appicon-256.png" --out "$ICONSET_DIR/icon_256x256@2x.png" > /dev/null 2>&1
    
    iconutil -c icns "$ICONSET_DIR" -o "$APP_RESOURCES/AppIcon.icns"
    rm -rf "$ICONSET_DIR"
fi

cat > "$APP_CONTENTS/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleDisplayName</key>
    <string>USBridgeAgent</string>
    <key>CFBundleExecutable</key>
    <string>USBridgeAgent</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>com.usbridge.agent</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>USBridgeAgent</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>1.0.0</string>
    <key>CFBundleVersion</key>
    <string>1.0.0</string>
    <key>LSMinimumSystemVersion</key>
    <string>12.3</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

if [ -n "$CODESIGN_IDENTITY" ] && command -v codesign >/dev/null 2>&1; then
    echo -e "${YELLOW}Signing app bundle with identity: $CODESIGN_IDENTITY${NC}"
    codesign --force --deep --sign "$CODESIGN_IDENTITY" "$APP_BUNDLE"
else
    rm -rf "$APP_CONTENTS/_CodeSignature"
    echo -e "${YELLOW}Skipping codesign for dev build (stable TCC is expected via fixed install path).${NC}"
fi

cp "$REPO_ROOT/config.yaml" "$DIST_DIR/config.yaml"

source "$SCRIPT_DIR/fetch_sunshine.sh"
fetch_sunshine_macos "$DIST_DIR/sunshine"

cat > "$DIST_DIR/README.txt" <<'README'
USBridgeAgent for macOS
=======================

Run:
  ./scripts/install_macos.sh
  open "$HOME/Applications/USBridgeAgent.app"

Video/input: Sunshine (Moonlight GameStream host) is bundled in ./sunshine/ and
is started automatically by the agent (Sunshine.app), including a one-time
admin credential bootstrap. The agent itself is not in the video/input path;
it only pairs with and relays PINs to Sunshine's local API (port 47990) on
behalf of usbridge_client.

Requirements:
  - Accessibility permission for mouse/keyboard injection
  - Screen Recording permission for screen capture (used by Sunshine)

Notes:
  - For stable macOS permissions, always run the same installed app path
  - Recommended install path: ~/Applications/USBridgeAgent.app
  - Dev builds are left unsigned by default; set USBRIDGE_CODESIGN_IDENTITY to sign explicitly
  - Snapshot capture uses the built-in screencapture utility
  - The agent remains API-compatible with usbridge_client

Configuration:
  config.yaml next to the .app, or ~/.config/usbridge-agent/

Application log:
  ~/Library/Logs/USBridgeAgent/app.log
  If USBRIDGE_LOG_DIR is set, logs are written there instead.
README

echo -e "${GREEN}Done.${NC}"
echo "Bundle: $APP_BUNDLE"
echo "Install/update for stable permissions: $REPO_ROOT/scripts/install_macos.sh"

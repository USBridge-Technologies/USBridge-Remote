#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION" 2>/dev/null || echo "0.0.0")"

DIST_DIR="$REPO_ROOT/dist/linux"
EXE_NAME="usbridge-agent"
OUTPUT_PATH="$DIST_DIR/$EXE_NAME"
BUILD_PKG="./cmd/usbridge_agent"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${GREEN}Building usbridge_agent for Linux${NC}"

if [[ "$(uname -s)" != "Linux" ]]; then
    echo -e "${YELLOW}Warning: This script is intended to run on Linux.${NC}"
fi

mkdir -p "$DIST_DIR"
rm -f "$OUTPUT_PATH"

export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

echo -e "${YELLOW}Compiling agent...${NC}"
go build -trimpath -ldflags "-s -w" -o "$OUTPUT_PATH" "$BUILD_PKG"
chmod +x "$OUTPUT_PATH"

# Fetch/build Sunshine (staged as dist/linux/sunshine/usr/bin/sunshine + assets)
source "$SCRIPT_DIR/fetch_sunshine.sh"
fetch_sunshine_linux "$DIST_DIR/sunshine"

# ── Build AppImage ─────────────────────────────────────────────────────────────
# The AppImage bundles the agent + Sunshine into a single relocatable package.
# Sunshine is built with SUNSHINE_BUILD_APPIMAGE=ON so it looks for its assets
# at ./usr/share/sunshine relative to cwd; the agent sets cwd = $APPDIR on
# launch, making this resolve to $APPDIR/usr/share/sunshine inside the AppImage.

APPDIR="$DIST_DIR/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"

# Agent binary
cp "$OUTPUT_PATH" "$APPDIR/usr/bin/$EXE_NAME"

# Sunshine binary + assets (from cmake install tree under dist/linux/sunshine/)
SUNSHINE_STAGING="$DIST_DIR/sunshine"
if [[ -f "$SUNSHINE_STAGING/usr/bin/sunshine" ]]; then
    cp "$SUNSHINE_STAGING/usr/bin/sunshine" "$APPDIR/usr/bin/sunshine"
    if [[ -d "$SUNSHINE_STAGING/usr/share/sunshine" ]]; then
        cp -R "$SUNSHINE_STAGING/usr/share/sunshine" "$APPDIR/usr/share/sunshine"
    fi
    echo -e "${GREEN}✓${NC} Sunshine bundled into AppDir"
else
    echo -e "${YELLOW}Warning: Sunshine binary not found at $SUNSHINE_STAGING/usr/bin/sunshine — AppImage will not include Sunshine${NC}"
fi

# Icon
ICON_SRC="$REPO_ROOT/assets/icons/appicon-256.png"
if [[ -f "$ICON_SRC" ]]; then
    cp "$ICON_SRC" "$APPDIR/usr/share/icons/hicolor/256x256/apps/$EXE_NAME.png"
    cp "$ICON_SRC" "$APPDIR/$EXE_NAME.png"
fi

# Desktop entry
cat > "$APPDIR/usr/share/applications/$EXE_NAME.desktop" <<DESKTOP
[Desktop Entry]
Name=USBridgeAgent
Exec=usbridge-agent
Icon=usbridge-agent
Type=Application
Categories=Utility;Network;
Comment=USBridge streaming agent (Sunshine host + pairing relay)
DESKTOP
cp "$APPDIR/usr/share/applications/$EXE_NAME.desktop" "$APPDIR/$EXE_NAME.desktop"

# Download linuxdeploy if not cached
LINUXDEPLOY="$DIST_DIR/linuxdeploy-x86_64.AppImage"
if [[ ! -f "$LINUXDEPLOY" ]]; then
    echo -e "${YELLOW}Downloading linuxdeploy...${NC}"
    curl -fL --progress-bar -o "$LINUXDEPLOY" \
        "https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage"
    chmod +x "$LINUXDEPLOY"
fi

# Build AppImage
echo -e "${YELLOW}Packaging AppImage...${NC}"
OUTPUT_APPIMAGE="$REPO_ROOT/dist/USBridgeAgent-Linux-x86_64-${VERSION}.AppImage"
rm -f "$OUTPUT_APPIMAGE"

ARCH=x86_64 "$LINUXDEPLOY" \
    --appdir "$APPDIR" \
    --executable "$APPDIR/usr/bin/$EXE_NAME" \
    --executable "$APPDIR/usr/bin/sunshine" \
    --desktop-file "$APPDIR/$EXE_NAME.desktop" \
    --icon-file "$APPDIR/$EXE_NAME.png" \
    --output appimage 2>&1

# linuxdeploy writes the AppImage to cwd — move it to dist/
PRODUCED="$(find "$REPO_ROOT" -maxdepth 2 -name 'USBridgeAgent*.AppImage' ! -path '*/AppDir/*' | head -1)"
if [[ -z "$PRODUCED" ]]; then
    PRODUCED="$(find . -maxdepth 2 -name '*.AppImage' ! -path '*/AppDir/*' | head -1)"
fi
if [[ -n "$PRODUCED" && "$PRODUCED" != "$OUTPUT_APPIMAGE" ]]; then
    mv "$PRODUCED" "$OUTPUT_APPIMAGE"
fi

echo -e "${GREEN}✓${NC} AppImage: $OUTPUT_APPIMAGE"
echo "Binary: $OUTPUT_PATH"

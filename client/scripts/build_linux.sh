#!/bin/bash
# Build USBridgeClient for Linux, packaged as a self-contained AppImage.
# Output: dist/USBridgeClient-Linux-x86_64-<VERSION>.AppImage
#
# linuxdeploy bundles every shared library the binary directly links against
# (libavcodec/libavutil/libswscale for Moonlight HW decode, libpulse, libopus,
# libssl, libvulkan, the GStreamer core libs, ...) into the AppImage, so the
# target machine needs no runtime packages installed for the default Moonlight
# streaming path. The legacy RTP video mode's GStreamer *element plugins*
# (libgstlibav.so etc.) are loaded via dlopen rather than linked, so they are
# not picked up by this scan — that path still needs a system GStreamer
# install (gstreamer1.0-plugins-{base,good,bad,ugly} gstreamer1.0-libav).
#
# Build deps (install before running this script):
#   Moonlight HW decode:  libavcodec-dev libavutil-dev libswscale-dev libpulse-dev
#   Moonlight core:       opus openssl pkg-config cmake
#
# One-liner: sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libpulse-dev \
#              libopus-dev libssl-dev pkg-config cmake

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m'

echo -e "${YELLOW}=> Building Moonlight Core...${NC}"
"$SCRIPT_DIR/build_moonlight.sh" || { echo -e "${RED}❌ Failed to build Moonlight Core${NC}"; exit 1; }

if [ -z "${USBRIDGE_LOGGING_ACTIVE:-}" ]; then
  export USBRIDGE_LOGGING_ACTIVE=1
  LOG_DIR="$REPO_ROOT/logs"
  mkdir -p "$LOG_DIR"
  LOG_FILE="$LOG_DIR/$(basename "$0" .sh).log"
  exec > >(tee -a "$LOG_FILE") 2>&1
  echo "=== $(date '+%Y-%m-%d %H:%M:%S') [$0] ==="
fi

VERSION="$(tr -d ' \t\n\r' < "$REPO_ROOT/VERSION" 2>/dev/null || echo "0.0.0")"

DIST_DIR="$REPO_ROOT/dist/linux"
EXE_NAME="usbridge-client"
OUTPUT_PATH="$DIST_DIR/$EXE_NAME"

mkdir -p "$DIST_DIR"
rm -f "$OUTPUT_PATH"

# go-gst (used for non-Moonlight RTP path) generates format-security warnings.
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"

# Verify Moonlight HW decode build deps are present before spending time compiling.
for pkg in libavcodec libavutil libswscale libpulse-simple; do
    if ! pkg-config --exists "$pkg" 2>/dev/null; then
        echo -e "${RED}❌ Missing build dep: $pkg${NC}"
        echo "   Install: sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libpulse-dev"
        exit 1
    fi
done

echo -e "${YELLOW}Compiling client...${NC}"
go build -ldflags "-X main.version=$VERSION" -o "$OUTPUT_PATH" ./cmd
chmod +x "$OUTPUT_PATH"

# ── Build AppImage ─────────────────────────────────────────────────────────────

APPDIR="$DIST_DIR/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"

cp "$OUTPUT_PATH" "$APPDIR/usr/bin/$EXE_NAME"

# Icon
ICON_SRC="$REPO_ROOT/Icon.png"
if [[ -f "$ICON_SRC" ]]; then
    cp "$ICON_SRC" "$APPDIR/usr/share/icons/hicolor/256x256/apps/$EXE_NAME.png"
    cp "$ICON_SRC" "$APPDIR/$EXE_NAME.png"
fi

# Desktop entry
cat > "$APPDIR/usr/share/applications/$EXE_NAME.desktop" <<DESKTOP
[Desktop Entry]
Name=USBridge Client
Exec=usbridge-client
Icon=usbridge-client
Type=Application
Categories=Network;RemoteAccess;
Comment=USBridge remote desktop client (Moonlight streaming)
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
OUTPUT_APPIMAGE="$REPO_ROOT/dist/USBridgeClient-Linux-x86_64-${VERSION}.AppImage"
rm -f "$OUTPUT_APPIMAGE"

ARCH=x86_64 "$LINUXDEPLOY" \
    --appdir "$APPDIR" \
    --executable "$APPDIR/usr/bin/$EXE_NAME" \
    --desktop-file "$APPDIR/$EXE_NAME.desktop" \
    --icon-file "$APPDIR/$EXE_NAME.png" \
    --output appimage 2>&1

# linuxdeploy writes the AppImage to cwd — move it to dist/
PRODUCED="$(find "$REPO_ROOT" -maxdepth 2 -name 'USBridgeClient*.AppImage' ! -path '*/AppDir/*' | head -1)"
if [[ -z "$PRODUCED" ]]; then
    PRODUCED="$(find . -maxdepth 2 -name '*.AppImage' ! -path '*/AppDir/*' | head -1)"
fi
if [[ -n "$PRODUCED" && "$PRODUCED" != "$OUTPUT_APPIMAGE" ]]; then
    mv "$PRODUCED" "$OUTPUT_APPIMAGE"
fi
chmod +x "$OUTPUT_APPIMAGE"

echo -e "${GREEN}✓${NC} AppImage: $OUTPUT_APPIMAGE"
echo "Binary: $OUTPUT_PATH"

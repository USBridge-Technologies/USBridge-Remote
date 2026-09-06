#!/bin/bash
# Build USBridgeClient for Linux, packaged as a self-contained AppImage.
# Output: dist/USBridgeClient-Linux-x86_64-<VERSION>.AppImage
#
# linuxdeploy bundles every shared library the binary directly links against
# (libavcodec/libavutil/libswscale for Moonlight HW decode, libpulse, libopus,
# libssl, libvulkan, ...) into the AppImage, so the target machine needs no
# runtime packages installed for the Moonlight streaming path. GStreamer is
# not linked, imported, or bundled anywhere in this build — the QR camera
# scanner reads /dev/videoN directly via V4L2 (v4l2camera_impl_linux.c).
#
# Build deps (install before running this script):
#   Moonlight HW decode:  libavcodec-dev libavutil-dev libswscale-dev libpulse-dev
#   Moonlight core:       opus openssl pkg-config cmake
#   Optional:             python3 (pip) -- fetches the local ui.parse/AI
#                          Vision ONNX runtime lib (see fetch_onnxruntime.sh);
#                          its absence only disables that one feature.
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

# local ui.parse ONNX offload (internal/localui, AI Vision's detector): the
# runtime lib is dlopen'd at runtime (via onnxruntime_go), not link-time
# linked, so linuxdeploy's ldd-based dependency walk below can never see or
# bundle it -- it needs its own explicit step, same as build_macos.sh's
# equivalent (see fetch_onnxruntime.sh's doc comment for why a PyPI wheel
# and not an apt package). Placed flat next to the executable in usr/bin/,
# matching local_ui_init.go's resolveLocalUIPath flat-layout candidate.
# Both failures are non-fatal (warn and continue): local ui.parse/AI Vision
# is an optional accelerator, never a hard dependency of the build.
echo -e "${YELLOW}Bundling local ui.parse (ONNX Runtime + models) for AI Vision...${NC}"
ORT_CACHE_DIR="$REPO_ROOT/.build-cache/onnxruntime-linux"
if [ ! -f "$ORT_CACHE_DIR/libonnxruntime.so" ]; then
    "$SCRIPT_DIR/fetch_onnxruntime.sh" "$ORT_CACHE_DIR" linux || true
fi
if [ -f "$ORT_CACHE_DIR/libonnxruntime.so" ]; then
    cp -L "$ORT_CACHE_DIR/libonnxruntime.so" "$APPDIR/usr/bin/libonnxruntime.so"
    chmod 755 "$APPDIR/usr/bin/libonnxruntime.so"
    echo -e "${GREEN}✓${NC} usr/bin/libonnxruntime.so"
else
    echo -e "${YELLOW}⚠${NC} Could not fetch libonnxruntime.so -- local ui.parse/AI Vision will stay unavailable in this build"
fi
LOCALUI_MODELS_SRC="$REPO_ROOT/internal/localui/models"
if [ -f "$LOCALUI_MODELS_SRC/icon_detect.onnx" ]; then
    mkdir -p "$APPDIR/usr/bin/localui/models"
    cp "$LOCALUI_MODELS_SRC"/*.onnx "$APPDIR/usr/bin/localui/models/"
    echo -e "${GREEN}✓${NC} usr/bin/localui/models/ ($(du -sh "$APPDIR/usr/bin/localui/models" | cut -f1))"
else
    echo -e "${YELLOW}⚠${NC} $LOCALUI_MODELS_SRC has no .onnx files -- local ui.parse/AI Vision will stay unavailable in this build"
fi

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

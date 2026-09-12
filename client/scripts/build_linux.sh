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
#   USB passthrough:      libusb-1.0-0-dev (enables -tags usbpass_gousb claim path)
#   Optional:             python3 (pip) -- fetches the local ui.parse/AI
#                          Vision ONNX runtime lib (see fetch_onnxruntime.sh);
#                          its absence only disables that one feature.
#
# One-liner: sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libpulse-dev \
#              libopus-dev libssl-dev libusb-1.0-0-dev pkg-config cmake

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
if ! pkg-config --exists libusb-1.0 2>/dev/null; then
    echo -e "${RED}❌ Missing build dep: libusb-1.0${NC}"
    echo "   Install: sudo apt-get install -y libusb-1.0-0-dev"
    exit 1
fi

echo -e "${YELLOW}Compiling client (usbpass_gousb + libusb)...${NC}"
CGO_ENABLED=1 go build -tags usbpass_gousb -ldflags "-X main.version=$VERSION" -o "$OUTPUT_PATH" ./cmd
chmod +x "$OUTPUT_PATH"

# ── Build AppImage ─────────────────────────────────────────────────────────────

APPDIR="$DIST_DIR/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"

cp "$OUTPUT_PATH" "$APPDIR/usr/bin/$EXE_NAME"

# Closed rust-shine USB passthrough AES helper (bin/usb-broker → usbridge-usb-broker).
# Go client launches this with --role client on mount; export itself is in-process Go.
# Prefer an explicit path, else a sibling rust-shine release build.
USB_BROKER_SRC="${USBRIDGE_USB_BROKER:-}"
if [[ -z "$USB_BROKER_SRC" || ! -f "$USB_BROKER_SRC" ]]; then
    for cand in \
        "$REPO_ROOT/../rust-shine/target/release/usbridge-usb-broker" \
        "$HOME/Projects/rust-shine/target/release/usbridge-usb-broker"
    do
        if [[ -f "$cand" ]]; then
            USB_BROKER_SRC="$cand"
            break
        fi
    done
fi
if [[ -n "$USB_BROKER_SRC" && -f "$USB_BROKER_SRC" ]]; then
    cp "$USB_BROKER_SRC" "$APPDIR/usr/bin/usbridge-usb-broker"
    chmod 755 "$APPDIR/usr/bin/usbridge-usb-broker"
    echo -e "${GREEN}✓${NC} usr/bin/usbridge-usb-broker (from $USB_BROKER_SRC)"
else
    echo -e "${YELLOW}⚠${NC} usbridge-usb-broker not found — USB passthrough attach will fail until you build rust-shine -p usb-broker and rebuild, or set USBRIDGE_USB_BROKER"
fi

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
# Also re-fetches a cache dir left over from before fetch_onnxruntime.sh
# switched to the onnxruntime-openvino wheel on linux (plain libonnxruntime.so
# present, but none of the OpenVINO EP files it now fetches alongside it) --
# without this second check, a dev box's pre-existing cache would keep
# silently shipping CPU-only builds forever, never picking up Intel GPU
# acceleration despite this script's own logic below now expecting it.
if [ ! -f "$ORT_CACHE_DIR/libonnxruntime.so" ] || [ ! -f "$ORT_CACHE_DIR/libonnxruntime_providers_openvino.so" ]; then
    "$SCRIPT_DIR/fetch_onnxruntime.sh" "$ORT_CACHE_DIR" linux || true
fi
if [ -f "$ORT_CACHE_DIR/libonnxruntime.so" ]; then
    cp -L "$ORT_CACHE_DIR/libonnxruntime.so" "$APPDIR/usr/bin/libonnxruntime.so"
    chmod 755 "$APPDIR/usr/bin/libonnxruntime.so"
    echo -e "${GREEN}✓${NC} usr/bin/libonnxruntime.so"
    # OpenVINO EP + GPU plugin (Intel iGPU acceleration -- see
    # fetch_onnxruntime.sh's doc comment): fetch_onnxruntime.sh drops these
    # next to libonnxruntime.so in the same cache dir for a linux target.
    # Bundled flat into usr/bin, same as libonnxruntime.so itself -- every
    # file here ships with RPATH=$ORIGIN (verified via readelf -d against
    # the wheel's own files), i.e. "look in my own directory", so
    # linuxdeploy's dependency walk below (which DOES walk every .so it
    # finds in the AppDir, not just the main executable -- confirmed live
    # via its own "Deploying dependencies for ELF file .../libonnxruntime_
    # providers_openvino.so" log line) needs to find providers_openvino.so's
    # actual DT_NEEDED entries (libopenvino.so.*, libopenvino_onnx_frontend
    # .so.*, libtbb.so.*) sitting right next to it in order to resolve and
    # then relocate them -- pre-splitting them into usr/lib ourselves before
    # that walk runs breaks the walk itself ("ERROR: Could not find
    # dependency: libopenvino_onnx_frontend.so.2541", confirmed live) rather
    # than helping it.
    OV_COUNT=0
    for f in "$ORT_CACHE_DIR"/libonnxruntime_providers_*.so "$ORT_CACHE_DIR"/libopenvino*.so* "$ORT_CACHE_DIR"/libtbb*.so*; do
        [ -f "$f" ] || continue
        cp -L "$f" "$APPDIR/usr/bin/$(basename "$f")"
        chmod 755 "$APPDIR/usr/bin/$(basename "$f")"
        OV_COUNT=$((OV_COUNT + 1))
    done
    if [ "$OV_COUNT" -gt 0 ]; then
        echo -e "${GREEN}✓${NC} usr/bin/{providers_openvino,libopenvino*,libtbb*}.so ($OV_COUNT files) -- Intel iGPU acceleration"
    else
        echo -e "${YELLOW}⚠${NC} No OpenVINO EP files in $ORT_CACHE_DIR (stale cache from before this build script fetched them?) -- delete $ORT_CACHE_DIR and rebuild to pick them up; AI Vision still works CPU-only until then"
    fi
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

# Deploy (no --output yet): populates usr/lib with every .so the bundle's
# own ELF files DT_NEED (including relocating libopenvino.so.*/
# libopenvino_onnx_frontend.so.*/libtbb.so.* out of usr/bin, per
# libonnxruntime_providers_openvino.so's own DT_NEEDED entries -- see this
# script's own comment above) and rewrites their RPATHs to match.
echo -e "${YELLOW}Deploying dependencies...${NC}"
ARCH=x86_64 "$LINUXDEPLOY" \
    --appdir "$APPDIR" \
    --executable "$APPDIR/usr/bin/$EXE_NAME" \
    --desktop-file "$APPDIR/$EXE_NAME.desktop" \
    --icon-file "$APPDIR/$EXE_NAME.png" 2>&1

# libusb-1.0 is on linuxdeploy's blacklist (treated as "system"), so the
# deploy step above skips it even though we link it for usbpass_gousb.
# Bundle it explicitly so AppImage hosts without a distro libusb still claim.
if ldd "$APPDIR/usr/bin/$EXE_NAME" 2>/dev/null | grep -q 'libusb-1.0.so'; then
    USB_SO="$(ldd "$APPDIR/usr/bin/$EXE_NAME" | awk '/libusb-1.0.so/{print $3; exit}')"
    if [[ -n "$USB_SO" && -f "$USB_SO" ]]; then
        cp -L "$USB_SO" "$APPDIR/usr/lib/libusb-1.0.so.0"
        chmod 755 "$APPDIR/usr/lib/libusb-1.0.so.0"
        echo -e "${GREEN}✓${NC} usr/lib/libusb-1.0.so.0 (forced; linuxdeploy blacklists libusb)"
    fi
fi

# libopenvino_intel_gpu_plugin.so is never a static ELF dependency of
# anything (OpenVINO dlopen's it by convention, only once
# AppendExecutionProviderOpenVINO's device_type=GPU request actually runs),
# so the deploy step above never touches it -- it's still sitting in
# usr/bin where it was originally bundled, while libopenvino.so.* just got
# relocated to usr/lib. OpenVINO's own plugin discovery looks in
# libopenvino.so's directory, not the main executable's, so it has to move
# alongside it or the GPU EP fails to init at runtime with "[OpenVINO]
# Device GPU is not available" (confirmed live) despite every library
# technically being present in the bundle. Whichever directory
# libopenvino.so.* actually ended up in above (usr/lib today, but this
# tracks it rather than hardcoding that) is where this has to land too.
GPU_PLUGIN_SRC="$APPDIR/usr/bin/libopenvino_intel_gpu_plugin.so"
if [ -f "$GPU_PLUGIN_SRC" ]; then
    OV_CORE_LIB="$(find "$APPDIR/usr/lib" -maxdepth 1 -name 'libopenvino.so.*' -print -quit)"
    if [ -n "$OV_CORE_LIB" ]; then
        OV_LIB_DIR="$(dirname "$OV_CORE_LIB")"
        if [ "$OV_LIB_DIR" != "$(dirname "$GPU_PLUGIN_SRC")" ]; then
            cp -L "$GPU_PLUGIN_SRC" "$OV_LIB_DIR/libopenvino_intel_gpu_plugin.so"
            chmod 755 "$OV_LIB_DIR/libopenvino_intel_gpu_plugin.so"
            echo -e "${GREEN}✓${NC} $(realpath --relative-to="$APPDIR" "$OV_LIB_DIR")/libopenvino_intel_gpu_plugin.so (colocated with libopenvino.so for OpenVINO's own plugin discovery)"
        fi
    else
        echo -e "${YELLOW}⚠${NC} libopenvino.so.* not found under usr/lib after deploy -- Intel GPU acceleration may not initialize at runtime"
    fi
fi

# Build AppImage
echo -e "${YELLOW}Packaging AppImage...${NC}"
OUTPUT_APPIMAGE="$REPO_ROOT/dist/USBridgeClient-Linux-x86_64-${VERSION}.AppImage"
rm -f "$OUTPUT_APPIMAGE"

# linuxdeploy writes its output to $PWD using its own default name derived
# from the desktop file (e.g. "USBridge_Client-x86_64.AppImage" — note the
# underscore, which does NOT match "USBridgeClient*"). The old code guessed
# that name back out of $REPO_ROOT with `find | head -1`, which is unsafe: a
# stale .AppImage left in $REPO_ROOT from an earlier build (linuxdeploy run
# manually, or a previous invocation that errored before cleanup) could win
# that match, silently shipping an OLD binary under the NEW version's
# filename — confirmed live, this is exactly what happened here (the client
# kept shipping 2.4.25's binary for several "2.4.26" builds in a row).
# Deleting every loose .AppImage in $REPO_ROOT before running linuxdeploy
# removes the ambiguity outright: whatever single .AppImage exists there
# afterward can only be this run's own output.
find "$REPO_ROOT" -maxdepth 1 -name '*.AppImage' -delete

ARCH=x86_64 "$LINUXDEPLOY" \
    --appdir "$APPDIR" \
    --output appimage 2>&1

PRODUCED="$(find "$REPO_ROOT" -maxdepth 1 -name '*.AppImage' | head -1)"
if [[ -z "$PRODUCED" ]]; then
    echo -e "${RED}❌ linuxdeploy did not produce an AppImage in $REPO_ROOT${NC}"
    exit 1
fi
mv "$PRODUCED" "$OUTPUT_APPIMAGE"
chmod +x "$OUTPUT_APPIMAGE"

# Fail loudly instead of silently shipping a stale binary under a fresh
# version number (exactly the bug worked around above): confirm the version
# string ldflags just stamped into main.version is present in the ELF that
# actually got packaged. Checked against $APPDIR/usr/bin -- the ELF
# linuxdeploy embedded into the AppImage -- not $OUTPUT_APPIMAGE itself:
# the AppImage is a compressed (zstd) squashfs image, so `strings` on the
# whole file finds nothing at all, compressed data isn't printable text.
if ! strings "$APPDIR/usr/bin/$EXE_NAME" | grep -qx "$VERSION"; then
    echo -e "${RED}❌ $APPDIR/usr/bin/$EXE_NAME does not contain version string '$VERSION' -- packaging picked up a stale binary${NC}"
    exit 1
fi

echo -e "${GREEN}✓${NC} AppImage: $OUTPUT_APPIMAGE"
echo "Binary: $OUTPUT_PATH"

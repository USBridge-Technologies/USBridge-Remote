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

# ── -streamer sunshine|rustshine (default: sunshine) ─────────────────────────
# Selects which streamhost.Backend implementation gets compiled in via the Go
# build tag of the same name (see internal/streamhost/factory_default.go).
# This open-source repo only ships the Sunshine backend; rustshine only
# builds if a sibling checkout has added its own factory_rustshine.go.
STREAMER="sunshine"
while [[ $# -gt 0 ]]; do
    case "$1" in
        -streamer|--streamer) STREAMER="$2"; shift 2 ;;
        -streamer=*|--streamer=*) STREAMER="${1#*=}"; shift ;;
        *) echo -e "${RED}Unknown argument: $1${NC}" >&2; exit 1 ;;
    esac
done
case "$STREAMER" in
    sunshine|rustshine) ;;
    *) echo -e "${RED}Unknown -streamer '$STREAMER' (expected sunshine|rustshine)${NC}" >&2; exit 1 ;;
esac

GO_TAGS=()
[[ "$STREAMER" == "rustshine" ]] && GO_TAGS=(-tags rustshine)

echo -e "${GREEN}Building usbridge_agent for Linux (streamer=$STREAMER)${NC}"

if [[ "$(uname -s)" != "Linux" ]]; then
    echo -e "${YELLOW}Warning: This script is intended to run on Linux.${NC}"
fi

mkdir -p "$DIST_DIR"
rm -f "$OUTPUT_PATH"

export CGO_ENABLED=1
export GOOS=linux
export GOARCH=amd64

echo -e "${YELLOW}Compiling agent...${NC}"
go build -trimpath "${GO_TAGS[@]}" -ldflags "-s -w -X main.version=$VERSION" -o "$OUTPUT_PATH" "$BUILD_PKG"
chmod +x "$OUTPUT_PATH"

# sunshine_capexec: a tiny, fully static (CGO_ENABLED=0 — zero dynamic deps)
# launcher that carries the CAP_SYS_ADMIN file capability for KMS screen
# capture instead of the streamer binary itself. A file capability puts the
# dynamic linker into secure-execution mode, which ignores RPATH/RUNPATH —
# breaking the streamer's bundled-library resolution the moment it's
# granted. Generic (usage: sunshine_capexec <target-binary> [args...]), so
# both streamers reuse the same launcher — see cmd/sunshine_capexec and
# internal/permissions/service_linux.go.
CAPEXEC_PATH="$DIST_DIR/sunshine-capexec"
echo -e "${YELLOW}Compiling sunshine_capexec (static)...${NC}"
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o "$CAPEXEC_PATH" ./cmd/sunshine_capexec
chmod +x "$CAPEXEC_PATH"

if [[ "$STREAMER" == "rustshine" ]]; then
    source "$SCRIPT_DIR/fetch_rustshine.sh"
    fetch_rustshine_linux "$DIST_DIR/rustshine"
else
    # Fetch/build Sunshine (staged as dist/linux/sunshine/usr/bin/sunshine + assets)
    source "$SCRIPT_DIR/fetch_sunshine.sh"
    fetch_sunshine_linux "$DIST_DIR/sunshine"
fi

# ── Build AppImage ─────────────────────────────────────────────────────────────
# The AppImage bundles the agent + Sunshine into a single relocatable package.
# Sunshine is built with SUNSHINE_BUILD_APPIMAGE=ON so it looks for its assets
# at ./usr/local/assets relative to cwd; the agent sets cwd = $APPDIR on
# launch, making this resolve to $APPDIR/usr/local/assets inside the AppImage.

APPDIR="$DIST_DIR/AppDir"
rm -rf "$APPDIR"
mkdir -p "$APPDIR/usr/bin" "$APPDIR/usr/share/applications" "$APPDIR/usr/share/icons/hicolor/256x256/apps"

# Agent binary
cp "$OUTPUT_PATH" "$APPDIR/usr/bin/$EXE_NAME"

# linuxdeploy's --executable list below is populated per-streamer: it re-scans
# each executable's dynamic deps and bundles whatever it finds on this build
# machine, so only the streamer actually shipped in this AppImage should be
# listed.
DEPLOY_EXECUTABLES=("$APPDIR/usr/bin/$EXE_NAME")

# sunshine_capexec launcher (static — not passed to linuxdeploy's
# --executable list below, it has no shared library deps to bundle). Used by
# both streamers' KMS-capture "Request" permission flow (see
# streamhost.{sunshine,rustshine}Backend.CapExecPath).
cp "$CAPEXEC_PATH" "$APPDIR/usr/bin/sunshine-capexec"

if [[ "$STREAMER" == "rustshine" ]]; then
    # gamestream-server binary (from dist/linux/rustshine/, staged by
    # fetch_rustshine.sh above). streamhost.rustshineBackend.BinaryPath()
    # resolves this as exeDir/rustshine/gamestream-server, where exeDir is
    # the agent's own directory (usr/bin inside the AppImage) — so it must
    # live in a "rustshine" subdirectory of usr/bin, not directly in it.
    RUSTSHINE_STAGING="$DIST_DIR/rustshine"
    if [[ -f "$RUSTSHINE_STAGING/gamestream-server" ]]; then
        mkdir -p "$APPDIR/usr/bin/rustshine"
        cp "$RUSTSHINE_STAGING/gamestream-server" "$APPDIR/usr/bin/rustshine/gamestream-server"
        DEPLOY_EXECUTABLES+=("$APPDIR/usr/bin/rustshine/gamestream-server")
        echo -e "${GREEN}✓${NC} rustshine (gamestream-server) bundled into AppDir"
    else
        echo -e "${YELLOW}Warning: gamestream-server not found at $RUSTSHINE_STAGING/gamestream-server — AppImage will not include rustshine${NC}"
    fi
else
    # Sunshine binary + assets (from cmake install tree under dist/linux/sunshine/)
    SUNSHINE_STAGING="$DIST_DIR/sunshine"
    if [[ -f "$SUNSHINE_STAGING/usr/bin/sunshine" ]]; then
        cp "$SUNSHINE_STAGING/usr/bin/sunshine" "$APPDIR/usr/bin/sunshine"
        DEPLOY_EXECUTABLES+=("$APPDIR/usr/bin/sunshine")
        if [[ -d "$SUNSHINE_STAGING/usr/local/assets" ]]; then
            mkdir -p "$APPDIR/usr/local"
            cp -R "$SUNSHINE_STAGING/usr/local/assets" "$APPDIR/usr/local/assets"
        else
            echo -e "${YELLOW}Warning: Sunshine assets not found at $SUNSHINE_STAGING/usr/local/assets — Sunshine will be missing shaders/web UI${NC}"
        fi
        # The fork's own release tarball already carries a usr/lib populated by
        # its own linuxdeploy pass (built on the fork's pinned ubuntu-22.04 CI
        # image, so these are guaranteed to match what the sunshine binary
        # actually needs — e.g. libminiupnpc.so.17, libicuuc.so.70). Seed
        # AppDir/usr/lib with those before running linuxdeploy below instead of
        # only relying on it to re-derive the same libraries from whatever
        # packages happen to be installed on *this* build machine, which may be
        # a different Ubuntu release with mismatched sonames.
        if [[ -d "$SUNSHINE_STAGING/usr/lib" ]]; then
            mkdir -p "$APPDIR/usr/lib"
            cp -R "$SUNSHINE_STAGING/usr/lib/." "$APPDIR/usr/lib/"
        fi
        echo -e "${GREEN}✓${NC} Sunshine bundled into AppDir"
    else
        echo -e "${YELLOW}Warning: Sunshine binary not found at $SUNSHINE_STAGING/usr/bin/sunshine — AppImage will not include Sunshine${NC}"
    fi
fi

# Icon
ICON_SRC="$REPO_ROOT/assets/icons/appicon-256.png"
if [[ -f "$ICON_SRC" ]]; then
    cp "$ICON_SRC" "$APPDIR/usr/share/icons/hicolor/256x256/apps/$EXE_NAME.png"
    cp "$ICON_SRC" "$APPDIR/$EXE_NAME.png"
fi

# Desktop entry
STREAMER_LABEL="Sunshine"
[[ "$STREAMER" == "rustshine" ]] && STREAMER_LABEL="rustshine"
cat > "$APPDIR/usr/share/applications/$EXE_NAME.desktop" <<DESKTOP
[Desktop Entry]
Name=USBridgeAgent
Exec=usbridge-agent
Icon=usbridge-agent
Type=Application
Categories=Utility;Network;
Comment=USBridge streaming agent ($STREAMER_LABEL host + pairing relay)
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

# Some CI/container runners don't have a working FUSE mount for AppImages to
# self-mount via; APPIMAGE_EXTRACT_AND_RUN makes every AppImage runtime in
# this process tree — linuxdeploy itself, and the appimagetool AppImage it
# downloads and shells out to for --output appimage — extract to a temp dir
# and exec from there instead, sidestepping FUSE entirely everywhere (same
# fix applied to the Sunshine fork's own release build, which hit the
# identical "fusermount not found" failure).
export APPIMAGE_EXTRACT_AND_RUN=1
# --executable "sunshine" (when bundled) makes this pass re-scan sunshine's
# own dynamic deps and bundle whatever it finds on *this* build machine --
# including libva.so*/libva-drm.so*, silently undoing the exclusion the
# fork's own CI already applied (and re-seeded into AppDir/usr/lib above)
# specifically because a mismatched bundled libva breaks VAAPI hardware
# encoding on the end user's host. Exclude them here too so this pass can't
# reintroduce them; harmless (no matching libraries) when bundling rustshine.
EXECUTABLE_ARGS=()
for exe in "${DEPLOY_EXECUTABLES[@]}"; do
    EXECUTABLE_ARGS+=(--executable "$exe")
done
ARCH=x86_64 "$LINUXDEPLOY" \
    --appdir "$APPDIR" \
    "${EXECUTABLE_ARGS[@]}" \
    --desktop-file "$APPDIR/$EXE_NAME.desktop" \
    --icon-file "$APPDIR/$EXE_NAME.png" \
    --exclude-library="libva.so*" \
    --exclude-library="libva-drm.so*" \
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

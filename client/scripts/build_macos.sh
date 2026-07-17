#!/bin/bash
# Build USBridgeClient for macOS as a self-contained .app bundle.
# All required Homebrew dylibs are copied into Contents/Frameworks/ so the
# app runs on machines without Homebrew installed.
#
# Requirements:
#   Required:  Go, Xcode Command Line Tools
#   Optional:  GStreamer (Homebrew) — bundled automatically if installed;
#              needed only for legacy RTP video mode.
#              Moonlight streaming uses VideoToolbox + CoreAudio (no GStreamer required).

set -e

SCRIPTS_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

echo "=> Building Moonlight Core..."
"$SCRIPTS_DIR/build_moonlight.sh" || { echo "❌ Failed to build Moonlight Core"; exit 1; }

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

OUTPUT_NAME="USBridgeClient"
DIST_ROOT="$REPO_ROOT/dist"
DIST_OS="$DIST_ROOT/macos"
if [ -d "$DIST_OS" ] && [ ! -w "$DIST_OS" ]; then
    echo "   sudo chown -R \"$USER\":\"$USER\" \"$DIST_OS\""
    exit 1
fi
DIST_DIR="$DIST_OS"
APP_BUNDLE_NAME="${OUTPUT_NAME}.app"
APP_CONTENTS_DIR="$DIST_DIR/$APP_BUNDLE_NAME/Contents"
APP_MACOS_DIR="$APP_CONTENTS_DIR/MacOS"
APP_RESOURCES_DIR="$APP_CONTENTS_DIR/Resources"
APP_FRAMEWORKS_DIR="$APP_CONTENTS_DIR/Frameworks"
APP_PLUGINS_DIR="$APP_CONTENTS_DIR/PlugIns"
BINARY_NAME="$OUTPUT_NAME"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# --------------------------------------------------------------------------
# Dylib bundling — mirrors what build_windows.sh does for DLLs
# --------------------------------------------------------------------------

# Returns 0 (true) when a dylib path is a macOS system library that must NOT
# be bundled (it is always present on the target machine).
is_system_dylib() {
    case "$1" in
        /usr/lib/*|/System/Library/*) return 0 ;;
    esac
    return 1
}

# Given an absolute dylib path (which may be a Cellar path or an opt symlink
# path), return a concrete file path that actually exists on this build machine.
resolve_dylib() {
    local path="$1"
    [ -f "$path" ] && echo "$path" && return
    local name
    name="$(basename "$path")"
    local d
    for d in \
        /opt/homebrew/opt/*/lib \
        /opt/homebrew/lib \
        /usr/local/opt/*/lib \
        /usr/local/lib; do
        [ -f "$d/$name" ] && echo "$d/$name" && return
    done
}

# bundle_homebrew_dylibs <binary> <frameworks_dir>
#
# BFS walk of all non-system dylib dependencies starting from <binary>.
# Each found dylib is:
#   1. Copied to <frameworks_dir>/
#   2. Its own install name is rewritten to @rpath/<basename>
#   3. All references inside it (and inside <binary>) are fixed to @rpath/<basename>
#
# bash 3.2 compatible (no associative arrays).
bundle_homebrew_dylibs() {
    local binary="$1"
    local frameworks_dir="$2"
    mkdir -p "$frameworks_dir"

    # BFS: files whose transitive deps we still need to walk
    local queue=("$binary")
    local idx=0
    # Track visited basenames to avoid re-processing the same lib
    local visited=()
    local bundle_count=0

    while [ "$idx" -lt "${#queue[@]}" ]; do
        local file="${queue[$idx]}"
        idx=$((idx + 1))
        [ -f "$file" ] || continue

        # otool -L: line 1 is the file itself; subsequent lines are deps
        while IFS= read -r dep; do
            [ -z "$dep" ] && continue
            is_system_dylib "$dep" && continue
            local name
            name="$(basename "$dep")"

            # Skip if already visited
            local seen=0
            local v
            for v in "${visited[@]+"${visited[@]}"}"; do
                [ "$v" = "$name" ] && seen=1 && break
            done
            [ "$seen" = "1" ] && continue
            visited+=("$name")

            local src
            src="$(resolve_dylib "$dep")" || true
            if [ -z "$src" ]; then
                echo -e "   ${YELLOW}⚠${NC} Cannot resolve: $dep"
                continue
            fi

            local dest="$frameworks_dir/$name"
            if [ ! -f "$dest" ]; then
                cp -L "$src" "$dest"
                chmod 755 "$dest"
                # Rewrite the dylib's own install name so it can be found via @rpath
                install_name_tool -id "@rpath/$name" "$dest" 2>/dev/null || true
                bundle_count=$((bundle_count + 1))
                echo -e "   ${GREEN}✓${NC} Frameworks/$name"
                # Walk this dylib's own deps on the next iteration
                queue+=("$dest")
            fi
        done < <(otool -L "$file" 2>/dev/null | awk 'NR>1 {print $1}')
    done

    echo -e "${GREEN}✓${NC} Bundled $bundle_count Homebrew dylibs"

    # Phase 2: rewrite all load-command paths in the binary and every bundled dylib
    # from their absolute Homebrew paths to @rpath/<basename>.
    local all_files=("$binary")
    local nm
    for nm in "${visited[@]+"${visited[@]}"}"; do
        local f="$frameworks_dir/$nm"
        [ -f "$f" ] && all_files+=("$f")
    done

    local fixfile
    for fixfile in "${all_files[@]}"; do
        [ -f "$fixfile" ] || continue
        while IFS= read -r dep; do
            [ -z "$dep" ] && continue
            is_system_dylib "$dep" && continue
            local dep_name
            dep_name="$(basename "$dep")"
            [ "$dep" = "@rpath/$dep_name" ] && continue
            install_name_tool -change "$dep" "@rpath/$dep_name" "$fixfile" 2>/dev/null || true
        done < <(otool -L "$fixfile" 2>/dev/null | awk 'NR>1 {print $1}')
    done
}

# --------------------------------------------------------------------------

create_app_icon() {
    local icon_png="$1"
    local icon_icns="$2"
    local iconset_dir

    if ! command -v sips >/dev/null 2>&1 || ! command -v iconutil >/dev/null 2>&1; then
        return
    fi

    iconset_dir="$(mktemp -d)/AppIcon.iconset"
    mkdir -p "$iconset_dir"
    sips -z 16 16     "$icon_png" --out "$iconset_dir/icon_16x16.png"       >/dev/null 2>&1
    sips -z 32 32     "$icon_png" --out "$iconset_dir/icon_16x16@2x.png"    >/dev/null 2>&1
    sips -z 32 32     "$icon_png" --out "$iconset_dir/icon_32x32.png"       >/dev/null 2>&1
    sips -z 64 64     "$icon_png" --out "$iconset_dir/icon_32x32@2x.png"    >/dev/null 2>&1
    sips -z 128 128   "$icon_png" --out "$iconset_dir/icon_128x128.png"     >/dev/null 2>&1
    sips -z 256 256   "$icon_png" --out "$iconset_dir/icon_128x128@2x.png"  >/dev/null 2>&1
    sips -z 256 256   "$icon_png" --out "$iconset_dir/icon_256x256.png"     >/dev/null 2>&1
    sips -z 512 512   "$icon_png" --out "$iconset_dir/icon_256x256@2x.png"  >/dev/null 2>&1
    sips -z 512 512   "$icon_png" --out "$iconset_dir/icon_512x512.png"     >/dev/null 2>&1
    sips -z 1024 1024 "$icon_png" --out "$iconset_dir/icon_512x512@2x.png"  >/dev/null 2>&1

    iconutil -c icns "$iconset_dir" -o "$icon_icns" >/dev/null 2>&1 || true
    rm -rf "$(dirname "$iconset_dir")"
}

echo -e "${GREEN}🍎 Building USBridgeClient for macOS${NC}"

# 1. Check optional GStreamer (needed only for legacy RTP video mode)

GST_LAUNCH=""
for p in "gst-launch-1.0" "/opt/homebrew/bin/gst-launch-1.0" "/usr/local/bin/gst-launch-1.0"; do
    if command -v "$p" &>/dev/null || [ -x "$p" ]; then
        GST_LAUNCH="$p"
        break
    fi
done

if [ -z "$GST_LAUNCH" ]; then
    :
else
    echo -e "   ${GREEN}✓${NC} gst-launch: $GST_LAUNCH"
fi

# 2. Build binary
rm -f "$REPO_ROOT/cmd/fyne_metadata_init.go"
rm -rf "$REPO_ROOT/cmd/$APP_BUNDLE_NAME"

HOMEBREW_PREFIX="/opt/homebrew"
[ -d "/usr/local/lib" ] && [ ! -d "/opt/homebrew/lib" ] && HOMEBREW_PREFIX="/usr/local"
export CGO_ENABLED=1
export CGO_LDFLAGS="-L${HOMEBREW_PREFIX}/lib ${CGO_LDFLAGS:-}"
export CGO_CPPFLAGS="-I${HOMEBREW_PREFIX}/include ${CGO_CPPFLAGS:-}"
export CGO_CFLAGS="${CGO_CFLAGS:-} -Wno-format-security"
rm -rf "$DIST_DIR"
mkdir -p "$APP_MACOS_DIR" "$APP_RESOURCES_DIR" "$APP_FRAMEWORKS_DIR" "$APP_PLUGINS_DIR"
APP_BINARY_PATH="$APP_MACOS_DIR/$BINARY_NAME"
VERSION=$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0")
go build -ldflags="-s -w -X main.version=$VERSION" -o "$APP_BINARY_PATH" ./cmd

# Tell the dynamic linker to look in Contents/Frameworks/ for bundled dylibs.
# This must come before bundle_homebrew_dylibs rewrites the load commands.
install_name_tool -add_rpath "@executable_path/../Frameworks" "$APP_BINARY_PATH" 2>/dev/null || true

chmod +x "$APP_BINARY_PATH"

# 3. Bundle all Homebrew dylibs the binary depends on (recursive)
echo -e "\n${YELLOW}📚 Bundling Homebrew dylibs → Contents/Frameworks/...${NC}"
bundle_homebrew_dylibs "$APP_BINARY_PATH" "$APP_FRAMEWORKS_DIR"

# 4. Bundle GStreamer plugins (optional — only needed for legacy RTP video mode)
GST_PLUGIN_SRC=""
for d in \
    /opt/homebrew/lib/gstreamer-1.0 \
    /opt/homebrew/opt/gstreamer/lib/gstreamer-1.0 \
    /usr/local/lib/gstreamer-1.0; do
    [ -d "$d" ] && GST_PLUGIN_SRC="$d" && break
done

if [ -n "$GST_PLUGIN_SRC" ]; then
    # GStreamer plugins go directly into Contents/Frameworks/ as flat dylibs.
    # codesign rejects any subdirectory inside Frameworks/ or PlugIns/ that lacks
    # proper bundle structure (Info.plist etc). Flattening avoids that.
    echo -e "\n${YELLOW}🔌 Bundling GStreamer plugins (RTP mode) → Contents/Frameworks/...${NC}"
    GST_PLUGIN_DEST="$APP_FRAMEWORKS_DIR"
    mkdir -p "$GST_PLUGIN_DEST"
    GST_PLUGINS=(
        libgstcoreelements.dylib
        libgstapp.dylib
        libgstrtp.dylib
        libgstrtpmanager.dylib
        libgstudp.dylib
        libgstvideoconvertscale.dylib
        libgstvideofilter.dylib
        libgstplayback.dylib
        libgsttypefindfunctions.dylib
        libgstjpeg.dylib
        libgstjpegformat.dylib
        libgstlibav.dylib
        libgstautodetect.dylib
        libgstaudioconvert.dylib
        libgstaudioresample.dylib
        libgstosxaudio.dylib
        libgstapplemedia.dylib
        libgstvideoparsers.dylib
        libgstvideoparsersbad.dylib
    )
    GST_PLUGIN_COUNT=0
    for plugin in "${GST_PLUGINS[@]}"; do
        if [ -f "$GST_PLUGIN_SRC/$plugin" ]; then
            cp -L "$GST_PLUGIN_SRC/$plugin" "$GST_PLUGIN_DEST/"
            chmod 755 "$GST_PLUGIN_DEST/$plugin"
            install_name_tool -id "@rpath/$plugin" "$GST_PLUGIN_DEST/$plugin" 2>/dev/null || true
            # Fix all Homebrew references inside the plugin
            while IFS= read -r dep; do
                [ -z "$dep" ] && continue
                is_system_dylib "$dep" && continue
                dep_name="$(basename "$dep")"
                install_name_tool -change "$dep" "@rpath/$dep_name" "$GST_PLUGIN_DEST/$plugin" 2>/dev/null || true
            done < <(otool -L "$GST_PLUGIN_SRC/$plugin" 2>/dev/null | awk 'NR>1 {print $1}')
            echo -e "   ${GREEN}✓${NC} $plugin"
            GST_PLUGIN_COUNT=$((GST_PLUGIN_COUNT + 1))
        fi
    done
    echo -e "${GREEN}✓${NC} GStreamer plugins: $GST_PLUGIN_COUNT bundled (flat in Contents/Frameworks/)"
    echo "   (set GST_PLUGIN_PATH=\$BUNDLE/Contents/Frameworks to use them)"
fi

# 5. Bundle QEMU (qemu-nbd, qemu-img — for VMDK/QCOW2/VDI image support)
echo -e "\n${YELLOW}📀 Bundling QEMU (qemu-nbd, qemu-img)...${NC}"
QEMU_COPIED=0
for _qtool in qemu-nbd qemu-img; do
    _src=""
    for _d in /opt/homebrew/bin /usr/local/bin; do
        [ -f "$_d/$_qtool" ] && _src="$_d/$_qtool" && break
    done
    if [ -n "$_src" ]; then
        _dest="$APP_MACOS_DIR/$_qtool"
        cp -L "$_src" "$_dest"
        chmod 755 "$_dest"
        # Allow it to find dylibs in the shared Frameworks/ dir
        install_name_tool -add_rpath "@executable_path/../Frameworks" "$_dest" 2>/dev/null || true
        # Bundle any new dylib deps it introduces (gnutls, libssh, zstd…) alongside
        # the ones already copied for the main binary — bundle_homebrew_dylibs skips
        # libs that are already present in Frameworks/.
        bundle_homebrew_dylibs "$_dest" "$APP_FRAMEWORKS_DIR"
        QEMU_COPIED=$((QEMU_COPIED + 1))
        echo -e "   ${GREEN}✓${NC} MacOS/$_qtool"
    else
        :
    fi
done

# 5b. Bundle FFmpeg (used by h264_decoder.go for legacy RTP H.264 decoding)
# findFFmpeg() checks the executable's own directory first, so placing ffmpeg in
# Contents/MacOS/ means it is found without Homebrew or PATH manipulation.
echo -e "\n${YELLOW}🎬 Bundling FFmpeg...${NC}"
_ff_src=""
for _d in /opt/homebrew/bin /usr/local/bin; do
    [ -f "$_d/ffmpeg" ] && _ff_src="$_d/ffmpeg" && break
done
if [ -n "$_ff_src" ]; then
    _ff_dest="$APP_MACOS_DIR/ffmpeg"
    cp -L "$_ff_src" "$_ff_dest"
    chmod 755 "$_ff_dest"
    install_name_tool -add_rpath "@executable_path/../Frameworks" "$_ff_dest" 2>/dev/null || true
    bundle_homebrew_dylibs "$_ff_dest" "$APP_FRAMEWORKS_DIR"
    echo -e "${GREEN}✓${NC} MacOS/ffmpeg"
else
    :
fi

# 5c. Bundle Tailscale (Go binary — statically linked, no dylib deps)
echo -e "\n${YELLOW}🔗 Bundling Tailscale...${NC}"
_ts_src=""
for _d in /opt/homebrew/bin /usr/local/bin; do
    [ -f "$_d/tailscale" ] && _ts_src="$_d/tailscale" && break
done
if [ -n "$_ts_src" ]; then
    cp -L "$_ts_src" "$APP_MACOS_DIR/tailscale"
    chmod 755 "$APP_MACOS_DIR/tailscale"
    echo -e "${GREEN}✓${NC} MacOS/tailscale"
else
    :
fi

# 6. Info.plist (written after bundling so icons/plist don't interfere with lib walk)
cat > "$APP_CONTENTS_DIR/Info.plist" << 'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDevelopmentRegion</key>
    <string>en</string>
    <key>CFBundleDisplayName</key>
    <string>USBridgeClient</string>
    <key>CFBundleExecutable</key>
    <string>USBridgeClient</string>
    <key>CFBundleIdentifier</key>
    <string>io.usbridge.client</string>
    <key>CFBundleInfoDictionaryVersion</key>
    <string>6.0</string>
    <key>CFBundleName</key>
    <string>USBridgeClient</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>$VERSION</string>
    <key>CFBundleVersion</key>
    <string>$VERSION</string>
    <key>LSMinimumSystemVersion</key>
    <string>10.15</string>
    <key>NSCameraUsageDescription</key>
    <string>USBridgeClient uses the camera to scan QR codes for device connection.</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

if [ -f "$REPO_ROOT/Icon.png" ]; then
    create_app_icon "$REPO_ROOT/Icon.png" "$APP_RESOURCES_DIR/AppIcon.icns"
    if [ -f "$APP_RESOURCES_DIR/AppIcon.icns" ]; then
        /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" "$APP_CONTENTS_DIR/Info.plist" >/dev/null 2>&1 || true
    fi
fi

# 6. Sign — must run AFTER all binaries/dylibs are bundled.
# Signing order: inner dylibs → MacOS/ executables → outer bundle with --deep.
# --options runtime is required for notarization (Hardened Runtime).
CODESIGN_IDENTITY="${USBRIDGE_CODESIGN_IDENTITY:-}"
if [ -z "$CODESIGN_IDENTITY" ] && command -v security >/dev/null 2>&1; then
    CODESIGN_IDENTITY=$(security find-identity -v -p codesigning 2>/dev/null \
        | grep "Developer ID Application" | head -1 \
        | awk '{print $2}')
fi

ENTITLEMENTS="$SCRIPTS_DIR/entitlements-macos.plist"

# codesign can transiently fail with "Operation not permitted" right after a file
# is rewritten by install_name_tool — macOS's Gatekeeper/AMFI validation daemon
# (syspolicyd) occasionally hasn't caught up with the just-modified file yet.
# This is not a real permissions problem: retrying immediately succeeds. Retry
# a few times with a short backoff instead of aborting the whole build.
codesign_retry() {
    local attempt=1
    local max_attempts=5
    until codesign "$@"; do
        if [ "$attempt" -ge "$max_attempts" ]; then
            echo -e "${RED}✗${NC} codesign failed after $max_attempts attempts: $*" >&2
            return 1
        fi
        echo -e "${YELLOW}⚠${NC}  codesign transient failure (attempt $attempt/$max_attempts), retrying..." >&2
        sleep 1
        attempt=$((attempt + 1))
    done
}

if [ -n "$CODESIGN_IDENTITY" ] && command -v codesign >/dev/null 2>&1; then
    echo -e "${YELLOW}Signing with identity: $CODESIGN_IDENTITY${NC}"
    # Sign each dylib individually (install_name_tool invalidated original signatures).
    # Dylibs don't take entitlements — only --options runtime + --timestamp for notarization.
    # Errors are NOT suppressed: a silent ad-hoc fallback causes Apple notarization rejection.
    while IFS= read -r dylib; do
        codesign_retry --force --sign "$CODESIGN_IDENTITY" \
            --options runtime --timestamp "$dylib"
    done < <(find "$APP_FRAMEWORKS_DIR" -name "*.dylib" -type f 2>/dev/null)
    # Sign standalone executables in MacOS/ (qemu-nbd, qemu-img, tailscale, ffmpeg)
    while IFS= read -r exe; do
        codesign_retry --force --sign "$CODESIGN_IDENTITY" \
            --options runtime --timestamp --entitlements "$ENTITLEMENTS" "$exe"
    done < <(find "$APP_MACOS_DIR" -type f -perm +111 ! -name "$BINARY_NAME" 2>/dev/null)
    # Sign the main binary explicitly before sealing the bundle
    codesign_retry --force --sign "$CODESIGN_IDENTITY" \
        --options runtime --timestamp --entitlements "$ENTITLEMENTS" \
        "$APP_MACOS_DIR/$BINARY_NAME"
    # Sign the outer bundle WITHOUT --deep: all inner components are already signed above.
    # --deep recurses into plain directories (e.g. gstreamer-1.0/) and fails treating them
    # as bundles; signing everything explicitly first avoids that error.
    codesign_retry --force --sign "$CODESIGN_IDENTITY" \
        --options runtime --timestamp --entitlements "$ENTITLEMENTS" \
        "$DIST_DIR/$APP_BUNDLE_NAME"
    # Verify every dylib got a real Developer ID (not ad-hoc) — catch silent failures early.
    _adhoc_count=0
    while IFS= read -r dylib; do
        if codesign -dv "$dylib" 2>&1 | grep -q "Signature=adhoc\|flags=0x2(adhoc)"; then
            echo -e "  ${RED}✗${NC} ad-hoc (signing failed): ${dylib##*/}"
            _adhoc_count=$((_adhoc_count + 1))
        fi
    done < <(find "$APP_FRAMEWORKS_DIR" -name "*.dylib" -type f 2>/dev/null)
    if [ "$_adhoc_count" -gt 0 ]; then
        echo -e "${RED}❌ $_adhoc_count dylib(s) did not get Developer ID — aborting (Apple will reject)${NC}"
        exit 1
    fi
else
    # Go linker embeds an ad-hoc signature; replace it so macOS doesn't reject as "damaged"
    echo -e "${YELLOW}No Developer ID found — signing ad-hoc${NC}"
    find "$APP_FRAMEWORKS_DIR" -name "*.dylib" -type f 2>/dev/null | while IFS= read -r dylib; do
        codesign --force --sign - "$dylib" 2>/dev/null || true
    done
    find "$APP_MACOS_DIR" -type f -perm +111 | while IFS= read -r exe; do
        codesign --force --sign - "$exe" 2>/dev/null || true
    done
    codesign --force --sign - "$DIST_DIR/$APP_BUNDLE_NAME"
fi
touch "$DIST_DIR/$APP_BUNDLE_NAME"

echo -e "${GREEN}   ✅ App bundle: $DIST_DIR/$APP_BUNDLE_NAME${NC}"

# 7. Dist extras
[ -f config.yaml ] && cp config.yaml "$DIST_DIR/"

cat > "$DIST_DIR/README.txt" << 'README'
USBridgeClient for macOS
=========================

Run:
  Open USBridgeClient.app
  (All Homebrew dependencies are bundled — Homebrew not required on the target machine.)

Bundle layout:
  Contents/MacOS/USBridgeClient    — main binary
  Contents/MacOS/ffmpeg            — FFmpeg (H.264 RTP legacy decode)
  Contents/MacOS/qemu-nbd          — QEMU NBD (VMDK/QCOW2/VDI image support)
  Contents/MacOS/qemu-img          — QEMU image tool
  Contents/MacOS/tailscale         — Tailscale CLI
  Contents/Frameworks/             — bundled dylibs (opus, openssl, glib, gstreamer, ffmpeg libs, gnutls…) + GStreamer plugins

Video modes:
  Moonlight streaming — VideoToolbox (GPU hardware decode) + CoreAudio audio.
    No external dependencies required.

  Legacy RTP mode — requires GStreamer plugins.
    If GStreamer was installed at build time its plugins are bundled above.
    To activate: export GST_PLUGIN_PATH="$BUNDLE/Contents/Frameworks"
    Or install GStreamer via Homebrew: brew install gstreamer gst-plugins-base gst-plugins-good gst-plugins-bad

Requirements:
  - macOS 10.15+

Configuration:
  config.yaml next to the .app, or ~/.config/usbridge-client/

Application log:
  ~/Library/Logs/USBridgeClient/app.log
README

ARCHIVE="$REPO_ROOT/dist/USBridgeClient-macOS-arm64-$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0").dmg"
rm -f "$ARCHIVE"
hdiutil create -volname "USBridgeClient" -srcfolder "$DIST_DIR" -ov -format UDZO "$ARCHIVE"

# ── Notarization (optional) ───────────────────────────────────────────────────
# Requires credentials stored in Keychain once via:
#   xcrun notarytool store-credentials "usbridge-notarytool" \
#       --apple-id "..." --team-id "..." --password "xxxx-xxxx-xxxx-xxxx"
# Skip with: USBRIDGE_SKIP_NOTARIZE=1
NOTARIZE_PROFILE="${USBRIDGE_NOTARIZE_PROFILE:-usbridge-notarytool}"
if [[ "${USBRIDGE_SKIP_NOTARIZE:-0}" != "1" ]] && command -v xcrun >/dev/null 2>&1; then
    if xcrun notarytool history --keychain-profile "$NOTARIZE_PROFILE" >/dev/null 2>&1; then
        echo -e "${YELLOW}Notarizing (this takes 1-3 minutes)...${NC}"
        _notarize_out=$(xcrun notarytool submit "$ARCHIVE" \
            --keychain-profile "$NOTARIZE_PROFILE" \
            --wait 2>&1)
        echo "$_notarize_out"
        if echo "$_notarize_out" | grep -q "status: Invalid\|status: Rejected"; then
            _sub_id=$(echo "$_notarize_out" | grep "^ *id:" | head -1 | awk '{print $2}')
            echo -e "${RED}❌ Notarization rejected by Apple${NC}"
            if [ -n "$_sub_id" ]; then
                echo "   Fetching rejection log..."
                xcrun notarytool log "$_sub_id" --keychain-profile "$NOTARIZE_PROFILE" 2>/dev/null \
                    | python3 -c "
import json,sys
d=json.load(sys.stdin)
for i in d.get('issues',[]):
    print(f\"  [{i['severity']}] {i['path'].split('/')[-1]}: {i['message']}\")
" 2>/dev/null || true
            fi
            exit 1
        fi
        echo -e "${YELLOW}Stapling notarization ticket...${NC}"
        # Staple the .app itself, not just the .dmg — if the ticket only
        # lives on the disk image, an app dragged out of it to /Applications
        # has no local ticket (Gatekeeper falls back to an online check).
        xcrun stapler staple "$DIST_DIR/$APP_BUNDLE_NAME"
        rm -f "$ARCHIVE"
        hdiutil create -volname "USBridgeClient" -srcfolder "$DIST_DIR" -ov -format UDZO "$ARCHIVE"
        echo -e "${GREEN}✓${NC} Notarized & stapled: $ARCHIVE"
    else
        echo -e "${YELLOW}Keychain profile '$NOTARIZE_PROFILE' not found — skipping notarization${NC}"
        echo "  Run once to enable: xcrun notarytool store-credentials \"$NOTARIZE_PROFILE\" --apple-id ... --team-id ... --password ..."
    fi
fi

# ── Signature validation ─────────────────────────────────────────────────────
echo -e "${YELLOW}Validating code signatures...${NC}"
sig_ok=true

if codesign --verify --deep --strict "$DIST_DIR/$APP_BUNDLE_NAME" 2>&1; then
    echo -e "  ${GREEN}✓${NC} codesign --verify --deep --strict: OK"
else
    echo -e "  ${RED}✗${NC} codesign deep verify FAILED"
    sig_ok=false
fi

while IFS= read -r -d '' bin; do
    if codesign --verify --strict "$bin" 2>/dev/null; then
        echo -e "  ${GREEN}✓${NC} signed: ${bin#$DIST_DIR/$APP_BUNDLE_NAME/}"
    else
        echo -e "  ${RED}✗${NC} unsigned: ${bin#$DIST_DIR/$APP_BUNDLE_NAME/}"
        sig_ok=false
    fi
done < <(find "$DIST_DIR/$APP_BUNDLE_NAME/Contents/MacOS" -type f -perm +111 -print0 2>/dev/null)

gk_result=$(spctl --assess -v "$DIST_DIR/$APP_BUNDLE_NAME" 2>&1 || true)
if echo "$gk_result" | grep -q "accepted"; then
    echo -e "  ${GREEN}✓${NC} Gatekeeper: accepted (notarized)"
elif echo "$gk_result" | grep -q "CSSMERR_TP_NOT_TRUSTED\|rejected"; then
    echo -e "  ${YELLOW}⚠${NC}  Gatekeeper: not accepted (ad-hoc signed / not notarized)"
else
    echo -e "  ${YELLOW}⚠${NC}  Gatekeeper: $gk_result"
fi

echo ""
